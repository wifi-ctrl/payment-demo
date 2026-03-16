package model

// UserID 用户唯一标识
type UserID string

// User 用户聚合根
type User struct {
	ID         UserID
	ExternalID string // 游戏平台账号 ID
	GameID     string
	Status     UserStatus
}

type UserStatus string

const (
	UserStatusActive UserStatus = "ACTIVE"
	UserStatusBanned UserStatus = "BANNED"
)

func (u *User) IsBanned() bool {
	return u.Status == UserStatusBanned
}
