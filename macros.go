package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"reflect"
)

func macrosInternalServerError(w http.ResponseWriter, err error) {
	log.Printf("Internal server error:\n%s\n", err.Error())
	http.Error(w, "", http.StatusInternalServerError)
}

func jsonResponse(w http.ResponseWriter, data any, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	err := json.NewEncoder(w).Encode(data)
	if err != nil {
		macrosInternalServerError(w, err)
	}
}

func textResponse(w http.ResponseWriter, text string, statusCode int) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(statusCode)

	_, err := w.Write([]byte(text))
	if err != nil {
		macrosInternalServerError(w, err)
	}
}

func mustRandomHash(length int) []byte {
	buffer := make([]byte, length)
	_, err := rand.Read(buffer)
	if err != nil {
		panic(err)
	}
	return buffer
}

func (env *Handler) mustGetIdFromServerContext(r *http.Request, keyType any) int64 {
	id, ok := r.Context().Value(keyType).(int64)
	if !ok {
		name := reflect.TypeOf(keyType).Name()
		fmt.Printf("FATAL: Failed getting %s from r.Context()\n", name)
		env.cancel()
	}
	return id
}
