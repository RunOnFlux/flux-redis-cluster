package config

import (
	"os"
	"strconv"
)

// RedisTarget returns the host and port to reach Redis on the node at ip.
// Local probes use the container port; remote probes use the host-mapped port.
func (c *Config) RedisTarget(ip string) (host string, port int) {
	host = ip
	port = c.HostRedisPort
	if c.MyIP != "" && ip == c.MyIP {
		host = "127.0.0.1"
		port = c.RedisPort
	}
	return host, port
}

// SentinelTarget returns the host and port to reach Sentinel on the node at ip.
func (c *Config) SentinelTarget(ip string) (host string, port int) {
	host = ip
	port = c.HostSentinelPort
	if c.MyIP != "" && ip == c.MyIP {
		host = "127.0.0.1"
		port = c.SentinelPort
	}
	return host, port
}

// SentinelMasterEndpoint returns the host/port Sentinel should use to monitor the master.
func (c *Config) SentinelMasterEndpoint(masterIP string) (host string, port int) {
	return masterIP, c.HostRedisPort
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
