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
	eventBus  chan []byte
}

// type Room struct {
// 	mutex    sync.RWMutex
// 	sessions map[int64]struct{}
// }

type SessionManager struct {
	mutex       sync.RWMutex
	sessions    map[int64]Session
	onlineUsers map[int64]struct{}
	rooms       map[int64]map[int64]struct{}
	idGen       *snowflake.Node
	db          *sql.DB
}

func NewSessionManager(idGen *snowflake.Node, db *sql.DB) *SessionManager {
	return &SessionManager{
		sessions:    make(map[int64]Session),
		onlineUsers: make(map[int64]struct{}),
		rooms:       make(map[int64]map[int64]struct{}),

		idGen: idGen,
		db:    db,
	}
}

func (sm *SessionManager) NewSession(userId int64) (Session, int64) {
	sm.mutex.Lock()

	sessionId := sm.idGen.Generate().Int64()

	session := Session{
		userId:   userId,
		eventBus: make(chan []byte, 32),
	}

	// add to sessions
	sm.sessions[sessionId] = session

	// subscribe for events sent to own user ID
	sm.enterRoom(sessionId, 0, userId)

	// add to onlineUsers
	_, exists := sm.onlineUsers[userId]
	if !exists {
		sm.onlineUsers[userId] = struct{}{}
		// TODO emit about online
	}
	sm.mutex.Unlock()

	slog.Debug(fmt.Sprintf("New session %d for user ID %d", sessionId, userId))
	return session, sessionId
}

func (sm *SessionManager) RemoveSession(sessionId int64) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	session, exists := sm.sessions[sessionId]
	if !exists {
		slog.Error(fmt.Sprintf("Session ID %d is supposed to be in sessions, but wasn't", sessionId))
		return
	}

	// close event bus chan
	close(session.eventBus)

	// remove from rooms
	if session.serverId != 0 {
		sm.leaveRoom(sessionId, session.serverId)
	}
	if session.channelId != 0 {
		sm.leaveRoom(sessionId, session.channelId)
	}

	userId := session.userId
	if userId == 0 {
		// this isn't supposed to happen as user ID is set in AddSession
		panic(fmt.Sprintf("Session ID %d is supposed to have an user ID assigned, but there wasn't", sessionId))
	}

	// remove from sessions
	delete(sm.sessions, sessionId)

	// remove session from room assigned to user ID
	sm.leaveRoom(sessionId, userId)

	if len(sm.rooms[userId]) < 1 {
		delete(sm.onlineUsers, userId)
		// TODO emit about user going offline
	}

	slog.Debug(fmt.Sprintf("Removed session %d of user ID %d", sessionId, userId))
}

func (sm *SessionManager) GetSession(sessionId int64) (Session, bool) {
	sm.mutex.RLock()
	session, exists := sm.sessions[sessionId]
	sm.mutex.RUnlock()

	return session, exists
}

func (sm *SessionManager) SetServer(sessionId int64, newServerId int64) {
	sm.mutex.Lock()

	session := sm.sessions[sessionId]
	sm.enterRoom(sessionId, session.serverId, newServerId)

	// update session with new server ID
	session.serverId = newServerId
	sm.sessions[sessionId] = session

	sm.mutex.Unlock()
}

func (sm *SessionManager) SetChannel(sessionId int64, newChannelId int64) {
	sm.mutex.Lock()

	session := sm.sessions[sessionId]
	sm.enterRoom(sessionId, session.channelId, newChannelId)

	// update session with new channel ID
	session.channelId = newChannelId
	sm.sessions[sessionId] = session

	sm.mutex.Unlock()
}

// only to be called from a function with mutex
func (sm *SessionManager) leaveRoom(sessionId int64, roomId int64) {
	_, exists := sm.rooms[roomId]
	if exists {
		delete(sm.rooms[roomId], sessionId)
		slog.Debug(fmt.Sprintf("Session ID %d left room ID %d", sessionId, roomId))
		if len(sm.rooms[roomId]) == 0 {
			delete(sm.rooms, roomId)
			slog.Debug(fmt.Sprintf("Room ID %d has been deleted due to being empty", roomId))
		}
	}
}

// only to be called from a function with mutex
func (sm *SessionManager) enterRoom(sessionId int64, prevRoomId int64, newRoomId int64) {
	if prevRoomId != 0 { // leave from previous room
		sm.leaveRoom(sessionId, prevRoomId)
	}
	{ // enter new room
		_, exists := sm.rooms[newRoomId]
		if !exists {
			sm.rooms[newRoomId] = make(map[int64]struct{})
		}
		sm.rooms[newRoomId][sessionId] = struct{}{}
		slog.Debug(fmt.Sprintf("Session ID %d entered room ID %d", sessionId, newRoomId))
	}
}

// only to be called from a function with mutex
func (sm *SessionManager) emit(msg []byte, roomId int64) {
	listeners, exists := sm.rooms[roomId]
	if !exists {
		return
	}

	for sessionId := range listeners {
		select {
		case sm.sessions[sessionId].eventBus <- msg:
		default:
			slog.Warn(fmt.Sprintf("dropping message for session %d, it's chan buffer is full", sessionId))
		}
	}
}

func (sm *SessionManager) EmitToRoom(msg []byte, roomId int64) {
	sm.mutex.RLock()
	sm.emit(msg, roomId)
	sm.mutex.RUnlock()
}

func (sm *SessionManager) EmitToServersUserIsIn(msg []byte, userId int64) error {
	serverIds, err := getServersIdsFromDatabase(sm.db, userId)
	if err != nil {
		return err
	}

	sm.mutex.RLock()
	for i := range serverIds {
		sm.emit(msg, serverIds[i])
	}
	sm.mutex.RUnlock()

	return nil
}

func (sm *SessionManager) EmitToServerMembers(msg []byte, serverId int64) error {
	userIds, err := getMemberIdsFromDatabase(sm.db, sm, serverId)
	if err != nil {
		return err
	}

	sm.mutex.RLock()
	for i := range userIds {
		sm.emit(msg, userIds[i])
	}
	sm.mutex.RUnlock()

	return nil
}
