package main

import (
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
	textResponse(w, "Hello go!", 200)
}

func (env *Handler) testAuth(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	textResponse(w, fmt.Sprintf("Hello %d!", userId), 200)
}

func (env *Handler) session(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	hash := mustRandomHash(32)
	sessionId := base64.URLEncoding.EncodeToString(hash)

	var sseMessage = func(event string, data []byte) []byte {
		var msg []byte
		if event != "" {
			msg = fmt.Appendf(msg, "event: %s\n", event)
		}
		msg = fmt.Appendf(msg, "data: %s\n\n", data)
		return msg
	}

	// send initial session id
	_, err := w.Write(sseMessage("session_id", []byte(sessionId)))
	if err != nil {
		slog.Warn(err.Error())
		return
	}
	w.(http.Flusher).Flush()

	// pubSub := env.rdb.Subscribe(r.Context(), "test")
	// for msg := range pubSub.Channel() {
	// 	fmt.Println(msg.Channel, msg.Payload)
	// 	w.Write([]byte(messageString("", msg.Payload)))
	// 	w.(http.Flusher).Flush()
	// }

	// ch := make(chan *nats.Msg, 64)
	// _, _ = env.nats.ChanSubscribe("test", ch)
	// for msg := range ch {
	// 	w.Write(sseMessage("", msg.Data))
	// 	w.(http.Flusher).Flush()
	// }

	// pubSub := env.rdb.Subscribe(r.Context(), "mychannel1")

	// for i := range 10 {
	// 	w.Write([]byte(messageString("", i)))
	// 	w.(http.Flusher).Flush()
	// 	time.Sleep(2 * time.Second)
	// }

	<-r.Context().Done()
}

func (env *Handler) testName(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name != "prototype585" {
		http.Error(w, "Wrong username", 400)
		return
	}
	code := r.URL.Query().Get("code")
	_, err := fmt.Fprintf(w, "%s - %s", name, code)
	if err != nil {
		return
	}
}

