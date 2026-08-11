"""
Failover Tests

Verify Redis Sentinel failover behaviour:
  1. Killing the master triggers election of a new master.
  2. Data written before failover survives and new data can be written.
  3. The old master rejoins as a replica after being restarted.
"""

import time

import redis as redis_lib
import pytest

from tests.helpers.cluster import (
    APP_NAME,
    BASE_NODES,
    REDIS_PASSWORD,
    RedisClusterManager,
)
from tests.helpers.mock_api import MockApiClient


def _sentinel_cli(command: str) -> str:
    return (
        "redis-cli -p 26379 -a {password} "
        "--tls "
        "--cert /etc/ssl/cluster/sentinel/server.crt "
        "--key /etc/ssl/cluster/sentinel/server.key "
        "--cacert /etc/ssl/cluster/ca/ca.crt "
        "{command}"
    ).format(password="secret", command=command)


def _local_role(cluster: RedisClusterManager, node_name: str) -> str:
    role_cmd = (
        "redis-cli -p 6379 -a secret "
        "--tls "
        "--cert /etc/ssl/cluster/redis/server.crt "
        "--key /etc/ssl/cluster/redis/server.key "
        "--cacert /etc/ssl/cluster/ca/ca.crt ROLE"
    )
    exit_code, output = cluster.exec_in_container(node_name, role_cmd)
    if exit_code != 0:
        return "unknown"
    lines = [
        line.strip()
        for line in output.splitlines()
        if line.strip() and not line.startswith("Warning:")
    ]
    return lines[0] if lines else "unknown"


def test_master_failover_elects_new_master(cluster: RedisClusterManager):
    """Kill the current master, verify a new master is elected, then recover."""
    old_master_ip = cluster.get_master_ip()
    assert old_master_ip is not None, "No master found before failover"
    old_master_name = cluster.get_master_node_name()
    assert old_master_name is not None, f"Cannot map master IP {old_master_ip} to a node"
    print(f"  Old master: {old_master_name} ({old_master_ip})")

    # Kill the master
    cluster.kill_node(old_master_name)
    print(f"  Killed {old_master_name}")

    # Wait for Sentinel to elect a new master
    new_master_ip = cluster.wait_for_master_change(old_master_ip, timeout=120)
    assert new_master_ip != old_master_ip
    assert new_master_ip is not None
    print(f"  New master IP: {new_master_ip}")

    # Verify new master accepts writes
    new_master_name = cluster.get_master_node_name()
    assert new_master_name is not None
    new_cfg = BASE_NODES[new_master_name]
    client = cluster.get_proxy_client(new_cfg.proxy_host_port)
    assert client.set("failover_write_test", "after_failover") is True
    assert client.get("failover_write_test") == "after_failover"
    print(f"  New master {new_master_name} accepts writes")

    # Start old master back
    cluster.start_node(old_master_name)
    print(f"  Started {old_master_name}")
    time.sleep(30)
    cluster.wait_for_healthy(timeout=120)


def test_data_survives_failover(cluster: RedisClusterManager):
    """Write keys before failover, verify they persist after failover."""
    old_master_ip = cluster.get_master_ip()
    assert old_master_ip is not None
    old_master_name = cluster.get_master_node_name()
    assert old_master_name is not None

    # Write unique keys before failover
    # Use a surviving node's proxy to write
    surviving_nodes = [n for n in BASE_NODES if n != old_master_name]
    surviving_cfg = BASE_NODES[surviving_nodes[0]]
    cluster.wait_for_proxy_ready(surviving_cfg.proxy_host_port)
    writer = cluster.get_proxy_client(surviving_cfg.proxy_host_port)
    writer.set("pre_failover_key1", "value1")
    writer.set("pre_failover_key2", "value2")
    print(f"  Wrote pre-failover keys via {surviving_nodes[0]}")

    # Kill master
    cluster.kill_node(old_master_name)
    print(f"  Killed master {old_master_name}")

    # Wait for new master
    new_master_ip = cluster.wait_for_master_change(old_master_ip, timeout=120)
    print(f"  New master: {new_master_ip}")

    # Verify old data still readable (retry since proxy needs to reroute)
    deadline = time.time() + 60
    verified = False
    while time.time() < deadline:
        try:
            reader = cluster.get_proxy_client(surviving_cfg.proxy_host_port)
            val1 = reader.get("pre_failover_key1")
            val2 = reader.get("pre_failover_key2")
            if val1 == "value1" and val2 == "value2":
                verified = True
                break
        except Exception:
            pass
        time.sleep(3)
    assert verified, "Pre-failover data not readable after failover"
    print("  ✓ Pre-failover data survived")

    # Write new data
    reader.set("post_failover_key", "new_value")
    assert reader.get("post_failover_key") == "new_value"
    print("  ✓ Post-failover write succeeded")

    # Verify both old and new data exist
    assert reader.get("pre_failover_key1") == "value1"
    assert reader.get("pre_failover_key2") == "value2"
    assert reader.get("post_failover_key") == "new_value"
    print("  ✓ Both old and new data coexist")

    # Recovery
    cluster.start_node(old_master_name)
    print(f"  Started {old_master_name}")
    time.sleep(30)
    cluster.wait_for_healthy(timeout=120)


