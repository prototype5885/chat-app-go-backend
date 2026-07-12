package main

import (
	"database/sql"

	"github.com/bwmarrin/snowflake"
)

type Handler struct {
	db    *sql.DB
	idGen *snowflake.Node
	sm    *SessionManager
}

// type Username string

// type RegistrationReq struct {
// 	Username string `validate:"required,min=6,max=32"`
// 	Password string `validate:"required,min=6,max=128"`
// }

// type LoginReq struct {
// 	Username string `validate:"required,min=6,max=32"`
// 	Password string `validate:"required,min=6,max=128"`
// }

type UserDatabase struct {
	Id           int64   `json:"id"`
	Username     string  `json:"username"`
	DisplayName  string  `json:"display_name"`
	Picture      *string `json:"picture,omitempty"`
	Banned       bool    `json:"banned"`
	CustomStatus *string `json:"custom_status,omitempty"`
}

type ServerDatabase struct {
	Id      int64   `json:"id"`
	OwnerID int64   `json:"owner_id"`
	Name    string  `json:"name"`
	Picture *string `json:"picture,omitempty"`
	Banner  *string `json:"banner,omitempty"`
	Roles   *string `json:"roles,omitempty"`
}

type ChannelDatabase struct {
	Id       int64  `json:"id"`
	ServerId int64  `json:"server_id"`
	Name     string `json:"name"`
}

type MessageDatabase struct {
	Id              int64  `json:"id"`
	SenderId        int64  `json:"sender_id"`
	ChannelId       int64  `json:"channel_id"`
	Message         string `json:"message"`
	AttachmentCount *int   `json:"attachment_count,omitempty"`
	Edited          *int64 `json:"edited,omitempty"`
}

type CreateServerReq struct {
	Name string `json:"name"`
}

// DTOs
type UserResponse struct {
	Id           int64   `json:"id"`
	Username     string  `json:"username"`
	DisplayName  string  `json:"display_name"`
	Picture      *string `json:"picture,omitempty"`
	CustomStatus *string `json:"custom_status,omitempty"`
	Online       bool    `json:"online"`
}

type Attachment struct {
	Name string `json:"name"`
	File string `json:"file"`
}

type MessageResponse struct {
	Id              int64        `json:"id"`
	SenderId        int64        `json:"sender_id"`
	ChannelId       int64        `json:"channel_id"`
	Message         string       `json:"message"`
	AttachmentCount *int         `json:"-"`
	Edited          *int64       `json:"edited,omitempty"`
	DisplayName     string       `json:"display_name"`
	Picture         *string      `json:"picture,omitempty"`
	Attachments     []Attachment `json:"attachments,omitempty"`
}