func (env *Handler) register(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		slog.Warn(err.Error())
		http.Error(w, "Couldn't parse form", 400)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	issues := validator.MergeValidationIssues(
		validator.UsernameSchema.Validate(username, false),
		validator.PasswordSchema.Validate(password, false),
	)
	if len(issues) != 0 {
		jsonResponse(w, issues, 400)
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
		http.Error(w, "Invalid form", 400)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	issues := validator.MergeValidationIssues(
		validator.UsernameSchema.Validate(username, false),
		validator.PasswordSchema.Validate(password, false),
	)
	if len(issues) != 0 {
		jsonResponse(w, issues, 400)
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
	textResponse(w, fmt.Sprintf("%d", userId), 200)
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
	jsonResponse(w, user, 200)
}

func (env *Handler) updateUserInfo(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	err := r.ParseForm()
	if err != nil {
		slog.Warn(err.Error())
		http.Error(w, "Invalid form", 400)
		return
	}

	displayName := strings.TrimSpace(r.FormValue("display_name"))

	issues := validator.MergeValidationIssues(
		validator.DisplaynameSchema.Validate(displayName, true),
	)
	if len(issues) != 0 {
		jsonResponse(w, issues, 400)
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

	type ResponseData struct {
		ID          int64  `json:"id"`
		DisplayName string `json:"display_name,omitempty"`
	}

	// sessions.emit(userID, {
	//   event: "self_user_info",
	//   data: responseData,
	// });

	// sessions.emitToServersUserisOn(userID, {
	//   event: "user_info",
	//   data: responseData,
	// });

	responseData := ResponseData{ID: userId, DisplayName: displayName}
	jsonResponse(w, responseData, 200)
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
			http.Error(w, "Invalid uploaded file", 400)
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
			http.Error(w, "Uploaded picture format isn't supported", 400)
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

	// TODO emit change

	textResponse(w, fileName, 200)
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
		http.Error(w, err.Error(), 400)
		return
	}
	p.Name = strings.TrimSpace(p.Name)

	issues := validator.MergeValidationIssues(
		validator.ServerNameSchema.Validate(p.Name, false),
	)
	if len(issues) != 0 {
		jsonResponse(w, issues, 400)
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

	jsonResponse(w, server, 200)
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

	jsonResponse(w, s, 200)
}

func (env *Handler) updateServerInfo(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	serverId := env.mustGetIdFromServerContext(r, ServerIdKeyType{})

	err := r.ParseForm()
	if err != nil {
		slog.Warn(err.Error())
		http.Error(w, "Invalid form", 400)
		return
	}

	serverName := strings.TrimSpace(r.FormValue("name"))

	issues := validator.MergeValidationIssues(
		validator.ServerNameSchema.Validate(serverName, true),
	)
	if len(issues) != 0 {
		jsonResponse(w, issues, 400)
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

	// sessions.emit(userID, {
	//   event: "server_info",
	//   data: s,
	// });

	jsonResponse(w, s, 200)
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
			http.Error(w, "Invalid uploaded file", 400)
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
			http.Error(w, "Uploaded picture format isn't supported", 400)
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

	// const q2 = `
	// 	SELECT
	// 	id, owner_id, name, picture, banner, roles
	//  	FROM servers WHERE id = ? AND owner_id = ?`
	// row := env.db.QueryRow(q2, serverId, userId)

	// var s ServerDatabase
	// err = row.Scan(&s.Id, &s.OwnerID, &s.Name, &s.Picture, &s.Banner, &s.Roles)
	// if err != nil {
	// 	unexpectedErrorResponse(w, err)
	// 	return
	// }

	// sessions.emitToServerList(serverID, {
	//   event: "server_info",
	//   data: s,
	// });

	// TODO emit change

	textResponse(w, fileName, 200)
}

func (env *Handler) getServers(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	servers, err := getServersFromDatabase(env.db, userId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	jsonResponse(w, servers, 200)
}

func (env *Handler) deleteServer(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	serverId := env.mustGetIdFromServerContext(r, ServerIdKeyType{})

	// TODO emit about deletion to affected users

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
		err := fmt.Errorf("Expected to delete server ID %d in deleteServer handler, but affected rows value was %d", serverId, rowsDeleted)
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
		http.Error(w, err.Error(), 400)
		return
	}
	p.Name = strings.TrimSpace(p.Name)

	issues := validator.MergeValidationIssues(
		validator.ChannelNameSchema.Validate(p.Name, false),
	)
	if len(issues) != 0 {
		jsonResponse(w, issues, 400)
		return
	}

	var channelId = env.idGen.Generate().Int64()

	const q = "INSERT INTO channels (id, server_id, name) VALUES (?, ?, ?)"
	_, err = env.db.Exec(q, channelId, serverId, p.Name)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	// TODO emit about channel creation

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

	jsonResponse(w, c, http.StatusOK)
}

func (env *Handler) updateChannelInfo(w http.ResponseWriter, r *http.Request) {
	channelId := env.mustGetIdFromServerContext(r, ChannelIdKeyType{})

	err := r.ParseForm()
	if err != nil {
		slog.Warn(err.Error())
		http.Error(w, "Invalid form", 400)
		return
	}

	channelName := strings.TrimSpace(r.FormValue("name"))

	issues := validator.MergeValidationIssues(
		validator.ChannelNameSchema.Validate(channelName, true),
	)
	if len(issues) != 0 {
		jsonResponse(w, issues, 400)
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

	var c ChannelDatabase
	err = row.Scan(&c.Id, &c.ServerId, &c.Name)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	// sessions.emit(userID, {
	//   event: "modify_channel",
	//   data: c,
	// });

	jsonResponse(w, c, 200)
}

func (env *Handler) getChannels(w http.ResponseWriter, r *http.Request) {
	serverId := env.mustGetIdFromServerContext(r, ServerIdKeyType{})

	channels, err := getChannelsFromDatabase(env.db, serverId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	// TODO set current server of session

	jsonResponse(w, channels, 200)
}

func (env *Handler) deleteChannel(w http.ResponseWriter, r *http.Request) {
	// userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	// serverId := env.mustGetIdFromServerContext(r, ServerIdKeyType{})
	channelId := env.mustGetIdFromServerContext(r, ChannelIdKeyType{})

	result, err := env.db.Exec("DELETE FROM channels WHERE id = ?", channelId)
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
		err := fmt.Errorf("Expected to delete channel ID %d in deleteChannel handler, but affected rows value was %d", channelId, rowsDeleted)
		unexpectedErrorResponse(w, err)
		return
	}

	// TODO emit to server id

	w.WriteHeader(http.StatusAccepted)
}

func (env *Handler) getMembers(w http.ResponseWriter, r *http.Request) {
	serverId := env.mustGetIdFromServerContext(r, ServerIdKeyType{})

	members, err := getMembersFromDatabase(env.db, serverId)
	if err != nil {
		unexpectedErrorResponse(w, err)
		return
	}

	jsonResponse(w, members, http.StatusOK)
}

func (env *Handler) createMessage(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	channelId := env.mustGetIdFromServerContext(r, ChannelIdKeyType{})

	message := strings.TrimSpace(r.FormValue("message"))

	issues := validator.MergeValidationIssues(
		validator.TextMessageSchema.Validate(message, true),
	)
	if len(issues) != 0 {
		jsonResponse(w, issues, 400)
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

	var displayName string
	var picture *string
	{
		row := env.db.QueryRow("SELECT display_name, picture FROM users WHERE id = ?", userId)
		err := row.Scan(&displayName, &picture)
		if err != nil {
			unexpectedErrorResponse(w, err)
			return
		}
	}

	// messageResponse := MessageResponse{
	// 	Id:          fmt.Sprintf("%d", messageId),
	// 	SenderId:    fmt.Sprintf("%d", userId),
	// 	ChannelId:   fmt.Sprintf("%d", channelId),
	// 	Message:     message,
	// 	DisplayName: displayName,
	// 	Picture:     picture,
	// 	Attachments: []Attachment{},
	// }

	// messageResponseJson, err := json.Marshal(messageResponse)
	// if err != nil {
	// internalServerErrorResponse(w, err)
	// 	return
	// }

	// subject := fmt.Sprintf("channel.%d.create_message", channelId)
	// err = env.nats.Publish(subject, messageResponseJson)
	// if err != nil {
	// internalServerErrorResponse(w, err)
	// 	return
	// }

	w.WriteHeader(202)
}

func (env *Handler) getMessages(w http.ResponseWriter, r *http.Request) {
	channelId := env.mustGetIdFromServerContext(r, ChannelIdKeyType{})

	queryParams := r.URL.Query()

	messageIdStr := queryParams.Get("messageID")
	direction := queryParams.Get("direction")

	const limit = 100
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
			http.Error(w, err.Error(), 400)
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
			http.Error(w, "Unknown direction value", 400)
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

	// subscribe for events if it has session id in header and not the first request
	// sessionIdStr := r.Header.Get("Session-Id")
	// if sessionIdStr != "" && messageIdStr == "" {
	// 	sessionId, err := strconv.ParseInt(sessionIdStr, 10, 64)
	// 	if err != nil {
	// 		http.Error(w, err.Error(), 400)
	// 		return
	// 	}
	// 	env.sm.Subscribe(sessionId, channelId)
	// }

	jsonResponse(w, messages, 200)
}

func (env *Handler) serveAvatars(w http.ResponseWriter, r *http.Request) {
	fileName := r.PathValue("fileName")
	if fileName == "" {
		http.Error(w, "Missing filename parameter", 400)
		return
	}

	// re := regexp.MustCompile(`^[a-fA-F0-9]{64}\.(?:jpg|webp)$`)
	re := regexp.MustCompile(`^[a-fA-F0-9]{64}\.jpg$`)
	if !re.MatchString(fileName) {
		http.Error(w, "Invalid filename", 400)
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
		http.Error(w, "Unsupported size parameter", 400)
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
			http.Error(w, "", 404)
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
