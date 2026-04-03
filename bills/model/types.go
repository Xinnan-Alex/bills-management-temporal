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
