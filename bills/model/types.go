package model

import (
	"time"
)

// FinalInvoice represents a closed bill with all its line items
type FinalInvoice struct {
	BillID       string
	CurrencyCode string
	TotalMinor   int64
	LineItems    []FinalLineItem
	ClosedAt     time.Time
}

// FinalLineItem represents a line item in a finalized invoice
type FinalLineItem struct {
	ID          string
	AmountMinor int64
	Description string
	CreatedAt   time.Time
}

// StartBillWorkflowInput is the single input struct for StartBillWorkflow
type StartBillWorkflowInput struct {
	BillID       string
	CurrencyCode string
	CloseTimeout time.Duration
}

// FinalizeBillActivityInput is the single input struct for FinalizeBillActivity
type FinalizeBillActivityInput struct {
	BillID       string
	CurrencyCode string
	LineItems    []FinalLineItem
}

// AddItemLineActivityInput is the single input struct for AddItemLineActivity
type AddItemLineActivityInput struct {
	BillID         string
	AmountMinor    int64
	CurrencyCode   string
	Description    string
	IdempotencyKey string
}

// AddItemRequest is the signal payload for adding a line item
type AddItemRequest struct {
	Description    string
	AmountMinor    int64
	IdempotencyKey string
}

// BillState holds the workflow's in-memory state
type BillState struct {
	Status       string
	RunningTotal int64
	SeenItems    map[string]bool
	FinalInvoice FinalInvoice
}
