package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

func removeSnowflakeNodeIdClaim(nodeId int64, rdb *redis.Client) error {
	log.Printf("Releasing claim on snowflake node ID (%d) from Redis...\n", nodeId)
	key := fmt.Sprintf("%s:%d", snowflakeNodeKeyprefix, nodeId)
	err := rdb.Del(context.Background(), key).Err()
	return err
}

func claimSnowflakeNodeId(rdb *redis.Client, cancel context.CancelFunc) (int64, error) {
	const duration = 15 * time.Minute
	const renewalInterval = 10 * time.Minute
	const maxNodes int64 = 1024

	var nodeId int64 = -1
	for i := range maxNodes {
		key := fmt.Sprintf("%s:%d", snowflakeNodeKeyprefix, i)
		success, err := rdb.SetNX(context.Background(), key, "1", duration).Result()
		if err != nil {
			return nodeId, err
		}

		if success {
			nodeId = i
			log.Printf("Claimed snowflake node ID (%d) in Redis\n", nodeId)
			break
		}
	}

	if nodeId == -1 {
		return nodeId, fmt.Errorf("All %d snowflake node IDs are currently in use!\n", maxNodes)
	}

	go func() {
		key := fmt.Sprintf("%s:%d", snowflakeNodeKeyprefix, nodeId)
		for {
			time.Sleep(renewalInterval)

			err := rdb.Expire(context.Background(), key, duration).Err()
			if err != nil {
				log.Printf("FATAL: Failed to renew expiration for snowflake node id %d in Redis: %v\n", nodeId, err)
				cancel()
			}
		}
	}()

	return nodeId, nil
}
