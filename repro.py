import time
import docker
from tests.helpers.cluster import RedisClusterManager

client = docker.from_env()
cluster = RedisClusterManager(client)

print("Waiting for cluster to be healthy...")
cluster.wait_for_healthy(timeout=180)

old_master_ip = cluster.get_master_ip()
old_master_name = cluster.get_master_node_name()
print(f"Old master: {old_master_name} ({old_master_ip})")

print(f"Killing {old_master_name}")
cluster.kill_node(old_master_name)

print("Waiting for master change...")
new_master_ip = cluster.wait_for_master_change(old_master_ip, timeout=120)
print(f"New master: {new_master_ip}")

print(f"Starting old master {old_master_name} back up...")
cluster.start_node(old_master_name)

print("Waiting 30 seconds for it to rejoin...")
time.sleep(30)

print(f"Fetching logs from {old_master_name}...")
logs = client.containers.get(cluster._container(old_master_name).name).logs().decode('utf-8')
print("--- LOGS ---")
for line in logs.splitlines()[-50:]:
    print(line)
print("------------")

role_cmd = (
    "redis-cli -p 6379 -a secret "
    "--tls "
    "--cert /etc/ssl/cluster/redis/server.crt "
    "--key /etc/ssl/cluster/redis/server.key "
    "--cacert /etc/ssl/cluster/ca/ca.crt "
    "ROLE"
)
exit_code, output = cluster.exec_in_container(old_master_name, role_cmd)
print(f"ROLE output:\n{output}")
