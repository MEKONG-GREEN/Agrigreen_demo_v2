"""Một lần chạy: byte ảnh chụp → phong bì kết quả (contracts/optimizer/result.v1.schema.json).

Băm sha256 tính trên ĐÚNG byte nhận được; Go so với băm đã lưu ở `input_snapshot`
trước khi nhận kết quả. Kết quả không chứa thời điểm tính → chạy lại cùng ảnh
chụp cho cùng byte (nộp lặp idempotent).
"""

from __future__ import annotations

import hashlib
import json
from typing import Any

from optcore.engine import SnapshotError, evaluate, normalize_numbers
from optcore.select_hubs import SolverUnavailable, select_hubs
from optcore.sensitivity import run_sensitivity
from pydantic import ValidationError

from . import ALGORITHM_VERSION, ENGINE_VERSION
from .snapshot import validate

RESULT_SCHEMA_VERSION = 1


def _envelope(snap: dict[str, Any] | None, sha: str) -> dict[str, Any]:
    snap = snap or {}
    return {
        "schema_version": RESULT_SCHEMA_VERSION,
        "simulation_id": snap.get("simulation_id"),
        "snapshot_id": snap.get("snapshot_id"),
        "snapshot_sha256": sha,
        "algorithm_version": ALGORITHM_VERSION,
        "engine_version": ENGINE_VERSION,
        "mode": (snap.get("options") or {}).get("mode", "evaluate"),
        "status": "completed",
        "error": None,
        "result": None,
        "sensitivity": None,
        "selection": None,
    }


def process(raw: bytes) -> dict[str, Any]:
    sha = hashlib.sha256(raw).hexdigest()
    try:
        data = json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        env = _envelope(None, sha)
        env.update(status="failed", error={"code": "optimizer.snapshot_invalid", "message": f"JSON hỏng: {exc}"})
        return env
    env = _envelope(data if isinstance(data, dict) else None, sha)
    try:
        validate(data)
    except ValidationError as exc:
        first = exc.errors()[0]
        loc = ".".join(str(x) for x in first.get("loc", ()))
        env.update(status="failed", error={"code": "optimizer.snapshot_invalid", "message": f"{loc}: {first.get('msg')}"})
        return env
    try:
        opts = data.get("options") or {}
        if opts.get("mode") == "select_hubs":
            selection = select_hubs(data, opts.get("max_hubs"))
            env["result"] = selection.pop("result")
            env["selection"] = selection
        else:
            env["result"] = evaluate(data)
        codes = opts.get("sensitivity_codes") or []
        if codes:
            env["sensitivity"] = run_sensitivity(data, env["result"], codes)
    except SnapshotError as exc:
        env.update(status="failed", error={"code": "optimizer.snapshot_incomplete", "message": str(exc)}, result=None, sensitivity=None)
    except SolverUnavailable as exc:
        env.update(status="failed", error={"code": "optimizer.solver_unavailable", "message": str(exc)}, result=None)
    return normalize_numbers(env)


def dumps(env: dict[str, Any]) -> bytes:
    """JSON ổn định (khóa sắp xếp, UTF-8) — cùng đầu vào cho cùng byte."""
    return json.dumps(env, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")
