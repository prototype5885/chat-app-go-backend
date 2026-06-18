package main

import (
	"chatapp/modules/validator"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

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
	_, err := fmt.Fprintf(w, "Hello go!")
	if err != nil {
		sugar.Warn(err)
		return
	}
}

func (env *Handler) testAuth(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})
	_, err := fmt.Fprintf(w, "Hello %d!", userId)
	if err != nil {
		sugar.Warn(err)
		return
	}
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
	_, err := w.Write(sseMessage("session_id", []byte(sessionId)))
	if err != nil {
		sugar.Warn(err)
		return
	}
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
	_, err := fmt.Fprintf(w, "%s - %s", name, code)
	if err != nil {
		return
	}
}

func (env *Handler) register(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		sugar.Warn(err)
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
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
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
			sugar.Error(err)
			http.Error(w, "", http.StatusInternalServerError)
		}
		return
	}
}

func (env *Handler) login(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		sugar.Warn(err)
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
			sugar.Error(err)
			http.Error(w, "", http.StatusInternalServerError)
		}
		return
	}

	match, err := argon2id.ComparePasswordAndHash(password, record.Password)
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
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
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	tokenCookie := setTokenCookie(token, TokenLifetimeSeconds)
	http.SetCookie(w, &tokenCookie)
}

