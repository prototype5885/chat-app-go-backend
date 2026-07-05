package main

import (
	"chatapp/internal/cache"
	"database/sql"
	"net/http"
	"time"
)

const TokenLifetimeDays = 28
const TokenLifetimeSeconds = 60 * 60 * 24 * TokenLifetimeDays

func insertToken(db *sql.DB, token string, userId int64) error {
	expTimestamp := time.Now().Unix() + TokenLifetimeSeconds
	_, err := db.Exec("INSERT INTO tokens (token, user_id, expiration) VALUES (?, ?, ?)", token, userId, expTimestamp)
	cache.TokenUpdate(token, cache.TokenCache{UserId: userId, Expiration: expTimestamp})
	return err
}

func getTokenData(db *sql.DB, token string) (int64, int64, error) {
	tokenCache, err := cache.TokenGetSet(db, token)
	if err != nil {
		return 0, 0, err
	}
	return tokenCache.UserId, tokenCache.Expiration, err
}

func deleteToken(db *sql.DB, token string) error {
	_, err := db.Exec("DELETE FROM tokens WHERE token = ?", token)
	return err
}

func updateTokenExpiration(db *sql.DB, token string, userId int64) error {
	expTimestamp := time.Now().Unix() + TokenLifetimeSeconds
	_, err := db.Exec("UPDATE tokens SET expiration = ? WHERE token = ?", expTimestamp, token)
	cache.TokenUpdate(token, cache.TokenCache{UserId: userId, Expiration: expTimestamp})
	return err
}

func setTokenCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     "token",
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	}
}
