package main

import (
	"context"
	"log"
	"reflect"
	"sort"
	"strings"
	"time"

	"flux-redis-cluster/internal/config"
	"flux-redis-cluster/internal/fluxapi"
	"flux-redis-cluster/internal/redis"
)

func runDaemon(args []string) {
	cfg := config.Load()

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

				if hasRemovedIP(knownIPs, ips) {
					log.Printf("Node(s) removed. Resetting local Sentinel state...")
					resetLocalSentinel(localClient, cfg.AppName)
				}
			}

			sCtx, sCancel := context.WithTimeout(context.Background(), 5*time.Second)
			sentinelIPs, sentinelErr := localClient.SentinelKnownIPs(sCtx, cfg.AppName)
			sCancel()
			if sentinelErr != nil {
				log.Printf("Failed to inspect local Sentinel state: %v", sentinelErr)
			} else if stale := staleIPs(sentinelIPs, ips); len(stale) > 0 {
				log.Printf("Local Sentinel has stale node IP(s) not in Flux locations: %s. Resetting local Sentinel state...", strings.Join(stale, ", "))
				resetLocalSentinel(localClient, cfg.AppName)
			}

			knownIPs = ips
		}

		// Removed manual Redis role management. Sentinel natively manages failover and replica roles.

		<-ticker.C
	}
}

func resetLocalSentinel(localClient *redis.LocalClient, appName string) {
	sCtx, sCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer sCancel()
	if err := localClient.ResetSentinel(sCtx, appName); err != nil {
		log.Printf("Error resetting Sentinel: %v", err)
	} else {
		log.Printf("Sentinel reset successfully")
	}
}

func hasRemovedIP(oldIPs, newIPs []string) bool {
	return len(staleIPs(oldIPs, newIPs)) > 0
}

func staleIPs(candidateIPs, currentIPs []string) []string {
	current := map[string]bool{}
	for _, ip := range currentIPs {
		current[ip] = true
	}

	var stale []string
	for _, ip := range candidateIPs {
		if !current[ip] {
			stale = append(stale, ip)
		}
	}
	sort.Strings(stale)
	return stale
}
