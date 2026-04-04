package bills

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"encore.app/bills/model"
	"encore.app/bills/repository"
	"encore.dev/beta/errs"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/mocks"
)

func TestCreateBillRequest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		currency string
		wantErr  bool
	}{
		{"valid USD", "USD", false},
		{"valid GEL", "GEL", false},
		{"invalid EUR", "EUR", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &CreateBillRequest{CurrencyCode: tt.currency}
			err := req.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func TestAddLineItemRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     AddLineItemRequest
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid",
			req:     AddLineItemRequest{Description: "Fee", AmountMinor: 100, IdempotencyKey: "k1"},
			wantErr: false,
		},
		{
			name:    "zero amount",
			req:     AddLineItemRequest{Description: "Fee", AmountMinor: 0, IdempotencyKey: "k1"},
			wantErr: true,
			errMsg:  "amountMinor must be positive",
		},
		{
			name:    "negative amount",
			req:     AddLineItemRequest{Description: "Fee", AmountMinor: -100, IdempotencyKey: "k1"},
			wantErr: true,
			errMsg:  "amountMinor must be positive",
		},
		{
			name:    "empty description",
			req:     AddLineItemRequest{Description: "", AmountMinor: 100, IdempotencyKey: "k1"},
			wantErr: true,
			errMsg:  "description is required",
		},
		{
			name:    "empty idempotency key",
			req:     AddLineItemRequest{Description: "Fee", AmountMinor: 100, IdempotencyKey: ""},
			wantErr: true,
			errMsg:  "idempotencyKey is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}
				e, ok := err.(*errs.Error)
				if !ok {
					t.Errorf("expected *errs.Error, got %T", err)
					return
				}
				if e.Code != errs.InvalidArgument {
					t.Errorf("expected InvalidArgument, got %v", e.Code)
				}
				if e.Message != tt.errMsg {
					t.Errorf("expected message %q, got %q", tt.errMsg, e.Message)
				}
			} else if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func TestListBillsRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		wantErr bool
	}{
		{"empty (all)", "", false},
		{"OPEN", "OPEN", false},
		{"CLOSED", "CLOSED", false},
		{"invalid", "PENDING", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ListBillsRequest{Status: tt.status}
			err := req.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func newMockService(tc *mocks.Client) *Service {
	return &Service{
		client:     tc,
		repository: repository.NewRepository(),
	}
}

// --- CreateBill tests ---

func TestCreateBill(t *testing.T) {
	ctx := context.Background()
	testCases := &mocks.Client{}
	run := &mocks.WorkflowRun{}
	testCases.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(run, nil)
	mockService := newMockService(testCases)

	resp, err := mockService.CreateBill(ctx, &CreateBillRequest{CurrencyCode: "USD"})
	if err != nil {
		t.Fatalf("CreateBill failed: %v", err)
	}
	if resp.BillID == "" {
		t.Error("expected non-empty bill ID")
	}
	if resp.Status != 201 {
		t.Errorf("expected status 201, got %d", resp.Status)
	}
	testCases.AssertExpectations(t)
}

func TestCreateBill_WorkflowStartFails(t *testing.T) {
	ctx := context.Background()
	testCases := &mocks.Client{}
	testCases.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, fmt.Errorf("temporal unavailable"))
	mockService := newMockService(testCases)

	// Count bills before the test
	billsBefore, err := mockService.repository.ListBills(ctx, "")
	if err != nil {
		t.Fatalf("ListBills failed: %v", err)
	}
	initialCount := len(billsBefore)

	// Attempt to create a bill - this should fail and clean up
	_, err = mockService.CreateBill(ctx, &CreateBillRequest{CurrencyCode: "GEL"})
	if err == nil {
		t.Fatal("expected error when workflow start fails")
	}

	// Verify the orphaned bill was cleaned up - count should be the same as before
	billsAfter, listErr := mockService.repository.ListBills(ctx, "")
	if listErr != nil {
		t.Fatalf("ListBills failed: %v", listErr)
	}
	finalCount := len(billsAfter)

	if finalCount != initialCount {
		t.Errorf("expected bill count to remain %d after failed creation, got %d (orphaned bill not cleaned up)", initialCount, finalCount)
	}
}

// --- CloseBill tests ---

func TestCloseBill(t *testing.T) {
	ctx := context.Background()
	mockClient := &mocks.Client{}
	run := &mocks.WorkflowRun{}
	mockClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(run, nil)

	updateHandle := &mocks.WorkflowUpdateHandle{}
	updateHandle.On("Get", mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		inv := args.Get(1).(*model.FinalInvoice)
		*inv = model.FinalInvoice{
			BillID:       "test-bill",
			CurrencyCode: "USD",
			TotalMinor:   1500,
		}
	})
	mockClient.On("UpdateWorkflow", mock.Anything, mock.Anything).Return(updateHandle, nil)
	svc := newMockService(mockClient)

	created, err := svc.CreateBill(ctx, &CreateBillRequest{CurrencyCode: "USD"})
	if err != nil {
		t.Fatalf("CreateBill failed: %v", err)
	}

	resp, err := svc.CloseBill(ctx, created.BillID)
	if err != nil {
		t.Fatalf("CloseBill failed: %v", err)
	}
	if resp.BillID != "test-bill" {
		t.Errorf("expected bill ID test-bill, got %s", resp.BillID)
	}
	if resp.TotalAmountMinor != 1500 {
		t.Errorf("expected total 1500, got %d", resp.TotalAmountMinor)
	}
	mockClient.AssertExpectations(t)
}

