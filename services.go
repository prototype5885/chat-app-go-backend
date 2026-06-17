package main

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// service that cleans expired tokens and vacuums both database files
func databaseCleanerService(closeServer context.CancelFunc, db *sql.DB, dbTokens *sql.DB) {
	time.Sleep(10 * time.Minute)

	const hoursInterval = 4
	for {
		result, err := dbTokens.Exec("DELETE FROM tokens WHERE expiration < ?", time.Now().Unix())
		if err != nil {
			log.Println(err)
			closeServer()
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			log.Println(err)
			closeServer()
		}

		_, err = dbTokens.Exec("VACUUM")
		if err != nil {
			log.Println(err)
			closeServer()
		}

		_, err = db.Exec("VACUUM")
		if err != nil {
			log.Println(err)
			closeServer()
		}

		log.Printf("Cleaned %d expired tokens and vacuumed db files! Next run in %d hours.\n", rowsAffected, hoursInterval)
		time.Sleep(hoursInterval * time.Hour)
	}
}
