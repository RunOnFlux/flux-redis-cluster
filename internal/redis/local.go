package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

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
