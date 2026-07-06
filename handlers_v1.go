package main

import (
	"chatapp/internal/cache"
	"chatapp/modules/validator"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"

	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
)

func (env *Handler) test(w http.ResponseWriter, _ *http.Request) {
	textResponse(w, "Hello go!", http.StatusOK)
}

func (env *Handler) testAuth(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	textResponse(w, fmt.Sprintf("Hello %d!", userId), http.StatusOK)
}

func (env *Handler) session(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		unexpectedErrorResponse(w, fmt.Errorf("flusher is not ok"))
		return
	}

	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	session, sessionId := env.sm.NewSession(userId)
	defer env.sm.RemoveSession(sessionId)
	defer slog.Debug(fmt.Sprintf("Finished SSE for session ID %d", sessionId))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// send initial session id
	msg := encodeServerSentEvent(SESSION_ID, []byte(strconv.FormatInt(sessionId, 10)))
	_, err := w.Write(msg)
	if err != nil {
		slog.Warn(err.Error())
		return
	}
	flusher.Flush()

	for {
		select {
		case msg, ok := <-session.eventBus:
			if !ok {
				return
			}
			_, err := w.Write(msg)
			if err != nil {
				slog.Warn(err.Error())
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (env *Handler) register(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		slog.Warn(err.Error())
		http.Error(w, "Couldn't parse form", http.StatusBadRequest)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	issues := validator.MergeValidationIssues(
		validator.UsernameSchema.Validate(username, false),
		validator.PasswordSchema.Validate(password, false),
	)
	if len(issues) != 0 {
		jsonResponseStruct(w, issues, http.StatusBadRequest)
		return
	}

	hashedPassword, err := argon2id.CreateHash(password, &argon2id.Params{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 4,
		SaltLength:  16,
		KeyLength:   32,
	})
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	_, err = env.db.Exec(`
		INSERT INTO users (id, username, display_name, password)
		VALUES (?, ?, ?, ?)`,
		env.idGen.Generate().Int64(), username, username, hashedPassword,
	)
	if err != nil {
		if isDuplicateError(env.db, err) {
			http.Error(w, "User with same username already exists", http.StatusConflict)
		} else {
			unexpectedErrorResponse(w, err)
		}
		return
	}
}

func (env *Handler) login(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		slog.Warn(err.Error())
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	issues := validator.MergeValidationIssues(
		validator.UsernameSchema.Validate(username, false),
		validator.PasswordSchema.Validate(password, false),
	)
	if len(issues) != 0 {
		jsonResponseStruct(w, issues, http.StatusBadRequest)
		return
	}

	badLogin := func() {
		time.Sleep(time.Duration(rand.Intn(2000)) * time.Millisecond)
		http.Error(w, "Bad login", http.StatusUnauthorized)
	}

	type UserRecord struct {
		Id       int64
		Password string
	}

	row := env.db.QueryRow(
		"SELECT id, password FROM users WHERE username = ?", username,
	)

	var record UserRecord
	err = row.Scan(&record.Id, &record.Password)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			badLogin()
		default:
			unexpectedErrorResponse(w, err)
		}
		return
	}

	match, err := argon2id.ComparePasswordAndHash(password, record.Password)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}
	if !match {
		badLogin()
		return
	}

	hash := mustRandomHash(32)
	token := base64.RawURLEncoding.EncodeToString(hash)
	err = insertToken(env.db, token, record.Id)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	tokenCookie := setTokenCookie(token, TokenLifetimeSeconds)
	http.SetCookie(w, tokenCookie)
}

func (env *Handler) logout(w http.ResponseWriter, r *http.Request) {
	token, err := r.Cookie("token")
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	err = deleteToken(env.db, token.Value)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	deleteTokenCookie := setTokenCookie("", -1)
	http.SetCookie(w, deleteTokenCookie)
}

func (env *Handler) delete(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	_, err := env.db.Exec("DELETE FROM users WHERE id = ?", userId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}
}

