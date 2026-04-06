package bills

import (
	"context"
	"testing"

	"encore.dev/beta/errs"
)

func TestAuthHandler_ValidToken(t *testing.T) {
	ctx := context.Background()
	// Use the actual secret value from the environment
	validToken := secrets.SuperSecretKey

	uid, err := AuthHandler(ctx, validToken)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if uid == "" {
		t.Fatal("expected non-empty UID")
	}

	// Verify deterministic: same token should produce the same UID
	uid2, err := AuthHandler(ctx, validToken)
	if err != nil {
		t.Fatalf("expected no error on second call, got %v", err)
	}
	if uid != uid2 {
		t.Errorf("expected deterministic UID, got %s and %s", uid, uid2)
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
