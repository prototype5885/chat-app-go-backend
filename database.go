package main

import (
	"chatapp/modules/validator"
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func initDatabaseCommands(db *pgxpool.Pool) error {
	var err error
	ctx := context.Background()

	_, err = db.Exec(ctx, fmt.Sprintf(`
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
		return err
	}

	_, err = db.Exec(ctx, fmt.Sprintf(`
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
		return err
	}

	_, err = db.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS channels (
			id BIGINT PRIMARY KEY,
			server_id BIGINT NOT NULL,
			name VARCHAR(%d) NOT NULL,
			FOREIGN KEY (server_id) REFERENCES servers (id) ON DELETE CASCADE
		);
	`, validator.ChannelNameSchema.Max))
	if err != nil {
		return err
	}

	_, err = db.Exec(ctx, fmt.Sprintf(`
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
		return err
	}

	_, err = db.Exec(ctx, `
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
		return err
	}

	_, err = db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS server_members (
			server_id BIGINT NOT NULL,
			member_id BIGINT NOT NULL,
			member_since TIMESTAMPTZ,
			PRIMARY KEY (server_id, member_id),
			FOREIGN KEY (server_id) REFERENCES servers (id) ON DELETE CASCADE,
			FOREIGN KEY (member_id) REFERENCES users (id) ON DELETE CASCADE
		);
	`)
	if err != nil {
		return err
	}

	return nil
}
