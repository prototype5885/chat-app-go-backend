package main

import (
	"sync"
)

type UserIdKeyType struct{}
type SessionIdKeyType struct{}
type ServerIdKeyType struct{}
type ChannelIdKeyType struct{}

const (
	SESSION_ID     = "session_id"
	SELF_USER_INFO = "self_user_info"
	USER_INFO      = "user_info"
	SERVER_INFO    = "server_info"
	DELETE_SERVER  = "delete_server"
	CREATE_CHANNEL = "create_channel"
	MODIFY_CHANNEL = "modify_channel"
	DELETE_CHANNEL = "delete_channel"
	CREATE_MESSAGE = "create_message"
	EDIT_MESSAGE   = "edit_message"
	DELETE_MESSAGE = "delete_message"
	TYPING         = "typing"
	USER_ONLINE    = "user_online"
)

var (
	avatarFilesMutex        sync.Mutex
	resizedAvatarFilesMutex sync.Mutex
)

const (
	SERVERS_SLICE_CAP     = 25
	CHANNELS_SLICE_CAP    = 10
	MESSAGES_SLICE_CAP    = 100
	ATTACHMENTS_SLICE_CAP = 5
	MEMBERS_SLICE_CAP     = 25
)
