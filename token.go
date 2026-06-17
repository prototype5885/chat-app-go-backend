package main

import (
	"database/sql"
	"net/http"
	"time"
)

const TokenLifetimeDays = 28
const TokenLifetimeSeconds = 60 * 60 * 24 * TokenLifetimeDays

func insertToken(db *sql.DB, token string, userId int64) error {
	expTimestamp := time.Now().Unix() + TokenLifetimeSeconds
	_, err := db.Exec("INSERT INTO tokens (token, user_id, expiration) VALUES (?, ?, ?)", token, userId, expTimestamp)
	return err
}

func getTokenData(db *sql.DB, token string) (int64, int64, error) {
	row := db.QueryRow("SELECT user_id, expiration FROM tokens WHERE token = ?", token)
	var userId int64
	var expiration int64
	err := row.Scan(&userId, &expiration)
	return userId, expiration, err
}

func deleteToken(db *sql.DB, token string) error {
	_, err := db.Exec("DELETE FROM tokens WHERE token = ?", token)
	return err
}

func updateTokenExpiration(db *sql.DB, token string) error {
	expTimestamp := time.Now().Unix() + TokenLifetimeSeconds
	_, err := db.Exec("UPDATE tokens SET expiration = ? WHERE token = ?", expTimestamp, token)
	return err
}

func setTokenCookie(value string, maxAge int) http.Cookie {
	return http.Cookie{
		Name:     "token",
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	}
}
