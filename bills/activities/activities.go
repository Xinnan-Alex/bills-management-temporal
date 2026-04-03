package activities

import (
	"context"
	"time"

	"encore.app/bills/model"
	"encore.app/bills/repository"
)

// Activities holds dependencies for Temporal activities
type Activities struct {
	Repo *repository.Repository
}

// Ref is a nil reference used by workflows to get activity method references.
// This enables compile-time type checking of activity invocations.
var Ref *Activities

func (a *Activities) FinalizeBillActivity(ctx context.Context, billID string, currencyCode string, lineItems []model.FinalLineItem) (model.FinalInvoice, error) {
	repoItems := make([]repository.LineItem, len(lineItems))
	for i, item := range lineItems {
		repoItems[i] = repository.LineItem{
			ID:          item.ID,
			AmountMinor: item.AmountMinor,
			Description: item.Description,
			CreatedAt:   item.CreatedAt,
		}
	}

	bill, err := a.Repo.CloseBill(ctx, billID, currencyCode, repoItems)
	if err != nil {
		return model.FinalInvoice{}, err
	}

	wfItems := make([]model.FinalLineItem, len(bill.LineItems))
	for i, item := range bill.LineItems {
		wfItems[i] = model.FinalLineItem{
			ID:          item.ID,
			AmountMinor: item.AmountMinor,
			Description: item.Description,
			CreatedAt:   item.CreatedAt,
		}
	}

	var closedAt time.Time
	if bill.ClosedAt != nil {
		closedAt = *bill.ClosedAt
	}

	return model.FinalInvoice{
		BillID:       bill.BillID,
		CurrencyCode: bill.CurrencyCode,
		TotalMinor:   bill.TotalMinor,
		LineItems:    wfItems,
		ClosedAt:     closedAt,
	}, nil
}

func (a *Activities) AddItemLineActivity(ctx context.Context, billID string, amountMinor int64, currencyCode string, description string, idempotencyKey string) (string, error) {
	return a.Repo.AddLineItem(ctx, billID, amountMinor, currencyCode, description, idempotencyKey)
}
