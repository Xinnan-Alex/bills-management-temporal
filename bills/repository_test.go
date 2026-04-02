package bills

import (
	"context"
	"testing"
)

func TestStoreBill(t *testing.T) {
	ctx := context.Background()
	r := NewRepository(billsDB)

	billID, err := r.StoreBill(ctx, "USD")
	if err != nil {
		t.Fatalf("StoreBill failed: %v", err)
	}
	if billID == "" {
		t.Fatal("expected non-empty bill ID")
	}

	bill, err := r.GetBill(ctx, billID)
	if err != nil {
		t.Fatalf("GetBill failed: %v", err)
	}
	if bill.Status != "OPEN" {
		t.Errorf("expected OPEN, got %s", bill.Status)
	}
	if bill.CurrencyCode != "USD" {
		t.Errorf("expected USD, got %s", bill.CurrencyCode)
	}
	if bill.TotalMinor != 0 {
		t.Errorf("expected 0 total, got %d", bill.TotalMinor)
	}
}

func TestAddLineItem(t *testing.T) {
	ctx := context.Background()
	r := NewRepository(billsDB)

	billID, err := r.StoreBill(ctx, "GEL")
	if err != nil {
		t.Fatalf("StoreBill failed: %v", err)
	}

	itemID, err := r.AddLineItem(ctx, billID, 1000, "GEL", "Service fee", "key-1")
	if err != nil {
		t.Fatalf("AddLineItem failed: %v", err)
	}
	if itemID == "" {
		t.Fatal("expected non-empty item ID")
	}

	bill, err := r.GetBill(ctx, billID)
	if err != nil {
		t.Fatalf("GetBill failed: %v", err)
	}
	if bill.TotalMinor != 1000 {
		t.Errorf("expected total 1000, got %d", bill.TotalMinor)
	}
	if len(bill.LineItems) != 1 {
		t.Fatalf("expected 1 line item, got %d", len(bill.LineItems))
	}
	if bill.LineItems[0].Description != "Service fee" {
		t.Errorf("expected 'Service fee', got %s", bill.LineItems[0].Description)
	}
}

func TestAddLineItem_IdempotencyPreventsDoubleCount(t *testing.T) {
	ctx := context.Background()
	r := NewRepository(billsDB)

	billID, err := r.StoreBill(ctx, "USD")
	if err != nil {
		t.Fatalf("StoreBill failed: %v", err)
	}

	// First insert
	itemID1, err := r.AddLineItem(ctx, billID, 500, "USD", "Fee A", "idempotent-key-1")
	if err != nil {
		t.Fatalf("first AddLineItem failed: %v", err)
	}

	// Duplicate insert with same idempotency key
	itemID2, err := r.AddLineItem(ctx, billID, 500, "USD", "Fee A", "idempotent-key-1")
	if err != nil {
		t.Fatalf("duplicate AddLineItem failed: %v", err)
	}

	// Should return the same item ID
	if itemID1 != itemID2 {
		t.Errorf("expected same item ID on duplicate, got %s and %s", itemID1, itemID2)
	}

	bill, err := r.GetBill(ctx, billID)
	if err != nil {
		t.Fatalf("GetBill failed: %v", err)
	}

	// Total should only be 500, not 1000
	if bill.TotalMinor != 500 {
		t.Errorf("expected total 500 (idempotent), got %d", bill.TotalMinor)
	}
	if len(bill.LineItems) != 1 {
		t.Errorf("expected 1 line item (idempotent), got %d", len(bill.LineItems))
	}
}

func TestCloseBill(t *testing.T) {
	ctx := context.Background()
	r := NewRepository(billsDB)

	billID, err := r.StoreBill(ctx, "USD")
	if err != nil {
		t.Fatalf("StoreBill failed: %v", err)
	}

	_, err = r.AddLineItem(ctx, billID, 2000, "USD", "Item 1", "close-key-1")
	if err != nil {
		t.Fatalf("AddLineItem failed: %v", err)
	}
	_, err = r.AddLineItem(ctx, billID, 3000, "USD", "Item 2", "close-key-2")
	if err != nil {
		t.Fatalf("AddLineItem failed: %v", err)
	}

	lineItems := []LineItem{
		{ID: "li-1", AmountMinor: 2000, Description: "Item 1"},
		{ID: "li-2", AmountMinor: 3000, Description: "Item 2"},
	}

	closed, err := r.CloseBill(ctx, billID, "USD", lineItems)
	if err != nil {
		t.Fatalf("CloseBill failed: %v", err)
	}
	if closed.Status != "CLOSED" {
		t.Errorf("expected CLOSED, got %s", closed.Status)
	}
	if closed.TotalMinor != 5000 {
		t.Errorf("expected total 5000, got %d", closed.TotalMinor)
	}
	if closed.ClosedAt == nil {
		t.Error("expected ClosedAt to be set")
	}

	// Verify via GetBill — closed bills use closed_total_minor
	bill, err := r.GetBill(ctx, billID)
	if err != nil {
		t.Fatalf("GetBill after close failed: %v", err)
	}
	if bill.Status != "CLOSED" {
		t.Errorf("expected CLOSED, got %s", bill.Status)
	}
	if bill.TotalMinor != 5000 {
		t.Errorf("expected closed total 5000, got %d", bill.TotalMinor)
	}
}

func TestGetBill_NotFound(t *testing.T) {
	ctx := context.Background()
	r := NewRepository(billsDB)

	_, err := r.GetBill(ctx, "00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected error for nonexistent bill")
	}
}

func TestListBills(t *testing.T) {
	ctx := context.Background()
	r := NewRepository(billsDB)

	// Create an open bill
	openID, err := r.StoreBill(ctx, "USD")
	if err != nil {
		t.Fatalf("StoreBill failed: %v", err)
	}

	// Create and close a bill
	closedID, err := r.StoreBill(ctx, "GEL")
	if err != nil {
		t.Fatalf("StoreBill failed: %v", err)
	}
	_, err = r.CloseBill(ctx, closedID, "GEL", []LineItem{})
	if err != nil {
		t.Fatalf("CloseBill failed: %v", err)
	}

	// List all
	all, err := r.ListBills(ctx, "")
	if err != nil {
		t.Fatalf("ListBills all failed: %v", err)
	}
	if len(all) < 2 {
		t.Errorf("expected at least 2 bills, got %d", len(all))
	}

	// List OPEN only
	open, err := r.ListBills(ctx, "OPEN")
	if err != nil {
		t.Fatalf("ListBills OPEN failed: %v", err)
	}
	foundOpen := false
	for _, b := range open {
		if b.BillID == openID {
			foundOpen = true
		}
		if b.Status != "OPEN" {
			t.Errorf("expected all OPEN, got %s for bill %s", b.Status, b.BillID)
		}
	}
	if !foundOpen {
		t.Error("expected to find the open bill in OPEN filter")
	}

	// List CLOSED only
	closed, err := r.ListBills(ctx, "CLOSED")
	if err != nil {
		t.Fatalf("ListBills CLOSED failed: %v", err)
	}
	foundClosed := false
	for _, b := range closed {
		if b.BillID == closedID {
			foundClosed = true
		}
		if b.Status != "CLOSED" {
			t.Errorf("expected all CLOSED, got %s for bill %s", b.Status, b.BillID)
		}
	}
	if !foundClosed {
		t.Error("expected to find the closed bill in CLOSED filter")
	}
}
