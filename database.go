package main

import (
	"chatapp/modules/validator"
	"database/sql"
	"fmt"
	"os"
	"path"
)

func initDatabase() (*sql.DB, *sql.DB, error) {
	const databaseFolder = "database"
	err := os.MkdirAll(databaseFolder, os.ModePerm)
	if err != nil {
		return nil, nil, err
	}

	db, err := sql.Open("sqlite3", path.Join(databaseFolder, "database.db"))
	if err != nil {
		return nil, nil, err
	}

	_, err = db.Exec(`
		PRAGMA journal_mode = WAL;
		PRAGMA synchronous = NORMAL;
		PRAGMA foreign_keys = ON;
	`)
	if err != nil {
		return nil, nil, err
	}

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
		return nil, nil, err
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
		return nil, nil, err
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
		return nil, nil, err
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
		return nil, nil, err
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
		return nil, nil, err
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
		return nil, nil, err
	}

	// separate sqlite file for tokens
	dbTokens, err := sql.Open("sqlite3", path.Join(databaseFolder, "tokens.db"))
	if err != nil {
		return nil, nil, err
	}

	_, err = dbTokens.Exec(`
		PRAGMA journal_mode = WAL;
		PRAGMA synchronous = NORMAL;
	`)
	if err != nil {
		return nil, nil, err
	}

	_, err = dbTokens.Exec(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS tokens (
			token CHAR(%d) PRIMARY KEY,
			user_id BIGINT NOT NULL,
			expiration BIGINT NOT NULL
    	);
	`, validator.TokenLength))
	if err != nil {
		return nil, nil, err
	}

	return db, dbTokens, nil
}
