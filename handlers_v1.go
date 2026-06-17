package main

import (
	"chatapp/modules/validator"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/mattn/go-sqlite3"
)

type Claims struct {
	UserID string `json:"user_id"`
}

func (env *Handler) test(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello go!")
}

func (env *Handler) testAuth(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	fmt.Fprintf(w, "Hello %d!", userId)
}

func (env Handler) session(w http.ResponseWriter, r *http.Request) {
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
	w.Write(sseMessage("session_id", []byte(sessionId)))
	w.(http.Flusher).Flush()

	// pubsub := env.rdb.Subscribe(r.Context(), "test")
	// for msg := range pubsub.Channel() {
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

	// pubsub := env.rdb.Subscribe(r.Context(), "mychannel1")

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
		http.Error(w, "Wrong username", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	fmt.Fprintf(w, "%s - %s", name, code)
}

func (env *Handler) register(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
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
		jsonResponse(w, issues, http.StatusBadRequest)
		return
	}

	hashedPassword, err := argon2id.CreateHash(password, argon2id.DefaultParams)
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}

	_, err = env.db.Exec(`
		INSERT INTO users (id, username, display_name, password) 
		VALUES ($1, $2, $2, $3)`,
		env.idGen.Generate().Int64(), username, hashedPassword,
	)
	if err != nil {
		var sqliteErr sqlite3.Error
		if errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique {
			http.Error(w, "User with same username already exists", http.StatusConflict)
		} else {
			macrosInternalServerError(w, err)
		}
		return
	}
}

func (env *Handler) login(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
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
		jsonResponse(w, issues, http.StatusBadRequest)
		return
	}

	badLogin := func() {
		time.Sleep(time.Duration(rand.Intn(2000)) * time.Millisecond)
		http.Error(w, "Bad login", http.StatusUnauthorized)
	}

	type UserRecord struct {
		Id       int64  `db:"id"`
		Password string `db:"password"`
	}

	record := UserRecord{}
	err = env.db.Get(&record,
		"SELECT id, password FROM users WHERE username = $1", username,
	)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			badLogin()
		default:
			macrosInternalServerError(w, err)
		}
		return
	}

	match, err := argon2id.ComparePasswordAndHash(password, record.Password)
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}
	if !match {
		badLogin()
		return
	}

	hash := mustRandomHash(32)
	token := base64.RawURLEncoding.EncodeToString(hash)
	err = insertToken(env.dbTokens, token, record.Id)
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}

	tokenCookie := setTokenCookie(token, TokenLifetimeSeconds)
	http.SetCookie(w, &tokenCookie)
}

func (env *Handler) logout(w http.ResponseWriter, r *http.Request) {
	token, err := r.Cookie("token")
	if err != nil {
		log.Println("Was unable to get cookie in logout handler")
		macrosInternalServerError(w, err)
		return
	}

	err = deleteToken(env.dbTokens, token.Value)
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}

	deleteTokenCookie := setTokenCookie("", -1)
	http.SetCookie(w, &deleteTokenCookie)
}

func (env *Handler) delete(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	_, err := env.db.Exec("DELETE FROM users WHERE id = $1", userId)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			break
		default:
			macrosInternalServerError(w, err)
		}
		return
	}
}

func (env *Handler) getUserID(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	textResponse(w, fmt.Sprintf("%d", userId), http.StatusOK)
}

func (env *Handler) getUserInfo(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	user := UserResponse{}
	err := env.db.Get(&user, `
		SELECT id, username, display_name, picture, custom_status
		FROM users
		WHERE id = $1
	`, userId)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			log.Printf("Tried to get own user info of user ID %d after auth middleware but user was not found\n", userId)
			macrosInternalServerError(w, err)
		default:
			macrosInternalServerError(w, err)
		}
		return
	}

	user.Online = true
	jsonResponse(w, user, http.StatusOK)
}

