package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (env *Handler) AuthUserMw(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCookie, err := r.Cookie("token")
		if err != nil {
			if errors.Is(err, http.ErrNoCookie) {
				http.Error(w, "No token", http.StatusUnauthorized)
			} else {
				macrosInternalServerError(w, err)
			}
			return
		}

		userId, expiration, err := getTokenData(env.dbTokens, tokenCookie.Value)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				cookie := setTokenCookie("", -1)
				http.SetCookie(w, &cookie)
				http.Error(w, "Token not found", http.StatusUnauthorized)
			} else {
				macrosInternalServerError(w, err)
			}
			return
		}

		// check if user is banned
		{
			var banned bool
			row := env.db.QueryRow(
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

		// handle expiration
		var secondsUntilExp int64 = expiration - time.Now().Unix()
		if secondsUntilExp < 0 { // minus means its past expiration
			cookie := setTokenCookie("", -1)
			http.SetCookie(w, &cookie)
			http.Error(w, "Token has expired", http.StatusUnauthorized)
			return
		} else if secondsUntilExp < (60*60*24)*(TokenLifetimeDays-1) { // if it's been at least 1 day since token exp was updated
			err := updateTokenExpiration(env.dbTokens, tokenCookie.Value)
			if err != nil {
				macrosInternalServerError(w, err)
				return
			}
			cookie := setTokenCookie(tokenCookie.Value, TokenLifetimeSeconds)
			http.SetCookie(w, &cookie)
		}

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
		row := env.db.QueryRow(q, serverId, userId)

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

		row := env.db.QueryRow(q, serverId, userId)

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
		row := env.db.QueryRow(q, channelId, userId)

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
		row := env.db.QueryRow(q, channelId, userId)

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
