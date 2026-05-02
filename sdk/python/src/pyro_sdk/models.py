"""Data models for the Pyro SDK."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from typing import Any


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


@dataclass
class ImageInfo:
    """Image metadata from the Pyro images API.

    Mirrors `internal/sandbox.ImageInfo`. Most fields are `omitzero` on the
    server, so dataclass defaults stay empty for in-flight or legacy entries.
    """

    name: str
    status: str = ""
    source: str = ""
    digest: str = ""
    error: str = ""
    rootfs_path: str = ""
    kernel_path: str = ""
    size: int = 0
    labels: dict[str, str] = field(default_factory=dict)
    created_at: str = ""

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> ImageInfo:
        return cls(
            name=data.get("name", ""),
            status=data.get("status", ""),
            source=data.get("source", ""),
            digest=data.get("digest", ""),
            error=data.get("error", ""),
            rootfs_path=data.get("rootfs_path", ""),
            kernel_path=data.get("kernel_path", ""),
            size=data.get("size", 0),
            labels=dict(data.get("labels") or {}),
            created_at=data.get("created_at", ""),
        )

    @property
    def is_ready(self) -> bool:
        return self.status == "ready"

    @property
    def is_terminal(self) -> bool:
        return self.status in ("ready", "failed")
