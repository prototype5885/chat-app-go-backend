package main

import "sync"

const MaxAttachmentCount = 4

const snowflakeNodeKeyprefix = "snowflake:node"

type UserIdKeyType struct{}
type ServerIdKeyType struct{}
type ChannelIdKeyType struct{}

var (
	avatarFilesMutex        sync.Mutex
	resizedAvatarFilesMutex sync.Mutex
	attachmentFilesMutex    sync.Mutex
)
