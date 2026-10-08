"""Bộ tham số đã chuẩn hóa đơn vị (FN-01) — chuyển từ `resolveParams` của prototype.

Quy ước: tiền = VNĐ, khối lượng = tấn, khoảng cách = km, diện tích = m².
Giá trị vào là giá trị THEO ĐƠN VỊ CỦA DANH MỤC (vd %, triệu đồng) đúng như
pf/masterdata lưu; module này quy đổi. Danh mục 57 tham số nằm ở phía Go
(`internal/platform/simulation/domain/catalog.go`) — ảnh chụp đầu vào mang sẵn
giá trị, min/max và cờ phê duyệt, nên optimizer không giữ bản sao danh mục.
"""

from __future__ import annotations

from collections.abc import Callable
from dataclasses import dataclass, field, replace
from typing import Any

MILLION = 1_000_000

# Giá trị danh mục của nhóm mở rộng #50–#57 (numOrCatalog của prototype): chỉ dùng
# khi ảnh chụp thiếu giá trị — Go luôn điền, nên đây là lưới an toàn.
_FLOW_CATALOG_BASE = {
    "boat_registered_tons": 100.0,
    "boat_straw_payload_tons": 90.0,
    "barge_small_payload_tons": 1_000.0,
    "barge_large_payload_tons": 2_000.0,
    "plant_operating_days": 330.0,
    "hub_processing_cost": 120_000.0,
    "hub_processing_loss_pct": 2.0,
    "harvest_window_days": 45.0,
}


@dataclass(frozen=True)
class CorrectionFactors:
    road: float
    waterway: float


@dataclass(frozen=True)
class SimulationParams:
    version: str
    correction_factors: CorrectionFactors  # 10
    plant_annual_demand_tons: float  # 12
    default_service_radius_km: float  # 13
    default_design_capacity_tons: float  # 14
    distance_error_threshold_pct: float  # 15
    straw_to_paddy_ratio: float  # 17
    collectable_ratio: float  # 18 (0..1)
    loss_ratio: float  # 19 (0..1)
    collection_labour_price_per_ton: float  # 20
    collection_consumable_price_per_ton: float  # 21
    road_freight_rate_per_ton_km: float  # 22
    waterway_freight_rate_per_ton_km: float  # 23
    warehouse_build_price_per_m2: float  # 24 VNĐ/m²
    yard_build_price_per_m2: float  # 25 VNĐ/m²
    bale_press_unit_price: float  # 26 VNĐ/máy
    forklift_unit_price: float  # 26 VNĐ/máy
    road_feasible_distance_km: float  # 27
    land_lease_price_per_m2_year: float  # 28
    warehouse_area_norm_m2_per_ton: float  # 29
    yard_area_norm_m2_per_ton: float  # 30
    bale_press_capacity_tons_year: float  # 31
    forklift_capacity_tons_year: float  # 32
    truck_payload_tons: float  # 33
    barge_payload_tons: float  # 34
    peak_inventory_factor: float  # 35
    road_speed_kmh: float  # 36
    waterway_speed_kmh: float  # 37
    handling_hours_per_trip: float  # 38
    capex_other: float  # 39 VNĐ
    staff_count: float  # 40
    staff_salary_per_month: float  # 41
    utility_cost_per_year: float  # 42
    maintenance_rate: float  # 43 (0..1)
    discount_rate: float  # 44 (0..1)
    opex_escalation: float  # 45 (0..1)
    ramp_up_ratio: float  # 46 (0..1)
    lifecycle_years: int  # 47
    min_roi_pct: float | None  # 48
    max_payback_years: float | None  # 49
    boat_registered_tons: float  # 50
    boat_straw_payload_tons: float  # 51
    barge_small_payload_tons: float  # 52
    barge_large_payload_tons: float  # 53
    plant_operating_days: int  # 54
    hub_processing_cost_per_ton: float  # 55
    hub_processing_loss_ratio: float  # 56 (0..1)
    harvest_window_days: int  # 57
    unapproved: tuple[dict[str, Any], ...] = field(default_factory=tuple)


