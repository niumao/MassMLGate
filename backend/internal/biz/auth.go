package biz

import (
	"context"
	"errors"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrAccountDisabled    = errors.New("account is disabled")
)

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(hash, password string) bool
}

type AuthBiz struct {
	users  UserRepo
	hasher PasswordHasher
}

func NewAuthBiz(users UserRepo, hasher PasswordHasher) *AuthBiz {
	return &AuthBiz{users: users, hasher: hasher}
}

func (b *AuthBiz) Login(ctx context.Context, email, password string) (*User, error) {
	u, err := b.users.GetByEmail(ctx, email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if !b.hasher.Verify(u.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}
	if u.Status == "disabled" {
		return nil, ErrAccountDisabled
	}
	return u, nil
}
