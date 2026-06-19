package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
)

func jsonResponse(w http.ResponseWriter, data any, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	err := json.NewEncoder(w).Encode(data)
	if err != nil {
		slog.Warn(err.Error())
	}
}

func handleUnexpectedError(w http.ResponseWriter, err error) {
	// if errors.Is(err, context.Canceled) {
	// 	return
	// }

	http.Error(w, "", http.StatusInternalServerError)
	slog.Error(err.Error())
}

func textResponse(w http.ResponseWriter, text string, statusCode int) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(statusCode)

	_, err := w.Write([]byte(text))
	if err != nil {
		slog.Warn(err.Error())
	}
}

func mustRandomHash(length int) []byte {
	buffer := make([]byte, length)
	_, err := rand.Read(buffer)
	if err != nil {
		panic(err.Error())
	}
	return buffer
}

func (env *Handler) mustGetIdFromServerContext(r *http.Request, keyType any) int64 {
	id, ok := r.Context().Value(keyType).(int64)
	if !ok {
		name := reflect.TypeOf(keyType).Name()
		panic(fmt.Sprintf("Failed getting %s from r.Context()", name))
	}
	return id
}

func closeRows(rows *sql.Rows) {
	err := rows.Close()
	if err != nil {
		slog.Error(err.Error())
	}
}

func rollbackTx(tx *sql.Tx) {
	err := tx.Rollback()
	if err != nil {
		slog.Error(err.Error())
	}
}
