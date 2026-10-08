"""Golden: optimizer tái lập kết quả của prototype mekong-green (ENGINE_VERSION 2026.09.24).

Các ca được sinh bằng cách chạy chính prototype (seed tất định của tests/simulation.test.ts
và scripts/demo.ts) — cách sinh ghi trong optimizer/README.md. Dung sai: tương đối 1e-9,
tuyệt đối ±1 (số đã làm tròn đến đơn vị).
"""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from optcore import calendar as cal
from optcore.engine import evaluate, normalize_numbers
from optcore.params import resolve
from optcore.sensitivity import run_sensitivity

from .golden_compare import compare, to_snake

GOLDEN = Path(__file__).parent / "golden"
CASES = sorted(p.name for p in GOLDEN.iterdir() if (p / "snapshot.json").exists())

# Lệch có chủ đích so với prototype (optimizer/README.md, "Lệch đã biết"):
KNOWN = {
    # Prototype đọc flow.boatTripsToHub trên kết quả MỘT Hub (trường không tồn tại) → trips vắng
    # mặt, vehiclesRequired = null. Optimizer tính số chuyến ghe thật của chặng này.
    "$.hubs[0].legs[1].vehicles_required",
    "$.hubs[1].legs[1].vehicles_required",
    "$.hubs[2].legs[1].vehicles_required",
    "$.hubs[3].legs[1].vehicles_required",
    "$.hubs[4].legs[1].vehicles_required",
}


def load(case: str) -> tuple[dict, dict]:
    d = GOLDEN / case
    return (
        json.loads((d / "snapshot.json").read_text(encoding="utf-8")),
        json.loads((d / "expected.json").read_text(encoding="utf-8")),
    )


@pytest.mark.parametrize("case", CASES)
def test_golden_scenario_result_matches_prototype(case: str) -> None:
    """CGH FN-04..FN-12, FN-17 + mô hình luồng rơm: cùng đầu vào → cùng kết quả như prototype."""
    snap, exp = load(case)
    actual = normalize_numbers(evaluate(snap))
    expected = to_snake(exp["result"])
    expected.pop("warning_count")
    diffs = compare(expected, actual, skip=KNOWN)
    assert not diffs, f"{case}: {len(diffs)} khác biệt, ví dụ:\n" + "\n".join(diffs[:25])
    assert len(actual["warnings"]) == exp["result"]["warningCount"]
    for h, eh in zip(actual["hubs"], exp["result"]["hubs"], strict=True):
        assert len(h["warnings"]) == eh["warningCount"]


@pytest.mark.parametrize("case", CASES)
def test_golden_harvest_calendar_matches_prototype(case: str) -> None:
    """Lịch thu hoạch theo ngày (thực tế trải đều / suy diễn hình thang) khớp buildHarvestCalendar."""
    snap, exp = load(case)
    p = resolve(snap["params"])
    got = cal.build(snap["seasons"], snap.get("harvest_observations", {}), p.harvest_window_days)
    for c in exp["calendar"]:
        mine = got[c["season_key"]]
        assert mine.source == c["source"]
        want = [(d["dayOfYear"], d["weight"]) for d in c["days"]]
        assert [d for d, _ in mine.days] == [d for d, _ in want]
        for (_, a), (_, b) in zip(mine.days, want, strict=True):
            assert abs(a - b) < 1e-12


def test_golden_sensitivity_matches_prototype() -> None:
    """CGH FN-18: độ nhạy một-tham-số-một-lần khớp prototype (trừ #10, xem README)."""
    snap, exp = load("case2_three_hubs_approved_discounted")
    base = evaluate(snap)
    report = normalize_numbers(run_sensitivity(snap, base, snap["options"]["sensitivity_codes"]))
    want = to_snake(exp["sensitivity"])
    # #10: prototype đọc khoảng cách từ cache nên ghi đè hệ số hiệu chỉnh không có tác dụng;
    # optimizer tính lại khoảng cách Haversine theo hệ số → dòng này lệch có chủ đích.
    want_rows = [r for r in want["rows"] if r["code"] != "distance_correction"]
    got_rows = [r for r in report["rows"] if r["code"] != "distance_correction"]
    assert [r["code"] for r in got_rows] == [r["code"] for r in want_rows]
    diffs = compare(want_rows, got_rows, "$.rows")
    assert not diffs, "\n".join(diffs[:25])
    assert report["skipped"] == [{"code": s["code"], "reason": s["reason"]} for s in want["skipped"]]
    dc = next(r for r in report["rows"] if r["code"] == "distance_correction")
    assert dc["swing"] > 0, "hệ số #10 phải làm đổi chi phí khi khoảng cách là Haversine hiệu chỉnh"
