package bills

import (
	"context"
	"errors"
	"time"

	"encore.app/bills/model"
	"encore.app/bills/workflow"
	"encore.dev/beta/errs"
	"encore.dev/storage/sqldb"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

// CreateBillRequest represents the request to create a new bill
type CreateBillRequest struct {
	CurrencyCode string `json:"currencyCode"`
}

func (r *CreateBillRequest) Validate() error {
	switch r.CurrencyCode {
	case "GEL", "USD":
		return nil
	default:
		return &errs.Error{Code: errs.InvalidArgument, Message: "currency must be GEL or USD"}
	}
}

// CreateBillResponse represents the response when creating a bill
type CreateBillResponse struct {
	BillID string `json:"billId"`
	Status int    `encore:"httpstatus"`
}

// CloseBillResponse represents the response when closing a bill
type CloseBillResponse struct {
	BillID           string              `json:"billId"`
	CurrencyCode     string              `json:"currencyCode"`
	TotalAmountMinor int64               `json:"totalAmountMinor"`
	LineItems        []CloseBillLineItem `json:"lineItems"`
	ClosedAt         time.Time           `json:"closedAt"`
}

// CloseBillLineItem represents a line item in a closed bill
type CloseBillLineItem struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	AmountMinor int64     `json:"amountMinor"`
	CreatedAt   time.Time `json:"createdAt"`
}

// CreateBill creates a new bill and starts a Temporal workflow
//
// encore:api auth method=POST path=/v1/bills
func (s *Service) CreateBill(ctx context.Context, req *CreateBillRequest) (*CreateBillResponse, error) {
	billID, err := s.repository.StoreBill(ctx, req.CurrencyCode)
	if err != nil {
		return nil, err
	}

	closeTimeout := time.Duration(cfg.BillCloseTimeout()) * time.Minute

	opts := client.StartWorkflowOptions{
		ID:        billID,
		TaskQueue: startBillTaskQueue,
	}
	_, err = s.client.ExecuteWorkflow(ctx, opts, workflow.StartBillWorkflow, model.StartBillWorkflowInput{
		BillID:       billID,
		CurrencyCode: req.CurrencyCode,
		CloseTimeout: closeTimeout,
	})
	if err != nil {
		return nil, err
	}

	return &CreateBillResponse{
		BillID: billID,
		Status: 201,
	}, nil
}

// CloseBill closes an open bill by sending an update to the Temporal workflow
//
// encore:api auth method=POST path=/v1/bills/:id/close
func (s *Service) CloseBill(ctx context.Context, id string) (*CloseBillResponse, error) {
	updateHandler, err := s.client.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   id,
		UpdateName:   "CloseBill",
		WaitForStage: client.WorkflowUpdateStageAccepted,
	})
	if err != nil {
		var notFound *serviceerror.NotFound
		if errors.As(err, &notFound) {
			return nil, &errs.Error{
				Code:    errs.FailedPrecondition,
				Message: "Bill is already closed",
			}
		}
		return nil, err
	}

	var finalInvoice model.FinalInvoice
	if err := updateHandler.Get(ctx, &finalInvoice); err != nil {
		return nil, err
	}

	lineItems := make([]CloseBillLineItem, len(finalInvoice.LineItems))
	for i, item := range finalInvoice.LineItems {
		lineItems[i] = CloseBillLineItem{
			ID:          item.ID,
			Description: item.Description,
			AmountMinor: item.AmountMinor,
			CreatedAt:   item.CreatedAt,
		}
	}

	return &CloseBillResponse{
		BillID:           finalInvoice.BillID,
		CurrencyCode:     finalInvoice.CurrencyCode,
		TotalAmountMinor: finalInvoice.TotalMinor,
		LineItems:        lineItems,
		ClosedAt:         finalInvoice.ClosedAt,
	}, nil
}

type AddLineItemRequest struct {
	Description    string `json:"description"`
	AmountMinor    int64  `json:"amountMinor"`
	IdempotencyKey string `json:"idempotencyKey"`
}

func (r *AddLineItemRequest) Validate() error {
	if r.AmountMinor <= 0 {
		return &errs.Error{Code: errs.InvalidArgument, Message: "amountMinor must be positive"}
	}
	if r.Description == "" {
		return &errs.Error{Code: errs.InvalidArgument, Message: "description is required"}
	}
	if r.IdempotencyKey == "" {
		return &errs.Error{Code: errs.InvalidArgument, Message: "idempotencyKey is required"}
	}
	return nil
}

