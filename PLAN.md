# Flux Redis Cluster Implementation Plan

## 1. Architecture Overview
Similar to the `flux-pg-cluster`, we will deploy a self-configuring, highly-available Redis cluster that dynamically discovers its members through the Flux API. 
We will use **Redis Sentinel** for high availability and failover, and a custom Go agent (`flux-agent`) for cluster coordination and primary routing.

Each node in the Flux network will run a single Docker container containing:
- **Redis Server**: The actual Redis database.
- **Redis Sentinel**: Monitors the Redis servers, handles failover, and elects a new master when needed.
- **Flux Agent (Go)**:
  - **Updater (Daemon)**: Periodically queries the Flux API to discover new peers and remove dead ones. It updates Sentinel and Redis replica configurations accordingly.
  - **Proxy**: A TCP proxy (e.g., on port 6380) that forwards all incoming traffic to the current Redis master by asking the local Sentinel who the master is.
- **Supervisord**: Manages all the above processes to ensure they stay alive.

## 2. Component Details

### A. Flux Agent (Go)
The Go agent will have several subcommands:
1. `init`: Runs before any service starts. Queries the Flux API to discover existing nodes. If it's the first node, it sets itself as master. If a master already exists, it configures itself as a replica. Generates `redis.conf` and `sentinel.conf` from templates. Also generates deterministic SSL/TLS certificates.
2. `daemon`: The reconciliation loop.
   - Polls the Flux API every X seconds.
   - Identifies new nodes and registers them with Sentinel.
   - Identifies removed/dead nodes and removes them from Sentinel (`SENTINEL RESET` or `SENTINEL REMOVE`).
   - Ensures the local Redis node's `replicaof` configuration is correct based on the current master.
3. `proxy`: A lightweight TCP proxy.
   - Listens on `PROXY_LISTEN_PORT`.
   - On new connection, it queries the local Sentinel (`SENTINEL get-master-addr-by-name <cluster-name>`).
   - Forwards the TCP connection to the returned master IP and port.

### B. Redis & Sentinel
- **Ports**: 
  - Redis: `6379`
  - Sentinel: `26379`
  - Proxy: `6380` (This is what applications will connect to).

## 3. Security and Hardening (New)

### A. SSL/TLS Encryption
- **Deterministic Certificates**: Just like `flux-pg-cluster`, we will use a `SSL_PASSPHRASE` environment variable to deterministically generate a shared Root CA and node certificates. This allows nodes to trust each other without needing to exchange certificates over the network.
- **Encrypted Communication**: 
  - Redis replication (Master <-> Replica) will use TLS (`tls-replication yes`).
  - Sentinel communication (Sentinel <-> Redis, Sentinel <-> Sentinel) will use TLS.
  - The TCP Proxy will support accepting TLS connections from clients and forwarding them to the Master over TLS.

### B. Authentication
- **Strong Passwords**: Require `REDIS_PASSWORD` for all Redis connections (`requirepass` and `masterauth`).
- **Sentinel Authentication**: Require `SENTINEL_PASSWORD` for Sentinel communication (`requirepass` and `sentinel auth-pass`).
- **ACLs**: Instead of just using `requirepass`, we can configure Redis ACLs to grant the proxy/clients only specific permissions, while the Go agent uses an admin user.

### C. Container and Process Hardening
- **Non-Root User**: The Redis and Sentinel processes will run as a dedicated, unprivileged `redis` user (via supervisord's `user=redis` directive).
- **Directory Permissions**: Data directories (`/var/lib/redis/data`) and configuration directories will be strictly owned by the `redis` user with `700` permissions.
- **Command Disabling/Renaming**: Dangerous Redis commands (`FLUSHDB`, `FLUSHALL`, `KEYS`, `DEBUG`, `CONFIG`) will be disabled or renamed to prevent accidental or malicious data loss by end users. (Note: `flux-agent` may need `CONFIG` internally, so it could be renamed to a secret string).

## 4. Project Structure
```text
flux-redis-cluster/
├── Dockerfile                  # Hardened image based on ubuntu or redis, non-root users
├── docker-compose.yml          # For local multi-node testing
├── docker-compose.test.yml     # For integration testing
├── supervisord.conf
├── redis.conf.tpl              # Template for redis server (hardened, TLS-enabled)
├── sentinel.conf.tpl           # Template for redis sentinel (hardened, TLS-enabled)
├── generate-certs.sh           # Deterministic cert generation script
├── README.md
├── cmd/
│   └── flux-agent/             # Go agent entrypoint
│       └── main.go
├── internal/
│   ├── config/                 # Environment variables parsing
│   ├── fluxapi/                # Flux API client
│   ├── redis/                  # Redis & Sentinel configuration & interaction logic
│   ├── proxy/                  # TCP Proxy implementation
│   └── security/               # Cert generation and auth logic
├── mock-api/                   # Mock Flux API for local testing
└── tests/                      # Pytest integration tests
```

## 5. Execution Steps
1. **Bootstrap Go Project**: Setup `go.mod`, directory structure.
2. **Implement Security Layer**: 
   - Write deterministic TLS certificate generation logic.
   - Setup Redis/Sentinel templates with TLS, strong auth, and disabled dangerous commands.
3. **Implement `fluxapi`**: Client to mock and real Flux API.
4. **Implement Redis config logic**: `init` command to generate configs, set up permissions.
5. **Implement Daemon**: The background updater polling the API and securely communicating with Sentinel.
6. **Implement Proxy**: The primary-routing proxy with TLS passthrough or termination.
7. **Containerization**: Write `Dockerfile` with the `redis` non-root user setup and `supervisord.conf`.
8. **Local Testing Environment**: Setup `docker-compose.yml` with `mock-api` and 3 Redis nodes.
9. **Testing**: Verify initialization, TLS communication, scaling up, failover, and scaling down.
