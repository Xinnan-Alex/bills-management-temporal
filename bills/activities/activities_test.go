package activities

import (
	"context"
	"testing"

	"encore.app/bills/model"
	"encore.app/bills/repository"
)

func TestFinalizeBillActivity(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewRepository()
	act := &Activities{Repo: repo}

	billID, err := repo.StoreBill(ctx, "USD")
	if err != nil {
		t.Fatalf("StoreBill failed: %v", err)
	}

	_, err = repo.AddLineItem(ctx, billID, 1500, "USD", "Service fee", "key-1")
	if err != nil {
		t.Fatalf("AddLineItem failed: %v", err)
	}
	_, err = repo.AddLineItem(ctx, billID, 2500, "USD", "Platform fee", "key-2")
	if err != nil {
		t.Fatalf("AddLineItem failed: %v", err)
	}

	lineItems := []model.FinalLineItem{
		{ID: "li-1", AmountMinor: 1500, Description: "Service fee"},
		{ID: "li-2", AmountMinor: 2500, Description: "Platform fee"},
	}

	inv, err := act.FinalizeBillActivity(ctx, billID, "USD", lineItems)
	if err != nil {
		t.Fatalf("FinalizeBillActivity failed: %v", err)
	}

	if inv.BillID != billID {
		t.Errorf("expected bill ID %s, got %s", billID, inv.BillID)
	}
	if inv.CurrencyCode != "USD" {
		t.Errorf("expected USD, got %s", inv.CurrencyCode)
	}
	if inv.TotalMinor != 4000 {
		t.Errorf("expected total 4000, got %d", inv.TotalMinor)
	}
	if len(inv.LineItems) != 2 {
		t.Fatalf("expected 2 line items, got %d", len(inv.LineItems))
	}
	if inv.ClosedAt.IsZero() {
		t.Error("expected ClosedAt to be set")
	}
}

func TestFinalizeBillActivity_EmptyLineItems(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewRepository()
	act := &Activities{Repo: repo}

	billID, err := repo.StoreBill(ctx, "GEL")
	if err != nil {
		t.Fatalf("StoreBill failed: %v", err)
	}

	inv, err := act.FinalizeBillActivity(ctx, billID, "GEL", nil)
	if err != nil {
		t.Fatalf("FinalizeBillActivity failed: %v", err)
	}

	if inv.TotalMinor != 0 {
		t.Errorf("expected total 0, got %d", inv.TotalMinor)
	}
	if len(inv.LineItems) != 0 {
		t.Errorf("expected 0 line items, got %d", len(inv.LineItems))
	}
	if inv.ClosedAt.IsZero() {
		t.Error("expected ClosedAt to be set")
	}
}

func TestAddItemLineActivity(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewRepository()
	act := &Activities{Repo: repo}

	billID, err := repo.StoreBill(ctx, "USD")
	if err != nil {
		t.Fatalf("StoreBill failed: %v", err)
	}

	itemID, err := act.AddItemLineActivity(ctx, billID, 750, "USD", "Delivery charge", "idem-1")
	if err != nil {
		t.Fatalf("AddItemLineActivity failed: %v", err)
	}
	if itemID == "" {
		t.Fatal("expected non-empty item ID")
	}

	bill, err := repo.GetBill(ctx, billID)
	if err != nil {
		t.Fatalf("GetBill failed: %v", err)
	}
	if bill.TotalMinor != 750 {
		t.Errorf("expected total 750, got %d", bill.TotalMinor)
	}
	if len(bill.LineItems) != 1 {
		t.Fatalf("expected 1 line item, got %d", len(bill.LineItems))
	}
	if bill.LineItems[0].Description != "Delivery charge" {
		t.Errorf("expected 'Delivery charge', got %s", bill.LineItems[0].Description)
	}
}

func TestAddItemLineActivity_Idempotent(t *testing.T) {
	ctx := context.Background()
	repo := repository.NewRepository()
	act := &Activities{Repo: repo}

	billID, err := repo.StoreBill(ctx, "USD")
	if err != nil {
		t.Fatalf("StoreBill failed: %v", err)
	}

	id1, err := act.AddItemLineActivity(ctx, billID, 500, "USD", "Fee", "same-key")
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	id2, err := act.AddItemLineActivity(ctx, billID, 500, "USD", "Fee", "same-key")
	if err != nil {
		t.Fatalf("duplicate call failed: %v", err)
	}

	if id1 != id2 {
		t.Errorf("expected same item ID on duplicate, got %s and %s", id1, id2)
	}

	bill, err := repo.GetBill(ctx, billID)
	if err != nil {
		t.Fatalf("GetBill failed: %v", err)
	}
	if bill.TotalMinor != 500 {
		t.Errorf("expected total 500 (idempotent), got %d", bill.TotalMinor)
	}
}
