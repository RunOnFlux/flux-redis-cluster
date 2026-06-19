package main

import (
	"context"
	"log"
	"reflect"
	"sort"
	"time"

	"flux-redis-cluster/internal/config"
	"flux-redis-cluster/internal/fluxapi"
	"flux-redis-cluster/internal/redis"
)

func runDaemon(args []string) {
	cfg := config.FromEnv()

	localClient, err := redis.NewLocalClient(cfg.RedisPassword, cfg.SentinelPassword)
	if err != nil {
		log.Fatalf("Failed to initialize local redis client: %v", err)
	}

	fluxClient := fluxapi.New(cfg.FluxAPIURL)
	var knownIPs []string

	log.Printf("Starting updater daemon for %s", cfg.AppName)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		ips, err := fluxClient.ListIPs(ctx, cfg.AppName)
		cancel()

		if err != nil {
			log.Printf("Failed to get IPs from Flux API: %v", err)
		} else {
			sort.Strings(ips)
			if len(knownIPs) > 0 && !reflect.DeepEqual(knownIPs, ips) {
				log.Printf("Topology changed: %v -> %v", knownIPs, ips)

				removed := false
				for _, kip := range knownIPs {
					found := false
					for _, ip := range ips {
						if ip == kip {
							found = true
							break
						}
					}
					if !found {
						removed = true
						break
					}
				}

				if removed {
					log.Printf("Node(s) removed. Resetting local Sentinel state...")
					sCtx, sCancel := context.WithTimeout(context.Background(), 5*time.Second)
					if err := localClient.ResetSentinel(sCtx, cfg.AppName); err != nil {
						log.Printf("Error resetting Sentinel: %v", err)
					} else {
						log.Printf("Sentinel reset successfully")
					}
					sCancel()
				}
			}
			knownIPs = ips
		}

		// Removed manual Redis role management. Sentinel natively manages failover and replica roles.

		<-ticker.C
	}
}
