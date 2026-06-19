package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
)

func jsonResponse(w http.ResponseWriter, data any, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	err := json.NewEncoder(w).Encode(data)
	if err != nil {
		logger.Warn(err.Error())
	}
}

func handleUnexpectedError(w http.ResponseWriter, err error) {
	// if errors.Is(err, context.Canceled) {
	// 	return
	// }

	http.Error(w, "", http.StatusInternalServerError)
	logger.Error(err.Error())
}

func textResponse(w http.ResponseWriter, text string, statusCode int) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(statusCode)

	_, err := w.Write([]byte(text))
	if err != nil {
		logger.Warn(err.Error())
	}
}

func mustRandomHash(length int) []byte {
	buffer := make([]byte, length)
	_, err := rand.Read(buffer)
	if err != nil {
		logger.Fatal(err.Error())
	}
	return buffer
}

func (env *Handler) mustGetIdFromServerContext(r *http.Request, keyType any) int64 {
	id, ok := r.Context().Value(keyType).(int64)
	if !ok {
		name := reflect.TypeOf(keyType).Name()
		logger.Fatal(fmt.Sprintf("Failed getting %s from r.Context()", name))
	}
	return id
}

func closeRows(rows *sql.Rows) {
	err := rows.Close()
	if err != nil {
		logger.Error(err.Error())
	}
}

func rollbackTx(tx *sql.Tx) {
	err := tx.Rollback()
	if err != nil {
		logger.Error(err.Error())
	}
}