def test_old_master_rejoins_as_replica(cluster: RedisClusterManager):
    """After failover, the old master should rejoin as a replica."""
    old_master_ip = cluster.get_master_ip()
    assert old_master_ip is not None
    old_master_name = cluster.get_master_node_name()
    assert old_master_name is not None
    old_cfg = BASE_NODES[old_master_name]
    print(f"  Old master: {old_master_name} ({old_master_ip})")

    # Kill master
    cluster.kill_node(old_master_name)
    print(f"  Killed {old_master_name}")

    # Wait for failover
    new_master_ip = cluster.wait_for_master_change(old_master_ip, timeout=120)
    print(f"  New master: {new_master_ip}")

    # Start old master back
    cluster.start_node(old_master_name)
    print(f"  Started {old_master_name}")
    time.sleep(30)

    # Verify old master rejoined as replica by checking its ROLE via
    # direct Redis connection inside the container
    role_cmd = (
        "redis-cli -p 6379 -a {password} "
        "--tls "
        "--cert /etc/ssl/cluster/redis/server.crt "
        "--key /etc/ssl/cluster/redis/server.key "
        "--cacert /etc/ssl/cluster/ca/ca.crt "
        "ROLE"
    ).format(password="secret")

    deadline = time.time() + 60
    is_replica = False
    while time.time() < deadline:
        try:
            exit_code, output = cluster.exec_in_container(old_master_name, role_cmd)
            if exit_code == 0 and "slave" in output.lower():
                is_replica = True
                break
        except Exception:
            pass
        time.sleep(5)

    assert is_replica, (
        f"Old master {old_master_name} did not rejoin as replica within 60s"
    )
    print(f"  ✓ {old_master_name} rejoined as replica")

    cluster.wait_for_healthy(timeout=120)


def test_permanently_removed_master_bootstraps_freshest_survivor(
    cluster: RedisClusterManager,
    mock_api: MockApiClient,
):
    """Recover even when Sentinels disagree and the old master never returns."""
    old_master_ip = cluster.get_master_ip()
    old_master_name = cluster.get_master_node_name()
    assert old_master_ip is not None
    assert old_master_name is not None

    surviving_nodes = [name for name in BASE_NODES if name != old_master_name]
    surviving_ips = [BASE_NODES[name].ip for name in surviving_nodes]

    # Ensure the replicas contain data worth preserving before isolating the
    # control plane and permanently removing the old master.
    writer = cluster.get_proxy_client(BASE_NODES[old_master_name].proxy_host_port)
    writer.set("permanent_removal_key", "survives")
    time.sleep(2)

    # Freeze reconciliation while constructing the exact production failure:
    # every survivor monitors a different, removed master and therefore cannot
    # form Sentinel quorum on its own.
    for node_name in surviving_nodes:
        exit_code, output = cluster.exec_in_container(
            node_name, "supervisorctl stop updater"
        )
        assert exit_code == 0, output

    stale_ips = ["192.0.2.64", "192.0.2.82"]
    for node_name, stale_ip in zip(surviving_nodes, stale_ips):
        commands = [
            f"SENTINEL REMOVE {APP_NAME}",
            f"SENTINEL MONITOR {APP_NAME} {stale_ip} 6379 2",
            f"SENTINEL SET {APP_NAME} auth-pass {REDIS_PASSWORD}",
        ]
        for command in commands:
            exit_code, output = cluster.exec_in_container(
                node_name, _sentinel_cli(command)
            )
            assert exit_code == 0, output

    cluster.kill_node(old_master_name)
    mock_api.set_nodes(surviving_ips)

    for node_name in surviving_nodes:
        exit_code, output = cluster.exec_in_container(
            node_name, "supervisorctl start updater"
        )
        assert exit_code == 0, output

    deadline = time.time() + 120
    elected_ip = None
    while time.time() < deadline:
        masters = [
            name for name in surviving_nodes if _local_role(cluster, name) == "master"
        ]
        reported = {cluster.get_master_ip()}
        if len(masters) == 1:
            candidate_ip = BASE_NODES[masters[0]].ip
            if reported == {candidate_ip}:
                elected_ip = candidate_ip
                break
        time.sleep(3)

    assert elected_ip in surviving_ips, "survivors did not converge on a new master"

    # Both surviving proxies must route writes to the recovered master, and the
    # data acknowledged before removal must still be present.
    for node_name in surviving_nodes:
        proxy = cluster.get_proxy_client(BASE_NODES[node_name].proxy_host_port)
        assert proxy.get("permanent_removal_key") == "survives"
        assert proxy.set(f"recovered_via_{node_name}", "ok") is True
