package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"flux-redis-cluster/internal/config"
	"flux-redis-cluster/internal/fluxapi"
	"flux-redis-cluster/internal/redis"
)

func runInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	redisTpl := fs.String("redis-template", "/app/redis.conf.tpl", "Redis config template")
	redisOut := fs.String("redis-out", "/etc/redis/redis.conf", "Redis config output")
	sentinelTpl := fs.String("sentinel-template", "/app/sentinel.conf.tpl", "Sentinel config template")
	sentinelOut := fs.String("sentinel-out", "/etc/redis/sentinel.conf", "Sentinel config output")
	certsScript := fs.String("certs-script", "/app/generate-certs.sh", "Shell script for cert generation")
	_ = fs.Parse(args)

	cfg := config.FromEnv()
	if cfg.MyIP == "" {
		log.Fatalf("MY_IP is required and could not be discovered")
	}

	log.Printf("Discovering cluster IPs for %s via %s", cfg.AppName, cfg.FluxAPIURL)
	c := fluxapi.New(cfg.FluxAPIURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	ips, err := c.ListIPs(ctx, cfg.AppName)
	cancel()

	if err != nil {
		log.Printf("Warning: failed to get IPs from Flux API: %v. Falling back to MY_IP", err)
	}

	have := false
	for _, ip := range ips {
		if ip == cfg.MyIP {
			have = true
			break
		}
	}
	if !have {
		ips = append(ips, cfg.MyIP)
	}
	sort.Strings(ips)
	cfg.ClusterIPs = ips

	log.Printf("Running certificate generation")
	if err := runCertsScript(*certsScript, cfg); err != nil {
		log.Fatalf("Certificate generation failed: %v", err)
	}

	log.Printf("Determining Master IP")
	var peerIPs []string
	for _, ip := range ips {
		if ip != cfg.MyIP {
			peerIPs = append(peerIPs, ip)
		}
	}

	masterIP := ""
	if len(peerIPs) == 0 {
		log.Printf("No other peers. I am the initial master.")
		masterIP = cfg.MyIP
	} else {
		log.Printf("Querying existing peers for master...")
		m, err := redis.GetMasterFromSentinels(peerIPs, cfg.AppName, cfg.SentinelPassword)
		if err != nil {
			log.Printf("Could not get master from peers: %v. Assuming lowest IP.", err)
			masterIP = ips[0]
		} else {
			log.Printf("Found existing master: %s", m)
			masterIP = m
		}
	}

	log.Printf("Rendering Redis templates")
	if err := os.MkdirAll(filepath.Dir(*redisOut), 0755); err != nil {
		log.Fatalf("mkdir redis dir: %v", err)
	}
	if err := redis.RenderConfig(*redisTpl, *redisOut, cfg, masterIP); err != nil {
		log.Fatalf("render redis config: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(*sentinelOut), 0755); err != nil {
		log.Fatalf("mkdir sentinel dir: %v", err)
	}
	if err := redis.RenderConfig(*sentinelTpl, *sentinelOut, cfg, masterIP); err != nil {
		log.Fatalf("render sentinel config: %v", err)
	}

	// Set correct ownership for configs
	_ = exec.Command("chown", "redis:redis", *redisOut).Run()
	_ = exec.Command("chown", "redis:redis", *sentinelOut).Run()

	// Set correct permissions
	_ = exec.Command("chmod", "600", *redisOut).Run()
	_ = exec.Command("chmod", "600", *sentinelOut).Run()

	log.Printf("Initialization complete")
}

func runCertsScript(script string, cfg *config.Config) error {
	cmd := exec.Command(script)
	cmd.Env = append(os.Environ(),
		"SSL_PASSPHRASE="+cfg.SSLPassphrase,
		"APP_NAME="+cfg.AppName,
		"MY_IP="+cfg.MyIP,
		"CLUSTER_IPS="+strings.Join(cfg.ClusterIPs, " "),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
