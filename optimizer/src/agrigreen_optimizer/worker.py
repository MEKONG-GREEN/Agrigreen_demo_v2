"""Xử lý một sự kiện `pf.simulation.job.requested` (mục 12).

Luồng: đọc phong bì → GET ảnh chụp qua Internal API của nền tảng (mTLS) → kiểm
sha256 khớp sự kiện → tính → POST phong bì kết quả (idempotent theo mô phỏng).
Việc nhận thông điệp từ RabbitMQ (ACK sau khi POST thành công) do lớp chạy bọc
ngoài lo — xem README; module này chỉ dùng thư viện chuẩn để không thêm phụ thuộc.
"""

from __future__ import annotations

import hashlib
import json
import ssl
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any

from .run import dumps, process

EVENT_TYPE = "pf.simulation.job.requested"


class WorkerError(RuntimeError):
    pass


@dataclass
class InternalAPI:
    base_url: str
    cert: str | None = None
    key: str | None = None
    ca: str | None = None
    timeout: float = 30.0

    def _ctx(self) -> ssl.SSLContext | None:
        if not self.base_url.startswith("https://"):
            return None
        ctx = ssl.create_default_context(cafile=self.ca)
        if self.cert:
            ctx.load_cert_chain(self.cert, self.key)
        return ctx

    def request(self, method: str, path: str, tenant: str, body: bytes | None = None) -> tuple[int, bytes]:
        req = urllib.request.Request(self.base_url.rstrip("/") + path, data=body, method=method)
        req.add_header("X-Tenant-ID", tenant)
        if body is not None:
            req.add_header("Content-Type", "application/json")
        try:
            with urllib.request.urlopen(req, timeout=self.timeout, context=self._ctx()) as resp:  # noqa: S310 - URL cấu hình, không từ dữ liệu
                return resp.status, resp.read()
        except urllib.error.HTTPError as exc:
            return exc.code, exc.read()


def handle_event(envelope: dict[str, Any], api: InternalAPI) -> dict[str, Any]:
    if envelope.get("event_type") != EVENT_TYPE:
        raise WorkerError(f"sự kiện không thuộc optimizer: {envelope.get('event_type')}")
    tenant = envelope["tenant_id"]
    payload = envelope["payload"]
    sim_id = payload["simulation_id"]
    status, raw = api.request("GET", f"/internal/v1/simulations/{sim_id}/snapshot", tenant)
    if status != 200:
        raise WorkerError(f"không đọc được ảnh chụp: HTTP {status}")
    if hashlib.sha256(raw).hexdigest() != payload["snapshot_sha256"]:
        raise WorkerError("băm ảnh chụp không khớp sự kiện — từ chối tính")
    env = process(raw)
    status, body = api.request("POST", f"/internal/v1/simulations/{sim_id}/result", tenant, dumps(env))
    if status not in (200, 201):
        raise WorkerError(f"nộp kết quả thất bại: HTTP {status} {body[:200]!r}")
    return json.loads(body or b"{}")
