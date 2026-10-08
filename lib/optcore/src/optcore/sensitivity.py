"""FN-18 — phân tích độ nhạy một-tham-số-một-lần (chuyển từ `runSensitivity`).

V1 không chạy Monte Carlo (BR-02). Biên độ xếp hạng (tornado) tính trên Chi phí
vòng đời đầy đủ chuỗi / tấn. min/max lấy từ khối `params` của ảnh chụp.
"""

from __future__ import annotations

from typing import Any

from .engine import evaluate
from .jsmath import js_round
from .params import UnsupportedOverride, resolve, with_override

DEFAULT_CODES = (
    "straw_to_paddy_ratio",
    "collectable_ratio",
    "loss_ratio",
    "collection_labour_price",
    "collection_consumable_price",
    "road_freight_rate",
    "waterway_freight_rate",
    "distance_correction",
    "peak_inventory_factor",
    "capex_other",
    "discount_rate_pct",
)
HIGH_SENSITIVITY_THRESHOLD_PCT = 20
METHOD = (
    "One-at-a-time (BR-02) — V1 KHÔNG chạy mô phỏng Monte Carlo. Biên độ xếp hạng tính trên "
    "Chi phí vòng đời đầy đủ chuỗi/tấn (CAPEX + Σ Total Delivered Logistics Cost), vì TCO per Ton "
    "theo FN-12 BR-01 không chứa Collection/Transportation Cost."
)


def _triple(low: Any, base: Any, high: Any) -> dict[str, Any]:
    return {"min": low, "base": base, "max": high}


def run_sensitivity(snapshot: dict[str, Any], base: dict[str, Any], codes: list[str] | tuple[str, ...] | None = None) -> dict[str, Any]:
    codes = list(codes or DEFAULT_CODES)
    entries = {e["code"]: e for e in snapshot["params"].get("values", [])}
    base_params = resolve(snapshot["params"])
    rows: list[dict[str, Any]] = []
    skipped: list[dict[str, Any]] = []
    for code in codes:
        e = entries.get(code)
        if e is None:
            skipped.append({"code": code, "reason": "Không có trong danh mục tham số"})
            continue
        if e.get("min") is None or e.get("max") is None or e.get("value") is None:
            skipped.append({"code": code, "reason": "Chưa khai báo giá trị min/max"})
            continue
        lo_v, hi_v = float(e["min"]), float(e["max"])
        try:
            low = evaluate(snapshot, with_override(base_params, code, lo_v))
            high = evaluate(snapshot, with_override(base_params, code, hi_v))
        except UnsupportedOverride as exc:
            skipped.append({"code": code, "reason": str(exc)})
            continue
        lf, hf, bf = low["financial"], high["financial"], base["financial"]
        swing = abs((hf["lifecycle_cost_per_ton"] or 0) - (lf["lifecycle_cost_per_ton"] or 0))
        rows.append(
            {
                "code": code,
                "number": e.get("number"),
                "min": lo_v,
                "base": float(e["value"]),
                "max": hi_v,
                "tco_per_ton": _triple(lf["tco_per_ton"], bf["tco_per_ton"], hf["tco_per_ton"]),
                "lifecycle_cost_per_ton": _triple(lf["lifecycle_cost_per_ton"], bf["lifecycle_cost_per_ton"], hf["lifecycle_cost_per_ton"]),
                "cost_per_ton": _triple(low["costs"]["cost_per_ton"], base["costs"]["cost_per_ton"], high["costs"]["cost_per_ton"]),
                "roi_pct": _triple(lf["roi_pct"], bf["roi_pct"], hf["roi_pct"]),
                "payback_years": _triple(lf["payback_years"], bf["payback_years"], hf["payback_years"]),
                "swing": js_round(swing),
            }
        )
    rows.sort(key=lambda r: -r["swing"])  # AC-02: tornado giảm dần (sort ổn định)
    base_lc = base["financial"]["lifecycle_cost_per_ton"]
    max_swing = rows[0]["swing"] if rows else 0
    swing_pct = (max_swing / base_lc) * 100 if base_lc else 0.0
    return {
        "parameter_set_version": base_params.version,
        "base_tco_per_ton": base["financial"]["tco_per_ton"],
        "base_lifecycle_cost_per_ton": base_lc,
        "rows": rows,
        "skipped": skipped,
        "high_sensitivity": swing_pct > HIGH_SENSITIVITY_THRESHOLD_PCT,  # BR-04
        "swing_pct": js_round(swing_pct, 2),
        "method": METHOD,
    }
