package main

import (
	"time"
)

// LinuxdoInviteCode 兼容原 Java 表结构 linuxdo_invite_code
type LinuxdoInviteCode struct {
	ID        uint64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Code      string     `gorm:"column:code;not null;uniqueIndex" json:"code"`
	Email     *string    `gorm:"column:email" json:"email,omitempty"`
	ClaimedAt *time.Time `gorm:"column:claimed_at" json:"claimed_at,omitempty"`
	CreatedAt time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

func (LinuxdoInviteCode) TableName() string {
	return "linuxdo_invite_code"
}

type LinuxDoInviteRecordVO struct {
	MaskedIdentifier string     `json:"maskedIdentifier"`
	ClaimedAt        *time.Time `json:"claimedAt"`
}

type LinuxDoInviteRecordsVO struct {
	List  []LinuxDoInviteRecordVO `json:"list"`
	Total int64                   `json:"total"`
}

type UploadRequest struct {
	Codes []string `json:"codes" binding:"required,min=1"`
}

type InviteLinkVO struct {
	Code      string     `json:"code"`
	Status    string     `json:"status"`
	Email     string     `json:"email,omitempty"`
	ClaimedAt *time.Time `json:"claimedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

type AdminCodesVO struct {
	List  []InviteLinkVO `json:"list"`
	Total int64          `json:"total"`
	Page  int            `json:"page"`
	Size  int            `json:"size"`
}

type Result[T any] struct {
	Code    int    `json:"code"`
	Data    T      `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
}

func OK[T any](data T) Result[T] {
	return Result[T]{Code: 200, Data: data}
}

func OKVoid() Result[any] {
	return Result[any]{Code: 200}
}

func Fail(msg string) Result[any] {
	return Result[any]{Code: 500, Message: msg}
}