func (env *Handler) logout(w http.ResponseWriter, r *http.Request) {
	token, err := r.Cookie("token")
	if err != nil {
		sugar.Errorw("Was unable to get token cookie in logout handler", "error", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	err = deleteToken(env.db, token.Value)
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	deleteTokenCookie := setTokenCookie("", -1)
	http.SetCookie(w, &deleteTokenCookie)
}

func (env *Handler) delete(w http.ResponseWriter, r *http.Request) {
	userId := env.mustGetIdFromServerContext(r, UserIdKeyType{})

	_, err := env.db.Exec("DELETE FROM users WHERE id = ?", userId)
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
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
			errMsg := fmt.Sprintf("Tried to get own user info of user ID %d after auth middleware but user was not found\n", userId)
			sugar.Errorw(errMsg, "error", err)
			http.Error(w, "", http.StatusInternalServerError)
		default:
			sugar.Error(err)
			http.Error(w, "", http.StatusInternalServerError)
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
		sugar.Warn(err)
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
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer func() {
		err := tx.Rollback()
		if err != nil {
			sugar.Error(err)
			return
		}
	}()

	{
		if displayName != "" {
			_, err := tx.Exec("UPDATE users SET display_name = ? WHERE id = ?", displayName, userId)
			if err != nil {
				sugar.Error(err)
				http.Error(w, "", http.StatusInternalServerError)
				return
			}
		}
	}

	err = tx.Commit()
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
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

	file, _, err := r.FormFile("file")
	if err != nil {
		sugar.Warn(err)
		http.Error(w, "Invalid uploaded file", http.StatusBadRequest)
		return
	}

	defer func() {
		err := file.Close()
		if err != nil {
			sugar.Error(err)
		}
	}()

	fileName, err := saveAvatar(file)
	if err != nil {
		var imgFormatErr *ImageFormatError
		if errors.As(err, &imgFormatErr) {
			http.Error(w, "Uploaded picture format isn't supported", http.StatusBadRequest)
		} else {
			sugar.Error(err)
			http.Error(w, "", http.StatusInternalServerError)
		}
		return
	}

	_, err = env.db.Exec("UPDATE users SET picture = ? WHERE id = ?", fileName, userId)
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	// TODO emit change

	_, err = w.Write([]byte(fileName))
	if err != nil {
		sugar.Warn(err)
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
		sugar.Warn(err)
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
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer func() {
		err := tx.Rollback()
		if err != nil {
			sugar.Error(err)
		}
	}()

	_, err = tx.Exec(
		"INSERT INTO servers (id, owner_id, name) VALUES (?, ?, ?)",
		serverId, userId, p.Name,
	)
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(
		"INSERT INTO channels (id, server_id, name) VALUES (?, ?, ?)",
		channelId, serverId, "Default channel",
	)
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	err = tx.Commit()
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
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
			errMsg := fmt.Sprintf("Created a server with ID %d but server was not found in database after creation\n", serverId)
			sugar.Errorw(errMsg, "error", err)
			http.Error(w, "", http.StatusInternalServerError)
		default:
			sugar.Error(err)
			http.Error(w, "", http.StatusInternalServerError)
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
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer func() {
		err := rows.Close()
		if err != nil {
			sugar.Error(err)
		}
	}()

	var servers []ServerDatabase
	for rows.Next() {
		var server ServerDatabase
		err := rows.Scan(&server.Id, &server.OwnerID, &server.Name, &server.Picture, &server.Banner, &server.Roles)
		if err != nil {
			sugar.Error(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		servers = append(servers, server)
	}

	err = rows.Err()
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, servers, 200)
}

func (env *Handler) getChannels(w http.ResponseWriter, r *http.Request) {
	serverId := env.mustGetIdFromServerContext(r, ServerIdKeyType{})

	const q = "SELECT id, server_id, name FROM channels WHERE server_id = ?"

	rows, err := env.db.Query(q, serverId)
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer func() {
		err := rows.Close()
		if err != nil {
			sugar.Error(err)
		}
	}()

	var channels []ChannelDatabase
	for rows.Next() {
		var channel ChannelDatabase
		err := rows.Scan(&channel.Id, &channel.ServerId, &channel.Name)
		if err != nil {
			sugar.Error(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		channels = append(channels, channel)
	}
	err = rows.Err()
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
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
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer func() {
		err := tx.Rollback()
		if err != nil {
			sugar.Error(err)
		}
	}()

	{
		_, err := tx.Exec(`
			INSERT INTO messages (id, sender_id, channel_id, message, attachment_count)
			VALUES (?, ?, ?, ?, ?)`,
			messageId, userId, channelId, message, attachmentsCount,
		)
		if err != nil {
			sugar.Error(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
	}
	{
		// TODO insert attachments
	}

	err = tx.Commit()
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	var displayName string
	var picture *string
	{
		row := env.db.QueryRow("SELECT display_name, picture FROM users WHERE id = ?", userId)
		err := row.Scan(&displayName, &picture)
		if err != nil {
			sugar.Error(err)
			http.Error(w, "", http.StatusInternalServerError)
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
	// 	sugar.Error(err)
	// 	http.Error(w, "", http.StatusInternalServerError)
	// 	return
	// }

	// subject := fmt.Sprintf("channel.%d.create_message", channelId)
	// err = env.nats.Publish(subject, messageResponseJson)
	// if err != nil {
	// 	sugar.Error(err)
	// 	http.Error(w, "", http.StatusInternalServerError)
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
		var messageId int64
		messageId, err = strconv.ParseInt(messageIdStr, 10, 64)
		if err != nil {
			sugar.Warn(err)
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
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	defer func() {
		err := mRows.Close()
		if err != nil {
			sugar.Error(err)
		}
	}()

	var messages []MessageResponse
	for mRows.Next() {
		var m MessageResponse
		err := mRows.Scan(
			&m.Id, &m.SenderId, &m.ChannelId, &m.Message, &m.AttachmentCount,
			&m.Edited, &m.DisplayName, &m.Picture,
		)
		if err != nil {
			sugar.Error(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		messages = append(messages, m)
	}
	err = mRows.Err()
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	// grab attachments for messages that have attachments
	stmt, err := env.db.Prepare("SELECT name, file FROM attachments WHERE message_id = ?")
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer func() {
		err := stmt.Close()
		if err != nil {
			sugar.Error(err)
		}
	}()

	for i := range messages {
		if *messages[i].AttachmentCount > 0 {
			aRows, err := stmt.Query(messages[i].Id)
			if err != nil {
				sugar.Error(err)
				http.Error(w, "", http.StatusInternalServerError)
				return
			}
			defer func() {
				err := aRows.Close()
				if err != nil {
					sugar.Error(err)
				}
			}()

			for aRows.Next() {
				var a Attachment
				err := aRows.Scan(&a.Name, &a.File)
				if err != nil {
					sugar.Error(err)
					http.Error(w, "", http.StatusInternalServerError)
					return
				}
				messages[i].Attachments = append(messages[i].Attachments, a)
			}
			err = aRows.Err()
			if err != nil {
				sugar.Error(err)
				http.Error(w, "", http.StatusInternalServerError)
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

	w.Header().Set("Cache-Control", "public, max-age=2592000, immutable")
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
	} else { // continue if only file missing error
		if !errors.Is(err, os.ErrNotExist) {
			sugar.Error(err)
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
	}

	// if requested resized avatar wasn't found,
	// and then the original wasn't found either,
	// no point continuing
	_, err = os.Stat(originalFilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "", http.StatusNotFound)
		} else {
			sugar.Error(err)
			http.Error(w, "", http.StatusInternalServerError)
		}
		return
	}

	// this shouldn't throw error as there are checks above
	size, err := strconv.Atoi(sizeStr)
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	err = generateResizedAvatar(fileName, size)
	if err != nil {
		sugar.Error(err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	http.ServeFile(w, r, resizedFilePath)
}
