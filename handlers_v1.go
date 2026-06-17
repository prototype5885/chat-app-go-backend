package main

import (
	"chatapp/modules/validator"
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
		VALUES (?, ?, ?, ?)`,
		env.idGen.Generate().Int64(), username, username, hashedPassword,
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
	err = insertToken(env.db, token, record.Id)
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

	err = deleteToken(env.db, token.Value)
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}

	deleteTokenCookie := setTokenCookie("", -1)
	http.SetCookie(w, &deleteTokenCookie)
}

func (env *Handler) delete(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	_, err := env.db.Exec("DELETE FROM users WHERE id = ?", userId)
	if err != nil {
		macrosInternalServerError(w, err)
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
			_, err := tx.Exec("UPDATE users SET display_name = ? WHERE id = ?", displayName, userId)
			if err != nil {
				macrosInternalServerError(w, err)
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

func (env *Handler) uploadUserAvatar(w http.ResponseWriter, r *http.Request) {
	file, _, err := r.FormFile("file")
	if err != nil {
		fmt.Println(err)
		http.Error(w, "Invalid uploaded file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	_, err = saveAvatar(file)
	if err != nil {
		var imgFormatErr *ImageFormatError
		if errors.As(err, &imgFormatErr) {
			http.Error(w, "Uploaded picture format isn't supported", http.StatusBadRequest)
		} else {
			macrosInternalServerError(w, err)
		}
		return
	}
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
		"INSERT INTO servers (id, owner_id, name) VALUES (?, ?, ?)",
		serverId, userId, p.Name,
	)
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}

	_, err = tx.Exec(
		"INSERT INTO channels (id, server_id, name) VALUES (?, ?, ?)",
		channelId, serverId, "Default channel",
	)
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}

	err = tx.Commit()
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}

	row := env.db.QueryRow(`
		SELECT id, owner_id, name, picture, banner, roles 
		FROM servers WHERE id = ?
	`, serverId)

	var server ServerDatabase
	err = row.Scan(&server.Id, &server.OwnerID, &server.Name, &server.Picture, &server.Banner, &server.Roles)
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

	var servers []ServerDatabase
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

	const q = "SELECT id, server_id, name FROM channels WHERE server_id = ?"

	rows, err := env.db.Query(q, serverId)
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}

	var channels []ChannelDatabase
	for rows.Next() {
		var channel ChannelDatabase
		err := rows.Scan(&channel.Id, &channel.ServerId, &channel.Name)
		if err != nil {
			macrosInternalServerError(w, err)
			return
		}
		channels = append(channels, channel)
	}
	if rows.Err() != nil {
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

	tx, err := env.db.Begin()
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}
	defer tx.Rollback()

	{
		_, err := tx.Exec(`
			INSERT INTO messages (id, sender_id, channel_id, message, attachment_count)
			VALUES (?, ?, ?, ?, ?)`,
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

	err = tx.Commit()
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}

	var displayName string
	var picture *string
	{
		row := env.db.QueryRow("SELECT display_name, picture FROM users WHERE id = ?", userId)
		err := row.Scan(&displayName, &picture)
		if err != nil {
			macrosInternalServerError(w, err)
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
	var mRows *sql.Rows
	var err error

	const queryBase = `
		SELECT 
		m.id, m.sender_id, m.channel_id, m.message, m.attachment_count, m.edited, u.display_name, u.picture 
		FROM messages m
		JOIN users u ON m.sender_id = u.id
		WHERE m.channel_id = ?%s
		ORDER BY m.id %s LIMIT %d`

	if messageIdStr != "" {
		messageId, err := strconv.ParseInt(messageIdStr, 10, 64)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		switch direction {
		case "before": // scrolling up
			var q = fmt.Sprintf(queryBase, " AND m.id < ?", "DESC", limit)
			mRows, err = env.db.Query(q, channelId, messageId)
		case "after": // scrolling down
			var q = fmt.Sprintf(queryBase, " AND m.id > ?", "ASC", limit)
			mRows, err = env.db.Query(q, channelId, messageId)
		default:
			http.Error(w, "Unknown direction value", http.StatusBadRequest)
			return
		}
	} else { // if just getting last ones
		var q = fmt.Sprintf(queryBase, "", "DESC", limit)
		mRows, err = env.db.Query(q, channelId)
	}
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}
	defer mRows.Close()

	var messages []MessageResponse
	for mRows.Next() {
		var m MessageResponse
		err := mRows.Scan(
			&m.Id, &m.SenderId, &m.ChannelId, &m.Message, &m.AttachmentCount,
			&m.Edited, &m.DisplayName, &m.Picture,
		)
		if err != nil {
			macrosInternalServerError(w, err)
			return
		}
		messages = append(messages, m)
	}
	if mRows.Err() != nil {
		macrosInternalServerError(w, err)
		return
	}

	// grab attachments for messages that have attachments
	stmt, err := env.db.Prepare("SELECT name, file FROM attachments WHERE message_id = ?")
	if err != nil {
		macrosInternalServerError(w, err)
		return
	}
	defer stmt.Close()

	for i := range messages {
		if *messages[i].AttachmentCount > 0 {
			aRows, err := stmt.Query(messages[i].Id)
			if err != nil {
				macrosInternalServerError(w, err)
				return
			}
			defer aRows.Close()

			for aRows.Next() {
				var a Attachment
				err := aRows.Scan(&a.Name, &a.File)
				if err != nil {
					macrosInternalServerError(w, err)
					return
				}
				messages[i].Attachments = append(messages[i].Attachments, a)
			}
			if aRows.Err() != nil {
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
