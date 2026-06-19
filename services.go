package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// service that cleans expired tokens and vacuums both database files
func databaseCleanerService(closeServer context.CancelFunc, db *sql.DB) {
	time.Sleep(10 * time.Minute)

	stmt, err := db.Prepare("DELETE FROM tokens WHERE expiration < ?")
	if err != nil {
		logger.Error(err.Error())
		closeServer()
	}

	const hoursInterval = 4
	for {
		result, err := stmt.Exec(time.Now().Unix())
		if err != nil {
			logger.Error(err.Error())
			closeServer()
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			logger.Error(err.Error())
			closeServer()
		}

		_, err = db.Exec("VACUUM")
		if err != nil {
			logger.Error(err.Error())
			closeServer()
		}

		logger.Info(
			fmt.Sprintf(
				"Cleaned %d expired tokens and vacuumed db files! Next run in %d hours.",
				rowsAffected, hoursInterval,
			),
		)
		time.Sleep(hoursInterval * time.Hour)
	}
}
