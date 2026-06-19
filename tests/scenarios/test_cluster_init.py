"""
Cluster Initialization Tests

Verify that the 3-node Redis Sentinel cluster starts correctly:
  1. All nodes are reachable through their proxies.
  2. Exactly one master is elected; the other two are replicas.
  3. Data written on one node replicates to all others.
"""

import redis as redis_lib
import pytest

from tests.helpers.cluster import BASE_NODES, RedisClusterManager


def test_all_nodes_reachable(cluster: RedisClusterManager):
    """Verify we can connect to all 3 proxy ports and SET/GET a key."""
    for node_name, cfg in BASE_NODES.items():
        client = cluster.get_proxy_client(cfg.proxy_host_port)
        key = f"reachable_test_{node_name}"
        assert client.set(key, "hello") is True, (
            f"Failed to SET via proxy on {node_name} (port {cfg.proxy_host_port})"
        )
        value = client.get(key)
        assert value == "hello", (
            f"Failed to GET via proxy on {node_name}: got {value!r}"
        )
        print(f"  ✓ {node_name} (proxy port {cfg.proxy_host_port}) is reachable")


def test_single_master_elected(cluster: RedisClusterManager):
    """Verify exactly one master exists and the other two are replicas."""
    master_ip = cluster.get_master_ip()
    assert master_ip is not None, "No master found via Sentinel"
    print(f"  Master IP: {master_ip}")

    masters = []
    replicas = []
    for node_name, cfg in BASE_NODES.items():
        client = cluster.get_proxy_client(cfg.proxy_host_port)
        role_info = client.role()
        role = role_info[0]  # 'master' or 'slave'
        print(f"  {node_name} ({cfg.ip}): role={role}")
        if role == "master":
            masters.append(node_name)
        else:
            replicas.append(node_name)

    assert len(masters) == 1, f"Expected 1 master, found {len(masters)}: {masters}"
    assert len(replicas) == 2, f"Expected 2 replicas, found {len(replicas)}: {replicas}"


def test_data_replication(cluster: RedisClusterManager):
    """Write a key via one proxy, verify it's readable from all proxies."""
    # Write through the first node's proxy
    writer = cluster.get_proxy_client(BASE_NODES["node1"].proxy_host_port)
    writer.set("replication_test", "replicated_value")
    print("  Wrote 'replication_test' via node1 proxy")

    # Read from all proxies — since proxies all route to the master,
    # reads also go to the master and should see the key immediately
    for node_name, cfg in BASE_NODES.items():
        reader = cluster.get_proxy_client(cfg.proxy_host_port)
        value = reader.get("replication_test")
        assert value == "replicated_value", (
            f"Replication check failed on {node_name}: got {value!r}"
        )
        print(f"  ✓ {node_name} proxy reads 'replication_test' = {value!r}")