func (env *Handler) updateUserInfo(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	displayName := strings.TrimSpace(r.FormValue("display_name"))

	issues := validator.MergeValidationIssues(
		validator.UsernameSchema.Validate(displayName, true),
	)
	if len(issues) != 0 {
		jsonResponse(w, issues, http.StatusBadRequest)
		return
	}

	tx, err := env.db.Begin()
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}
	defer tx.Rollback()

	{
		if displayName != "" {
			_, err := tx.Exec(
				"UPDATE users SET display_name = $1 WHERE id = $2",
				displayName, userId,
			)
			if err != nil {
				switch {
				case errors.Is(err, context.Canceled):
					break
				default:
					macrosInternalServerError(w, err)
				}
				return
			}
		}
	}

	err = tx.Commit()
	if err != nil {
		macrosInternalServerError(w, err)
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

func (env *Handler) createServer(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	type Payload struct {
		Name string `json:"name"`
	}

	var p Payload
	err := json.NewDecoder(r.Body).Decode(&p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.Name = strings.TrimSpace(p.Name)

	issues := validator.MergeValidationIssues(
		validator.ServerNameSchema.Validate(p.Name, true),
	)
	if len(issues) != 0 {
		jsonResponse(w, issues, http.StatusBadRequest)
		return
	}

	serverId := env.idGen.Generate().Int64()
	channelId := env.idGen.Generate().Int64()

	tx, err := env.db.Begin()
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		"INSERT INTO servers (id, owner_id, name) VALUES ($1, $2, $3)",
		serverId, userId, p.Name,
	)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			break
		default:
			macrosInternalServerError(w, err)
		}
		return
	}

	_, err = tx.Exec(
		"INSERT INTO channels (id, server_id, name) VALUES ($1, $2, $3)",
		channelId, serverId, "Default channel",
	)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			break
		default:
			macrosInternalServerError(w, err)
		}
		return
	}

	err = tx.Commit()
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			break
		default:
			macrosInternalServerError(w, err)
		}
		return
	}

	server := ServerDatabase{}
	err = env.db.Get(&server, `
		SELECT id, owner_id, name, picture, banner, roles 
		FROM servers WHERE id = $1
	`, serverId)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			log.Printf("Created a server with ID %d but server was not found in database after creation\n", serverId)
			macrosInternalServerError(w, err)
		default:
			macrosInternalServerError(w, err)
		}
		return
	}

	jsonResponse(w, server, 200)
}

func (env *Handler) getServers(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	const q = `
		SELECT id, owner_id, name, picture, banner, roles FROM servers s WHERE s.owner_id = ?
		UNION
		SELECT id, owner_id, name, picture, banner, roles FROM servers s
		JOIN server_members m ON s.id = m.server_id
		WHERE m.member_id = ?
	`

	rows, err := env.db.Query(q, userId, userId)
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}
	defer rows.Close()

	servers := []ServerDatabase{}
	for rows.Next() {
		var server ServerDatabase
		err := rows.Scan(&server.Id, &server.OwnerID, &server.Name, &server.Picture, &server.Banner, &server.Roles)
		if err != nil {
			macrosInternalServerError(w, err)
			return
		}
		servers = append(servers, server)
	}

	if rows.Err() != nil {
		macrosInternalServerError(w, err)
		return
	}

	jsonResponse(w, servers, 200)
}

func (env *Handler) getChannels(w http.ResponseWriter, r *http.Request) {
	serverId := env.mustGetIdFromServerContext(r, ServerIdKeyType{})

	const q = "SELECT * FROM channels WHERE server_id = $1"

	channels := []ChannelDatabase{}
	err := env.db.Select(&channels, q, serverId)
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}

	// TODO set current server of session

	jsonResponse(w, channels, 200)

}

