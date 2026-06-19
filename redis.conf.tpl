bind 0.0.0.0
port 0
tls-port 6379

tls-cert-file /etc/ssl/cluster/redis/server.crt
tls-key-file /etc/ssl/cluster/redis/server.key
tls-ca-cert-file /etc/ssl/cluster/ca/ca.crt
tls-auth-clients no
tls-replication yes
tls-cluster yes

replica-announce-ip {{ .AnnounceIP }}
replica-announce-port {{ .AnnounceRedisPort }}

requirepass {{ .RedisPassword }}
masterauth {{ .RedisPassword }}

dir /var/lib/redis/data
appendonly yes

rename-command FLUSHDB ""
rename-command FLUSHALL ""
rename-command KEYS ""
rename-command DEBUG ""
# rename-command CONFIG "{{ .ConfigCommandName }}"
rename-command CONFIG "{{ .ConfigCommandName }}"
