package workflow

import (
	"time"
)

type FinalInvoice struct {
	BillID       string
	CurrencyCode string
	TotalMinor   int64
	LineItems    []FinalLineItem
	ClosedAt     time.Time
}

type FinalLineItem struct {
	ID          string
	AmountMinor int64
	Description string
	CreatedAt   time.Time
}

type AddItemRequest struct {
	Description    string
	AmountMinor    int64
	IdempotencyKey string
}

type BillState struct {
	Status       string
	RunningTotal int64
	SeenItems    map[string]bool
	FinalInvoice FinalInvoice
}
