from __future__ import annotations

from typing import List

import requests


class MockApiClient:
    """Client for the mock Flux API used in integration tests."""

    def __init__(self, base_url: str = "http://localhost:8080") -> None:
        self.base_url = base_url.rstrip("/")

    def set_nodes(self, ips: List[str]) -> None:
        """Register a list of node IPs with the mock API."""
        nodes = [
            {
                "ip": ip,
                "name": f"node-{ip.replace('.', '-')}",
                "ports": [],
            }
            for ip in ips
        ]
        response = requests.post(
            f"{self.base_url}/admin/set-nodes",
            json={"nodes": nodes},
            timeout=10,
        )
        response.raise_for_status()

    def reset(self) -> None:
        """Reset the mock API to its default state."""
        response = requests.post(f"{self.base_url}/admin/reset", timeout=10)
        response.raise_for_status()

    def health(self) -> bool:
        """Check if the mock API is healthy."""
        try:
            response = requests.get(f"{self.base_url}/health", timeout=5)
            response.raise_for_status()
        except requests.RequestException:
            return False
        return response.json().get("status") == "ok"