// AddItemIntoBill sends a signal to add an item to an open bill
//
// encore:api auth method=POST path=/v1/bills/:billID/line-items
func (s *Service) AddItemIntoBill(ctx context.Context, billID string, req *AddLineItemRequest) error {
	err := s.client.SignalWorkflow(ctx, billID, "", workflow.AddLineItemSignal, model.AddItemRequest{
		Description:    req.Description,
		AmountMinor:    req.AmountMinor,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		var notFound *serviceerror.NotFound
		if errors.As(err, &notFound) {
			return &errs.Error{
				Code:    errs.FailedPrecondition,
				Message: "Bill is closed, cannot add more items",
			}
		}
		return err
	}
	return nil
}

type GetBillResponse struct {
	BillID       string            `json:"billId"`
	Status       string            `json:"status"`
	CurrencyCode string            `json:"currencyCode"`
	TotalMinor   int64             `json:"totalMinor"`
	LineItems    []GetBillLineItem `json:"lineItems"`
	ClosedAt     *time.Time        `json:"closedAt,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}

type GetBillLineItem struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	AmountMinor int64     `json:"amountMinor"`
	CreatedAt   time.Time `json:"createdAt"`
}

// GetBill retrieves an open or closed bill
//
// encore:api auth method=GET path=/v1/bills/:billID
func (s *Service) GetBill(ctx context.Context, billID string) (*GetBillResponse, error) {
	bill, err := s.repository.GetBill(ctx, billID)
	if err != nil {
		if errors.Is(err, sqldb.ErrNoRows) {
			return nil, &errs.Error{
				Code:    errs.NotFound,
				Message: "Bill not found",
			}
		}
		return nil, err
	}

	lineItems := make([]GetBillLineItem, len(bill.LineItems))
	for i, item := range bill.LineItems {
		lineItems[i] = GetBillLineItem{
			ID:          item.ID,
			Description: item.Description,
			AmountMinor: item.AmountMinor,
			CreatedAt:   item.CreatedAt,
		}
	}

	return &GetBillResponse{
		BillID:       bill.BillID,
		Status:       bill.Status,
		CurrencyCode: bill.CurrencyCode,
		TotalMinor:   bill.TotalMinor,
		LineItems:    lineItems,
		ClosedAt:     bill.ClosedAt,
		CreatedAt:    bill.CreatedAt,
		UpdatedAt:    bill.UpdatedAt,
	}, nil
}

// ListBillsRequest represents the query parameters for listing bills
type ListBillsRequest struct {
	Status string `query:"status"` // Optional: "OPEN" or "CLOSED"
}

// ListBillsResponse represents the response when listing bills
type ListBillsResponse struct {
	Bills []ListBillsItem `json:"bills"`
}

// ListBillsItem represents a bill summary in a list
type ListBillsItem struct {
	BillID       string     `json:"billId"`
	Status       string     `json:"status"`
	CurrencyCode string     `json:"currencyCode"`
	TotalMinor   int64      `json:"totalMinor"`
	ClosedAt     *time.Time `json:"closedAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

func (r *ListBillsRequest) Validate() error {
	if r.Status != "" && r.Status != "OPEN" && r.Status != "CLOSED" {
		return &errs.Error{Code: errs.InvalidArgument, Message: "status must be OPEN or CLOSED"}
	}
	return nil
}

// ListBills retrieves bills, optionally filtered by status
//
// encore:api auth method=GET path=/v1/bills
func (s *Service) ListBills(ctx context.Context, req *ListBillsRequest) (*ListBillsResponse, error) {
	bills, err := s.repository.ListBills(ctx, req.Status)
	if err != nil {
		return nil, err
	}

	items := make([]ListBillsItem, len(bills))
	for i, b := range bills {
		items[i] = ListBillsItem{
			BillID:       b.BillID,
			Status:       b.Status,
			CurrencyCode: b.CurrencyCode,
			TotalMinor:   b.TotalMinor,
			ClosedAt:     b.ClosedAt,
			CreatedAt:    b.CreatedAt,
			UpdatedAt:    b.UpdatedAt,
		}
	}

	return &ListBillsResponse{Bills: items}, nil
}
