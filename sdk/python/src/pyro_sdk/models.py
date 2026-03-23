"""Data models for the Pyro SDK."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime


@dataclass
class ExecResult:
    """Result of executing a command in a sandbox."""

    exit_code: int
    stdout: str
    stderr: str


@dataclass
class FileInfo:
    """Metadata about a file in a sandbox."""

    size: int
    mode: int


@dataclass
class SandboxInfo:
    """Sandbox metadata from the API."""

    id: str
    image: str
    state: str
    pid: int
    vsock_cid: int
    created_at: str
    expires_at: str
    api_key_id: str = ""
    vcpu: int = 1
    mem_mib: int = 256

    @classmethod
    def from_dict(cls, data: dict) -> SandboxInfo:
        return cls(
            id=data.get("id", ""),
            image=data.get("image", ""),
            state=data.get("state", ""),
            pid=data.get("pid", 0),
            vsock_cid=data.get("vsock_cid", 0),
            created_at=data.get("created_at", ""),
            expires_at=data.get("expires_at", ""),
            api_key_id=data.get("api_key_id", ""),
            vcpu=data.get("vcpu", 1),
            mem_mib=data.get("mem_mib", 256),
        )
