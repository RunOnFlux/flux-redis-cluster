package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"flux-redis-cluster/internal/config"
	"flux-redis-cluster/internal/redis"
)

func runHealth(args []string) {
	fs := flag.NewFlagSet("health", flag.ExitOnError)
	timeout := fs.Duration("timeout", 5*time.Second, "health check timeout")
	_ = fs.Parse(args)

	cfg := config.Load()
	localClient, err := redis.NewLocalClient(cfg.RedisPassword, cfg.SentinelPassword)
	if err != nil {
		log.Fatalf("health: failed to initialize local redis client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if err := checkLocalClusterHealth(ctx, localClient, cfg.AppName); err != nil {
		log.Fatalf("health: unhealthy: %v", err)
	}
	log.Printf("health: ok")
}

func checkLocalClusterHealth(ctx context.Context, localClient *redis.LocalClient, appName string) error {
	if err := localClient.PingRedis(ctx); err != nil {
		return fmt.Errorf("local Redis PING failed: %w", err)
	}
	if err := localClient.PingSentinel(ctx); err != nil {
		return fmt.Errorf("local Sentinel PING failed: %w", err)
	}

	master, err := localClient.SentinelMaster(ctx, appName)
	if err != nil {
		return fmt.Errorf("Sentinel master lookup failed: %w", err)
	}
	masterIP := master["ip"]
	masterPort := master["port"]
	if masterIP == "" || masterPort == "" {
		return fmt.Errorf("Sentinel master has incomplete address: ip=%q port=%q", masterIP, masterPort)
	}

	flags := master["flags"]
	if hasAnyFlag(flags, "s_down", "o_down", "disconnected") {
		return fmt.Errorf("Sentinel master %s:%s has unhealthy flags: %s", masterIP, masterPort, flags)
	}

	quorum, _ := strconv.Atoi(master["quorum"])
	otherSentinels, _ := strconv.Atoi(master["num-other-sentinels"])
	if quorum > 0 && otherSentinels+1 < quorum {
		return fmt.Errorf("Sentinel quorum unavailable: have=%d required=%d", otherSentinels+1, quorum)
	}

	return nil
}

func hasAnyFlag(flags string, want ...string) bool {
	parts := strings.Split(flags, ",")
	seen := map[string]bool{}
	for _, part := range parts {
		seen[strings.TrimSpace(part)] = true
	}
	for _, flag := range want {
		if seen[flag] {
			return true
		}
	}
	return false
}
