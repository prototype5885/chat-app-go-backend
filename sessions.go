package main

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type SessionManager struct {
	mutex sync.RWMutex
	// sessions      map[string]Session
	// onlineTracker map[string]map[string]struct{} // userID -> set of sessionIDs
	subs map[int64]map[int64]struct{}
	// sessionChans  map[string]chan EventMessage   // sessionID -> Go channel (replaces eventBus)

	db  *pgxpool.Pool
	rdb *redis.Client
	// redisPubSub *redis.PubSub
	ctx context.Context
}

func (sm *SessionManager) Subscribe(sessionId int64, roomId int64) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.subs[roomId][sessionId] = struct{}{}
}

func (sm *SessionManager) Unsubscribe(sessionId int64, roomId int64) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	delete(sm.subs[roomId], sessionId)
}

func (sm *SessionManager) IsSubbedTo(sessionId int64, roomId int64) bool {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	sessionsInRoom, roomExists := sm.subs[roomId]
	if !roomExists {
		return false
	}

	_, sessionExists := sessionsInRoom[sessionId]
	return sessionExists
}
