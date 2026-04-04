package workflow

import (
	"fmt"
	"time"

	"encore.app/bills/activities"
	"encore.app/bills/model"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

var (
	AddLineItemSignal   = "AddLineItem"
	maxSignalsBeforeCAN = 1000
)

func StartBillWorkflow(ctx workflow.Context, input model.StartBillWorkflowInput) error {
	billID := input.BillID
	currency := input.CurrencyCode
	closeTimeout := input.CloseTimeout

	// Restore state from continue-as-new or initialize fresh
	var state model.BillState
	if input.CarryOverState != nil {
		state = *input.CarryOverState
	} else {
		state = model.BillState{
			Status:       "OPEN",
			RunningTotal: 0,
			SeenItems:    map[string]bool{},
		}
	}

	signalCount := 0

	logger := workflow.GetLogger(ctx)
	logger.Info(
		"Starting workflow",
		"name", "Start Bill Workflow",
		"billID", billID,
		"currency", currency,
	)

	retryPolicy := &temporal.RetryPolicy{
		InitialInterval:    time.Second,
		BackoffCoefficient: 2.0,
		MaximumInterval:    30 * time.Second,
		MaximumAttempts:    5,
	}

	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout:    30 * time.Second,
		ScheduleToCloseTimeout: 5 * time.Minute,
		RetryPolicy:            retryPolicy,
	}

	// Signal channel for line items
	lineItemCh := workflow.GetSignalChannel(ctx, AddLineItemSignal)

	// Internal channel so update handler can wake the main loop
	updateChan := workflow.NewChannel(ctx)

	// Shared finalize logic used by both manual close and auto-close
	finalizeBill := func(wCtx workflow.Context) (model.FinalInvoice, error) {
		state.FinalInvoice.BillID = billID
		state.FinalInvoice.CurrencyCode = currency
		state.FinalInvoice.TotalMinor = state.RunningTotal
		state.FinalInvoice.ClosedAt = workflow.Now(wCtx)

		var inv model.FinalInvoice
		activityCtx := workflow.WithActivityOptions(wCtx, activityOpts)
		err := workflow.ExecuteActivity(
			activityCtx,
			activities.Ref.FinalizeBillActivity,
			model.FinalizeBillActivityInput{
				BillID:       billID,
				CurrencyCode: currency,
				LineItems:    state.FinalInvoice.LineItems,
			},
		).Get(wCtx, &inv)
		if err != nil {
			return model.FinalInvoice{}, err
		}
		state.FinalInvoice = inv
		return inv, nil
	}

	workflow.SetUpdateHandlerWithOptions(
		ctx,
		"CloseBill",
		func(uctx workflow.Context) (model.FinalInvoice, error) {
			state.Status = "CLOSED"
			updateChan.Send(uctx, true)

			return finalizeBill(uctx)
		},
		workflow.UpdateHandlerOptions{
			Validator: func(ctx workflow.Context) error {
				if state.Status == "CLOSED" {
					return fmt.Errorf("bill is already closed")
				}
				return nil
			},
		},
	)

	// Auto-close after configured timeout
	closeTimer := workflow.NewTimer(ctx, closeTimeout)

	var autoCloseErr error

	for state.Status == "OPEN" {
		selector := workflow.NewSelector(ctx)

		// Handle AddLineItem signal
		selector.AddReceive(lineItemCh, func(c workflow.ReceiveChannel, more bool) {
			var req model.AddItemRequest
			c.Receive(ctx, &req)

			if state.Status != "OPEN" {
				logger.Info(
					"Ignoring line item because bill is closed",
					"billID", billID,
				)
				return
			}

			dedupeKey := req.IdempotencyKey

			if state.SeenItems[dedupeKey] {
				logger.Info(
					"Ignoring duplicate line item",
					"billID", billID,
					"idempotencyKey", req.IdempotencyKey,
				)
				return
			}

			var itemID string
			activityCtx := workflow.WithActivityOptions(ctx, activityOpts)
			err := workflow.ExecuteActivity(
				activityCtx,
				activities.Ref.AddItemLineActivity,
				model.AddItemLineActivityInput{
					BillID:         billID,
					AmountMinor:    req.AmountMinor,
					CurrencyCode:   currency,
					Description:    req.Description,
					IdempotencyKey: req.IdempotencyKey,
				},
			).Get(ctx, &itemID)

			if err != nil {
				logger.Error("Failed to persist line item", "billID", billID, "error", err)
				return
			}

			state.SeenItems[dedupeKey] = true
			state.RunningTotal += req.AmountMinor
			signalCount++

			state.FinalInvoice.LineItems = append(state.FinalInvoice.LineItems, model.FinalLineItem{
				ID:          itemID,
				AmountMinor: req.AmountMinor,
				Description: req.Description,
				CreatedAt:   workflow.Now(ctx),
			})

			logger.Info(
				"Line item received",
				"billID", billID,
				"itemID", itemID,
				"amountMinor", req.AmountMinor,
				"idempotencyKey", req.IdempotencyKey,
			)
		})

		// Handle timer-based auto close
		selector.AddFuture(closeTimer, func(f workflow.Future) {
			if state.Status != "OPEN" {
				return
			}

			logger.Info("Close time reached, auto-closing bill", "billID", billID)
			state.Status = "CLOSED"

			_, err := finalizeBill(ctx)
			if err != nil {
				logger.Error("Failed to finalize bill on auto-close", "billID", billID, "error", err)
				autoCloseErr = err
			}
		})

		// Handle explicit close via update
		selector.AddReceive(updateChan, func(c workflow.ReceiveChannel, more bool) {
			var ignored bool
			c.Receive(ctx, &ignored)
			logger.Info("Bill closed via update", "billID", billID)
		})

		// Wait for one event: signal, timer, or update
		selector.Select(ctx)

		// Continue-as-new to prevent unbounded history growth
		if signalCount >= maxSignalsBeforeCAN && state.Status == "OPEN" {
			logger.Info("Continuing as new to compact history",
				"billID", billID,
				"signalCount", signalCount,
			)
			return workflow.NewContinueAsNewError(ctx, StartBillWorkflow, model.StartBillWorkflowInput{
				BillID:         billID,
				CurrencyCode:   currency,
				CloseTimeout:   closeTimeout,
				CarryOverState: &state,
			})
		}
	}

	// Drain any buffered signals that arrived before the workflow closed
	for {
		var req model.AddItemRequest
		ok := lineItemCh.ReceiveAsync(&req)
		if !ok {
			break
		}
		logger.Warn("Discarding line item received after bill closed",
			"billID", billID,
			"idempotencyKey", req.IdempotencyKey,
			"amountMinor", req.AmountMinor,
		)
	}

	workflow.Await(ctx, func() bool { return workflow.AllHandlersFinished(ctx) })
	return autoCloseErr
}
