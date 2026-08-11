package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"flux-redis-cluster/internal/config"

	redislib "github.com/redis/go-redis/v9"
)

// NodeState is the minimum replication state needed to select a recovery
// master. Offset is comparable between nodes that share ReplicationID.
type NodeState struct {
	IP            string
	Role          string
	MasterIP      string
	LinkState     string
	ReplicationID string
	Offset        int64
}

func loadRedisTLSConfig() (*tls.Config, error) {
	caCert, err := os.ReadFile("/etc/ssl/cluster/ca/ca.crt")
	if err != nil {
		return nil, fmt.Errorf("read ca cert: %w", err)
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("parse ca cert")
	}

	cert, err := tls.LoadX509KeyPair(
		"/etc/ssl/cluster/redis/server.crt",
		"/etc/ssl/cluster/redis/server.key",
	)
	if err != nil {
		return nil, fmt.Errorf("load redis key pair: %w", err)
	}

	return &tls.Config{
		RootCAs:            caCertPool,
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	}, nil
}

func redisClientForNode(cfg *config.Config, ip string) (*redislib.Client, error) {
	tlsConfig, err := loadRedisTLSConfig()
	if err != nil {
		return nil, err
	}
	host, port := cfg.RedisTarget(ip)
	return redislib.NewClient(&redislib.Options{
		Addr:      fmt.Sprintf("%s:%d", host, port),
		Password:  cfg.RedisPassword,
		TLSConfig: tlsConfig,
	}), nil
}

// ProbeRedisNode returns the role and replication progress of a cluster node.
func ProbeRedisNode(ctx context.Context, cfg *config.Config, ip string) (NodeState, error) {
	client, err := redisClientForNode(cfg, ip)
	if err != nil {
		return NodeState{IP: ip}, err
	}
	defer client.Close()

	roleResult, err := client.Do(ctx, "ROLE").Result()
	if err != nil {
		return NodeState{IP: ip}, err
	}
	fields, ok := roleResult.([]interface{})
	if !ok || len(fields) < 2 {
		return NodeState{IP: ip}, fmt.Errorf("invalid ROLE response %T", roleResult)
	}

	state := NodeState{IP: ip, Role: valueString(fields[0])}
	switch state.Role {
	case "master":
		state.LinkState = "connected"
		state.Offset = valueInt64(fields[1])
	case "slave", "replica":
		if len(fields) < 5 {
			return NodeState{IP: ip}, fmt.Errorf("invalid replica ROLE response")
		}
		state.MasterIP = valueString(fields[1])
		state.LinkState = valueString(fields[3])
		state.Offset = valueInt64(fields[4])
	default:
		return NodeState{IP: ip}, fmt.Errorf("unsupported Redis role %q", state.Role)
	}

	if info, infoErr := client.Info(ctx, "replication").Result(); infoErr == nil {
		parsed := parseInfo(info)
		state.ReplicationID = parsed["master_replid"]
	}
	return state, nil
}

// ProbeRedisNodes probes all advertised nodes concurrently. Unreachable nodes
// are omitted and returned in the error map so a caller can require a complete,
// consistent survivor view before forcing recovery.
func ProbeRedisNodes(ctx context.Context, cfg *config.Config, ips []string) ([]NodeState, map[string]error) {
	type result struct {
		state NodeState
		err   error
	}
	results := make(chan result, len(ips))
	var wg sync.WaitGroup
	for _, ip := range ips {
		ip := ip
		wg.Add(1)
		go func() {
			defer wg.Done()
			state, err := ProbeRedisNode(ctx, cfg, ip)
			results <- result{state: state, err: err}
		}()
	}
	wg.Wait()
	close(results)

	states := make([]NodeState, 0, len(ips))
	errs := make(map[string]error)
	for result := range results {
		if result.err != nil {
			errs[result.state.IP] = result.err
			continue
		}
		states = append(states, result.state)
	}
	return states, errs
}

// PromoteRedisNode makes a reachable survivor writable. The command is
// idempotent, allowing every updater to converge on the same candidate.
func PromoteRedisNode(ctx context.Context, cfg *config.Config, ip string) error {
	client, err := redisClientForNode(cfg, ip)
	if err != nil {
		return err
	}
	defer client.Close()
	return client.Do(ctx, "REPLICAOF", "NO", "ONE").Err()
}

// ConfigureRedisReplica points a survivor at the newly selected master.
func ConfigureRedisReplica(ctx context.Context, cfg *config.Config, ip, masterIP string) error {
	client, err := redisClientForNode(cfg, ip)
	if err != nil {
		return err
	}
	defer client.Close()
	masterHost, masterPort := cfg.RedisTarget(masterIP)
	return client.Do(ctx, "REPLICAOF", masterHost, masterPort).Err()
}

func parseInfo(info string) map[string]string {
	parsed := make(map[string]string)
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			parsed[key] = value
		}
	}
	return parsed
}

func valueString(value interface{}) string {
	switch value := value.(type) {
	case string:
		return value
	case []byte:
		return string(value)
	default:
		return fmt.Sprint(value)
	}
}

func valueInt64(value interface{}) int64 {
	switch value := value.(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case uint64:
		return int64(value)
	case string:
		parsed, _ := strconv.ParseInt(value, 10, 64)
		return parsed
	case []byte:
		parsed, _ := strconv.ParseInt(string(value), 10, 64)
		return parsed
	default:
		parsed, _ := strconv.ParseInt(fmt.Sprint(value), 10, 64)
		return parsed
	}
}
