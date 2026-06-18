# Flux Redis Cluster

A highly available, self-configuring Redis cluster architecture designed specifically to run seamlessly on the Flux Cloud. This project provides an autonomous Redis deployment with automatic failover, managed entirely by a custom Go agent (`flux-agent`).

## Architecture

The cluster relies on **Redis Sentinel** for high availability and failover, but removes the manual configuration burden by automating peer discovery, certificate generation, and node lifecycle management.

Key components of each node:
1. **Redis Server**: The core data store, running in either `master` or `replica` mode.
2. **Redis Sentinel**: Monitors the Redis servers, handles failovers, and acts as the source of truth for the current topology.
3. **Flux Agent**: A custom Go daemon responsible for:
    - **Initialization (`init`)**: Discovers peers via the Flux Location API. If the node is the first to boot, it configures itself as the master. Otherwise, it joins as a replica.
    - **Reconciliation (`daemon`)**: Continuously polls the Flux API for topology changes, dynamically registering new nodes to Sentinel and removing dead nodes.
    - **Routing Proxy (`proxy`)**: A built-in TCP proxy that listens for incoming client connections and transparently forwards them to the active Redis master, solving NAT hairpinning and IP fluidity issues.
    - **Deterministic TLS**: Generates deterministic TLS certificates on boot, ensuring zero-configuration encrypted communication between all cluster members.

## Security

- All communication between Redis and Sentinel nodes is encrypted via TLS.
- Client connections to the proxy are secured using TLS passthrough.
- The processes within the Docker container run under a restricted, non-root `redis` user, with strict permission enforcement on data directories.

## Local Testing Environment

A local testing infrastructure is provided using `docker-compose` and a mock Flux API server to simulate the production environment. 

### Prerequisites
- Docker and Docker Compose
- Python 3.10+
- `pytest` and `redis-py`

### Running the Integration Tests

The integration tests spin up the mock API and three Redis nodes, verify the cluster initialization, and simulate a master node failure to ensure Sentinel elects a new master and the proxy routes traffic correctly.

1. Set up the Python virtual environment and install dependencies:
   ```bash
   python3 -m venv venv
   source venv/bin/activate
   pip install -r tests/requirements.txt
   ```

2. Run the test suite:
   ```bash
   pytest tests/test_cluster.py -v
   ```

The test suite automatically handles booting the `docker-compose` environment and tearing it down after execution.
