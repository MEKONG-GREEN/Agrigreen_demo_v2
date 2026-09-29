"""Quy tắc nghiệp vụ chính (CGH BRD Simulation v1.4) trên engine Python."""

from __future__ import annotations

import copy
import json
from pathlib import Path

import pytest

from agrigreen_optimizer.engine import evaluate
from agrigreen_optimizer.jsmath import fmt_vi, js_round, jsum
from agrigreen_optimizer.run import dumps, process

GOLDEN = Path(__file__).parent / "golden"


def snap(case: str = "case1_two_hubs_unapproved") -> dict:
    return json.loads((GOLDEN / case / "snapshot.json").read_text(encoding="utf-8"))


def test_js_rounding_and_summation_match_v8() -> None:
    assert js_round(2.5) == 3 and js_round(-2.5) == -2 and js_round(0.125, 2) == 0.13
    assert jsum([0.1] * 10) == 0.9999999999999999  # cộng tuần tự như reduce, không bù sai số
    assert fmt_vi(1234567.0) == "1.234.567" and fmt_vi(1.25, 1) == "1,3"


def test_fn01_br04_official_only_when_all_params_approved() -> None:
    """CGH FN-01 BR-04: còn tham số chưa phê duyệt → không được đánh dấu Chính thức."""
    unapproved = evaluate(snap("case1_two_hubs_unapproved"))
    assert unapproved["can_be_marked_official"] is False
    assert unapproved["unapproved_parameters"]
    approved = evaluate(snap("case2_three_hubs_approved_discounted"))
    assert approved["can_be_marked_official"] is True
    assert approved["unapproved_parameters"] == []


def test_fn05_ac03_each_cooperative_counted_once() -> None:
    r = evaluate(snap("case3_five_hubs_lease_manual_baseline"))
    refs = [c["cooperative_ref"] for h in r["hubs"] for c in h["cooperatives"]]
    assert len(refs) == len(set(refs))


def test_fn11_ac01_ac02_cost_per_ton_is_total_over_delivered() -> None:
    r = evaluate(snap("case2_three_hubs_approved_discounted"))
    c = r["costs"]
    assert c["total_delivered_logistics_cost"] == pytest.approx(
        c["collection_cost"] + c["warehouse_cost"] + c["handling_cost"] + c["transportation_cost"], abs=2
    )
    assert c["cost_per_ton"] == pytest.approx(c["total_delivered_logistics_cost"] / r["delivered_tons"], abs=1)
    avg = sum(h["costs"]["cost_per_ton"] for h in r["hubs"]) / len(r["hubs"])
    assert c["cost_per_ton"] != pytest.approx(avg, abs=1)  # không phải bình quân số học


def test_fn17_baseline_has_no_capex_warehouse_handling() -> None:
    b = evaluate(snap())["baseline"]
    assert b["available"] and b["capex"] == 0 and b["warehouse_cost"] == 0 and b["handling_cost"] == 0


def test_fn12_manual_baseline_source_is_labelled() -> None:
    f = evaluate(snap("case3_five_hubs_lease_manual_baseline"))["financial"]
    assert f["baseline_source"] == "manual" and f["baseline_cost_per_ton"] == 950_000


def test_flow_hub_inventory_never_exceeds_capacity_and_mass_balance() -> None:
    r = evaluate(snap("case4_small_capacity_overflow"))
    fl = r["flow"]
    for h in fl["hubs"]:
        assert max(h["inventory_curve"]) <= h["max_inventory_tons"] + 1
    # Chở thẳng hút gần hết rơm nên kho 5 000 t chỉ dùng một phần → cảnh báo sức chứa dư (FN-06 BR-03).
    assert r["hubs"][0]["capacity_warning"] is not None and r["hubs"][0]["capacity_warning"]["shortfall_pct"] > 0
    balance = fl["direct_field_plant_tons"] + fl["field_hub_tons"] + fl["uncollected_tons"]
    assert abs(balance - r["collectable_tons"]) / r["collectable_tons"] < 0.02


def test_same_snapshot_gives_identical_bytes() -> None:
    """Không có thời điểm tính trong kết quả → nộp lặp cho cùng byte (idempotent phía nền tảng)."""
    raw = (GOLDEN / "case1_two_hubs_unapproved" / "snapshot.json").read_bytes()
    assert dumps(process(raw)) == dumps(process(raw))


def test_missing_route_fails_with_incomplete_snapshot() -> None:
    s = snap()
    s["routes"] = [r for r in s["routes"] if r["to"] != "plant"]
    env = process(json.dumps(s).encode())
    assert env["status"] == "failed" and env["error"]["code"] == "optimizer.snapshot_incomplete"
    assert env["result"] is None


def test_haversine_routes_follow_correction_factor() -> None:
    """#10: khoảng cách nguồn Haversine hiệu chỉnh = straight_km × hệ số của bộ tham số."""
    s = snap()
    base = evaluate(copy.deepcopy(s))
    for v in s["params"]["values"]:
        if v["code"] == "distance_correction":
            v["text_value"] = {"road": "1.5", "waterway": "1.6"}
    more = evaluate(s)
    assert more["costs"]["transportation_cost"] > base["costs"]["transportation_cost"]
