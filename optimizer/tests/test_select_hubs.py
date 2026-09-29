"""THỬ NGHIỆM select_hubs (MILP HiGHS) — bỏ qua khi chưa cài phụ thuộc tùy chọn [milp]."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

pytest.importorskip("highspy")

from agrigreen_optimizer.run import process  # noqa: E402
from agrigreen_optimizer.select_hubs import select_hubs  # noqa: E402

SNAP = json.loads((Path(__file__).parent / "golden" / "case3_five_hubs_lease_manual_baseline" / "snapshot.json").read_text(encoding="utf-8"))


def test_select_hubs_respects_max_hubs_and_reevaluates() -> None:
    out = select_hubs(SNAP, max_hubs=2)
    assert out["experimental"] is True and out["solver_status"] == "Optimal"
    assert 1 <= len(out["selected_hub_refs"]) <= 2
    assert out["result"]["hub_count"] == len(out["selected_hub_refs"])
    assert {h["hub_ref"] for h in out["result"]["hubs"]} == set(out["selected_hub_refs"])


def test_select_hubs_mode_through_envelope() -> None:
    s = dict(SNAP, options={"mode": "select_hubs", "sensitivity_codes": [], "max_hubs": 1})
    env = process(json.dumps(s).encode())
    assert env["status"] == "completed", env["error"]
    assert env["mode"] == "select_hubs" and env["selection"]["experimental"] is True
    assert len(env["selection"]["selected_hub_refs"]) <= 1
