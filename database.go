package main

import (
	"chatapp/modules/validator"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
)

const driverSqlite = 0
const driverMysql = 1

func initDatabase() (db *sql.DB, err error) {
	// if empty then sqlite will be used instead
	mysqlConnString := os.Getenv("MYSQL_CONN_STRING")

	if mysqlConnString == "" { // sqlite
		dbPath := filepath.Join("database", "database.db")
		err = os.MkdirAll(filepath.Dir(dbPath), 0755)
		if err != nil {
			return
		}
		db, err = sql.Open("sqlite3", dbPath)
		if err != nil {
			return
		}

		db.SetMaxOpenConns(1)

		_, err = db.Exec(`
			PRAGMA journal_mode = WAL;
			PRAGMA synchronous = NORMAL;
			PRAGMA foreign_keys = ON;
		`)
		if err != nil {
			return
		}
	} else { // mysql or mariadb
		db, err = sql.Open("mysql", mysqlConnString)
		if err != nil {
			return
		}

		db.SetMaxOpenConns(10)

		err = db.Ping()
		if err != nil {
			return
		}
	}

	slog.Info(fmt.Sprintf("Database driver used: %s", reflect.TypeOf(db.Driver()).String()))
	_ = getDatabaseDriver(db) // quick overkill check to see if database is really valid

	_, err = db.Exec(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS users (
			id BIGINT PRIMARY KEY,
			username VARCHAR(%d) NOT NULL UNIQUE,
			display_name VARCHAR(%d) NOT NULL,
			picture TEXT,
			password TEXT NOT NULL,
			banned BOOLEAN NOT NULL DEFAULT false,
			custom_status TEXT
		);
	`, validator.UsernameSchema.Max, validator.DisplaynameSchema.Max))
	if err != nil {
		return
	}

	_, err = db.Exec(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS servers (
			id BIGINT PRIMARY KEY,
			owner_id BIGINT NOT NULL,
			name VARCHAR(%d) NOT NULL,
			picture TEXT,
			banner TEXT,
			roles TEXT,
			FOREIGN KEY (owner_id) REFERENCES users (id) ON DELETE CASCADE
		);
	`, validator.ServerNameSchema.Max))
	if err != nil {
		return
	}

	_, err = db.Exec(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS channels (
			id BIGINT PRIMARY KEY,
			server_id BIGINT NOT NULL,
			name VARCHAR(%d) NOT NULL,
			FOREIGN KEY (server_id) REFERENCES servers (id) ON DELETE CASCADE
		);
	`, validator.ChannelNameSchema.Max))
	if err != nil {
		return
	}

	_, err = db.Exec(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS messages (
			id BIGINT PRIMARY KEY,
			sender_id BIGINT NOT NULL,
			channel_id BIGINT NOT NULL,
			message VARCHAR(%d) NOT NULL,
			attachment_count SMALLINT,
			edited TEXT,
			FOREIGN KEY (sender_id) REFERENCES users (id) ON DELETE CASCADE,
			FOREIGN KEY (channel_id) REFERENCES channels (id) ON DELETE CASCADE
		);
	`, validator.TextMessageSchema.Max))
	if err != nil {
		return
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS attachments (
			message_id BIGINT NOT NULL,
			channel_id BIGINT NOT NULL,
			name TEXT NOT NULL,
			file TEXT NOT NULL,
			FOREIGN KEY (message_id) REFERENCES messages (id) ON DELETE CASCADE,
			FOREIGN KEY (channel_id) REFERENCES channels (id) ON DELETE CASCADE
		);
	`)
	if err != nil {
		return
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS server_members (
			server_id BIGINT NOT NULL,
			member_id BIGINT NOT NULL,
			member_since BIGINT NOT NULL,
			PRIMARY KEY (server_id, member_id),
			FOREIGN KEY (server_id) REFERENCES servers (id) ON DELETE CASCADE,
			FOREIGN KEY (member_id) REFERENCES users (id) ON DELETE CASCADE
		);
	`)
	if err != nil {
		return
	}

	_, err = db.Exec(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS tokens (
			token CHAR(%d) PRIMARY KEY,
			user_id BIGINT NOT NULL,
			expiration BIGINT NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
    	);
	`, validator.TokenLength))
	if err != nil {
		return
	}

	return
}

func getServersFromDatabase(db *sql.DB, userId int64) (servers []ServerDatabase, err error) {
	var rows *sql.Rows

	const q = `
		SELECT id, owner_id, name, picture, banner, roles FROM servers s WHERE s.owner_id = ?
		UNION
		SELECT id, owner_id, name, picture, banner, roles FROM servers s
		JOIN server_members m ON s.id = m.server_id
		WHERE m.member_id = ?
	`

	rows, err = db.Query(q, userId, userId)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var s ServerDatabase
		err = rows.Scan(&s.Id, &s.OwnerID, &s.Name, &s.Picture, &s.Banner, &s.Roles)
		if err != nil {
			return
		}
		servers = append(servers, s)
	}
	err = rows.Err()
	return
}

func getServersIdsFromDatabase(db *sql.DB, userId int64) (serverIds []int64, err error) {
	var rows *sql.Rows

	const q = `
		SELECT id FROM servers WHERE owner_id = ?
		UNION
		SELECT server_id FROM server_members WHERE member_id = ?
	`

	rows, err = db.Query(q, userId, userId)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var serverId int64
		err = rows.Scan(&serverId)
		if err != nil {
			return
		}
		serverIds = append(serverIds, serverId)
	}
	err = rows.Err()
	return
}

func getChannelsFromDatabase(db *sql.DB, serverId int64) (channels []ChannelDatabase, err error) {
	var rows *sql.Rows

	const q = "SELECT id, server_id, name FROM channels WHERE server_id = ?"

	rows, err = db.Query(q, serverId)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var c ChannelDatabase
		err = rows.Scan(&c.Id, &c.ServerId, &c.Name)
		if err != nil {
			return
		}
		channels = append(channels, c)
	}
	err = rows.Err()
	return
}

// args are either channelId alone or channelId and messageId
func getMessagesFromDatabase(db *sql.DB, q string, args ...any) (messages []MessageResponse, err error) {
	var rows *sql.Rows

	rows, err = db.Query(q, args...)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var m MessageResponse
		err = rows.Scan(
			&m.Id, &m.SenderId, &m.ChannelId, &m.Message, &m.AttachmentCount,
			&m.Edited, &m.DisplayName, &m.Picture,
		)
		if err != nil {
			return
		}
		messages = append(messages, m)
	}
	err = rows.Err()
	return
}

func getAttachmentsFromDatabase(db *sql.DB, messageId int64) (attachments []Attachment, err error) {
	var rows *sql.Rows

	const q = "SELECT name, file FROM attachments WHERE message_id = ?"
	rows, err = db.Query(q, messageId)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var a Attachment
		err = rows.Scan(&a.Name, &a.File)
		if err != nil {
			return
		}
		attachments = append(attachments, a)
	}
	err = rows.Err()
	return
}

func getMembersFromDatabase(db *sql.DB, serverId int64) (users []UserResponse, err error) {
	var rows *sql.Rows

	const q = `
		SELECT u.id, u.username, u.display_name, u.picture, u.custom_status
		FROM users u JOIN servers s ON s.owner_id = u.id WHERE s.id = :s_id
		UNION
		SELECT u.id, u.username, u.display_name, u.picture, u.custom_status
		FROM users u JOIN server_members sm ON sm.member_id = u.id WHERE sm.server_id = :s_id
    `
	rows, err = db.Query(q, serverId)
	if err != nil {
		return
	}
	defer closeRows(rows)

	for rows.Next() {
		var u UserResponse
		err = rows.Scan(&u.Id, &u.Username, &u.DisplayName, &u.Picture, &u.CustomStatus)
		if err != nil {
			return
		}

		u.Online = true // TODO check if online

		users = append(users, u)

	}
	err = rows.Err()
	return
}

func getDatabaseDriver(db *sql.DB) int {
	dbDriverStr := reflect.TypeOf(db.Driver()).String()

	switch dbDriverStr {
	case "*sqlite3.SQLiteDriver":
		return driverSqlite
	case "*mysql.MySQLDriver":
		return driverMysql
	default:
		panic(fmt.Sprintf("Unknown database driver: %s", dbDriverStr))
	}
}

func isDuplicateError(db *sql.DB, err error) bool {
	dbDriver := getDatabaseDriver(db)

	switch dbDriver {
	case driverSqlite:
		return strings.Contains(err.Error(), "UNIQUE constraint failed")
	case driverMysql:
		return strings.Contains(err.Error(), "Duplicate entry")
	default:
		panic("How did it reach panic in isDuplicateError?")
	}
}
