package redis

import (
	"bytes"
	"fmt"
	"os"
	"text/template"

	"flux-redis-cluster/internal/config"
)

type TemplateData struct {
	ClusterName          string
	MasterIP             string
	MasterPort           int
	AnnounceIP           string
	AnnounceRedisPort    int
	AnnounceSentinelPort int
	RedisPassword        string
	SentinelPassword     string
	ConfigCommandName    string
}

func RenderConfig(inPath, outPath string, cfg *config.Config, masterIP string) error {
	data, err := os.ReadFile(inPath)
	if err != nil {
		return err
	}

	tmpl, err := template.New("config").Parse(string(data))
	if err != nil {
		return err
	}

	masterHost, masterPort := cfg.SentinelMasterEndpoint(masterIP)
	td := TemplateData{
		ClusterName:          cfg.AppName,
		MasterIP:             masterHost,
		MasterPort:           masterPort,
		AnnounceIP:           cfg.MyIP,
		AnnounceRedisPort:    cfg.HostRedisPort,
		AnnounceSentinelPort: cfg.HostSentinelPort,
		RedisPassword:        cfg.RedisPassword,
		SentinelPassword:     cfg.SentinelPassword,
		ConfigCommandName:    cfg.ConfigCommandName,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, td); err != nil {
		return err
	}

	// Add replicaof directive if we are not the master
	if masterIP != cfg.MyIP && outPath == "/etc/redis/redis.conf" {
		buf.WriteString(fmt.Sprintf("\nreplicaof %s %d\n", masterIP, cfg.HostRedisPort))
	}

	return os.WriteFile(outPath, buf.Bytes(), 0644)
}
