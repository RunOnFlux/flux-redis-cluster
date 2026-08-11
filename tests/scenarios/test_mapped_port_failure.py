"""Production-path regression tests for an unreachable advertised Redis port."""

import time

import redis as redis_lib

from tests.helpers.cluster import BASE_NODES, REDIS_PASSWORD, RedisClusterManager


def test_unreachable_advertised_port_reproduces_replica_proxy_tls_reset(
    unreachable_host_port_cluster: RedisClusterManager,
):
    cluster = unreachable_host_port_cluster

    # The deterministic startup fallback makes the lowest advertised IP the
    # initial master. Its proxy takes the local 6379 fast path and still works.
    cluster.wait_for_proxy_ready(BASE_NODES["node1"].proxy_host_port, timeout=60)

    # A replica proxy takes the production path to master-IP:HOST_REDIS_PORT.
    # Nothing listens on 16379 in this fixture, so it accepts the client socket
    # and then closes it before the TLS handshake can complete.
    client = redis_lib.Redis(
        host="localhost",
        port=BASE_NODES["node2"].proxy_host_port,
        password=REDIS_PASSWORD,
        ssl=True,
        ssl_cert_reqs="none",
        socket_connect_timeout=2,
        socket_timeout=2,
    )

    deadline = time.time() + 30
    last_error = None
    while time.time() < deadline:
        try:
            client.ping()
        except (redis_lib.ConnectionError, redis_lib.TimeoutError) as exc:
            last_error = exc
            break
        time.sleep(1)

    assert last_error is not None, "replica proxy unexpectedly reached port 16379"
    assert "SSL" in str(last_error) or "reset" in str(last_error).lower()

    exit_code, proxy_log = cluster.exec_in_container(
        "node2", "tail -n 100 /var/log/supervisor/proxy.err.log"
    )
    assert exit_code == 0
    assert "dial 172.20.0.10:16379 failed" in proxy_log
