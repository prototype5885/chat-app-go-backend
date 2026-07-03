package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync"

	"github.com/bwmarrin/snowflake"
)

type Session struct {
	userId    int64
	serverId  int64
	channelId int64
}

type SessionManager struct {
	mutex             sync.RWMutex
	sessions          map[int64]Session
	sessionsPerUserId map[int64][]int64
	onlineUsers       map[int64]struct{}
	// subs              map[int64]map[int64]struct{}

	idGen *snowflake.Node
	db    *sql.DB
}

func NewSessionManager(idGen *snowflake.Node, db *sql.DB) *SessionManager {
	return &SessionManager{
		sessions:          make(map[int64]Session),
		sessionsPerUserId: make(map[int64][]int64),
		onlineUsers:       make(map[int64]struct{}),
		// subs:              make(map[int64]map[int64]struct{}),
		idGen: idGen,
		db:    db,
	}
}

func (sm *SessionManager) NewSession(userId int64) int64 {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sessionId := sm.idGen.Generate().Int64()

	// add to sessions
	sm.sessions[sessionId] = Session{userId: userId}

	// add to sessionsPerUserId
	sm.sessionsPerUserId[userId] = append(sm.sessionsPerUserId[userId], sessionId)

	// add to onlineUsers
	_, exists := sm.onlineUsers[userId]
	if !exists {
		sm.onlineUsers[userId] = struct{}{}
		// TODO emit about online
	}

	slog.Debug(fmt.Sprintf("New session %d for user ID %d", sessionId, userId))

	return sessionId
}

func (sm *SessionManager) RemoveSession(sessionId int64) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	session, exists := sm.sessions[sessionId]
	if !exists {
		slog.Error(fmt.Sprintf("Session ID %d is supposed to be in sessions, but wasn't", sessionId))
		return
	}

	userId := session.userId
	if userId == 0 {
		// this isn't supposed to happen as user ID is set in AddSession
		panic(fmt.Sprintf("Session ID %d is supposed to have an user ID assigned, but there wasn't", sessionId))
	}

	// remove from sessions
	delete(sm.sessions, sessionId)

	// remove from sessionsPerUserId
	sessionsLen := len(sm.sessionsPerUserId[userId])
	if sessionsLen > 1 {
		for i := range sessionsLen {
			if sm.sessionsPerUserId[userId][i] == sessionId {
				sm.sessionsPerUserId[userId][i] = sm.sessionsPerUserId[userId][sessionsLen-1]
				sm.sessionsPerUserId[userId] = sm.sessionsPerUserId[userId][:sessionsLen-1]
				break
			}
		}
	} else {
		delete(sm.sessionsPerUserId, userId)
	}

	// remove from onlineUsers
	if len(sm.sessionsPerUserId[userId]) < 1 {
		delete(sm.onlineUsers, userId)
		// TODO emit about user going offline
	}

	slog.Debug(fmt.Sprintf("Removed session %d for user ID %d", sessionId, userId))
}

func (sm *SessionManager) isUserOnline(userId int64) bool {
	sm.mutex.RLock()
	_, isOnline := sm.onlineUsers[userId]
	sm.mutex.RUnlock()

	return isOnline
}

// func (sm *SessionManager) Subscribe(sessionId int64, roomId int64) {
// 	sm.mutex.Lock()
// 	sm.subs[roomId][sessionId] = struct{}{}
// 	sm.mutex.Unlock()
// }

// func (sm *SessionManager) Unsubscribe(sessionId int64, roomId int64) {
// 	sm.mutex.Lock()
// 	delete(sm.subs[roomId], sessionId)
// 	sm.mutex.Unlock()
// }

// func (sm *SessionManager) IsSubbedTo(sessionId int64, roomId int64) bool {
// 	sm.mutex.RLock()
// 	defer sm.mutex.RUnlock()

// 	sessionsInRoom, roomExists := sm.subs[roomId]
// 	if !roomExists {
// 		return false
// 	}

// 	_, sessionExists := sessionsInRoom[sessionId]
// 	return sessionExists
// }