func TestCloseBill_WorkflowNotFound(t *testing.T) {
	ctx := context.Background()
	mockClient := &mocks.Client{}
	mockClient.On("UpdateWorkflow", mock.Anything, mock.Anything).Return(nil, serviceerror.NewNotFound("workflow not found"))
	mockService := newMockService(mockClient)

	_, err := mockService.CloseBill(ctx, "nonexistent-bill")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *errs.Error, got %T", err)
	}
	if e.Code != errs.FailedPrecondition {
		t.Errorf("expected FailedPrecondition, got %v", e.Code)
	}
}

// --- AddItemIntoBill tests ---

func TestAddItemIntoBill(t *testing.T) {
	ctx := context.Background()
	mockClient := &mocks.Client{}
	mockClient.On("SignalWorkflow", mock.Anything, "bill-123", "", "AddLineItem", mock.Anything).Return(nil)
	mockService := newMockService(mockClient)

	err := mockService.AddItemIntoBill(ctx, "bill-123", &AddLineItemRequest{
		Description:    "Test fee",
		AmountMinor:    500,
		IdempotencyKey: "key-1",
	})
	if err != nil {
		t.Fatalf("AddItemIntoBill failed: %v", err)
	}
	mockClient.AssertExpectations(t)
}

func TestAddItemIntoBill_WorkflowNotFound(t *testing.T) {
	ctx := context.Background()
	mockClient := &mocks.Client{}
	mockClient.On("SignalWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(serviceerror.NewNotFound("workflow not found"))
	mockService := newMockService(mockClient)

	err := mockService.AddItemIntoBill(ctx, "closed-bill", &AddLineItemRequest{
		Description:    "Fee",
		AmountMinor:    100,
		IdempotencyKey: "k",
	})
	if err == nil {
		t.Fatal("expected error for closed bill")
	}
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *errs.Error, got %T", err)
	}
	if e.Code != errs.FailedPrecondition {
		t.Errorf("expected FailedPrecondition, got %v", e.Code)
	}
}

// --- GetBill tests ---

func TestGetBill(t *testing.T) {
	ctx := context.Background()
	mockClient := &mocks.Client{}
	run := &mocks.WorkflowRun{}
	mockClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(run, nil)
	mockService := newMockService(mockClient)

	created, err := mockService.CreateBill(ctx, &CreateBillRequest{CurrencyCode: "GEL"})
	if err != nil {
		t.Fatalf("CreateBill failed: %v", err)
	}

	resp, err := mockService.GetBill(ctx, created.BillID)
	if err != nil {
		t.Fatalf("GetBill failed: %v", err)
	}
	if resp.BillID != created.BillID {
		t.Errorf("expected bill ID %s, got %s", created.BillID, resp.BillID)
	}
	if resp.Status != "OPEN" {
		t.Errorf("expected OPEN, got %s", resp.Status)
	}
	if resp.CurrencyCode != "GEL" {
		t.Errorf("expected GEL, got %s", resp.CurrencyCode)
	}
}

func TestGetBill_NotFound(t *testing.T) {
	ctx := context.Background()
	mockService := newMockService(&mocks.Client{})

	_, err := mockService.GetBill(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent bill")
	}
}

// --- ListBills tests ---

func TestListBills_Empty(t *testing.T) {
	ctx := context.Background()
	mockService := newMockService(&mocks.Client{})

	resp, err := mockService.ListBills(ctx, &ListBillsRequest{})
	if err != nil {
		t.Fatalf("ListBills failed: %v", err)
	}
	if resp.Bills == nil {
		t.Error("expected non-nil bills slice")
	}
}

func TestListBills_WithFilter(t *testing.T) {
	ctx := context.Background()
	mockClient := &mocks.Client{}
	run := &mocks.WorkflowRun{}
	mockClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(run, nil)
	mockService := newMockService(mockClient)

	_, err := mockService.CreateBill(ctx, &CreateBillRequest{CurrencyCode: "USD"})
	if err != nil {
		t.Fatalf("CreateBill failed: %v", err)
	}
	_, err = mockService.CreateBill(ctx, &CreateBillRequest{CurrencyCode: "GEL"})
	if err != nil {
		t.Fatalf("CreateBill failed: %v", err)
	}

	resp, err := mockService.ListBills(ctx, &ListBillsRequest{Status: "OPEN"})
	if err != nil {
		t.Fatalf("ListBills failed: %v", err)
	}
	if len(resp.Bills) < 2 {
		t.Errorf("expected at least 2 open bills, got %d", len(resp.Bills))
	}
	for _, b := range resp.Bills {
		if b.Status != "OPEN" {
			t.Errorf("expected all bills OPEN, got %s", b.Status)
		}
	}
}