def _js_round_int(value: float) -> int:
    import math

    return int(math.floor(value + 0.5))


def resolve(param_block: dict[str, Any]) -> SimulationParams:
    """Dựng bộ tham số từ khối `params` của ảnh chụp."""
    entries = {e["code"]: e for e in param_block.get("values", [])}

    def raw(code: str) -> float | None:
        e = entries.get(code)
        if e is None or e.get("value") is None:
            return None
        return float(e["value"])

    def num(code: str, fallback: float = 0.0) -> float:
        v = raw(code)
        return fallback if v is None else v

    def num_or_catalog(code: str) -> float:
        v = raw(code)
        return _FLOW_CATALOG_BASE[code] if v is None else v

    def text(code: str) -> dict[str, float]:
        e = entries.get(code)
        tv = (e or {}).get("text_value") or {}
        return {k: float(v) for k, v in tv.items() if v is not None}

    correction = text("distance_correction")
    equipment = text("equipment_price")
    return SimulationParams(
        version=str(param_block.get("set_version", "")),
        correction_factors=CorrectionFactors(
            road=correction.get("road", num("distance_correction", 1.3)),
            waterway=correction.get("waterway", num("distance_correction", 1.3)),
        ),
        plant_annual_demand_tons=num("plant_annual_demand"),
        default_service_radius_km=num("default_service_radius_km", 30),
        default_design_capacity_tons=num("default_design_capacity_tons", 60_000),
        distance_error_threshold_pct=num("distance_error_threshold_pct", 10),
        straw_to_paddy_ratio=num("straw_to_paddy_ratio", 1),
        collectable_ratio=num("collectable_ratio", 45) / 100,
        loss_ratio=num("loss_ratio", 5) / 100,
        collection_labour_price_per_ton=num("collection_labour_price"),
        collection_consumable_price_per_ton=num("collection_consumable_price"),
        road_freight_rate_per_ton_km=num("road_freight_rate"),
        waterway_freight_rate_per_ton_km=num("waterway_freight_rate"),
        warehouse_build_price_per_m2=num("warehouse_build_price") * MILLION,
        yard_build_price_per_m2=num("yard_build_price") * MILLION,
        bale_press_unit_price=equipment.get("bale_press", num("equipment_price")) * MILLION,
        forklift_unit_price=equipment.get("forklift", num("equipment_price")) * MILLION,
        road_feasible_distance_km=num("road_feasible_distance_km", 120),
        land_lease_price_per_m2_year=num("land_lease_price"),
        warehouse_area_norm_m2_per_ton=num("warehouse_area_norm", 0.85),
        yard_area_norm_m2_per_ton=num("yard_area_norm", 0.6),
        bale_press_capacity_tons_year=num("bale_press_capacity", 18_000),
        forklift_capacity_tons_year=num("forklift_capacity", 25_000),
        truck_payload_tons=num("truck_payload_tons", 15),
        barge_payload_tons=num("barge_payload_tons", 300),
        peak_inventory_factor=num("peak_inventory_factor", 0.3),
        road_speed_kmh=num("road_speed_kmh", 40),
        waterway_speed_kmh=num("waterway_speed_kmh", 12),
        handling_hours_per_trip=num("handling_hours_per_trip", 3.5),
        capex_other=num("capex_other") * MILLION,
        staff_count=num("staff_count"),
        staff_salary_per_month=num("staff_salary_month"),
        utility_cost_per_year=num("utility_cost_year"),
        maintenance_rate=num("maintenance_rate_pct") / 100,
        discount_rate=num("discount_rate_pct") / 100,
        opex_escalation=num("opex_escalation_pct") / 100,
        ramp_up_ratio=num("ramp_up_pct", 100) / 100,
        lifecycle_years=max(1, _js_round_int(num("lifecycle_years", 10))),
        min_roi_pct=raw("min_roi_pct"),
        max_payback_years=raw("max_payback_years"),
        boat_registered_tons=num_or_catalog("boat_registered_tons"),
        boat_straw_payload_tons=num_or_catalog("boat_straw_payload_tons"),
        barge_small_payload_tons=num_or_catalog("barge_small_payload_tons"),
        barge_large_payload_tons=num_or_catalog("barge_large_payload_tons"),
        plant_operating_days=max(1, _js_round_int(num_or_catalog("plant_operating_days"))),
        hub_processing_cost_per_ton=num_or_catalog("hub_processing_cost"),
        hub_processing_loss_ratio=num_or_catalog("hub_processing_loss_pct") / 100,
        harvest_window_days=max(1, _js_round_int(num_or_catalog("harvest_window_days"))),
        unapproved=tuple(dict(u) for u in param_block.get("unapproved", [])),
    )