func (env *Handler) createMessage(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	channelId := env.mustGetIdFromServerContext(r, ChannelIdKeyType{})

	message := strings.TrimSpace(r.FormValue("message"))

	issues := validator.MergeValidationIssues(
		validator.TextMessageSchema.Validate(message, true),
	)
	if len(issues) != 0 {
		jsonResponse(w, issues, http.StatusBadRequest)
		return
	}

	// TODO handle attachments
	attachmentsCount := 0

	messageId := env.idGen.Generate().Int64()

	tx := env.db.MustBegin()
	defer tx.Rollback()

	{
		_, err := tx.Exec(`
			INSERT INTO messages (id, sender_id, channel_id, message, attachment_count)
			VALUES ($1, $2, $3, $4, $5)`,
			messageId, userId, channelId, message, attachmentsCount,
		)
		if err != nil {
			macrosInternalServerError(w, err)

			return
		}
	}
	{
		// TODO insert attachments
	}

	err := tx.Commit()
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}

	type UserRecord struct {
		DisplayName string  `db:"display_name" json:"display_name"`
		Picture     *string `db:"picture" json:"picture,omitempty"`
	}

	userRecord := UserRecord{}
	err = env.db.Get(&userRecord, "SELECT display_name, picture FROM users WHERE id = $1", userId)
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}

	// messageResponse := MessageResponse{
	// 	Id:          fmt.Sprintf("%d", messageId),
	// 	SenderId:    fmt.Sprintf("%d", userId),
	// 	ChannelId:   fmt.Sprintf("%d", channelId),
	// 	Message:     message,
	// 	DisplayName: userRecord.DisplayName,
	// 	Picture:     userRecord.Picture,
	// 	Attachments: []Attachment{},
	// }

	// messageResponseJson, err := json.Marshal(messageResponse)
	// if err != nil {
	// 	macrosInternalServerError(w, err)
	// 	return
	// }

	// subject := fmt.Sprintf("channel.%d.create_message", channelId)
	// err = env.nats.Publish(subject, messageResponseJson)
	// if err != nil {
	// 	macrosInternalServerError(w, err)
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
	var messages = []MessageResponse{}
	var err error

	if messageIdStr != "" {
		messageId, err := strconv.ParseInt(messageIdStr, 10, 64)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		switch direction {
		case "before":
			const q = `
				SELECT m.*, u.display_name, u.picture FROM messages m
				JOIN users u ON m.sender_id = u.id
				WHERE m.channel_id = $1 AND m.id < $2
				ORDER BY m.id DESC LIMIT $3`
			err = env.db.Select(&messages, q, channelId, messageId, limit)
		case "after":
			const q = `
				SELECT m.*, u.display_name, u.picture FROM messages m
				JOIN users u ON m.sender_id = u.id
				WHERE m.channel_id = $1 AND m.id > $2
				ORDER BY m.id ASC LIMIT $3`
			err = env.db.Select(&messages, q, channelId, messageId, limit)
		default:
			http.Error(w, "Unknown direction value", http.StatusBadRequest)
			return
		}
	} else {
		const q = `
			SELECT m.*, u.display_name, u.picture FROM messages m
			JOIN users u ON m.sender_id = u.id
			WHERE m.channel_id = $1
			ORDER BY m.id DESC LIMIT $2`
		err = env.db.Select(&messages, q, channelId, limit)
	}
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}

	// grab attachments for messages that have attachments
	const q = "SELECT name, file FROM attachments WHERE message_id = $1"
	for i := range messages {
		if *messages[i].AttachmentCount > 0 {
			err := env.db.Select(&messages[i].Attachments, q, messages[i].Id)
			if err != nil {
				macrosInternalServerError(w, err)
				return
			}
		}
	}

	// subscribe for events if has session id in header and not the first request
	// sessionIdStr := r.Header.Get("Session-Id")
	// if sessionIdStr != "" && messageIdStr == "" {
	// 	sessionId, err := strconv.ParseInt(sessionIdStr, 10, 64)
	// 	if err != nil {
	// 		http.Error(w, err.Error(), http.StatusBadRequest)
	// 		return
	// 	}
	// 	env.sm.Subscribe(sessionId, channelId)
	// }

	jsonResponse(w, messages, 200)
}
