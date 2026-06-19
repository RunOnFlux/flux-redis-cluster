bind 0.0.0.0
port 0
tls-port 26379

tls-cert-file /etc/ssl/cluster/sentinel/server.crt
tls-key-file /etc/ssl/cluster/sentinel/server.key
tls-ca-cert-file /etc/ssl/cluster/ca/ca.crt
tls-auth-clients no
tls-replication yes




dir /var/lib/redis/data
loglevel debug

# Master info will be configured by flux-agent
sentinel monitor {{ .ClusterName }} {{ .MasterIP }} {{ .MasterPort }} 2
sentinel auth-pass {{ .ClusterName }} {{ .RedisPassword }}
sentinel down-after-milliseconds {{ .ClusterName }} 5000
sentinel failover-timeout {{ .ClusterName }} 10000
sentinel parallel-syncs {{ .ClusterName }} 1
sentinel rename-command {{ .ClusterName }} CONFIG {{ .ConfigCommandName }}

requirepass {{ .SentinelPassword }}
