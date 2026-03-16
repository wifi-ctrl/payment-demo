package model

import "time"

// SessionID 会话唯一标识
type SessionID string

// Session 会话聚合根
type Session struct {
	ID          SessionID
	UserID      UserID
	AccessToken string
	ExpiresAt   time.Time
}

func (s *Session) IsExpired(now time.Time) bool {
	return now.After(s.ExpiresAt)
}
