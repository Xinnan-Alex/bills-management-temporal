package bills

import (
	"context"
	"time"

	"encore.app/bills/repository"
	"encore.app/bills/workflow"
)

// Activities holds dependencies for Temporal activities
type Activities struct {
	repo *repository.Repository
}

func (a *Activities) FinalizeBillActivity(ctx context.Context, billID string, currencyCode string, lineItems []workflow.FinalLineItem) (workflow.FinalInvoice, error) {
	repoItems := make([]repository.LineItem, len(lineItems))
	for i, item := range lineItems {
		repoItems[i] = repository.LineItem{
			ID:          item.ID,
			AmountMinor: item.AmountMinor,
			Description: item.Description,
			CreatedAt:   item.CreatedAt,
		}
	}

	bill, err := a.repo.CloseBill(ctx, billID, currencyCode, repoItems)
	if err != nil {
		return workflow.FinalInvoice{}, err
	}

	wfItems := make([]workflow.FinalLineItem, len(bill.LineItems))
	for i, item := range bill.LineItems {
		wfItems[i] = workflow.FinalLineItem{
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

	return workflow.FinalInvoice{
		BillID:       bill.BillID,
		CurrencyCode: bill.CurrencyCode,
		TotalMinor:   bill.TotalMinor,
		LineItems:    wfItems,
		ClosedAt:     closedAt,
	}, nil
}

func (a *Activities) AddItemLineActivity(ctx context.Context, billID string, amountMinor int64, currencyCode string, description string, idempotencyKey string) (string, error) {
	return a.repo.AddLineItem(ctx, billID, amountMinor, currencyCode, description, idempotencyKey)
}
