package controller

import (
	"context"

	"github.com/iakinsey/delver/types"
	"github.com/iakinsey/delver/types/errs"
)

func GetCurrentToken(ctx context.Context) *types.Token {
	if t, ok := ctx.Value(types.AuthHeader).(*types.Token); ok {
		return t
	}

	return nil
}

func GetCurrentUser(ctx context.Context) *types.User {
	if u, ok := ctx.Value(types.UserHeader).(*types.User); ok {
		return u
	}

	return nil
}

func CurrentUserMatchesEmail(ctx context.Context, email string) error {
	currentUser := GetCurrentUser(ctx)

	if currentUser == nil || currentUser.Email != email {
		return errs.NewAuthError("Unauthorized")
	}

	return nil
}
