package redis

import (
	"bytes"
	"os"
	"text/template"

	"flux-redis-cluster/internal/config"
)

type TemplateData struct {
	ClusterName       string
	MasterIP          string
	RedisPassword     string
	SentinelPassword  string
	ConfigCommandName string
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

	td := TemplateData{
		ClusterName:       cfg.AppName,
		MasterIP:          masterIP,
		RedisPassword:     cfg.RedisPassword,
		SentinelPassword:  cfg.SentinelPassword,
		ConfigCommandName: cfg.ConfigCommandName,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, td); err != nil {
		return err
	}

	// Add replicaof directive if we are not the master
	if masterIP != cfg.MyIP && outPath == "/etc/redis/redis.conf" {
		buf.WriteString("\nreplicaof " + masterIP + " 6379\n")
	}

	return os.WriteFile(outPath, buf.Bytes(), 0644)
}
