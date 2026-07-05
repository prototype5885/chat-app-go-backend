package cache

import (
	"database/sql"
	"errors"

	lru "github.com/hashicorp/golang-lru/v2"
)

type TokenCache struct {
	UserId     int64
	Expiration int64
}

var tokenCache, _ = lru.New[string, *TokenCache](defaultCacheSize)

func TokenGetSet(db *sql.DB, token string) (*TokenCache, error) {
	data, exists := tokenCache.Get(token)
	if exists {
		return data, nil
	}

	err := TokenRefresh(db, token)
	if err != nil {
		return nil, err
	}

	data, exists = tokenCache.Get(token)
	if !exists {
		return nil, errors.New("token cache missing after refresh")
	}
	return data, nil
}

func TokenRefresh(db *sql.DB, token string) error {
	var data TokenCache

	row := db.QueryRow("SELECT user_id, expiration FROM tokens WHERE token = ?", token)
	err := row.Scan(&data.UserId, &data.Expiration)
	if err != nil {
		return err
	}

	tokenCache.Add(token, &data)

	return nil
}

func TokenUpdate(token string, data TokenCache) {
	tokenCache.Add(token, &data)
}
