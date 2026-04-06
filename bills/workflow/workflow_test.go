package workflow

import (
	"testing"
	"time"

	"encore.app/bills/activities"
	"encore.app/bills/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"go.temporal.io/sdk/testsuite"
)

type BillWorkflowSuite struct {
	suite.Suite
	testsuite.WorkflowTestSuite
	env *testsuite.TestWorkflowEnvironment
}

func (s *BillWorkflowSuite) SetupTest() {
	s.env = s.NewTestWorkflowEnvironment()
	s.env.RegisterActivity(&activities.Activities{})
}

func (s *BillWorkflowSuite) AfterTest(suiteName, testName string) {
	s.env.AssertExpectations(s.T())
}

func TestUnitTestSuite(t *testing.T) {
	suite.Run(t, new(BillWorkflowSuite))
}

func (s *BillWorkflowSuite) Test_AutoClose_NoItems() {
	s.env.OnActivity(activities.Ref.FinalizeBillActivity, mock.Anything, model.FinalizeBillActivityInput{
		BillID:       "bill-1",
		CurrencyCode: "GEL",
		LineItems:    nil,
	}).Return(model.FinalInvoice{
		BillID:       "bill-1",
		CurrencyCode: "GEL",
		TotalMinor:   0,
		ClosedAt:     time.Now(),
	}, nil)

	s.env.ExecuteWorkflow(StartBillWorkflow, model.StartBillWorkflowInput{
		BillID:       "bill-1",
		CurrencyCode: "GEL",
		CloseTimeout: 1 * time.Hour,
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())
}

// Test 2: Add line items then close manually via update
func (s *BillWorkflowSuite) Test_ManualClose_WithItems() {
	// Mock AddItemLineActivity
	s.env.OnActivity(activities.Ref.AddItemLineActivity, mock.Anything, model.AddItemLineActivityInput{
		BillID:         "bill-1",
		AmountMinor:    500,
		CurrencyCode:   "GEL",
		Description:    "Test item",
		IdempotencyKey: "key-1",
	}).
		Return("item-uuid-1", nil)

	// Mock FinalizeBillActivity
	s.env.OnActivity(activities.Ref.FinalizeBillActivity, mock.Anything, mock.Anything).
		Return(model.FinalInvoice{
			BillID:       "bill-1",
			CurrencyCode: "GEL",
			TotalMinor:   500,
			LineItems: []model.FinalLineItem{
				{ID: "item-uuid-1", AmountMinor: 500, Description: "Test item"},
			},
			ClosedAt: time.Now(),
		}, nil)

	// Send a signal to add a line item, then send an update to close
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow("AddLineItem", model.AddItemRequest{
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
				inv := result.(model.FinalInvoice)
				s.Equal(int64(500), inv.TotalMinor)
				s.Equal("bill-1", inv.BillID)
			},
		})
	}, time.Millisecond*100)

	s.env.ExecuteWorkflow(StartBillWorkflow, model.StartBillWorkflowInput{
		BillID:       "bill-1",
		CurrencyCode: "GEL",
		CloseTimeout: 1 * time.Hour,
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())
}

// Test 3: Duplicate idempotency key is ignored (in-memory dedup)
func (s *BillWorkflowSuite) Test_DuplicateLineItem_Ignored() {
	// Only expect ONE call to AddItemLineActivity despite two signals
	s.env.OnActivity(activities.Ref.AddItemLineActivity, mock.Anything, model.AddItemLineActivityInput{
		BillID:         "bill-1",
		AmountMinor:    100,
		CurrencyCode:   "USD",
		Description:    "Item",
		IdempotencyKey: "dup-key",
	}).
		Return("item-1", nil).Once()

	s.env.OnActivity(activities.Ref.FinalizeBillActivity, mock.Anything, mock.Anything).
		Return(model.FinalInvoice{
			BillID:       "bill-1",
			CurrencyCode: "USD",
			TotalMinor:   100,
			ClosedAt:     time.Now(),
		}, nil)

	// Send duplicate signals
	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow("AddLineItem", model.AddItemRequest{
			Description: "Item", AmountMinor: 100, IdempotencyKey: "dup-key",
		})
	}, 0)

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow("AddLineItem", model.AddItemRequest{
			Description: "Item", AmountMinor: 100, IdempotencyKey: "dup-key",
		})
	}, time.Millisecond*50)

	// Let the timer fire to auto-close
	s.env.ExecuteWorkflow(StartBillWorkflow, model.StartBillWorkflowInput{
		BillID:       "bill-1",
		CurrencyCode: "USD",
		CloseTimeout: 1 * time.Second,
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())
}

// Test 4: Activity failure is handled gracefully
func (s *BillWorkflowSuite) Test_AddItemActivity_Failure() {
	s.env.OnActivity(activities.Ref.AddItemLineActivity, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return("", assert.AnError)

	s.env.OnActivity(activities.Ref.FinalizeBillActivity, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(model.FinalInvoice{BillID: "bill-1", CurrencyCode: "GEL"}, nil)

	s.env.RegisterDelayedCallback(func() {
		s.env.SignalWorkflow("AddLineItem", model.AddItemRequest{
			Description: "Fail", AmountMinor: 100, IdempotencyKey: "fail-key",
		})
	}, 0)

	// Auto-close via short timer
	s.env.ExecuteWorkflow(StartBillWorkflow, model.StartBillWorkflowInput{
		BillID:       "bill-1",
		CurrencyCode: "GEL",
		CloseTimeout: 1 * time.Second,
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())
}

// Test 5: Update validator rejects CloseBill on already-closed bill
func (s *BillWorkflowSuite) Test_CloseBill_AlreadyClosed_Rejected() {
	// Mock FinalizeBillActivity for the first close
	s.env.OnActivity(activities.Ref.FinalizeBillActivity, mock.Anything, mock.Anything).
		Return(model.FinalInvoice{
			BillID:       "bill-1",
			CurrencyCode: "USD",
			TotalMinor:   0,
			ClosedAt:     time.Now(),
		}, nil).Once()

	// Track whether the second update was rejected
	secondUpdateRejected := false

	// First close: should succeed
	s.env.RegisterDelayedCallback(func() {
		s.env.UpdateWorkflow("CloseBill", "update-1", &testsuite.TestUpdateCallback{
			OnAccept: func() {},
			OnComplete: func(result interface{}, err error) {
				s.NoError(err)
			},
		})
	}, time.Millisecond*10)

	// Second close: should be rejected by the validator
	// Use a longer delay to ensure the first update has completed
	s.env.RegisterDelayedCallback(func() {
		s.env.UpdateWorkflow("CloseBill", "update-2", &testsuite.TestUpdateCallback{
			OnReject: func(err error) {
				secondUpdateRejected = true
				s.Error(err)
				s.Contains(err.Error(), "already closed")
			},
			OnAccept: func() {
				s.Fail("expected update to be rejected, but it was accepted")
			},
			OnComplete: func(result interface{}, err error) {
				// This should not be called if the update was rejected
				if !secondUpdateRejected {
					s.Fail("expected update to be rejected, but it completed")
				}
			},
		})
	}, time.Millisecond*100)

	s.env.ExecuteWorkflow(StartBillWorkflow, model.StartBillWorkflowInput{
		BillID:       "bill-1",
		CurrencyCode: "USD",
		CloseTimeout: 1 * time.Hour,
	})

	s.True(s.env.IsWorkflowCompleted())
	s.NoError(s.env.GetWorkflowError())
}
