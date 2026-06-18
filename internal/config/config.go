package config

import (
	"os"
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
