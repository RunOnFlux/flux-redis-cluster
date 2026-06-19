package config

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type Config struct {
	AppName           string
	MyIP              string
	FluxAPIURL        string
	RedisPassword     string
	SentinelPassword  string
	SSLPassphrase     string
	ClusterIPs        []string
	ConfigCommandName string
}

func FromEnv() *Config {
	c := &Config{
		AppName:           os.Getenv("APP_NAME"),
		MyIP:              os.Getenv("MY_IP"),
		FluxAPIURL:        os.Getenv("FLUX_API_URL"),
		RedisPassword:     os.Getenv("REDIS_PASSWORD"),
		SentinelPassword:  os.Getenv("SENTINEL_PASSWORD"),
		SSLPassphrase:     os.Getenv("SSL_PASSPHRASE"),
		ConfigCommandName: os.Getenv("CONFIG_COMMAND_NAME"),
	}
	if c.MyIP == "" {
		ip, err := discoverMyIP(c)
		if err == nil {
			c.MyIP = ip
		}
	}
	if c.AppName == "" {
		c.AppName = "flux-redis-cluster"
	}
	if c.FluxAPIURL == "" {
		c.FluxAPIURL = "http://localhost:8080"
	}
	if c.ConfigCommandName == "" {
		c.ConfigCommandName = "FLUX_CONFIG" // renamed from CONFIG
	}
	return c
}

func discoverMyIP(cfg *Config) (string, error) {
	host, _ := os.Hostname()
	// Local testing shortcut: if FLUX_API_URL points at the test mock
	if strings.Contains(cfg.FluxAPIURL, "172.20.0.5") {
		switch host {
		case "redis-cluster-node1":
			return "172.20.0.10", nil
		case "redis-cluster-node2":
			return "172.20.0.11", nil
		case "redis-cluster-node3":
			return "172.20.0.12", nil
		}
		if ip := pickIPInSubnet("172.20."); ip != "" {
			return ip, nil
		}
		if ip := pickFirstNonLoopbackIP(); ip != "" {
			return ip, nil
		}
		return "", errors.New("no IP discovered (local testing mode)")
	}

	// Production: query external echo services
	for _, url := range []string{"http://ifconfig.me", "http://ipinfo.io/ip"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			return ip, nil
		}
	}
	// Fall back to first non-loopback
	if ip := pickFirstNonLoopbackIP(); ip != "" {
		return ip, nil
	}
	return "", errors.New("no IP discovered (production mode)")
}

func pickIPInSubnet(prefix string) string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ip, _, err := net.ParseCIDR(a.String())
		if err != nil {
			continue
		}
		v4 := ip.To4()
		if v4 != nil && strings.HasPrefix(v4.String(), prefix) {
			return v4.String()
		}
	}
	return ""
}

func pickFirstNonLoopbackIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ip, _, err := net.ParseCIDR(a.String())
		if err != nil {
			continue
		}
		if ip.IsLoopback() {
			continue
		}
		v4 := ip.To4()
		if v4 != nil {
			return v4.String()
		}
	}
	return ""
}
