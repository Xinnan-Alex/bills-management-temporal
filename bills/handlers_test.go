package bills

import (
	"testing"

	"encore.dev/beta/errs"
)

func TestCreateBillRequest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		currency string
		wantErr  bool
	}{
		{"valid USD", "USD", false},
		{"valid GEL", "GEL", false},
		{"invalid EUR", "EUR", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &CreateBillRequest{CurrencyCode: tt.currency}
			err := req.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func TestAddLineItemRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     AddLineItemRequest
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid",
			req:     AddLineItemRequest{Description: "Fee", AmountMinor: 100, IdempotencyKey: "k1"},
			wantErr: false,
		},
		{
			name:    "zero amount",
			req:     AddLineItemRequest{Description: "Fee", AmountMinor: 0, IdempotencyKey: "k1"},
			wantErr: true,
			errMsg:  "amountMinor must be positive",
		},
		{
			name:    "negative amount",
			req:     AddLineItemRequest{Description: "Fee", AmountMinor: -100, IdempotencyKey: "k1"},
			wantErr: true,
			errMsg:  "amountMinor must be positive",
		},
		{
			name:    "empty description",
			req:     AddLineItemRequest{Description: "", AmountMinor: 100, IdempotencyKey: "k1"},
			wantErr: true,
			errMsg:  "description is required",
		},
		{
			name:    "empty idempotency key",
			req:     AddLineItemRequest{Description: "Fee", AmountMinor: 100, IdempotencyKey: ""},
			wantErr: true,
			errMsg:  "idempotencyKey is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}
				e, ok := err.(*errs.Error)
				if !ok {
					t.Errorf("expected *errs.Error, got %T", err)
					return
				}
				if e.Code != errs.InvalidArgument {
					t.Errorf("expected InvalidArgument, got %v", e.Code)
				}
				if e.Message != tt.errMsg {
					t.Errorf("expected message %q, got %q", tt.errMsg, e.Message)
				}
			} else if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func TestListBillsRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		wantErr bool
	}{
		{"empty (all)", "", false},
		{"OPEN", "OPEN", false},
		{"CLOSED", "CLOSED", false},
		{"invalid", "PENDING", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &ListBillsRequest{Status: tt.status}
			err := req.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}
