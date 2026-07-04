package main

import (
	"fmt"
	"sync"
)

type UserIdKeyType struct{}
type SessionIdKeyType struct{}
type ServerIdKeyType struct{}
type ChannelIdKeyType struct{}

type SseMessage struct {
	event string
	data  string
}

const (
	SESSION_ID     = "session_id"
	SELF_USER_INFO = "self_user_info"
	USER_INFO      = "user_info"
	SERVER_INFO    = "server_info"
	DELETE_SERVER  = "delete_server"
	MODIFY_CHANNEL = "modify_channel"
	CREATE_MESSAGE = "create_message"
)

func (sseMsg *SseMessage) Encode() []byte {
	var msg []byte
	if sseMsg.event != "" {
		msg = fmt.Appendf(msg, "event: %s\n", sseMsg.event)
	}
	msg = fmt.Appendf(msg, "data: %s\n\n", sseMsg.data)
	return msg
}

var (
	avatarFilesMutex        sync.Mutex
	resizedAvatarFilesMutex sync.Mutex
)
