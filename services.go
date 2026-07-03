package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// service that cleans expired tokens and vacuums sqlite database file
func databaseCleanerService(closeServer context.CancelFunc, db *sql.DB) {
	time.Sleep(10 * time.Minute) // delay start for 10 mins

	const q = "DELETE FROM tokens WHERE expiration < ?"
	const hoursInterval = 4
	for {
		result, err := db.Exec(q, time.Now().Unix())
		if err != nil {
			slog.Error(err.Error())
			closeServer()
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			slog.Error(err.Error())
			closeServer()
		}

		if getDatabaseDriver(db) == driverSqlite {
			_, err = db.Exec("VACUUM")
			if err != nil {
				slog.Error(err.Error())
				closeServer()
			}

			slog.Info(
				fmt.Sprintf(
					"Cleaned %d expired tokens and vacuumed db file! Next run in %d hours.",
					rowsAffected, hoursInterval,
				),
			)
		} else {
			slog.Info(
				fmt.Sprintf(
					"Cleaned %d expired tokens! Next run in %d hours.",
					rowsAffected, hoursInterval,
				),
			)
		}

		time.Sleep(hoursInterval * time.Hour)
	}
}
