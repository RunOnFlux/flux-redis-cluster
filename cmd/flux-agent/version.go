package main

import (
	"log"
	"os"
	"strings"
)

func agentVersion() string {
	if version != "" && version != "dev" {
		return version
	}
	if data, err := os.ReadFile("/app/VERSION"); err == nil {
		if v := strings.TrimSpace(string(data)); v != "" {
			return v
		}
	}
	return "dev"
}

func logAgentVersion(component string) {
	log.Printf("flux-redis-cluster flux-agent %s version=%s", component, agentVersion())
}
