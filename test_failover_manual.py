import time
import docker
from tests.helpers.cluster import RedisClusterManager, BASE_NODES

def main():
    d = docker.from_env()
    manager = RedisClusterManager(d)
    print("Waiting for healthy...")
    manager.wait_for_healthy(timeout=180)
    master_ip = manager.get_master_ip()
    master_node = manager.get_master_node_name()
    print(f"Master is {master_node} ({master_ip})")
    
    print(f"Killing {master_node}...")
    manager.kill_node(master_node)
    
    print("Waiting for master change...")
    try:
        new_ip = manager.wait_for_master_change(master_ip, timeout=120)
        print(f"New master: {new_ip}")
    except Exception as e:
        print(f"Error: {e}")
main()
