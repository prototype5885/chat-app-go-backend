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
				unexpectedErrorResponse(w, err)
			}
			return
		}

		userId, expiration, err := getTokenData(env.db, tokenCookie.Value)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				cookie := setTokenCookie("", -1)
				http.SetCookie(w, cookie)
				http.Error(w, "Token not found", http.StatusUnauthorized)
			} else {
				unexpectedErrorResponse(w, err)
			}
			return
		}

		// check if user is banned
		{
			var banned bool
			row := env.db.QueryRow("SELECT banned FROM users WHERE id = ?", userId)
			err := row.Scan(&banned)
			if err != nil {
				unexpectedErrorResponse(w, err)
				return
			}
			if banned {
				// set delete token cookie header
				http.Error(w, "User is banned", http.StatusUnauthorized)
				return
			}
		}

		// handle expiration
		secondsUntilExp := expiration - time.Now().Unix()
		if secondsUntilExp < 0 { // minus means its past expiration
			cookie := setTokenCookie("", -1)
			http.SetCookie(w, cookie)
			http.Error(w, "Token has expired", http.StatusUnauthorized)
			return
		} else if secondsUntilExp < (60*60*24)*(TokenLifetimeDays-1) { // if it's been at least 1 day since token exp was updated
			err := updateTokenExpiration(env.db, tokenCookie.Value)
			if err != nil {
				unexpectedErrorResponse(w, err)
				return
			}
			cookie := setTokenCookie(tokenCookie.Value, TokenLifetimeSeconds)
			http.SetCookie(w, cookie)
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), UserIdKeyType{}, userId)))
	})
}

func (env *Handler) IsServerOwnerMw(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

		serverIdStr := r.PathValue("serverId")
		if serverIdStr == "" {
			http.Error(w, "Missing server ID parameter", 400)
			return
		}
		serverId, err := strconv.ParseInt(serverIdStr, 10, 64)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		q := `SELECT EXISTS(SELECT 1 FROM servers WHERE id = ? AND owner_id = ?) as result`
		row := env.db.QueryRow(q, serverId, userId)

		var isOwner bool
		err = row.Scan(&isOwner)
		if err != nil {
			unexpectedErrorResponse(w, err)
			return
		}

		if !isOwner {
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
			http.Error(w, "Missing server ID parameter", 400)
			return
		}
		serverId, err := strconv.ParseInt(serverIdStr, 10, 64)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		q := `
		SELECT EXISTS (
			SELECT 1 FROM server_members WHERE server_id = ? AND member_id = ?
			UNION
			SELECT 1 FROM servers WHERE id = ? AND owner_id = ?
		) as result`

		row := env.db.QueryRow(q, serverId, userId, serverId, userId)

		var hasAccess bool
		err = row.Scan(&hasAccess)
		if err != nil {
			unexpectedErrorResponse(w, err)
			return
		}

		if !hasAccess {
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
			http.Error(w, "Missing channel ID parameter", 400)
			return
		}
		channelId, err := strconv.ParseInt(channelIdStr, 10, 64)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		q := `
			SELECT server_id FROM channels
			JOIN servers ON channels.server_id = servers.id
			WHERE channels.id = ? AND servers.owner_id = ?
		`
		row := env.db.QueryRow(q, channelId, userId)

		var serverId int64
		err = row.Scan(&serverId)
		if err != nil {
			unexpectedErrorResponse(w, err)
			return
		}

		if serverId == 0 {
			text := fmt.Sprintf("You don't own channel ID %d", channelId)
			http.Error(w, text, http.StatusForbidden)
			return
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, ChannelIdKeyType{}, channelId)
		ctx = context.WithValue(ctx, ServerIdKeyType{}, serverId)
		rNew := r.WithContext(ctx)

		next.ServeHTTP(w, rNew)
	})
}

func (env *Handler) HasChannelAccessMw(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

		channelIdStr := r.PathValue("channelId")
		if channelIdStr == "" {
			http.Error(w, "Missing channel ID parameter", 400)
			return
		}
		channelId, err := strconv.ParseInt(channelIdStr, 10, 64)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		q := `
		SELECT EXISTS (
			SELECT 1 FROM channels c
			JOIN servers s ON c.server_id = s.id
			LEFT JOIN server_members m ON s.id = m.server_id AND m.member_id = ?
			WHERE c.id = ?
			AND (s.owner_id = ? OR m.member_id IS NOT NULL)
		) as result`
		row := env.db.QueryRow(q, userId, channelId, userId)

		var hasAccess bool
		err = row.Scan(&hasAccess)
		if err != nil {
			unexpectedErrorResponse(w, err)
			return
		}

		if !hasAccess {
			text := fmt.Sprintf("You have no access to channel ID %d", channelId)
			http.Error(w, text, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ChannelIdKeyType{}, channelId)))
	})
}

//func SetHeaderMw(next http.Handler) http.Handler {
//	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//		w.Header().Set("XDD", "lol")
//		next.ServeHTTP(w, r)
//	})
//}
