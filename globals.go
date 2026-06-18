package main

import "sync"

type UserIdKeyType struct{}
type ServerIdKeyType struct{}
type ChannelIdKeyType struct{}

var (
	avatarFilesMutex        sync.Mutex
	resizedAvatarFilesMutex sync.Mutex
)
