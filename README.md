# Flux Redis Cluster

![Version](https://img.shields.io/badge/version-1.0.0-blue.svg)
![Redis](https://img.shields.io/badge/Redis-6.0.16-red.svg)
![Sentinel](https://img.shields.io/badge/Sentinel-High%20Availability-green.svg)
![Docker](https://img.shields.io/badge/Docker-required-blue.svg)

This project creates a self-configuring, highly-available Redis cluster that dynamically discovers its members through the Flux API. The cluster uses Redis Sentinel for high availability and automatic failover, and automatically adapts to nodes being added or removed from the environment.

## Prerequisites

- Docker
- Docker Compose
- Access to Flux network for API calls

## Quick Start

### Production Deployment on Flux Network

#### Architecture Overview

```text
   ┌───────────────────┐       ┌───────────────────┐       ┌───────────────────┐
   │      Node 1       │       │      Node 2       │       │       Node 3      │
   │  ┌─────────────┐  │       │  ┌─────────────┐  │       │  ┌─────────────┐  │
   │  │  Your App   │  │       │  │  Your App   │  │       │  │  Your App   │  │
   │  │ (Component) │  │       │  │ (Component) │  │       │  │ (Component) │  │
   │  └──────┬──────┘  │       │  └──────┬──────┘  │       │  └──────┬──────┘  │
   │         │ :6380   │       │         │ :6380   │       │         │ :6380   │
   │  ┌──────▼──────┐  │       │  ┌──────▼──────┐  │       │  ┌──────▼──────┐  │
   │  │   Proxy     │  │       │  │   Proxy     │  │       │  │   Proxy     │  │
   │  │(primary-    │  │       │  │(primary-    │  │       │  │(primary-    │  │
   │  │  routing)   │  │       │  │  routing)   │  │       │  │  routing)   │  │
   │  └──────┬──────┘  │       │  └──────┬──────┘  │       │  └──────┬──────┘  │
   │         │         │       │         │         │       │         │         │
   │  ┌──────▼──────┐  │       │  ┌──────▼──────┐  │       │  ┌──────▼──────┐  │
   │  │   Redis     │  │       │  │   Redis     │  │       │  │   Redis     │  │
   │  │ + Sentinel  │  │       │  │ + Sentinel  │  │       │  │ + Sentinel  │  │
   │  │   MASTER    │◄─┼───────┼─►│   REPLICA   │◄─┼───────┼─►│   REPLICA   │  │
   │  │(Read+Write) │  │       │  │ (Read-Only) │  │       │  │ (Read-Only) │  │
   │  └─────────────┘  │       │  └─────────────┘  │       │  └─────────────┘  │
   └───────────────────┘       └───────────────────┘       └───────────────────┘
            │                            │                           │
            └────────────────────────────┼───────────────────────────┘
                            Replication via Public Internet
Key Points:
• Each application connects to its local proxy on port 6380
• The proxy polls local Sentinel to discover the current master and forwards all connections there
• After a failover the proxy automatically reroutes new connections to the new master
• Redis instances replicate data across nodes via public internet over TLS
• Only MASTER accepts writes; REPLICA nodes are read-only
```

1. **Deploy on Flux**:
  - Log in to home.runonflux.io and navigate to Applications > Register New App.
  - Add a component for Redis.
  - Use the built Docker image: `runonflux/flux-redis-cluster:latest`.
  - Set the Container Data for the component to `/var/lib/redis/data`.
  - Add these ports to the `Cont. Ports` field: `[6379, 26379, 6380]`.
  - Using the `Ports` field, map those ports to new ones, for example: `[16379, 26380, 16380]`.
  - For the `Domains` field, add this: `["","",""]`.
  - Use the following sample to set the environment variables for the Redis component:

   ```json
   [
      "HOST_REDIS_PORT=16379",
      "HOST_SENTINEL_PORT=26380",
      "REDIS_PASSWORD=your-super-secret-password",
      "SSL_PASSPHRASE=your-ssl-passphrase"
   ]
   ```

2. **Connect from other Flux components**:
   ```bash
   # Recommended - connect via proxy (always routes to current master):
   redis-cli -h [REDIS_COMPONENT_NAME] -p 6380 -a [REDIS_PASSWORD] --tls --insecure

   # Direct connection to a specific node (bypasses proxy - use only for read replicas or diagnostics):
   redis-cli -h [REDIS_COMPONENT_NAME] -p 6379 -a [REDIS_PASSWORD] --tls --insecure
   ```
   > Replace `[REDIS_COMPONENT_NAME]` with the name you gave the Redis component in your Flux app.

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `HOST_REDIS_PORT` | Host Redis port mapping | `6379` |
| `HOST_SENTINEL_PORT` | Host Sentinel port mapping | `26379` |
| `REDIS_PORT` | Internal Redis port | `6379` |
| `SENTINEL_PORT` | Internal Sentinel port | `26379` |
| `REDIS_PASSWORD` | Redis password (used for both requirepass and masterauth) | Required |
| `SSL_PASSPHRASE` | Deterministic passphrase for certificate generation | Required |
| `SSL_CERT_VALIDITY_DAYS` | Certificate validity period in days | `3650` |
| `UPDATE_INTERVAL_SECONDS` | Update daemon reconciliation interval | `60` |
| `PROXY_LISTEN_PORT` | Port the primary-routing proxy listens on inside the container | `6380` |
| `PROXY_HEALTH_INTERVAL_SECONDS` | How often (seconds) the proxy polls Sentinel to discover the current master | `3` |

## How It Works

### Startup Process

1. **Discovery Phase**: Container calls `https://api.runonflux.io/apps/location/{APP_NAME}` to get all cluster member IPs.
2. **Certificate Generation**: Deterministically generates TLS certificates for the root CA, Redis Server, Sentinel, and Proxy using the `SSL_PASSPHRASE`.
3. **Configuration Generation**: Creates Redis and Sentinel configuration files dynamically based on whether it is the first node (initial master) or subsequent node (replica).
4. **Service Startup**: Supervisord starts Redis, Sentinel, Proxy, and the cluster update daemon.

### Dynamic Membership

- **Background Process**: Continuously monitors Flux API.
- **Automatic Adjustments**: 
  - Adds new Sentinel instances to the local Sentinel configuration using `SENTINEL MONITOR`.
  - Removes dead or unavailable Sentinel and Redis nodes using `SENTINEL REMOVE`.
- **Self-Registration**: New nodes automatically join the cluster as replicas when they start up.

### Service Management

The supervisord configuration manages four main processes:

- **redis**: The Redis server process (Master or Replica).
- **sentinel**: The Redis Sentinel process handling high-availability elections.
- **updater**: Background daemon that maintains cluster membership and syncs with Flux API.
- **proxy**: TCP master-routing proxy on port 6380.

### Access Redis

#### Master-Routing Proxy (Recommended)

Each node runs a lightweight TCP proxy on **port 6380** that automatically routes all connections to the current Redis master over TLS. Your application does not need to know which node is the master - just connect to any cluster node on port 6380 and writes will always land on the correct node, even after a failover.

```text
App → any-node:6380 (proxy) → discovers master via local Sentinel → forwards to master:6379
```

After a failover, the proxy detects the new master within `PROXY_HEALTH_INTERVAL_SECONDS` (default 3 s) and routes new connections there automatically.

## Files Overview

- **Dockerfile**: Multi-stage build - Go binary compiled inside Docker, no local Go toolchain needed.
- **docker-compose.yml**: Service definition with networking and volumes for testing.
- **docker-compose.test.yml**: Testing overrides with shorter timeouts.
- **redis.conf.tpl**: Template for Redis configuration.
- **sentinel.conf.tpl**: Template for Sentinel configuration.
- **generate-certs.sh**: Deterministic Root CA and certificate generator.
- **supervisord.conf**: Process management configuration (redis, sentinel, updater, proxy).
- **cmd/flux-agent/**: Go source for the agent binary (init, daemon, proxy).

## Local Testing

For local development and testing, this repository includes a complete mock environment:

1. **Start local test cluster**:
   ```bash
   docker compose up -d --build
   ```

2. **Access local services**:
   - **Mock Flux API**: http://localhost:8080
   - **Master-routing proxy** (node 1): `localhost:6380` → always connects to current master.
   - **Redis direct** (per-node):
     - Node 1: `localhost:6379`
     - Node 2: `localhost:6381` (mapped from 6379)
     - Node 3: `localhost:6382` (mapped from 6379)

3. **Connect to Redis (Requires python setup or redis-cli with TLS disabled for external access)**:
   The local setup requires Python `redis-py` to easily handle the self-signed certificates.

### Integration Test Suite

A pytest-based integration test suite is included under `tests/` for verifying cluster behaviour under initialization and failover scenarios.

**Install dependencies**:
```bash
python3 -m venv venv
source venv/bin/activate
pip install -r tests/requirements.txt
```

**Run tests**:
```bash
pytest tests/test_cluster.py -v
```

The test suite automatically builds images, tests initialization and role assignments, triggers a master node kill, and verifies that a failover successfully executes and proxy traffic routes correctly.

### Logs

Check logs for each component:
```bash
/var/log/supervisor/redis.out.log
/var/log/supervisor/sentinel.out.log
/var/log/supervisor/updater.out.log
/var/log/supervisor/proxy.out.log
```

## Related Projects

- Flux Postgres Cluster: [https://github.com/RunOnFlux/flux-pg-cluster](https://github.com/RunOnFlux/flux-pg-cluster)
- Flux MongoDB Cluster: [https://github.com/RunOnFlux/flux-mongodb-cluster](https://github.com/RunOnFlux/flux-mongodb-cluster)
- Flux Mysql Cluster: [https://github.com/RunOnFlux/Flux-Shared-DB](https://github.com/RunOnFlux/Flux-Shared-DB)
