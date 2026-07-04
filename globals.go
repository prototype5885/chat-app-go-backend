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
