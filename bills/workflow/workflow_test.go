package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"
)

// Stubs so the test env can resolve string-based activity names.
// OnActivity mocks override these at runtime.
func FinalizeBillActivity(ctx context.Context, billID string, currencyCode string, lineItems []FinalLineItem) (FinalInvoice, error) {
	return FinalInvoice{}, nil
}

func AddItemLineActivity(ctx context.Context, billID string, amountMinor int64, currencyCode string, description string, idempotencyKey string) (string, error) {
	return "", nil
}

type BillWorkflowSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
}

func (s *BillWorkflowSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
	s.env.RegisterActivity(FinalizeBillActivity)
	s.env.RegisterActivity(AddItemLineActivity)
}

func (s *BillWorkflowSuite) AfterTest(suiteName, testName string) {
	s.env.AssertExpectations(s.T())
}

func TestUnitTestSuite(t *testing.T) {
	suite.Run(t, new(BillWorkflowSuite))
}

func (s *BillWorkflowSuite) Test_AutoClose_NoItems() {
	s.env.OnActivity("FinalizeBillActivity", mock.Anything, "bill-1", "GEL", []FinalLineItem(nil)).Return(FinalInvoice{
		BillID:       "bill-1",
		CurrencyCode: "GEL",
		TotalMinor:   0,
		ClosedAt:     time.Now(),
	}, nil)

	s.env.ExecuteWorkflow(StartBillWorkflow, "bill-1", "GEL", 1*time.Hour)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())
}

// Test 2: Add line items then close manually via update
func (s *BillWorkflowSuite) Test_ManualClose_WithItems() {
	// Mock AddItemLineActivity
	s.env.OnActivity("AddItemLineActivity", mock.Anything, "bill-1", int64(500), "GEL", "Test item", "key-1").
		Return("item-uuid-1", nil)

	// Mock FinalizeBillActivity
	s.env.OnActivity("FinalizeBillActivity", mock.Anything, "bill-1", "GEL", mock.Anything).
		Return(FinalInvoice{
			BillID:       "bill-1",
			CurrencyCode: "GEL",
			TotalMinor:   500,
			LineItems: []FinalLineItem{
				{ID: "item-uuid-1", AmountMinor: 500, Description: "Test item"},
			},
			ClosedAt: time.Now(),
		}, nil)

	// Send a signal to add a line item, then send an update to close
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow("AddLineItem", AddItemRequest{
			Description:    "Test item",
			AmountMinor:    500,
			IdempotencyKey: "key-1",
		})
	}, time.Millisecond*0) // fires immediately

	s.env.RegisterDelayedCallback(func() {
		s.env.UpdateWorkflow("CloseBill", "update-id", &testsuite.TestUpdateCallback{
			OnAccept: func() {},
			OnComplete: func(result interface{}, err error) {
				s.NoError(err)
				inv := result.(FinalInvoice)
				s.Equal(int64(500), inv.TotalMinor)
				s.Equal("bill-1", inv.BillID)
			},
		})
	}, time.Millisecond*100)

	s.env.ExecuteWorkflow(StartBillWorkflow, "bill-1", "GEL", 1*time.Hour)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())
}

// Test 3: Duplicate idempotency key is ignored (in-memory dedup)
func (s *BillWorkflowSuite) Test_DuplicateLineItem_Ignored() {
	// Only expect ONE call to AddItemLineActivity despite two signals
	s.env.OnActivity("AddItemLineActivity", mock.Anything, "bill-1", int64(100), "USD", "Item", "dup-key").
		Return("item-1", nil).Once()

	s.env.OnActivity("FinalizeBillActivity", mock.Anything, "bill-1", "USD", mock.Anything).
		Return(FinalInvoice{
			BillID:       "bill-1",
			CurrencyCode: "USD",
			TotalMinor:   100,
			ClosedAt:     time.Now(),
		}, nil)

	// Send duplicate signals
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow("AddLineItem", AddItemRequest{
			Description: "Item", AmountMinor: 100, IdempotencyKey: "dup-key",
		})
	}, 0)

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow("AddLineItem", AddItemRequest{
			Description: "Item", AmountMinor: 100, IdempotencyKey: "dup-key",
		})
	}, time.Millisecond*50)

	// Let the timer fire to auto-close
	s.env.ExecuteWorkflow(StartBillWorkflow, "bill-1", "USD", 1*time.Second)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())
}

// Test 4: Activity failure is handled gracefully
func (s *BillWorkflowSuite) Test_AddItemActivity_Failure() {
	s.env.OnActivity("AddItemLineActivity", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("", assert.AnError)

	s.env.OnActivity("FinalizeBillActivity", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(FinalInvoice{BillID: "bill-1", CurrencyCode: "GEL"}, nil)

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow("AddLineItem", AddItemRequest{
			Description: "Fail", AmountMinor: 100, IdempotencyKey: "fail-key",
		})
	}, 0)

	// Auto-close via short timer
	s.env.ExecuteWorkflow(StartBillWorkflow, "bill-1", "GEL", 1*time.Second)

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())
}
