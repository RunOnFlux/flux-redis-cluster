from __future__ import annotations

import time
from dataclasses import dataclass, field
from typing import Optional

import docker
import redis as redis_lib


@dataclass(frozen=True)
class NodeConfig:
    container_name: str
    ip: str
    proxy_host_port: int
    redis_port: int = 6379
    sentinel_port: int = 26379


BASE_NODES = {
    "node1": NodeConfig("redis-cluster-node1", "172.20.0.10", 6380),
    "node2": NodeConfig("redis-cluster-node2", "172.20.0.11", 6381),
    "node3": NodeConfig("redis-cluster-node3", "172.20.0.12", 6382),
}

NETWORK_NAME = "flux-redis-cluster_cluster_network"

REDIS_PASSWORD = "secret"
SENTINEL_PASSWORD = "secret"
APP_NAME = "flux-redis-cluster"


class RedisClusterManager:
    """Manages the 3-node Redis Sentinel test cluster."""

    def __init__(self, docker_client: docker.DockerClient) -> None:
        self.docker_client = docker_client

    # ── Redis client helpers ──────────────────────────────────────────

    def get_redis_client(
        self,
        host: str,
        port: int,
        password: str = REDIS_PASSWORD,
    ) -> redis_lib.Redis:
        """Return a Redis client connecting directly to a Redis instance."""
        return redis_lib.Redis(
            host=host,
            port=port,
            password=password,
            ssl=True,
            ssl_cert_reqs="none",
            decode_responses=True,
        )

    def get_proxy_client(self, host_port: int) -> redis_lib.Redis:
        """Return a Redis client connecting through the TCP proxy on localhost."""
        return redis_lib.Redis(
            host="localhost",
            port=host_port,
            password=REDIS_PASSWORD,
            ssl=True,
            ssl_cert_reqs="none",
            decode_responses=True,
        )

    # ── Master discovery ──────────────────────────────────────────────

    def get_master_ip(self) -> Optional[str]:
        """Query each node's Sentinel for the current master IP.

        Returns the master IP string, or None if all nodes fail.
        """
        sentinel_cmd = (
            "redis-cli -p 26379 -a {password} "
            "--tls "
            "--cert /etc/ssl/cluster/sentinel/server.crt "
            "--key /etc/ssl/cluster/sentinel/server.key "
            "--cacert /etc/ssl/cluster/ca/ca.crt "
            "SENTINEL get-master-addr-by-name {app}"
        ).format(password=SENTINEL_PASSWORD, app=APP_NAME)

        for node_name in BASE_NODES:
            try:
                exit_code, output = self.exec_in_container(node_name, sentinel_cmd)
                if exit_code != 0:
                    continue
                # Output looks like:
                #   172.20.0.10
                #   6379
                lines = output.strip().splitlines()
                if len(lines) >= 1:
                    ip = lines[0].strip()
                    if ip.startswith("172."):
                        return ip
            except Exception:
                continue
        return None

    def get_master_node_name(self) -> Optional[str]:
        """Return the BASE_NODES key (e.g. 'node1') for the current master."""
        master_ip = self.get_master_ip()
        if master_ip is None:
            return None
        for name, cfg in BASE_NODES.items():
            if cfg.ip == master_ip:
                return name
        return None

    # ── Cluster health ────────────────────────────────────────────────

    def wait_for_healthy(
        self, expected_nodes: int = 3, timeout: int = 120
    ) -> bool:
        """Poll until expected_nodes are reachable via Sentinel and a master is known.

        Returns True on success, raises TimeoutError otherwise.
        """
        deadline = time.time() + timeout
        while time.time() < deadline:
            master_ip = self.get_master_ip()
            if master_ip is None:
                time.sleep(5)
                continue

            # Check that each expected node's Sentinel is responding
            reachable = 0
            for node_name in list(BASE_NODES)[:expected_nodes]:
                sentinel_cmd = (
                    "redis-cli -p 26379 -a {password} "
                    "--tls "
                    "--cert /etc/ssl/cluster/sentinel/server.crt "
                    "--key /etc/ssl/cluster/sentinel/server.key "
                    "--cacert /etc/ssl/cluster/ca/ca.crt "
                    "PING"
                ).format(password=SENTINEL_PASSWORD)
                try:
                    exit_code, output = self.exec_in_container(node_name, sentinel_cmd)
                    if exit_code == 0 and "PONG" in output:
                        reachable += 1
                except Exception:
                    pass

            if reachable >= expected_nodes:
                return True

            time.sleep(5)

        raise TimeoutError(
            f"Cluster did not become healthy within {timeout}s. "
            f"Master: {self.get_master_ip()}"
        )

    def wait_for_master_change(
        self, old_master_ip: str, timeout: int = 120
    ) -> str:
        """Poll until the master IP changes from old_master_ip.

        Returns the new master IP or raises TimeoutError.
        """
        deadline = time.time() + timeout
        while time.time() < deadline:
            new_master_ip = self.get_master_ip()
            if new_master_ip and new_master_ip != old_master_ip:
                return new_master_ip
            time.sleep(5)
        raise TimeoutError(
            f"Master did not change from {old_master_ip} within {timeout}s"
        )

    # ── Container lifecycle ───────────────────────────────────────────

    def _container(self, node_name: str):
        """Get the Docker container object for a node."""
        cfg = BASE_NODES[node_name]
        return self.docker_client.containers.get(cfg.container_name)

    def kill_node(self, node_name: str) -> None:
        """Hard-stop a container (timeout=0)."""
        self._container(node_name).stop(timeout=0)

    def start_node(self, node_name: str) -> None:
        """Start a previously stopped container."""
        self._container(node_name).start()

    def exec_in_container(self, node_name: str, cmd: str) -> tuple[int, str]:
        """Run a shell command inside a container.

        Returns (exit_code, output_string).
        """
        container = self._container(node_name)
        exit_code, output = container.exec_run(["sh", "-c", cmd])
        output_str = output.decode("utf-8", errors="replace") if output else ""
        return exit_code, output_str
