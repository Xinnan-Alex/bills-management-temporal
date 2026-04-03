package workflow

import (
	"time"

	"encore.app/bills/activities"
	"encore.app/bills/model"
	"go.temporal.io/sdk/workflow"
)

var (
	AddLineItemSignal = "AddLineItem"
)

func StartBillWorkflow(ctx workflow.Context, billID string, currency string, closeTimeout time.Duration) error {
	state := model.BillState{
		Status:       "OPEN",
		RunningTotal: 0,
		SeenItems:    map[string]bool{},
	}

	logger := workflow.GetLogger(ctx)
	logger.Info(
		"Starting workflow",
		"name", "Start Bill Workflow",
		"billID", billID,
		"currency", currency,
	)

	// Signal channel for line items
	lineItemCh := workflow.GetSignalChannel(ctx, AddLineItemSignal)

	// Internal channel so update handler can wake the main loop
	updateChan := workflow.NewChannel(ctx)

	workflow.SetUpdateHandler(
		ctx,
		"CloseBill",
		func(uctx workflow.Context) (model.FinalInvoice, error) {
			if state.Status == "CLOSED" {
				return state.FinalInvoice, nil
			}

			state.Status = "CLOSED"
			updateChan.Send(uctx, true)

			// Build final invoice from current in-memory state
			state.FinalInvoice.BillID = billID
			state.FinalInvoice.CurrencyCode = currency
			state.FinalInvoice.TotalMinor = state.RunningTotal
			state.FinalInvoice.ClosedAt = workflow.Now(uctx)

			var inv model.FinalInvoice
			activityCtx := workflow.WithActivityOptions(uctx, workflow.ActivityOptions{
				StartToCloseTimeout: time.Second * 30,
			})
			err := workflow.ExecuteActivity(
				activityCtx,
				activities.Ref.FinalizeBillActivity,
				billID,
				currency,
				state.FinalInvoice.LineItems,
			).Get(uctx, &inv)

			if err != nil {
				return model.FinalInvoice{}, err
			}

			state.FinalInvoice = inv
			return inv, nil
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
			activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
				StartToCloseTimeout: time.Second * 30,
			})
			err := workflow.ExecuteActivity(
				activityCtx,
				activities.Ref.AddItemLineActivity,
				billID,
				req.AmountMinor,
				currency,
				req.Description,
				req.IdempotencyKey,
			).Get(ctx, &itemID)

			if err != nil {
				logger.Error("Failed to persist line item", "billID", billID, "error", err)
				return
			}

			state.SeenItems[dedupeKey] = true
			state.RunningTotal += req.AmountMinor

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

			// Build final invoice from current state
			state.FinalInvoice.BillID = billID
			state.FinalInvoice.CurrencyCode = currency
			state.FinalInvoice.TotalMinor = state.RunningTotal
			state.FinalInvoice.ClosedAt = workflow.Now(ctx)

			// Finalize to database (same as manual close)
			activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
				StartToCloseTimeout: time.Second * 30,
			})
			err := workflow.ExecuteActivity(
				activityCtx,
				activities.Ref.FinalizeBillActivity,
				billID,
				currency,
				state.FinalInvoice.LineItems,
			).Get(ctx, &state.FinalInvoice)

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
