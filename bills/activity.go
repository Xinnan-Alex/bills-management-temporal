package bills

import (
	"context"
	"time"

	"encore.app/bills/workflow"
)

func FinalizeBillActivity(ctx context.Context, billID string, currencyCode string, lineItems []workflow.FinalLineItem) (workflow.FinalInvoice, error) {
	repoItems := make([]LineItem, len(lineItems))
	for i, item := range lineItems {
		repoItems[i] = LineItem{
			ID:          item.ID,
			AmountMinor: item.AmountMinor,
			Description: item.Description,
			CreatedAt:   item.CreatedAt,
		}
	}

	bill, err := repo.CloseBill(ctx, billID, currencyCode, repoItems)
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

func AddItemLineActivity(ctx context.Context, billID string, amountMinor int64, currencyCode string, description string, idempotencyKey string) (string, error) {
	return repo.AddLineItem(ctx, billID, amountMinor, currencyCode, description, idempotencyKey)
}
