package main

import (
	"context"
	"fmt"
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
			}

			reconcileTopology(cfg, localClient, ips)

			knownIPs = ips
		}

		// Removed manual Redis role management. Sentinel natively manages failover and replica roles.

		<-ticker.C
	}
}

func reconcileTopology(cfg *config.Config, localClient *redis.LocalClient, ips []string) {
	if len(ips) == 0 {
		log.Printf("Recovery skipped: Flux returned no surviving nodes")
		return
	}

	sCtx, sCancel := context.WithTimeout(context.Background(), 5*time.Second)
	master, masterErr := localClient.SentinelMaster(sCtx, cfg.AppName)
	sCancel()
	masterIP := ""
	if masterErr == nil {
		masterIP = master["ip"]
	}

	// Native Sentinel owns ordinary failover. The agent intervenes only when
	// the configured master has been authoritatively removed from Flux (or the
	// local monitor disappeared), which avoids promoting during a mere network
	// partition.
	if masterIP == "" || !containsIP(ips, masterIP) {
		if masterErr != nil {
			log.Printf("Local Sentinel has no usable master monitor: %v", masterErr)
		} else {
			log.Printf("Local Sentinel master %s is no longer in Flux locations; bootstrapping from survivors", masterIP)
		}
		rCtx, rCancel := context.WithTimeout(context.Background(), 12*time.Second)
		err := recoverFromSurvivors(rCtx, cfg, localClient, ips)
		rCancel()
		if err != nil {
			log.Printf("Survivor recovery deferred: %v", err)
		}
		return
	}

	// A removed replica or Sentinel can safely be forgotten once the monitored
	// master is still part of the authoritative topology.
	sCtx, sCancel = context.WithTimeout(context.Background(), 5*time.Second)
	sentinelIPs, sentinelErr := localClient.SentinelKnownIPs(sCtx, cfg.AppName)
	sCancel()
	if sentinelErr != nil {
		log.Printf("Failed to inspect local Sentinel state: %v", sentinelErr)
		return
	}
	if stale := staleIPs(sentinelIPs, ips); len(stale) > 0 {
		log.Printf("Local Sentinel has stale non-master node IP(s) not in Flux locations: %s. Resetting local Sentinel state...", strings.Join(stale, ", "))
		resetLocalSentinel(localClient, cfg.AppName)
	}
}

func recoverFromSurvivors(
	ctx context.Context,
	cfg *config.Config,
	localClient *redis.LocalClient,
	ips []string,
) error {
	states, probeErrs := redis.ProbeRedisNodes(ctx, cfg, ips)
	if len(states) != len(ips) {
		return fmt.Errorf("only %d/%d advertised Redis survivors reachable; waiting for Flux to remove unavailable nodes (errors: %v)", len(states), len(ips), probeErrs)
	}

	candidate, err := selectRecoveryCandidate(states)
	if err != nil {
		return err
	}
	log.Printf(
		"Recovery candidate selected: %s role=%s offset=%d replid=%s",
		candidate.IP, candidate.Role, candidate.Offset, candidate.ReplicationID,
	)

	if err := redis.PromoteRedisNode(ctx, cfg, candidate.IP); err != nil {
		return fmt.Errorf("promote %s: %w", candidate.IP, err)
	}
	for _, state := range states {
		if state.IP == candidate.IP {
			continue
		}
		if err := redis.ConfigureRedisReplica(ctx, cfg, state.IP, candidate.IP); err != nil {
			log.Printf("Recovery: failed to point %s at %s: %v", state.IP, candidate.IP, err)
		}
	}

	quorum := 2
	if len(ips) < quorum {
		quorum = len(ips)
	}
	if err := localClient.ReconfigureSentinel(
		ctx,
		cfg.AppName,
		candidate.IP,
		cfg.HostRedisPort,
		quorum,
		cfg.RedisPassword,
		cfg.ConfigCommandName,
	); err != nil {
		return fmt.Errorf("reconfigure local Sentinel: %w", err)
	}
	log.Printf("Recovery complete: local Sentinel now monitors %s:%d with quorum %d", candidate.IP, cfg.HostRedisPort, quorum)
	return nil
}

func selectRecoveryCandidate(states []redis.NodeState) (redis.NodeState, error) {
	if len(states) == 0 {
		return redis.NodeState{}, fmt.Errorf("no reachable Redis survivors")
	}
	sorted := append([]redis.NodeState(nil), states...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Offset != sorted[j].Offset {
			return sorted[i].Offset > sorted[j].Offset
		}
		if (sorted[i].Role == "master") != (sorted[j].Role == "master") {
			return sorted[i].Role == "master"
		}
		return sorted[i].IP < sorted[j].IP
	})
	return sorted[0], nil
}

func containsIP(ips []string, wanted string) bool {
	for _, ip := range ips {
		if ip == wanted {
			return true
		}
	}
	return false
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
