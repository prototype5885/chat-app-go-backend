package main

import (
	"context"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

// days in miliseconds
const TokenLifetimeDays = 28
const TokenLifetimeSeconds = 60 * 60 * 24 * TokenLifetimeDays
const TokenLifetimeRedis = time.Duration(TokenLifetimeSeconds) * time.Second

func insertTokenInRedis(rdb *redis.Client, ctx context.Context, token string, userId int64) error {
	err := rdb.Set(ctx, token, userId, TokenLifetimeRedis).Err()
	return err
}

func deleteTokenFromRedis(rdb *redis.Client, ctx context.Context, token string) error {
	err := rdb.Del(ctx, token).Err()
	return err
}

func updateTokenExpInRedis(rdb *redis.Client, ctx context.Context, token string) error {
	err := rdb.Expire(ctx, token, TokenLifetimeRedis).Err()
	return err
}

func setTokenCookie(token string) http.Cookie {
	return http.Cookie{
		Name:     "token",
		Value:    token,
		Path:     "/",
		MaxAge:   TokenLifetimeSeconds,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	}
}

func deleteTokenCookie() http.Cookie {
	return http.Cookie{
		Name:     "token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	}
}
