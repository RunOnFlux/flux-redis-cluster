import json
import time
import subprocess
import requests
import pytest
import redis

MOCK_API_URL = "http://localhost:8080"
APP_NAME = "flux-redis-cluster"
REDIS_PASSWORD = "secret"
PROXY_PORTS = [6380, 6381, 6382]
NODE_IPS = ["172.20.0.10", "172.20.0.11", "172.20.0.12"]

def update_mock_api(nodes):
    resp = requests.post(f"{MOCK_API_URL}/admin/set-nodes", json={"nodes": nodes})
    resp.raise_for_status()

def get_redis_client(port):
    return redis.Redis(
        host="localhost",
        port=port,
        password=REDIS_PASSWORD,
        ssl=True,
        ssl_cert_reqs="none", # self-signed certs
        decode_responses=True
    )

@pytest.fixture(scope="module", autouse=True)
def setup_cluster():
    # Make sure we're clean
    subprocess.run(["docker", "compose", "down", "-v"], check=True)
    
    # Start mock API first so we can configure it
    subprocess.run(["docker", "compose", "up", "-d", "mock-api"], check=True)
    
    # Wait for mock-api to be ready
    for _ in range(10):
        try:
            requests.get(f"{MOCK_API_URL}/health")
            break
        except requests.exceptions.ConnectionError:
            time.sleep(1)
            
    # Reset Mock API
    requests.post(f"{MOCK_API_URL}/admin/reset")
    
    # Set all 3 nodes
    nodes = [
        {"ip": "172.20.0.10", "name": "node1", "ports": {}},
        {"ip": "172.20.0.11", "name": "node2", "ports": {}},
        {"ip": "172.20.0.12", "name": "node3", "ports": {}}
    ]
    update_mock_api(nodes)
    
    # Start the rest of the cluster
    subprocess.run(["docker", "compose", "up", "--build", "-d"], check=True)
    
    print("Waiting for cluster to initialize...")
    time.sleep(30) # wait for init, certs, and redis to start
    
    yield
    
    subprocess.run(["docker", "compose", "down", "-v"], check=True)

def test_cluster_initialization():
    # Wait for proxy to come up and elect a master
    time.sleep(15)
    
    # Test writing to the proxy on node1
    client1 = get_redis_client(PROXY_PORTS[0])
    
    # Check if we can write
    assert client1.set("testkey", "testvalue") == True
    
    # Check if we can read from proxy on node2
    client2 = get_redis_client(PROXY_PORTS[1])
    assert client2.get("testkey") == "testvalue"

def test_failover():
    client1 = get_redis_client(PROXY_PORTS[0])
    
    # Determine the current master by looking at role
    role_info = client1.role()
    master_ip = role_info[1] if role_info[0] == "master" else role_info[0]
    
    # Stop the master node
    master_container = ""
    if master_ip == "172.20.0.10":
        master_container = "redis-cluster-node1"
    elif master_ip == "172.20.0.11":
        master_container = "redis-cluster-node2"
    else:
        master_container = "redis-cluster-node3"
        
    print(f"Stopping master container: {master_container}")
    subprocess.run(["docker", "stop", master_container], check=True)
    
    print("Waiting for failover...")
    time.sleep(20) # wait for Sentinel to detect failure and elect new master
    
    # Try writing to a surviving proxy (try both to find one that is up)
    alive_port = PROXY_PORTS[1] if master_container == "redis-cluster-node1" else PROXY_PORTS[0]
    
    client_alive = get_redis_client(alive_port)
    assert client_alive.set("failover_key", "survived") == True
    assert client_alive.get("failover_key") == "survived"

    # Start the old master again
    print(f"Starting old master container: {master_container}")
    subprocess.run(["docker", "start", master_container], check=True)
    time.sleep(20) # wait for it to join as replica
