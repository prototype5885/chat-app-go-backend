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
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
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
		address = "127.0.0.1"
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

	sm := &SessionManager{db: db, ctx: ctx}

	// this is used to inject dependencies into handlers
	h := Handler{db: db, idGen: idGen, sm: sm}

	// setup http server
	router := chi.NewRouter()
	if printRequests, _ := strconv.ParseBool(os.Getenv("PRINT_REQUESTS")); printRequests {
		router.Use(middleware.Logger)
	}
	router.Use(middleware.Recoverer)

	router.Route("/api/v1", func(v1 chi.Router) {
		v1.Get("/test", h.test)
		v1.Post("/user/register", h.register)
		v1.Post("/user/login", h.login)
		v1.With(h.AuthUserMw).Get("/test_auth", h.testAuth)
		v1.With(h.AuthUserMw).Get("/sse", h.session)
		v1.With(h.AuthUserMw).Get("/session", h.session)
		v1.With(h.AuthUserMw).Get("/user/logout", h.logout)
		v1.With(h.AuthUserMw).Delete("/user/delete", h.delete)
		v1.With(h.AuthUserMw).Get("/user_id", h.getUserID)
		v1.With(h.AuthUserMw).Get("/user", h.getUserInfo)
		v1.With(h.AuthUserMw).Patch("/user", h.updateUserInfo)
		v1.With(h.AuthUserMw).Post("/user/upload/avatar", h.uploadUserAvatar)
		v1.With(h.AuthUserMw).Post("/server", h.createServer)
		v1.With(h.AuthUserMw, h.HasServerAccessMw).Get("/server/{serverId}", h.getServerInfo)
		v1.With(h.AuthUserMw, h.IsServerOwnerMw).Patch("/server/{serverId}", h.updateServerInfo)
		v1.With(h.AuthUserMw, h.IsServerOwnerMw).Post("/server/{serverId}/upload/avatar", h.uploadServerAvatar)
		v1.With(h.AuthUserMw).Get("/servers", h.getServers)
		v1.With(h.AuthUserMw, h.IsServerOwnerMw).Delete("/server/{serverId}", h.deleteServer)
		v1.With(h.AuthUserMw, h.IsServerOwnerMw).Post("/server/{serverId}/channel", h.createChannel)
		v1.With(h.AuthUserMw, h.IsChannelOwnerMw).Get("/channel/{channelId}", h.getChannelInfo)
		v1.With(h.AuthUserMw, h.IsChannelOwnerMw).Patch("/channel/{channelId}", h.updateChannelInfo)
		v1.With(h.AuthUserMw, h.HasServerAccessMw).Get("/server/{serverId}/channels", h.getChannels)
		v1.With(h.AuthUserMw, h.IsChannelOwnerMw).Delete("/channel/{channelId}", h.deleteChannel)
		v1.With(h.AuthUserMw, h.HasServerAccessMw).Get("/server/{serverId}/members", h.getMembers)
		v1.With(h.AuthUserMw, h.HasChannelAccessMw).Post("/channel/{channelId}/message", h.createMessage)
		v1.With(h.AuthUserMw).Patch("/message/{messageId}", h.editMessage)
		// delete_message
		v1.With(h.AuthUserMw, h.HasChannelAccessMw).Get("/channel/{channelId}/messages", h.getMessages)
		// typing
		// get_attachment
		v1.Get("/test/{name}", h.testName)
		// v1.Get("/messages", handleMessages)
		// v1.With(AuthUserMw).Post("/server", handleCreateServer)
	})
	router.With(h.AuthUserMw).Get("/avatars/{fileName}", h.serveAvatars)

	hostAddress := fmt.Sprintf("%s:%s", address, port)
	go func() {
		slog.Info("Listening on " + hostAddress)
		err = http.ListenAndServe(hostAddress, router)
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
