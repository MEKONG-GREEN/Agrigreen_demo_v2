"""Xử lý một sự kiện `pf.simulation.job.requested` (mục 12).

Luồng: đọc phong bì → GET ảnh chụp qua Internal API của nền tảng (mTLS) → kiểm
sha256 khớp sự kiện → tính → POST phong bì kết quả (idempotent theo mô phỏng).
Việc nhận thông điệp từ RabbitMQ (ACK sau khi POST thành công) do `consumer.py`
lo; module này chỉ dùng thư viện chuẩn.

Phân loại lỗi cho lớp nhận: `WorkerError(permanent=True)` là lỗi không sửa được bằng
cách thử lại (phong bì sai, băm lệch, HTTP 4xx) → chuyển DLQ ngay; còn lại (mạng,
HTTP 5xx, 408, 429) → trả lại hàng đợi, quorum queue đếm lần giao (R-62).
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
    def __init__(self, message: str, *, permanent: bool = False) -> None:
        super().__init__(message)
        self.permanent = permanent


def _http_permanent(status: int) -> bool:
    """4xx là lỗi của chính yêu cầu (trừ 408/429); 5xx và lỗi mạng thì thử lại."""
    return 400 <= status < 500 and status not in (408, 429)


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

    def request(
        self, method: str, path: str, tenant: str, body: bytes | None = None, headers: dict[str, str] | None = None
    ) -> tuple[int, bytes]:
        req = urllib.request.Request(self.base_url.rstrip("/") + path, data=body, method=method)
        req.add_header("X-Tenant-ID", tenant)
        for k, v in (headers or {}).items():
            req.add_header(k, v)
        if body is not None:
            req.add_header("Content-Type", "application/json")
        try:
            with urllib.request.urlopen(req, timeout=self.timeout, context=self._ctx()) as resp:  # noqa: S310 - URL cấu hình, không từ dữ liệu
                return resp.status, resp.read()
        except urllib.error.HTTPError as exc:
            return exc.code, exc.read()
        except (urllib.error.URLError, OSError) as exc:  # mạng/TLS: thử lại
            raise WorkerError(f"không gọi được Internal API: {exc}") from exc


def handle_event(envelope: dict[str, Any], api: InternalAPI, traceparent: str | None = None) -> dict[str, Any]:
    """Xử lý một phong bì; `traceparent` (header AMQP, R-90) được chuyển tiếp sang Internal API."""
    if envelope.get("event_type") != EVENT_TYPE:
        raise WorkerError(f"sự kiện không thuộc optimizer: {envelope.get('event_type')}", permanent=True)
    try:
        tenant = str(envelope["tenant_id"])
        payload = envelope["payload"]
        sim_id = str(payload["simulation_id"])
        want_sha = str(payload["snapshot_sha256"])
    except (KeyError, TypeError) as exc:
        raise WorkerError(f"phong bì thiếu trường {exc}", permanent=True) from exc
    headers = {"traceparent": traceparent} if traceparent else None
    status, raw = api.request("GET", f"/internal/v1/simulations/{sim_id}/snapshot", tenant, headers=headers)
    if status != 200:
        raise WorkerError(f"không đọc được ảnh chụp: HTTP {status}", permanent=_http_permanent(status))
    if hashlib.sha256(raw).hexdigest() != want_sha:
        raise WorkerError("băm ảnh chụp không khớp sự kiện — từ chối tính", permanent=True)
    env = process(raw)
    status, body = api.request("POST", f"/internal/v1/simulations/{sim_id}/result", tenant, dumps(env), headers=headers)
    if status not in (200, 201):
        raise WorkerError(f"nộp kết quả thất bại: HTTP {status} {body[:200]!r}", permanent=_http_permanent(status))
    return json.loads(body or b"{}")
