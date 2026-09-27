package model

import "time"

type Link struct {
	ID          int64      `json:"id"`
	Title       *string    `json:"title"`
	OriginalUrl string     `json:"originalUrl"`
	Code        string     `json:"code"`
	CreatedAt   time.Time  `json:"createdAt"`
	ValidTill   *time.Time `json:"validTill"`
	Clicks      int        `json:"clicks"`
	UserID      int64      `json:"userID"`
}

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Email        *string   `json:"email"`
	CreatedAt    time.Time `json:"createdAt"`
	PasswordHash string    `json:"-"`
}

type LoginUser struct {
	ID           int64  `json:"id"`
	PasswordHash string `json:"-"`
}

type Token struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"userID"`
	TokenHash string    `json:"tokenHash"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}