# Tham số hỗ trợ trong phân tích độ nhạy (FN-18) — withOverride của prototype.
SENSITIVITY_SUPPORTED = (
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
    "opex_escalation_pct",
    "ramp_up_pct",
    "warehouse_build_price",
    "yard_build_price",
    "warehouse_area_norm",
    "yard_area_norm",
    "staff_count",
    "staff_salary_month",
    "utility_cost_year",
    "maintenance_rate_pct",
    "boat_straw_payload_tons",
    "barge_small_payload_tons",
    "hub_processing_cost",
    "hub_processing_loss_pct",
    "harvest_window_days",
    "plant_operating_days",
)


class UnsupportedOverride(ValueError):
    pass


def with_override(p: SimulationParams, code: str, value: float) -> SimulationParams:
    """Áp một giá trị ghi đè (FN-18) — cùng bảng ánh xạ với `withOverride` của prototype."""
    m: dict[str, Callable[[], dict[str, Any]]] = {
        "straw_to_paddy_ratio": lambda: {"straw_to_paddy_ratio": value},
        "collectable_ratio": lambda: {"collectable_ratio": value / 100},
        "loss_ratio": lambda: {"loss_ratio": value / 100},
        "collection_labour_price": lambda: {"collection_labour_price_per_ton": value},
        "collection_consumable_price": lambda: {"collection_consumable_price_per_ton": value},
        "road_freight_rate": lambda: {"road_freight_rate_per_ton_km": value},
        "waterway_freight_rate": lambda: {"waterway_freight_rate_per_ton_km": value},
        "distance_correction": lambda: {"correction_factors": CorrectionFactors(road=value, waterway=value * 1.1)},
        "peak_inventory_factor": lambda: {"peak_inventory_factor": value},
        "capex_other": lambda: {"capex_other": value * MILLION},
        "discount_rate_pct": lambda: {"discount_rate": value / 100},
        "opex_escalation_pct": lambda: {"opex_escalation": value / 100},
        "ramp_up_pct": lambda: {"ramp_up_ratio": value / 100},
        "warehouse_build_price": lambda: {"warehouse_build_price_per_m2": value * MILLION},
        "yard_build_price": lambda: {"yard_build_price_per_m2": value * MILLION},
        "warehouse_area_norm": lambda: {"warehouse_area_norm_m2_per_ton": value},
        "yard_area_norm": lambda: {"yard_area_norm_m2_per_ton": value},
        "staff_count": lambda: {"staff_count": value},
        "staff_salary_month": lambda: {"staff_salary_per_month": value},
        "utility_cost_year": lambda: {"utility_cost_per_year": value},
        "maintenance_rate_pct": lambda: {"maintenance_rate": value / 100},
        "boat_straw_payload_tons": lambda: {"boat_straw_payload_tons": value},
        "barge_small_payload_tons": lambda: {"barge_small_payload_tons": value},
        "hub_processing_cost": lambda: {"hub_processing_cost_per_ton": value},
        "hub_processing_loss_pct": lambda: {"hub_processing_loss_ratio": value / 100},
        "harvest_window_days": lambda: {"harvest_window_days": _js_round_int(value)},
        "plant_operating_days": lambda: {"plant_operating_days": _js_round_int(value)},
    }
    if code not in m:
        raise UnsupportedOverride(f'Tham số "{code}" chưa được hỗ trợ trong phân tích độ nhạy.')
    return replace(p, **m[code]())
