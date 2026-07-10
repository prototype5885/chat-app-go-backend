package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/bwmarrin/snowflake"
	"github.com/joho/godotenv"
)

func main() {
	// load env
	err := godotenv.Load()
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			panic(err.Error())
		}
	}

	address := os.Getenv("ADDRESS")
	if address == "" {
		address = "localhost"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "1848"
	}

	// set logger level
	logLevel := strings.ToUpper(os.Getenv("LOG_LEVEL"))
	switch logLevel {
	case "DEBUG":
		slog.SetLogLoggerLevel(slog.LevelDebug)
	case "INFO":
		slog.SetLogLoggerLevel(slog.LevelInfo)
	case "WARN":
		slog.SetLogLoggerLevel(slog.LevelWarn)
	case "ERROR":
		slog.SetLogLoggerLevel(slog.LevelError)
	default:
		logLevel = "INFO"
		slog.SetLogLoggerLevel(slog.LevelInfo)
	}
	slog.Info("Current logger level: " + logLevel)

	// handle shutdowns/panics gracefully
	ctx, closeServer := context.WithCancel(context.Background())

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signalChan
		slog.Info(fmt.Sprintf("Received signal: %v", sig))
		closeServer()
	}()

	db, err := initDatabase()
	if err != nil {
		panic(err.Error())
	}

	go databaseCleanerService(closeServer, db)

	snowflake.Epoch = 1772841600
	idGen, err := snowflake.NewNode(0)
	if err != nil {
		panic(err.Error())
	}

	sm := NewSessionManager(idGen, db)

	// this is used to inject dependencies into handlers
	h := Handler{db: db, idGen: idGen, sm: sm}

	// setup http server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/test", h.test)

	mux.HandleFunc("POST /api/v1/user/register", h.register)
	mux.HandleFunc("POST /api/v1/user/login", h.login)

	// from now on endpoints require user to be logged in
	mux.Handle("GET /api/v1/test_auth", h.AuthUser(http.HandlerFunc(h.testAuth)))

	// client requests at beginning
	mux.Handle("GET /api/v1/session", h.AuthUser(http.HandlerFunc(h.session)))
	mux.Handle("GET /api/v1/user_id", h.AuthUser(http.HandlerFunc(h.getUserID)))

	// user
	mux.Handle("GET /api/v1/user/logout", h.AuthUser(http.HandlerFunc(h.logout)))
	mux.Handle("DELETE /api/v1/user/delete", h.AuthUser(http.HandlerFunc(h.delete)))
	mux.Handle("GET /api/v1/user", h.AuthUser(http.HandlerFunc(h.getUserInfo)))
	mux.Handle("PATCH /api/v1/user", h.AuthUser(h.RateLimiter(http.HandlerFunc(h.updateUserInfo))))
	mux.Handle("POST /api/v1/user/upload/avatar", h.AuthUser(h.RateLimiter(http.HandlerFunc(h.uploadUserAvatar))))

	// servers
	mux.Handle("POST /api/v1/server", h.AuthUser(h.RateLimiter(http.HandlerFunc(h.createServer))))
	mux.Handle("GET /api/v1/server/{serverId}", h.AuthUser(h.IsServerOwner(http.HandlerFunc(h.getServerInfo))))
	mux.Handle("PATCH /api/v1/server/{serverId}", h.AuthUser(h.RateLimiter(h.IsServerOwner(http.HandlerFunc(h.updateServerInfo)))))
	mux.Handle("POST /api/v1/server/{serverId}/upload/avatar", h.AuthUser(h.RateLimiter(h.IsServerOwner(http.HandlerFunc(h.uploadServerAvatar)))))
	mux.Handle("GET /api/v1/servers", h.AuthUser(http.HandlerFunc(h.getServers)))
	mux.Handle("DELETE /api/v1/server/{serverId}", h.AuthUser(h.RateLimiter(h.IsServerOwner(http.HandlerFunc(h.deleteServer)))))

	// channels
	mux.Handle("POST /api/v1/server/{serverId}/channel", h.AuthUser(h.RateLimiter(h.IsServerOwner(http.HandlerFunc(h.createChannel)))))
	mux.Handle("GET /api/v1/channel/{channelId}", h.AuthUser(h.IsChannelOwner(http.HandlerFunc(h.getChannelInfo))))
	mux.Handle("PATCH /api/v1/channel/{channelId}", h.AuthUser(h.RateLimiter(h.IsChannelOwner(http.HandlerFunc(h.updateChannelInfo)))))
	mux.Handle("GET /api/v1/server/{serverId}/channels", h.AuthUser(h.AuthSessionId(h.HasServerAccess(http.HandlerFunc(h.getChannels)))))
	mux.Handle("DELETE /api/v1/channel/{channelId}", h.AuthUser(h.RateLimiter(h.IsChannelOwner(http.HandlerFunc(h.deleteChannel)))))

	// server members
	mux.Handle("GET /api/v1/server/{serverId}/members", h.AuthUser(h.HasServerAccess(http.HandlerFunc(h.getMembers))))

	// chat messages
	mux.Handle("POST /api/v1/channel/{channelId}/message", h.AuthUser(h.RateLimiter(h.HasChannelAccess(http.HandlerFunc(h.createMessage)))))
	mux.Handle("PATCH /api/v1/channel/{channelId}/message/{messageId}", h.AuthUser(h.RateLimiter(http.HandlerFunc(h.editMessage))))
	mux.Handle("DELETE /api/v1/channel/{channelId}/message/{messageId}", h.AuthUser(h.RateLimiter(http.HandlerFunc(h.deleteMessage))))
	mux.Handle("GET /api/v1/channel/{channelId}/messages", h.AuthUser(h.AuthSessionId(h.HasChannelAccess(http.HandlerFunc(h.getMessages)))))

	// chat typing
	mux.Handle("POST /api/v1/channel/{channelId}/typing/{action}", h.AuthUser(h.HasChannelAccess(http.HandlerFunc(h.typing))))

	// file serving
	mux.Handle("GET /avatars/{fileName}", h.AuthUser(http.HandlerFunc(h.serveAvatars)))
	mux.Handle("GET /attachments/{fileName}", h.AuthUser(http.HandlerFunc(h.serveAttachments)))
	mux.Handle("GET /svelte/", http.StripPrefix("/svelte/", http.FileServer(http.Dir("public/svelte/dist"))))
	mux.Handle("GET /flutter/", http.StripPrefix("/flutter/", http.FileServer(http.Dir("public/flutter/web"))))

	var handler http.Handler = mux

	// logger middleware
	if printRequests, _ := strconv.ParseBool(os.Getenv("PRINT_REQUESTS")); printRequests {
		handler = RequestPrinter(mux)
	}

	// panic recoverer middleware
	handler = Recoverer(handler)

	// cors
	corsOrigin := os.Getenv("CORS_ORIGIN")
	if corsOrigin != "" {
		slog.Info("CORS enabled for origin: " + corsOrigin)
		handler = Cors(handler, corsOrigin)
	}

	hostAddress := fmt.Sprintf("%s:%s", address, port)
	go func() {
		slog.Info("Listening on " + hostAddress)
		err = http.ListenAndServe(hostAddress, handler)
		if err != nil {
			slog.Error(err.Error())
			closeServer()
		}
	}()

	// handle shutdown
	<-ctx.Done()
	slog.Info("Shutting down server...")

	{
		slog.Info("Closing sqlite connections...")
		err := db.Close()
		if err != nil {
			slog.Error(err.Error())
		}
	}
}
