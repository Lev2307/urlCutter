package model

import "time"

type Link struct {
	ID          int        `json:"id"`
	Title       *string    `json:"title"`
	OriginalUrl string     `json:"originalUrl"`
	Code        string     `json:"code"`
	CreatedAt   time.Time  `json:"createdAt"`
	ValidTill   *time.Time `json:"validTill"`
	Clicks      int        `json:"clicks"`
	UserID      *int64     `json:"userID"`
}
