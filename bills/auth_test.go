package bills

import (
	"context"
	"testing"

	"encore.dev/beta/errs"
)

func TestAuthHandler_ValidToken(t *testing.T) {
	ctx := context.Background()
	uid, err := AuthHandler(ctx, "superSecretToken")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if uid == "" {
		t.Fatal("expected non-empty UID")
	}
}

func TestAuthHandler_InvalidToken(t *testing.T) {
	ctx := context.Background()
	_, err := AuthHandler(ctx, "wrongToken")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	if errs.Code(err) != errs.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", errs.Code(err))
	}
}

func TestAuthHandler_EmptyToken(t *testing.T) {
	ctx := context.Background()
	_, err := AuthHandler(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
	if errs.Code(err) != errs.Unauthenticated {
		t.Errorf("expected Unauthenticated, got %v", errs.Code(err))
	}
}
