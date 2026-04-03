package bills

import (
	"context"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

//encore:authhandler
func AuthHandler(ctx context.Context, token string) (auth.UID, error) {
	// Validate the token and look up the user id and user data,
	// for example by calling Firebase Auth.
	if token != "superSecretToken" {
		return "", &errs.Error{
			Code:    errs.Unauthenticated,
			Message: "invalid token",
		}
	}
	return auth.UID(uuid.New().String()), nil
}
