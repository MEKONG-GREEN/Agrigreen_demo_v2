"""CLI và worker: đọc ảnh chụp → ghi kết quả; xử lý sự kiện qua Internal API (máy chủ giả)."""

from __future__ import annotations

import hashlib
import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

import pytest

from agrigreen_optimizer.cli import main
from agrigreen_optimizer.worker import InternalAPI, WorkerError, handle_event

from .paths import GOLDEN  # noqa: E402
RAW = (GOLDEN / "case1_two_hubs_unapproved" / "snapshot.json").read_bytes()


def test_cli_evaluate_writes_result(tmp_path: Path) -> None:
    src = tmp_path / "snapshot.json"
    out = tmp_path / "result.json"
    src.write_bytes(RAW)
    assert main(["evaluate", "--snapshot", str(src), "--out", str(out)]) == 0
    env = json.loads(out.read_text(encoding="utf-8"))
    assert env["status"] == "completed" and env["result"]["hub_count"] == 2


def test_cli_evaluate_invalid_snapshot_exit_2(tmp_path: Path) -> None:
    src = tmp_path / "bad.json"
    src.write_text('{"schema_version": 1}', encoding="utf-8")
    assert main(["evaluate", "--snapshot", str(src), "--out", str(tmp_path / "o.json")]) == 2


class _Fake(BaseHTTPRequestHandler):
    posted: list[tuple[str, str, bytes]] = []
    snapshot = RAW

    def log_message(self, *args: object) -> None:  # noqa: D401 - im lặng trong kiểm thử
        pass

    def do_GET(self) -> None:  # noqa: N802
        self.send_response(200)
        self.end_headers()
        self.wfile.write(self.snapshot)

    def do_POST(self) -> None:  # noqa: N802
        body = self.rfile.read(int(self.headers["Content-Length"]))
        _Fake.posted.append((self.path, self.headers["X-Tenant-ID"], body))
        self.send_response(201)
        self.end_headers()
        self.wfile.write(b'{"status":"completed"}')


@pytest.fixture()
def api() -> InternalAPI:
    srv = HTTPServer(("127.0.0.1", 0), _Fake)
    t = threading.Thread(target=srv.serve_forever, daemon=True)
    t.start()
    yield InternalAPI(f"http://127.0.0.1:{srv.server_address[1]}")
    srv.shutdown()


def _event(sha: str) -> dict:
    return {
        "event_type": "pf.simulation.job.requested",
        "tenant_id": "00000000-0000-7000-a000-000000000001",
        "payload": {"simulation_id": "00000000-0000-7000-9000-000000000001", "snapshot_sha256": sha},
    }


def test_worker_fetches_verifies_and_posts(api: InternalAPI) -> None:
    _Fake.posted.clear()
    out = handle_event(_event(hashlib.sha256(RAW).hexdigest()), api)
    assert out["status"] == "completed"
    path, tenant, body = _Fake.posted[0]
    assert path.endswith("/result") and tenant.endswith("000000000001")
    assert json.loads(body)["snapshot_sha256"] == hashlib.sha256(RAW).hexdigest()


def test_worker_refuses_tampered_snapshot(api: InternalAPI) -> None:
    _Fake.posted.clear()
    with pytest.raises(WorkerError):
        handle_event(_event("0" * 64), api)
    assert not _Fake.posted
