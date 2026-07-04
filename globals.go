package main

import (
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
	size := len("data: \n\n") + len(sseMsg.data)
	if sseMsg.event != "" {
		size += len("event: \n") + len(sseMsg.event)
	}

	buf := make([]byte, 0, size)

	if sseMsg.event != "" {
		buf = append(buf, "event: "...)
		buf = append(buf, sseMsg.event...)
		buf = append(buf, '\n')
	}
	buf = append(buf, "data: "...)
	buf = append(buf, sseMsg.data...)
	buf = append(buf, '\n', '\n')

	return buf
}

var (
	avatarFilesMutex        sync.Mutex
	resizedAvatarFilesMutex sync.Mutex
)
