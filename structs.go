package main

import (
	"context"

	"github.com/bwmarrin/snowflake"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

type Handler struct {
	db     *pgxpool.Pool
	rdb    *redis.Client
	nats   *nats.Conn
	idGen  *snowflake.Node
	sm     *SessionManager
	cancel context.CancelFunc
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
	ID           string  `db:"id" json:"id"`
	Username     string  `db:"username" json:"username"`
	DisplayName  string  `db:"display_name" json:"display_name"`
	Picture      *string `db:"picture" json:"picture,omitempty"`
	Banned       bool    `db:"banned" json:"banned"`
	CustomStatus *string `db:"custom_status" json:"custom_status,omitempty"`
}

type ServerDatabase struct {
	Id      string  `db:"id" json:"id"`
	OwnerID string  `db:"owner_id" json:"owner_id"`
	Name    string  `db:"name" json:"name"`
	Picture *string `db:"picture" json:"picture,omitempty"`
	Banner  *string `db:"banner" json:"banner,omitempty"`
	Roles   *string `db:"roles" json:"roles,omitempty"`
}

type ChannelDatabase struct {
	Id       string `db:"id" json:"id"`
	ServerId string `db:"server_id" json:"server_id"`
	Name     string `db:"name" json:"name"`
}

type MessageDatabase struct {
	Id              string  `db:"id" json:"id"`
	SenderId        string  `db:"sender_id" json:"sender_id"`
	ChannelId       string  `db:"channel_id" json:"channel_id"`
	Message         string  `db:"message" json:"message"`
	AttachmentCount *int    `db:"attachment_count" json:"attachment_count,omitempty"`
	Edited          *string `db:"edited" json:"edited,omitempty"`
}

type CreateServerReq struct {
	Name string `json:"name"`
}

// DTOs
type UserResponse struct {
	ID           string  `db:"id" json:"id"`
	Username     string  `db:"username" json:"username"`
	DisplayName  string  `db:"display_name" json:"display_name"`
	Picture      *string `db:"picture" json:"picture,omitempty"`
	CustomStatus *string `db:"custom_status" json:"custom_status,omitempty"`
	Online       bool    `json:"online"`
}

type Attachment struct {
	Name string `db:"name" json:"name"`
	File string `db:"file" json:"file"`
}

type MessageResponse struct {
	Id              string       `db:"id" json:"id"`
	SenderId        string       `db:"sender_id" json:"sender_id"`
	ChannelId       string       `db:"channel_id" json:"channel_id"`
	Message         string       `db:"message" json:"message"`
	AttachmentCount *int         `db:"attachment_count" json:"-"`
	Edited          *string      `db:"edited" json:"edited,omitempty"`
	DisplayName     string       `db:"display_name" json:"display_name"`
	Picture         *string      `db:"picture" json:"picture,omitempty"`
	Attachments     []Attachment `db:"attachments" json:"attachments"`
}
