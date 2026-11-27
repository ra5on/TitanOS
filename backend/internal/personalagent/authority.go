package personalagent

import (
	"context"
	"errors"
)

type authorityKey struct{}
type authority struct {
	resources, consent func(context.Context) error
}

var ErrAuthority = errors.New("native action is no longer authorized")

// WithAuthority binds the broker's live checks to one fenced native operation.
// The narrow consent check is safe inside an app's storage transaction.
func WithAuthority(ctx context.Context, resources, consent func(context.Context) error) context.Context {
	return context.WithValue(ctx, authorityKey{}, authority{resources, consent})
}
func Reauthorize(ctx context.Context) error {
	a, _ := ctx.Value(authorityKey{}).(authority)
	return checkAuthority(ctx, a.resources)
}
func CheckConsent(ctx context.Context) error {
	a, _ := ctx.Value(authorityKey{}).(authority)
	return checkAuthority(ctx, a.consent)
}
func checkAuthority(ctx context.Context, check func(context.Context) error) error {
	if ctx.Err() != nil || check == nil {
		return errors.Join(ErrNoEffect, ErrAuthority)
	}
	if err := check(ctx); err != nil {
		return errors.Join(ErrNoEffect, ErrAuthority, err)
	}
	return nil
}
