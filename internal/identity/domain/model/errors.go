package model

import "errors"

var (
	ErrUserNotFound   = errors.New("user not found")
	ErrUserBanned     = errors.New("user is banned")
	ErrInvalidToken   = errors.New("invalid access token")
	ErrSessionExpired = errors.New("session expired")
)
