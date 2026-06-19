package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"flux-redis-cluster/internal/config"
	"flux-redis-cluster/internal/redis"
)

func runProxy(args []string) {
	fs := flag.NewFlagSet("proxy", flag.ExitOnError)
	listenAddr := fs.String("listen", ":6380", "listen address")
	_ = fs.Parse(args)

	cfg := config.FromEnv()
	localClient, err := redis.NewLocalClient(cfg.RedisPassword, cfg.SentinelPassword)
	if err != nil {
		log.Fatalf("proxy: failed to init local redis client: %v", err)
	}

	log.Printf("Starting flux-agent proxy on %s", *listenAddr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var primaryIP atomic.Value
	primaryIP.Store("")

	// Background polling of Sentinel for master IP
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		
		// Initial probe
		probeMaster(ctx, cfg, localClient, &primaryIP)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				probeMaster(ctx, cfg, localClient, &primaryIP)
			}
		}
	}()

	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("proxy: listen %s: %v", *listenAddr, err)
	}

	go func() {
		sigc := make(chan os.Signal, 1)
		signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
		<-sigc
		log.Printf("proxy: shutdown signal received")
		_ = listener.Close()
		cancel()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				log.Printf("proxy: accept error: %v", err)
				continue
			}
		}
		go handleProxyConn(conn, primaryIP.Load().(string), cfg.MyIP)
	}
}

func probeMaster(ctx context.Context, cfg *config.Config, client *redis.LocalClient, primaryIP *atomic.Value) {
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	ip, err := client.GetMasterFromLocalSentinel(pctx, cfg.AppName)
	if err != nil {
		return
	}

	prev, _ := primaryIP.Load().(string)
	if ip != prev {
		log.Printf("proxy: master changed: %q -> %q", prev, ip)
		primaryIP.Store(ip)
	}
}

func handleProxyConn(client net.Conn, primaryIP, myIP string) {
	defer client.Close()
	if primaryIP == "" {
		log.Printf("proxy: rejecting connection from %s - no master known yet", client.RemoteAddr())
		return
	}

	dialHost := primaryIP
	if myIP != "" && primaryIP == myIP {
		dialHost = "127.0.0.1"
	}

	target := fmt.Sprintf("%s:6379", dialHost)
	upstream, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		log.Printf("proxy: dial %s failed: %v", target, err)
		return
	}
	defer upstream.Close()

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, upstream); done <- struct{}{} }()
	<-done
}
