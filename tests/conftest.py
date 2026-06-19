from __future__ import annotations

import subprocess
import time
from pathlib import Path

import docker
import pytest

from tests.helpers.cluster import RedisClusterManager
from tests.helpers.mock_api import MockApiClient


@pytest.fixture(scope="session")
def docker_client():
    return docker.from_env()


@pytest.fixture(scope="session")
def project_dir() -> Path:
    return Path(__file__).parent.parent.resolve()


@pytest.fixture(scope="session")
def compose_cmd() -> list[str]:
    return ["docker", "compose", "-f", "docker-compose.yml", "-f", "docker-compose.test.yml"]


@pytest.fixture(scope="session")
def built_image(project_dir: Path, compose_cmd: list[str]):
    subprocess.run(compose_cmd + ["build"], cwd=project_dir, check=True)
    return True


@pytest.fixture(scope="module")
def running_cluster(project_dir: Path, compose_cmd: list[str], built_image):
    subprocess.run(
        compose_cmd + ["down", "-v", "--remove-orphans"],
        cwd=project_dir,
        check=False,
    )
    time.sleep(10)
    subprocess.run(compose_cmd + ["up", "-d"], cwd=project_dir, check=True)
    yield
    subprocess.run(
        compose_cmd + ["down", "-v", "--remove-orphans"],
        cwd=project_dir,
        check=False,
    )
    time.sleep(10)


@pytest.fixture(scope="module")
def cluster(running_cluster, docker_client):
    manager = RedisClusterManager(docker_client)
    manager.wait_for_healthy(timeout=180)
    yield manager


@pytest.fixture(scope="module")
def mock_api(running_cluster):
    return MockApiClient("http://localhost:8080")
