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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

func main() {
	// handle shutdowns/panics gracefully
	ctx, cancel := context.WithCancel(context.Background())

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signalChan
		fmt.Println()
		log.Printf("Received signal (%v)\n", sig)
		cancel()
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

	postgresUrl := os.Getenv("POSTGRES_URL")
	if postgresUrl == "" {
		log.Fatal("POSTGRES_URL is missing from env or empty")
	}

	redisAddress := os.Getenv("REDIS_ADDRESS")
	if redisAddress == "" {
		log.Fatal("REDIS_ADDRESS is missing from env or empty")
	}

	redisPassword := os.Getenv("REDIS_PASSWORD")

	natsUrl := os.Getenv("NATS_URL")
	if natsUrl == "" {
		log.Fatal("NATS_URL is missing from env or empty")
	}

	// PostgreSQL
	db, err := pgxpool.New(context.Background(), postgresUrl)
	if err != nil {
		log.Println("Database connection pool create error:")
		log.Fatal(err)
	}

	err = db.Ping(context.Background())
	if err != nil {
		log.Println("Database ping error:")
		log.Fatal(err)
	}
	log.Println("Connected to PostgreSQL!")

	err = initDatabaseCommands(db)
	if err != nil {
		log.Fatal(err)
	}

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddress,
		Password: redisPassword,
		DB:       0,
		Protocol: 2,
	})

	err = rdb.Ping(context.Background()).Err()
	if err != nil {
		log.Println("Redis ping error:")
		log.Fatal(err)
	}
	log.Println("Connected to Redis!")

	// NATS
	nats, err := nats.Connect(natsUrl)
	if err != nil {
		log.Println("NATS connect error:")
		log.Fatal(err)
	}
	log.Println("Connected to NATS!")

	// look for a not yet claimed snowflake node id in redis
	snowflake.Epoch = 1772841600
	nodeId, err := claimSnowflakeNodeId(rdb, cancel)
	if err != nil {
		log.Println("Snowflake node ID claim error:")
		log.Fatal(err)
	}
	idGen, err := snowflake.NewNode(nodeId)
	if err != nil {
		log.Println("Snowflake ID generator setup error:")
		log.Fatal(err)
	}

	sm := &SessionManager{db: db, rdb: rdb, ctx: ctx}

	// this is used to inject dependencies into handlers
	h := Handler{db: db, rdb: rdb, nats: nats, idGen: idGen, sm: sm, cancel: cancel}

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
		// upload_user_avatar
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
			cancel()
		}
	}()

	// handle shutdown
	<-ctx.Done()
	log.Println("Shutting down server...")

	// delete the snowflake node id claim from redis
	if nodeId != -1 {
		err = removeSnowflakeNodeIdClaim(nodeId, rdb)
		if err != nil {
			log.Println("Error releasing snowflake node ID claim from redis:")
			log.Println(err)
		}
	}

	// close down connections
	if rdb != nil {
		log.Println("Closing redis connection...")
		err = rdb.Close()
		if err != nil {
			log.Println("Error closing redis connection:")
			log.Println(err)
		}
	}
	if db != nil {
		log.Println("Closing postgres connection pool...")
		db.Close()
	}

	if nats != nil {
		log.Println("Closing NATS connection...")
		nats.Close()
	}
}
