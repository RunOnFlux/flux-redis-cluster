package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sort"

	"github.com/redis/go-redis/v9"
)

type LocalClient struct {
	redis    *redis.Client
	sentinel *redis.Client
}

func NewLocalClient(redisPassword, sentinelPassword string) (*LocalClient, error) {
	caCert, err := os.ReadFile("/etc/ssl/cluster/ca/ca.crt")
	if err != nil {
		return nil, fmt.Errorf("read ca cert: %v", err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	redisCert, err := tls.LoadX509KeyPair("/etc/ssl/cluster/redis/server.crt", "/etc/ssl/cluster/redis/server.key")
	if err != nil {
		return nil, fmt.Errorf("load redis key pair: %v", err)
	}

	sentinelCert, err := tls.LoadX509KeyPair("/etc/ssl/cluster/sentinel/server.crt", "/etc/ssl/cluster/sentinel/server.key")
	if err != nil {
		return nil, fmt.Errorf("load sentinel key pair: %v", err)
	}

	redisTLS := &tls.Config{
		RootCAs:            caCertPool,
		Certificates:       []tls.Certificate{redisCert},
		InsecureSkipVerify: true,
	}

	sentinelTLS := &tls.Config{
		RootCAs:            caCertPool,
		Certificates:       []tls.Certificate{sentinelCert},
		InsecureSkipVerify: true,
	}

	rClient := redis.NewClient(&redis.Options{
		Addr:      "127.0.0.1:6379",
		Password:  redisPassword,
		TLSConfig: redisTLS,
	})

	sClient := redis.NewClient(&redis.Options{
		Addr:      "127.0.0.1:26379",
		Password:  sentinelPassword,
		TLSConfig: sentinelTLS,
	})

	return &LocalClient{
		redis:    rClient,
		sentinel: sClient,
	}, nil
}

func (c *LocalClient) ResetSentinel(ctx context.Context, appName string) error {
	return c.sentinel.Do(ctx, "SENTINEL", "RESET", appName).Err()
}

func (c *LocalClient) PingRedis(ctx context.Context) error {
	return c.redis.Ping(ctx).Err()
}

func (c *LocalClient) PingSentinel(ctx context.Context) error {
	return c.sentinel.Ping(ctx).Err()
}

func (c *LocalClient) SentinelMaster(ctx context.Context, appName string) (map[string]string, error) {
	res, err := c.sentinel.Do(ctx, "SENTINEL", "master", appName).Result()
	if err != nil {
		return nil, err
	}
	return sentinelFields(res)
}

func (c *LocalClient) SentinelKnownIPs(ctx context.Context, appName string) ([]string, error) {
	seen := map[string]bool{}

	master, err := c.sentinel.Do(ctx, "SENTINEL", "master", appName).Result()
	if err != nil {
		return nil, err
	}
	if fields, err := sentinelFields(master); err == nil {
		addSentinelIP(seen, fields)
	} else {
		return nil, err
	}

	for _, cmd := range []string{"replicas", "sentinels"} {
		res, err := c.sentinel.Do(ctx, "SENTINEL", cmd, appName).Result()
		if err != nil {
			return nil, err
		}
		items, ok := res.([]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid SENTINEL %s response", cmd)
		}
		for _, item := range items {
			fields, err := sentinelFields(item)
			if err != nil {
				continue
			}
			addSentinelIP(seen, fields)
		}
	}

	out := make([]string, 0, len(seen))
	for ip := range seen {
		out = append(out, ip)
	}
	sort.Strings(out)
	return out, nil
}

func addSentinelIP(seen map[string]bool, fields map[string]string) {
	if ip := fields["ip"]; ip != "" {
		seen[ip] = true
	}
}

func sentinelFields(value interface{}) (map[string]string, error) {
	switch fields := value.(type) {
	case []interface{}:
		return sentinelArrayFields(fields), nil
	case map[interface{}]interface{}:
		parsed := map[string]string{}
		for key, val := range fields {
			parsed[fmt.Sprint(key)] = fmt.Sprint(val)
		}
		return parsed, nil
	case map[string]interface{}:
		parsed := map[string]string{}
		for key, val := range fields {
			parsed[key] = fmt.Sprint(val)
		}
		return parsed, nil
	case map[string]string:
		return fields, nil
	default:
		return nil, fmt.Errorf("invalid SENTINEL response type %T", value)
	}
}

func sentinelArrayFields(fields []interface{}) map[string]string {
	parsed := map[string]string{}
	for i := 0; i+1 < len(fields); i += 2 {
		key, _ := fields[i].(string)
		if key == "" {
			continue
		}
		switch val := fields[i+1].(type) {
		case string:
			parsed[key] = val
		case []byte:
			parsed[key] = string(val)
		default:
			parsed[key] = fmt.Sprint(val)
		}
	}
	return parsed
}

func (c *LocalClient) EnsureReplicaOf(ctx context.Context, configCmdName, masterIP string) error {
	res, err := c.redis.Do(ctx, "ROLE").Result()
	if err != nil {
		return err
	}

	roleArr, ok := res.([]interface{})
	if !ok || len(roleArr) == 0 {
		return fmt.Errorf("invalid role response")
	}
	role, ok := roleArr[0].(string)
	if !ok {
		return fmt.Errorf("invalid role string")
	}

	if masterIP == "127.0.0.1" {
		if role != "master" {
			return c.redis.Do(ctx, "REPLICAOF", "NO", "ONE").Err()
		}
	} else {
		if role == "master" {
			return c.redis.Do(ctx, "REPLICAOF", masterIP, "6379").Err()
		}

		if len(roleArr) > 1 {
			currentMasterIP, _ := roleArr[1].(string)
			if currentMasterIP != masterIP {
				return c.redis.Do(ctx, "REPLICAOF", masterIP, "6379").Err()
			}
		}
	}
	return nil
}

func (c *LocalClient) GetMasterFromLocalSentinel(ctx context.Context, appName string) (string, error) {
	masterAddr, err := c.sentinel.Do(ctx, "SENTINEL", "get-master-addr-by-name", appName).Result()
	if err != nil {
		return "", err
	}
	addrArr, ok := masterAddr.([]interface{})
	if !ok || len(addrArr) < 2 {
		return "", fmt.Errorf("invalid master addr response")
	}
	ip, _ := addrArr[0].(string)
	return ip, nil
}
