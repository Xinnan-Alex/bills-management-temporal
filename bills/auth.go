package bills

import (
	"context"
	"crypto/sha256"
	"fmt"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
)

//encore:authhandler
func AuthHandler(ctx context.Context, token string) (auth.UID, error) {
	if token != secrets.SuperSecretKey {
		return "", &errs.Error{
			Code:    errs.Unauthenticated,
			Message: "invalid token",
		}
	}
	// Derive a deterministic UID from the token so the same token
	// always maps to the same user identity.
	hash := sha256.Sum256([]byte(token))
	return auth.UID(fmt.Sprintf("%x", hash[:16])), nil
}
