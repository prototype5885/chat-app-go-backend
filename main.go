package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/snowflake"
	"github.com/go-chi/chi/v5"
	"github.com/joho/godotenv"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	// handle shutdowns/panics gracefully
	ctx, closeServer := context.WithCancel(context.Background())

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signalChan
		fmt.Println()
		log.Printf("Received signal (%v)\n", sig)
		closeServer()
	}()

	// load env
	godotenv.Load()

	address := os.Getenv("ADDRESS")
	if address == "" {
		address = "127.0.0.1"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "1848"
	}

	db, dbTokens, err := initDatabase()
	if err != nil {
		log.Println("SQlite setup error")
		log.Fatal(err)
	}

	go databaseCleanerService(closeServer, db, dbTokens)

	snowflake.Epoch = 1772841600
	idGen, err := snowflake.NewNode(0)
	if err != nil {
		log.Println("Snowflake ID generator setup error:")
		log.Fatal(err)
	}

	sm := &SessionManager{db: db, ctx: ctx}

	// this is used to inject dependencies into handlers
	h := Handler{db: db, dbTokens: dbTokens, idGen: idGen, sm: sm, cancel: closeServer}

	// setup http server
	router := chi.NewRouter()
	// router.Use(SetHeaderMw)
	// router.Use(middleware.Logger)

	router.Route("/api/v1", func(v1 chi.Router) {
		v1.Get("/test", h.test)
		v1.With(h.AuthUserMw).Get("/test_auth", h.testAuth)
		v1.Get("/sse", h.session)
		v1.With(h.AuthUserMw).Get("/session", h.session)
		// session
		v1.Post("/user/register", h.register)
		v1.Post("/user/login", h.login)
		v1.With(h.AuthUserMw).Get("/user/logout", h.logout)
		v1.With(h.AuthUserMw).Delete("/user/delete", h.delete)
		v1.With(h.AuthUserMw).Get("/user_id", h.getUserID)
		v1.With(h.AuthUserMw).Get("/user", h.getUserInfo)
		v1.With(h.AuthUserMw).Patch("/user", h.updateUserInfo)
		v1.With(h.AuthUserMw).Post("/user/upload/avatar", h.uploadUserAvatar)
		v1.With(h.AuthUserMw).Post("/server", h.createServer)
		// get_server_info
		// update_server_info
		// upload_server_avatar
		v1.With(h.AuthUserMw).Get("/servers", h.getServers)
		// delete_server
		// create_channel
		// get_channel_info
		// update_channel_info
		v1.With(h.AuthUserMw, h.HasServerAccessMw).Get("/server/{serverId}/channels", h.getChannels)
		// delete_channel
		// get_members
		v1.With(h.AuthUserMw, h.HasChannelAccessMw).Post("/channel/{channelId}/message", h.createMessage)
		// edit_message
		// delete_message
		v1.With(h.AuthUserMw, h.HasChannelAccessMw).Get("/channel/{channelId}/messages", h.getMessages)
		// typing
		// serve_avatars
		// get_attachment
		v1.Get("/test/{name}", h.testName)
		// v1.Get("/messages", handleMessages)
		// v1.With(AuthUserMw).Post("/server", handleCreateServer)
	})

	hostAddress := fmt.Sprintf("%s:%s", address, port)
	go func() {
		log.Printf("Listening on %s\n", hostAddress)
		err = http.ListenAndServe(hostAddress, router)
		if err != nil {
			log.Println("Server error:")
			log.Println(err)
			closeServer()
		}
	}()

	// handle shutdown
	<-ctx.Done()
	log.Println("Shutting down server...")

	log.Println("Closing sqlite connections...")
	{
		err := db.Close()
		if err != nil {
			log.Println(err)
		}
	}
	{
		err := dbTokens.Close()
		if err != nil {
			log.Println(err)
		}
	}

}
