package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/redis/go-redis/v9"
)

func (env *Handler) AuthUserMw(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCookie, err := r.Cookie("token")
		if err != nil {
			http.Error(w, "No token", http.StatusUnauthorized)
			return
		}
		token := tokenCookie.Value

		// get user ID from redis
		userIdStr, err := env.rdb.Get(r.Context(), token).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				deleteTokenCookie := deleteTokenCookie()
				http.SetCookie(w, &deleteTokenCookie)
				http.Error(w, "Token has expired or is invalid", http.StatusUnauthorized)
			} else if errors.Is(err, context.Canceled) {
			} else {
				macrosInternalServerError(w, err)
			}
			return
		}
		userId, err := strconv.ParseInt(userIdStr, 10, 64)
		if err != nil {
			macrosInternalServerError(w, err)
			return
		}

		// check if user is banned
		{
			var banned bool
			row := env.db.QueryRow(r.Context(),
				"SELECT banned FROM users WHERE id = $1", userId)
			err := row.Scan(&banned)
			if err != nil {
				switch {
				case errors.Is(err, context.Canceled):
					break
				default:
					macrosInternalServerError(w, err)
				}
				return
			}
			if banned {
				// set delete token cookie header
				http.Error(w, "User is banned", http.StatusUnauthorized)
				return
			}
		}

		// {
		// 	updateTokenExpInRedis(env.rdb, r.Context(), token)
		// 	tokenCookie := setTokenCookie(token)
		// 	http.SetCookie(w, &tokenCookie)
		// }

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), UserIdKeyType{}, userId)))
	})
}

func (env *Handler) IsServerOwnerMw(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

		serverIdStr := r.PathValue("serverId")
		if serverIdStr == "" {
			http.Error(w, "Missing server ID parameter", http.StatusBadRequest)
			return
		}
		serverId, err := strconv.ParseInt(serverIdStr, 10, 64)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		q := `SELECT EXISTS(SELECT 1 FROM servers WHERE id = $1 AND owner_id = $2) as result`
		row := env.db.QueryRow(r.Context(), q, serverId, userId)

		var isOwner bool
		err = row.Scan(&isOwner)
		if err != nil {
			switch {
			case errors.Is(err, context.Canceled):
				break
			default:
				macrosInternalServerError(w, err)
			}
			return
		}

		if isOwner == false {
			text := fmt.Sprintf("You don't own server ID %d", serverId)
			http.Error(w, text, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ServerIdKeyType{}, serverId)))
	})
}

func (env *Handler) HasServerAccessMw(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

		serverIdStr := r.PathValue("serverId")
		if serverIdStr == "" {
			http.Error(w, "Missing server ID parameter", http.StatusBadRequest)
			return
		}
		serverId, err := strconv.ParseInt(serverIdStr, 10, 64)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		q := `
		SELECT EXISTS (
			SELECT 1 FROM server_members WHERE server_id = $1 AND member_id = $2
			UNION
			SELECT 1 FROM servers WHERE id = $1 AND owner_id = $2
		) as result`

		row := env.db.QueryRow(r.Context(), q, serverId, userId)

		var hasAccess bool
		err = row.Scan(&hasAccess)
		if err != nil {
			switch {
			case errors.Is(err, context.Canceled):
				break
			default:
				macrosInternalServerError(w, err)
			}
			return
		}

		if hasAccess == false {
			text := fmt.Sprintf("You have no access to server ID %d", serverId)
			http.Error(w, text, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ServerIdKeyType{}, serverId)))
	})
}

func (env *Handler) IsChannelOwnerMw(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

		channelIdStr := r.PathValue("channelId")
		if channelIdStr == "" {
			http.Error(w, "Missing channel ID parameter", http.StatusBadRequest)
			return
		}
		channelId, err := strconv.ParseInt(channelIdStr, 10, 64)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		q := `
		SELECT EXISTS (
			SELECT 1 FROM channels
			JOIN servers ON channels.server_id = servers.id
			WHERE channels.id = $1 AND servers.owner_id = $2
		) as result`
		row := env.db.QueryRow(r.Context(), q, channelId, userId)

		var isOwner bool
		err = row.Scan(&isOwner)
		if err != nil {
			switch {
			case errors.Is(err, context.Canceled):
				break
			default:
				macrosInternalServerError(w, err)
			}
			return
		}

		if isOwner == false {
			text := fmt.Sprintf("You don't own channel ID %d", channelId)
			http.Error(w, text, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ChannelIdKeyType{}, channelId)))
	})
}

func (env *Handler) HasChannelAccessMw(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

		channelIdStr := r.PathValue("channelId")
		if channelIdStr == "" {
			http.Error(w, "Missing channel ID parameter", http.StatusBadRequest)
			return
		}
		channelId, err := strconv.ParseInt(channelIdStr, 10, 64)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		q := `
		SELECT EXISTS (
			SELECT 1 FROM channels c
			JOIN servers s ON c.server_id = s.id
			LEFT JOIN server_members m ON s.id = m.server_id AND m.member_id = $2
			WHERE c.id = $1
			AND (s.owner_id = $2 OR m.member_id IS NOT NULL)
		) as result`
		row := env.db.QueryRow(r.Context(), q, channelId, userId)

		var hasAccess bool
		err = row.Scan(&hasAccess)
		if err != nil {
			switch {
			case errors.Is(err, context.Canceled):
				break
			default:
				macrosInternalServerError(w, err)
			}
			return
		}

		if hasAccess == false {
			text := fmt.Sprintf("You have no access to channel ID %d", channelId)
			http.Error(w, text, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ChannelIdKeyType{}, channelId)))
	})
}

func SetHeaderMw(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("XDD", "lol")
		next.ServeHTTP(w, r)
	})
}
