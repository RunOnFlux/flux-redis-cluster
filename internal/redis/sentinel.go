package redis

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

func GetMasterFromSentinels(ips []string, appName string, password string) (string, error) {
	caCert, err := os.ReadFile("/etc/ssl/cluster/ca/ca.crt")
	if err != nil {
		return "", fmt.Errorf("read ca cert: %v", err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	cert, err := tls.LoadX509KeyPair("/etc/ssl/cluster/sentinel/server.crt", "/etc/ssl/cluster/sentinel/server.key")
	if err != nil {
		return "", fmt.Errorf("load key pair: %v", err)
	}

	tlsConfig := &tls.Config{
		RootCAs:            caCertPool,
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true, // We have mutual TLS, IP sans should match but just in case
	}

	for _, ip := range ips {
		client := redis.NewSentinelClient(&redis.Options{
			Addr:      fmt.Sprintf("%s:26379", ip),
			Password:  password,
			TLSConfig: tlsConfig,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		masterAddr, err := client.GetMasterAddrByName(ctx, appName).Result()
		cancel()
		client.Close()

		if err == nil && len(masterAddr) == 2 {
			return masterAddr[0], nil
		}
	}

	return "", fmt.Errorf("no master found in %d sentinels", len(ips))
}
