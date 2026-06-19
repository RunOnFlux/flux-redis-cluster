"""
Proxy Routing Tests

Verify the TCP proxy correctly routes connections to the Redis master:
  1. All 3 proxies can accept writes (proving they all route to master).
  2. After killing the master, a surviving proxy reroutes to the new master.
  3. After failover, all surviving proxies reroute to the new master.
"""

import time

import redis as redis_lib
import pytest

from tests.helpers.cluster import BASE_NODES, RedisClusterManager


def test_proxy_routes_writes_from_any_node(cluster: RedisClusterManager):
    """Connect to each proxy port, write a unique key — all should succeed."""
    for node_name, cfg in BASE_NODES.items():
        cluster.wait_for_proxy_ready(cfg.proxy_host_port)
        client = cluster.get_proxy_client(cfg.proxy_host_port)
        key = f"proxy_write_{node_name}"
        value = f"from_{node_name}"
        result = client.set(key, value)
        assert result is True, (
            f"Write via proxy on {node_name} (port {cfg.proxy_host_port}) failed"
        )
        got = client.get(key)
        assert got == value, (
            f"Read-back via {node_name} proxy: expected {value!r}, got {got!r}"
        )
        print(f"  ✓ Proxy on {node_name} (port {cfg.proxy_host_port}) routed write OK")


def test_proxy_reroutes_after_failover(cluster: RedisClusterManager):
    """Kill master, verify a surviving proxy reroutes writes to new master."""
    old_master_ip = cluster.get_master_ip()
    assert old_master_ip is not None
    old_master_name = cluster.get_master_node_name()
    assert old_master_name is not None
    print(f"  Old master: {old_master_name} ({old_master_ip})")

    # Pick a surviving node's proxy port
    surviving_nodes = [n for n in BASE_NODES if n != old_master_name]
    surviving_cfg = BASE_NODES[surviving_nodes[0]]
    surviving_port = surviving_cfg.proxy_host_port
    print(f"  Using surviving proxy: {surviving_nodes[0]} (port {surviving_port})")

    # Write pre-kill key
    pre_client = cluster.get_proxy_client(surviving_port)
    pre_client.set("proxy_reroute_pre", "pre_kill")
    print("  Wrote 'proxy_reroute_pre' key")

    # Kill master
    cluster.kill_node(old_master_name)
    print(f"  Killed master {old_master_name}")

    # Wait for Sentinel to elect new master
    new_master_ip = cluster.wait_for_master_change(old_master_ip, timeout=120)
    print(f"  New master: {new_master_ip}")

    # Retry loop: proxy needs time to detect new master
    deadline = time.time() + 60
    post_write_ok = False
    last_err = None
    while time.time() < deadline:
        try:
            client = cluster.get_proxy_client(surviving_port)
            client.set("proxy_reroute_post", "post_kill")
            post_write_ok = True
            break
        except Exception as exc:
            last_err = exc
            time.sleep(3)

    assert post_write_ok, (
        f"Proxy on port {surviving_port} did not reroute within 60s: {last_err}"
    )
    print("  ✓ Post-kill write succeeded via surviving proxy")

    # Verify both keys exist
    client = cluster.get_proxy_client(surviving_port)
    assert client.get("proxy_reroute_pre") == "pre_kill"
    assert client.get("proxy_reroute_post") == "post_kill"
    print("  ✓ Both pre-kill and post-kill keys verified")

    # Recovery
    cluster.start_node(old_master_name)
    print(f"  Started {old_master_name}")
    time.sleep(30)
    cluster.wait_for_healthy(timeout=120)


def test_all_proxies_reroute_after_failover(cluster: RedisClusterManager):
    """Kill master, verify all surviving proxies reroute writes."""
    old_master_ip = cluster.get_master_ip()
    assert old_master_ip is not None
    old_master_name = cluster.get_master_node_name()
    assert old_master_name is not None
    print(f"  Old master: {old_master_name} ({old_master_ip})")

    surviving_nodes = [n for n in BASE_NODES if n != old_master_name]

    # Kill master
    cluster.kill_node(old_master_name)
    print(f"  Killed master {old_master_name}")

    # Wait for Sentinel to elect new master
    new_master_ip = cluster.wait_for_master_change(old_master_ip, timeout=120)
    print(f"  New master: {new_master_ip}")

    # Verify each surviving proxy can route writes within 60s
    for node_name in surviving_nodes:
        cfg = BASE_NODES[node_name]
        port = cfg.proxy_host_port
        key = f"all_proxy_reroute_{node_name}"

        deadline = time.time() + 60
        write_ok = False
        last_err = None
        while time.time() < deadline:
            try:
                client = cluster.get_proxy_client(port)
                client.set(key, f"rerouted_via_{node_name}")
                write_ok = True
                break
            except Exception as exc:
                last_err = exc
                time.sleep(3)

        assert write_ok, (
            f"Proxy on {node_name} (port {port}) did not reroute within 60s: {last_err}"
        )
        print(f"  ✓ Proxy on {node_name} (port {port}) rerouted after failover")

    # Verify all written keys
    for node_name in surviving_nodes:
        cfg = BASE_NODES[node_name]
        client = cluster.get_proxy_client(cfg.proxy_host_port)
        key = f"all_proxy_reroute_{node_name}"
        value = client.get(key)
        assert value == f"rerouted_via_{node_name}", (
            f"Key {key} not found or wrong value: {value!r}"
        )

    # Recovery
    cluster.start_node(old_master_name)
    print(f"  Started {old_master_name}")
    time.sleep(30)
    cluster.wait_for_healthy(timeout=120)
