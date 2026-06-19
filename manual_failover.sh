#!/bin/bash
docker compose -f docker-compose.yml -f docker-compose.test.yml down -v
docker compose -f docker-compose.yml -f docker-compose.test.yml build
docker compose -f docker-compose.yml -f docker-compose.test.yml up -d
echo "Waiting 20 seconds for cluster to initialize..."
sleep 20
echo "Killing Node 1..."
docker stop redis-cluster-node1
echo "Waiting 30 seconds for failover..."
sleep 30
echo "Node 2 Sentinel logs:"
docker logs redis-cluster-node2 | grep -i sentinel
echo "Node 3 Sentinel logs:"
docker logs redis-cluster-node3 | grep -i sentinel
