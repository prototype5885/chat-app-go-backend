package main

import (
	"database/sql"
	"log/slog"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
)

type UserCache struct {
	displayName string
	picture     string
}

var userCache *lru.Cache[int64, *UserCache]
var userCacheMutex sync.RWMutex

func SetupCache() (err error) {
	userCache, err = lru.New[int64, *UserCache](4096 * 4)
	return
}

func UserCacheGetSet(db *sql.DB, userId int64) (*UserCache, error) {
	slog.Debug("Getting from cache")
	userCacheMutex.RLock()
	data, exists := userCache.Get(userId)
	userCacheMutex.RUnlock()

	if exists {
		return data, nil
	} else {
		err := UserCacheRefresh(db, userId)
		if err != nil {
			return nil, err
		}
		return UserCacheGetSet(db, userId)
	}
}

func UserCacheRefresh(db *sql.DB, userId int64) error {
	slog.Debug("Getting from db")
	var u UserCache
	row := db.QueryRow("SELECT display_name, picture FROM users WHERE id = ?", userId)
	err := row.Scan(&u.displayName, &u.picture)
	if err != nil {
		return err
	}

	userCacheMutex.Lock()
	userCache.Add(userId, &u)
	userCacheMutex.Unlock()

	return nil
}
