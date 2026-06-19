package config

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const ClusterEnvFile = "/etc/cluster_env"

type Config struct {
	AppName           string
	MyIP              string
	FluxAPIURL        string
	RedisPassword     string
	SentinelPassword  string
	SSLPassphrase     string
	ClusterIPs        []string
	ConfigCommandName string
	HostRedisPort     int
	HostSentinelPort  int
	RedisPort         int
	SentinelPort      int
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
		ip, err := DiscoverMyIP(c, 0)
		if err == nil {
			c.MyIP = ip
		}
	}
	if c.AppName == "" {
		// Resolved in init via DiscoverAppName(); do not default here.
	}
	if c.RedisPassword == "" {
		c.RedisPassword = "secret"
	}
	if c.SentinelPassword == "" {
		c.SentinelPassword = c.RedisPassword
	}
	if c.SSLPassphrase == "" {
		c.SSLPassphrase = "secretseed"
	}
	if c.FluxAPIURL == "" {
		c.FluxAPIURL = "https://api.runonflux.io"
	}
	if c.ConfigCommandName == "" {
		c.ConfigCommandName = "FLUX_CONFIG" // renamed from CONFIG
	}
	c.HostRedisPort = envInt("HOST_REDIS_PORT", 6379)
	c.HostSentinelPort = envInt("HOST_SENTINEL_PORT", 26379)
	c.RedisPort = envInt("REDIS_PORT", 6379)
	c.SentinelPort = envInt("SENTINEL_PORT", 26379)
	return c
}

// Load merges environment defaults with values persisted by init in
// /etc/cluster_env (APP_NAME, MY_IP).
func Load() *Config {
	c := FromEnv()
	_ = LoadClusterEnv(c)
	if c.AppName == "" {
		name, _ := DiscoverAppName(10 * time.Second)
		c.AppName = name
	}
	return c
}

type hostinfoResp struct {
	Status string `json:"status"`
	Data   struct {
		AppName string `json:"appName"`
		IP      string `json:"ip"`
	} `json:"data"`
}

func fetchHostinfo() (*hostinfoResp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://fluxnode.service:16101/hostinfo", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var parsed hostinfoResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if parsed.Status != "success" {
		return nil, errors.New("hostinfo: non-success status")
	}
	return &parsed, nil
}

// AppNameFromHostinfo queries the Flux node hostinfo API for the running app name.
func AppNameFromHostinfo() string {
	info, err := fetchHostinfo()
	if err != nil || info == nil {
		return ""
	}
	return info.Data.AppName
}

// IPFromHostinfo returns this node's public IP from the Flux hostinfo API.
func IPFromHostinfo() string {
	info, err := fetchHostinfo()
	if err != nil || info == nil {
		return ""
	}
	ip := strings.TrimSpace(info.Data.IP)
	if net.ParseIP(ip) != nil {
		return ip
	}
	return ""
}

// ResolveAppName returns APP_NAME from env or Flux hostinfo (single attempt).
func ResolveAppName() string {
	name, _ := DiscoverAppName(0)
	return name
}

// DiscoverAppName resolves APP_NAME from env or Flux hostinfo with optional retry.
// When maxWait is zero, hostinfo is queried once. Returns the fallback default
// only after retries are exhausted.
func DiscoverAppName(maxWait time.Duration) (string, error) {
	if v := os.Getenv("APP_NAME"); v != "" {
		return v, nil
	}

	deadline := time.Now()
	if maxWait > 0 {
		deadline = deadline.Add(maxWait)
	}
	delay := 1 * time.Second
	var lastErr error

	for {
		if v := AppNameFromHostinfo(); v != "" {
			return v, nil
		}
		lastErr = errors.New("fluxnode hostinfo unavailable or appName empty")

		if maxWait <= 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(delay)
		if delay < 5*time.Second {
			delay += 1 * time.Second
		}
	}

	return "flux-redis-cluster", lastErr
}

func LoadClusterEnv(c *Config) error {
	f, err := os.Open(ClusterEnvFile)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "APP_NAME":
			c.AppName = val
		case "MY_IP":
			c.MyIP = val
		case "HOST_REDIS_PORT":
			if n, err := strconv.Atoi(val); err == nil {
				c.HostRedisPort = n
			}
		case "HOST_SENTINEL_PORT":
			if n, err := strconv.Atoi(val); err == nil {
				c.HostSentinelPort = n
			}
		}
	}
	return scanner.Err()
}

func (c *Config) WriteClusterEnv() error {
	lines := []string{
		"APP_NAME=" + c.AppName,
		"MY_IP=" + c.MyIP,
		"HOST_REDIS_PORT=" + strconv.Itoa(c.HostRedisPort),
		"HOST_SENTINEL_PORT=" + strconv.Itoa(c.HostSentinelPort),
	}
	return os.WriteFile(ClusterEnvFile, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func discoverMyIPOnce(cfg *Config) (string, error) {
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

	if ip := IPFromHostinfo(); ip != "" {
		return ip, nil
	}
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
	if ip := pickFirstNonLoopbackIP(); ip != "" {
		return ip, nil
	}
	return "", errors.New("no IP discovered (production mode)")
}

// DiscoverMyIP resolves this node's public IP with optional retry (hostinfo first on Flux).
func DiscoverMyIP(cfg *Config, maxWait time.Duration) (string, error) {
	if cfg.MyIP != "" {
		return cfg.MyIP, nil
	}
	if ip, err := discoverMyIPOnce(cfg); err == nil && ip != "" {
		return ip, nil
	}

	deadline := time.Now()
	if maxWait > 0 {
		deadline = deadline.Add(maxWait)
	}
	delay := 1 * time.Second
	var lastErr error
	for maxWait > 0 {
		if ip, err := discoverMyIPOnce(cfg); err == nil && ip != "" {
			return ip, nil
		} else if err != nil {
			lastErr = err
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(delay)
		if delay < 5*time.Second {
			delay += 1 * time.Second
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no IP discovered")
	}
	return "", lastErr
}

func discoverMyIP(cfg *Config) (string, error) {
	return DiscoverMyIP(cfg, 0)
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
