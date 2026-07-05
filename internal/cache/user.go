package cache

import (
	"database/sql"

	lru "github.com/hashicorp/golang-lru/v2"
)

type UserCache struct {
	DisplayName string
	Picture     string
}

var userCache, _ = lru.New[int64, *UserCache](defaultCacheSize)

func UserGetSet(db *sql.DB, userId int64) (*UserCache, error) {
	data, exists := userCache.Get(userId)
	if exists {
		return data, nil
	}

	err := UserRefresh(db, userId)
	if err != nil {
		return nil, err
	}

	return UserGetSet(db, userId)
}

func UserRefresh(db *sql.DB, userId int64) error {
	var data UserCache

	row := db.QueryRow("SELECT display_name, picture FROM users WHERE id = ?", userId)
	err := row.Scan(&data.DisplayName, &data.Picture)
	if err != nil {
		return err
	}

	userCache.Add(userId, &data)

	return nil
}
