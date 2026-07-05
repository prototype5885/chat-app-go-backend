package main

import (
	"database/sql"

	lru "github.com/hashicorp/golang-lru/v2"
)

type UserCache struct {
	displayName string
	picture     string
}

var userCache *lru.Cache[int64, *UserCache]

func SetupCache() (err error) {
	userCache, err = lru.New[int64, *UserCache](4096 * 4)
	return
}

func UserCacheGetSet(db *sql.DB, userId int64) (*UserCache, error) {
	data, exists := userCache.Get(userId)
	if exists {
		return data, nil
	}

	err := UserCacheRefresh(db, userId)
	if err != nil {
		return nil, err
	}

	return UserCacheGetSet(db, userId)
}

func UserCacheRefresh(db *sql.DB, userId int64) error {
	var data UserCache

	row := db.QueryRow("SELECT display_name, picture FROM users WHERE id = ?", userId)
	err := row.Scan(&data.displayName, &data.picture)
	if err != nil {
		return err
	}

	userCache.Add(userId, &data)

	return nil
}