func (env *Handler) getUserID(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	textResponse(w, fmt.Sprintf("%d", userId), http.StatusOK)
}

func (env *Handler) getUserInfo(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	const q = "SELECT id, username, display_name, picture, custom_status FROM users WHERE id = ?"
	row := env.db.QueryRow(q, userId)

	var user UserResponse
	err := row.Scan(&user.Id, &user.Username, &user.DisplayName, &user.Picture, &user.CustomStatus)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	user.Online = true
	jsonResponseStruct(w, user, http.StatusOK)
}

func (env *Handler) updateUserInfo(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	err := r.ParseForm()
	if err != nil {
		slog.Warn(err.Error())
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	displayName := strings.TrimSpace(r.FormValue("display_name"))

	issues := validator.MergeValidationIssues(
		validator.DisplaynameSchema.Validate(displayName, true),
	)
	if len(issues) != 0 {
		jsonResponseStruct(w, issues, http.StatusBadRequest)
		return
	}

	tx, err := env.db.Begin()
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}
	defer rollbackTx(tx)

	{
		if displayName != "" {
			_, err := tx.Exec("UPDATE users SET display_name = ? WHERE id = ?", displayName, userId)
			if err != nil {
				unexpectedErrorResponse(w, err)
				return
			}
		}
	}

	err = tx.Commit()
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	err = cache.UserRefresh(env.db, userId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	type ResponseData struct {
		Id          int64  `json:"id"`
		DisplayName string `json:"display_name,omitempty"`
	}

	responseData := ResponseData{
		Id:          userId,
		DisplayName: displayName,
	}

	responseJson, err := json.Marshal(responseData)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	env.sm.EmitToRoom(SELF_USER_INFO, responseJson, userId)
	env.sm.EmitToServersUserIsIn(USER_INFO, responseJson, userId)

	jsonResponse(w, responseJson, http.StatusOK)
}

func (env *Handler) uploadUserAvatar(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	file, _, err := r.FormFile("file")
	if err != nil {
		slog.Warn(err.Error())

		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "Uploaded avatar is larger than 1 mb", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Invalid uploaded file", http.StatusBadRequest)
		}
		return
	}

	defer func() {
		err := file.Close()
		if err != nil {
			slog.Error(err.Error())
		}
	}()

	fileName, err := saveAvatar(file)
	if err != nil {
		_, isImageFormatError := errors.AsType[*ImageFormatError](err)
		if isImageFormatError {
			http.Error(w, "Uploaded picture format isn't supported", http.StatusBadRequest)
		} else {
			unexpectedErrorResponse(w, err)
		}
		return
	}

	_, err = env.db.Exec("UPDATE users SET picture = ? WHERE id = ?", fileName, userId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	err = cache.UserRefresh(env.db, userId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	type UserAvatarResponse struct {
		Id      int64  `json:"id"`
		Picture string `json:"picture"`
	}

	avatarResponse := UserAvatarResponse{Id: userId, Picture: fileName}

	avatarResponseJson, err := json.Marshal(avatarResponse)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	env.sm.EmitToRoom(SELF_USER_INFO, avatarResponseJson, userId)
	env.sm.EmitToServersUserIsIn(USER_INFO, avatarResponseJson, userId)

	textResponse(w, fileName, http.StatusOK)
}

func (env *Handler) createServer(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	type Payload struct {
		Name string `json:"name"`
	}

	var p Payload
	err := json.NewDecoder(r.Body).Decode(&p)
	if err != nil {
		slog.Warn(err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.Name = strings.TrimSpace(p.Name)

	issues := validator.MergeValidationIssues(
		validator.ServerNameSchema.Validate(p.Name, false),
	)
	if len(issues) != 0 {
		jsonResponseStruct(w, issues, http.StatusBadRequest)
		return
	}

	serverId := env.idGen.Generate().Int64()
	channelId := env.idGen.Generate().Int64()

	tx, err := env.db.Begin()
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}
	defer rollbackTx(tx)

	_, err = tx.Exec(
		"INSERT INTO servers (id, owner_id, name) VALUES (?, ?, ?)",
		serverId, userId, p.Name,
	)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	_, err = tx.Exec(
		"INSERT INTO channels (id, server_id, name) VALUES (?, ?, ?)",
		channelId, serverId, "Default channel",
	)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	err = tx.Commit()
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	row := env.db.QueryRow(`
		SELECT id, owner_id, name, picture, banner, roles
		FROM servers WHERE id = ?
	`, serverId)

	var server ServerDatabase
	err = row.Scan(&server.Id, &server.OwnerID, &server.Name, &server.Picture, &server.Banner, &server.Roles)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	jsonResponseStruct(w, server, http.StatusOK)
}

func (env *Handler) getServerInfo(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	serverId := env.mustGetIdFromServerContext(r, ServerIdKeyType{})

	const q = `
		SELECT
		id, owner_id, name, picture, banner, roles
	 	FROM servers WHERE id = ? AND owner_id = ?`
	row := env.db.QueryRow(q, serverId, userId)

	var s ServerDatabase
	err := row.Scan(&s.Id, &s.OwnerID, &s.Name, &s.Picture, &s.Banner, &s.Roles)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	jsonResponseStruct(w, s, http.StatusOK)
}

func (env *Handler) updateServerInfo(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	serverId := env.mustGetIdFromServerContext(r, ServerIdKeyType{})

	err := r.ParseForm()
	if err != nil {
		slog.Warn(err.Error())
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	serverName := strings.TrimSpace(r.FormValue("name"))

	issues := validator.MergeValidationIssues(
		validator.ServerNameSchema.Validate(serverName, true),
	)
	if len(issues) != 0 {
		jsonResponseStruct(w, issues, http.StatusBadRequest)
		return
	}

	tx, err := env.db.Begin()
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}
	defer rollbackTx(tx)

	if serverName != "" {
		const q = "UPDATE servers SET name = ? WHERE id = ? AND owner_id = ?"
		_, err := tx.Exec(q, serverName, serverId, userId)
		if err != nil {
			unexpectedErrorResponse(w, err)
			return
		}
	}

	err = tx.Commit()
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	const q = `
		SELECT id, owner_id, name, picture, banner, roles
		FROM servers WHERE id = ?
	`
	row := env.db.QueryRow(q, serverId)

	var s ServerDatabase
	err = row.Scan(&s.Id, &s.OwnerID, &s.Name, &s.Picture, &s.Banner, &s.Roles)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	serverJson, err := json.Marshal(s)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	env.sm.EmitToServerMembers(SERVER_INFO, serverJson, serverId)

	jsonResponse(w, serverJson, http.StatusOK)
}

func (env *Handler) uploadServerAvatar(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	serverId := env.mustGetIdFromServerContext(r, ServerIdKeyType{})

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	file, _, err := r.FormFile("file")
	if err != nil {
		slog.Warn(err.Error())

		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "Uploaded avatar is larger than 1 mb", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "Invalid uploaded file", http.StatusBadRequest)
		}
		return
	}

	defer func() {
		err := file.Close()
		if err != nil {
			slog.Error(err.Error())
		}
	}()

	fileName, err := saveAvatar(file)
	if err != nil {
		_, isImageFormatError := errors.AsType[*ImageFormatError](err)
		if isImageFormatError {
			http.Error(w, "Uploaded picture format isn't supported", http.StatusBadRequest)
		} else {
			unexpectedErrorResponse(w, err)
		}
		return
	}

	const q = "UPDATE servers SET picture = ? WHERE id = ? AND owner_id = ?"
	_, err = env.db.Exec(q, fileName, serverId, userId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	const q2 = `
		SELECT
		id, owner_id, name, picture, banner, roles
	 	FROM servers WHERE id = ? AND owner_id = ?
	`
	row := env.db.QueryRow(q2, serverId, userId)

	var s ServerDatabase
	err = row.Scan(&s.Id, &s.OwnerID, &s.Name, &s.Picture, &s.Banner, &s.Roles)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	serverJson, err := json.Marshal(s)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	env.sm.EmitToServerMembers(SERVER_INFO, serverJson, serverId)

	textResponse(w, fileName, http.StatusOK)
}

func (env *Handler) getServers(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	servers, err := getServersFromDatabase(env.db, userId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	jsonResponseStruct(w, servers, http.StatusOK)
}

func (env *Handler) deleteServer(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	serverId := env.mustGetIdFromServerContext(r, ServerIdKeyType{})

	env.sm.EmitToServerMembers(DELETE_SERVER, []byte(strconv.FormatInt(serverId, 10)), serverId)

	result, err := env.db.Exec("DELETE FROM servers WHERE id = ? AND owner_id = ?", serverId, userId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}
	rowsDeleted, err := result.RowsAffected()
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}
	if rowsDeleted != 1 {
		err := fmt.Errorf("expected to delete server ID %d in deleteServer handler, but affected rows value was %d", serverId, rowsDeleted)
		unexpectedErrorResponse(w, err)
		return
	}
}

func (env *Handler) createChannel(w http.ResponseWriter, r *http.Request) {
	serverId := env.mustGetIdFromServerContext(r, ServerIdKeyType{})

	type Payload struct {
		Name string `json:"name"`
	}

	var p Payload
	err := json.NewDecoder(r.Body).Decode(&p)
	if err != nil {
		slog.Warn(err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.Name = strings.TrimSpace(p.Name)

	issues := validator.MergeValidationIssues(
		validator.ChannelNameSchema.Validate(p.Name, false),
	)
	if len(issues) != 0 {
		jsonResponseStruct(w, issues, http.StatusBadRequest)
		return
	}

	var channelId = env.idGen.Generate().Int64()

	const q = "INSERT INTO channels (id, server_id, name) VALUES (?, ?, ?)"
	_, err = env.db.Exec(q, channelId, serverId, p.Name)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	channel := ChannelDatabase{
		Id:       channelId,
		ServerId: serverId,
		Name:     p.Name,
	}

	channelJson, err := json.Marshal(channel)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	env.sm.EmitToRoom(CREATE_CHANNEL, channelJson, serverId)

	w.WriteHeader(http.StatusAccepted)
}

func (env *Handler) getChannelInfo(w http.ResponseWriter, r *http.Request) {
	channelId := env.mustGetIdFromServerContext(r, ChannelIdKeyType{})

	const q = `
		SELECT
		id, server_id, name
	 	FROM channels WHERE id = ?`
	row := env.db.QueryRow(q, channelId)

	var c ChannelDatabase
	err := row.Scan(&c.Id, &c.ServerId, &c.Name)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	jsonResponseStruct(w, c, http.StatusOK)
}

func (env *Handler) updateChannelInfo(w http.ResponseWriter, r *http.Request) {
	channelId := env.mustGetIdFromServerContext(r, ChannelIdKeyType{})

	err := r.ParseForm()
	if err != nil {
		slog.Warn(err.Error())
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	channelName := strings.TrimSpace(r.FormValue("name"))

	issues := validator.MergeValidationIssues(
		validator.ChannelNameSchema.Validate(channelName, true),
	)
	if len(issues) != 0 {
		jsonResponseStruct(w, issues, http.StatusBadRequest)
		return
	}

	tx, err := env.db.Begin()
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}
	defer rollbackTx(tx)

	if channelName != "" {
		const q = "UPDATE channels SET name = ? WHERE id = ?"
		_, err := tx.Exec(q, channelName, channelId)
		if err != nil {
			unexpectedErrorResponse(w, err)
			return
		}
	}

	err = tx.Commit()
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	const q = "SELECT id, server_id, name FROM channels WHERE id = ?"
	row := env.db.QueryRow(q, channelId)

	var channel ChannelDatabase
	err = row.Scan(&channel.Id, &channel.ServerId, &channel.Name)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	channelJson, err := json.Marshal(channel)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	env.sm.EmitToRoom(MODIFY_CHANNEL, channelJson, channel.ServerId)

	jsonResponseStruct(w, channel, http.StatusOK)
}

func (env *Handler) getChannels(w http.ResponseWriter, r *http.Request) {
	serverId := env.mustGetIdFromServerContext(r, ServerIdKeyType{})
	sessionId := env.mustGetIdFromServerContext(r, SessionIdKeyType{})

	channels, err := getChannelsFromDatabase(env.db, serverId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	env.sm.SetServer(sessionId, serverId)

	jsonResponseStruct(w, channels, http.StatusOK)
}

func (env *Handler) deleteChannel(w http.ResponseWriter, r *http.Request) {
	serverId := env.mustGetIdFromServerContext(r, ServerIdKeyType{})
	channelId := env.mustGetIdFromServerContext(r, ChannelIdKeyType{})

	result, err := env.db.Exec("DELETE FROM channels WHERE id = ? AND server_id = ?", channelId, serverId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	rowsDeleted, err := result.RowsAffected()
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}
	if rowsDeleted != 1 {
		err := fmt.Errorf("expected to delete channel ID %d in deleteChannel handler, but affected rows value was %d", channelId, rowsDeleted)
		unexpectedErrorResponse(w, err)
		return
	}

	env.sm.EmitToRoom(DELETE_CHANNEL, []byte(strconv.FormatInt(channelId, 10)), serverId)

	w.WriteHeader(http.StatusAccepted)
}

func (env *Handler) getMembers(w http.ResponseWriter, r *http.Request) {
	serverId := env.mustGetIdFromServerContext(r, ServerIdKeyType{})

	members, err := getMembersFromDatabase(env.db, env.sm, serverId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	jsonResponseStruct(w, members, http.StatusOK)
}

func (env *Handler) createMessage(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	channelId := env.mustGetIdFromServerContext(r, ChannelIdKeyType{})

	message := strings.TrimSpace(r.FormValue("message"))

	issues := validator.MergeValidationIssues(
		validator.TextMessageSchema.Validate(message, true),
	)
	if len(issues) != 0 {
		jsonResponseStruct(w, issues, http.StatusBadRequest)
		return
	}

	// TODO handle attachments
	attachmentsCount := 0

	messageId := env.idGen.Generate().Int64()

	tx, err := env.db.Begin()
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}
	defer rollbackTx(tx)

	{
		_, err := tx.Exec(`
			INSERT INTO messages (id, sender_id, channel_id, message, attachment_count)
			VALUES (?, ?, ?, ?, ?)`,
			messageId, userId, channelId, message, attachmentsCount,
		)
		if err != nil {
			unexpectedErrorResponse(w, err)
			return
		}
	}
	{
		// TODO insert attachments
	}

	err = tx.Commit()
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	userCache, err := cache.UserGetSet(env.db, userId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	messageResponse := MessageResponse{
		Id:          messageId,
		SenderId:    userId,
		ChannelId:   channelId,
		Message:     message,
		DisplayName: userCache.DisplayName,
		Picture:     userCache.Picture,
		Attachments: []Attachment{},
	}

	messageResponseJson, err := json.Marshal(messageResponse)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	env.sm.EmitToRoom(CREATE_MESSAGE, messageResponseJson, channelId)

	w.WriteHeader(http.StatusAccepted)
}

func (env *Handler) editMessage(w http.ResponseWriter, r *http.Request) {
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

	messageIdStr := r.PathValue("messageId")
	if messageIdStr == "" {
		http.Error(w, "Missing message ID parameter", http.StatusBadRequest)
		return
	}
	messageId, err := strconv.ParseInt(messageIdStr, 10, 64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	type Payload struct {
		Message string `json:"message"`
	}

	var p Payload
	err = json.NewDecoder(r.Body).Decode(&p)
	if err != nil {
		slog.Warn(err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.Message = strings.TrimSpace(p.Message)

	issues := validator.MergeValidationIssues(
		validator.TextMessageSchema.Validate(p.Message, false),
	)
	if len(issues) != 0 {
		jsonResponseStruct(w, issues, http.StatusBadRequest)
		return
	}

	editedTimestamp := time.Now().Unix()

	const q = "UPDATE messages SET message = ?, edited = ? WHERE id = ? AND sender_id = ? AND channel_id = ?"

	result, err := env.db.Exec(q, p.Message, editedTimestamp, messageId, userId, channelId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	rowsUpdated, err := result.RowsAffected()
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}
	if rowsUpdated != 1 {
		err := fmt.Errorf("not authorised to edit message ID %d", messageId)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	type MessageEditResponse struct {
		Id        int64  `json:"id"`
		SenderId  int64  `json:"sender_id"`
		ChannelId int64  `json:"channel_id"`
		Message   string `json:"message"`
		Edited    int64  `json:"edited"`
	}

	var editedMsg = MessageEditResponse{
		Id:        messageId,
		SenderId:  userId,
		ChannelId: channelId,
		Message:   p.Message,
		Edited:    editedTimestamp,
	}

	editedMsgJson, err := json.Marshal(editedMsg)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	env.sm.EmitToRoom(EDIT_MESSAGE, editedMsgJson, editedMsg.ChannelId)

	w.WriteHeader(http.StatusAccepted)
}

func (env *Handler) deleteMessage(w http.ResponseWriter, r *http.Request) {
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

	messageIdStr := r.PathValue("messageId")
	if messageIdStr == "" {
		http.Error(w, "Missing message ID parameter", http.StatusBadRequest)
		return
	}
	messageId, err := strconv.ParseInt(messageIdStr, 10, 64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	const q = "DELETE FROM messages WHERE id = ? AND sender_id = ? AND channel_id = ?"
	result, err := env.db.Exec(q, messageId, userId, channelId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}
	if rowsAffected != 1 {
		err := fmt.Errorf("not authorised to delete message ID %d", messageId)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	env.sm.EmitToRoom(DELETE_MESSAGE, []byte(strconv.FormatInt(messageId, 10)), channelId)

	w.WriteHeader(http.StatusAccepted)
}

func (env *Handler) getMessages(w http.ResponseWriter, r *http.Request) {
	channelId := env.mustGetIdFromServerContext(r, ChannelIdKeyType{})

	queryParams := r.URL.Query()

	messageIdStr := queryParams.Get("messageID")
	direction := queryParams.Get("direction")

	const limit = MESSAGES_SLICE_CAP
	var messages []MessageResponse
	var err error

	const queryBase = `
		SELECT
		m.id, m.sender_id, m.channel_id, m.message, m.attachment_count, m.edited, u.display_name, u.picture
		FROM messages m
		JOIN users u ON m.sender_id = u.id
		WHERE m.channel_id = ?`

	if messageIdStr != "" {
		var messageId int64
		messageId, err = strconv.ParseInt(messageIdStr, 10, 64)
		if err != nil {
			slog.Warn(err.Error())
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		switch direction {
		case "before": // scrolling up
			const q = queryBase + " AND m.id < ? ORDER by m.id DESC LIMIT ?"
			messages, err = getMessagesFromDatabase(env.db, q, channelId, messageId, limit)
		case "after": // scrolling down
			const q = queryBase + " AND m.id > ? ORDER by m.id ASC LIMIT ?"
			messages, err = getMessagesFromDatabase(env.db, q, channelId, messageId, limit)
		default:
			http.Error(w, "Unknown direction value", http.StatusBadRequest)
			return
		}
	} else { // if just getting last ones
		const q = queryBase + " ORDER by m.id DESC LIMIT ?"
		messages, err = getMessagesFromDatabase(env.db, q, channelId, limit)
	}
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	// grab attachments for messages that have attachments
	for i := range messages {
		if messages[i].AttachmentCount != nil && *messages[i].AttachmentCount > 0 {
			messages[i].Attachments, err = getAttachmentsFromDatabase(env.db, messages[i].Id)
			if err != nil {
				unexpectedErrorResponse(w, err)
				return
			}
		}
	}

	// subscribe for events if first request
	if messageIdStr == "" {
		sessionId := env.mustGetIdFromServerContext(r, SessionIdKeyType{})
		env.sm.SetChannel(sessionId, channelId)
	}

	jsonResponseStruct(w, messages, http.StatusOK)
}

func (env *Handler) typing(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	channelId := env.mustGetIdFromServerContext(r, ChannelIdKeyType{})
	action := r.PathValue("action")

	var buf []byte
	const bufLengthBase = 1 + 1 + 19 // action byte + space + maximum possible snowflake ID digits (ranges from 17 to 19)
	var bufLength = bufLengthBase

	switch action {
	case "start":
		userCache, err := cache.UserGetSet(env.db, userId)
		if err != nil {
			unexpectedErrorResponse(w, err)
			return
		}
		displayName := userCache.DisplayName

		// "1 987654321 displayname"
		bufLength = bufLengthBase + 1 + len(displayName) // that 1 is space between id and display name
		buf = make([]byte, 0, bufLength)
		buf = append(buf, "1"...) // 1 in beginning means start
		buf = append(buf, ' ')
		buf = strconv.AppendInt(buf, userId, 10)
		buf = append(buf, ' ')
		buf = append(buf, displayName...)
	case "stop":
		// "1 987654321"
		buf = make([]byte, 0, bufLength)
		buf = append(buf, "0"...) // 0 in beginning means stop
		buf = append(buf, ' ')
		buf = strconv.AppendInt(buf, userId, 10)
	default:
		http.Error(w, "unsupported action parameter", http.StatusBadRequest)
		return
	}

	if len(buf) > bufLength {
		slog.Warn(fmt.Sprintf("Length of buf is supposed to less or equal than %d, but was %d instead", bufLength, len(buf)))
	}

	env.sm.EmitToRoom(TYPING, buf, channelId)

	w.WriteHeader(http.StatusAccepted)
}

func (env *Handler) serveAvatars(w http.ResponseWriter, r *http.Request) {
	fileName := r.PathValue("fileName")
	if fileName == "" {
		http.Error(w, "Missing filename parameter", http.StatusBadRequest)
		return
	}

	// re := regexp.MustCompile(`^[a-fA-F0-9]{64}\.(?:jpg|webp)$`)
	re := regexp.MustCompile(`^[a-fA-F0-9]{64}\.jpg$`)
	if !re.MatchString(fileName) {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	queryParams := r.URL.Query()
	sizeStr := queryParams.Get("size")

	originalFilePath := filepath.Join("public", "avatars", fileName)

	// if requesting original size avatar
	if sizeStr == "" {
		http.ServeFile(w, r, originalFilePath)
		return
	}

	// validate optional size parameter
	if sizeStr != "80" && sizeStr != "96" {
		http.Error(w, "Unsupported size parameter", http.StatusBadRequest)
		return
	}

	// return resized avatar
	resizedFilePath := filepath.Join("public", "avatars", sizeStr, fileName)
	_, err := os.Stat(resizedFilePath)
	if err == nil { // serve if resized avatar was found
		http.ServeFile(w, r, resizedFilePath)
		return
	}
	// continue if only file missing error
	if !errors.Is(err, os.ErrNotExist) {
		unexpectedErrorResponse(w, err)
		return
	}

	// if requested resized avatar wasn't found,
	// and then the original wasn't found either,
	// no point continuing
	_, err = os.Stat(originalFilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "", http.StatusNotFound)
		} else {
			unexpectedErrorResponse(w, err)
		}
		return
	}

	// this shouldn't throw error as there are checks above
	size, err := strconv.Atoi(sizeStr)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	err = generateResizedAvatar(fileName, size)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	http.ServeFile(w, r, resizedFilePath)
}
