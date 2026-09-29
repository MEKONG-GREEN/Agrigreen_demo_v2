"""Engine đánh giá kịch bản Hub — chuyển công thức từ `simulateScenario` (engine.ts).

Chuỗi: FN-04 khoảng cách → FN-05 vùng phục vụ → FN-06 sản lượng theo mùa vụ →
FN-07 Collection Cost → FN-08 Transportation Cost (3 chặng) → FN-09 kho & thiết
bị → FN-10 CAPEX/OPEX → FN-11 Cost/Ton → FN-12 TCO/ROI/Payback; đối chứng FN-17
No-Hub Baseline; mô hình luồng rơm theo ngày (flow.py).

Khác prototype có chủ đích:
- Không định tuyến: mọi khoảng cách lấy từ `routes` của ảnh chụp bất biến (mục 16).
  Khoảng cách nguồn Haversine hiệu chỉnh được tính lại = straight_km × hệ số #10
  của bộ tham số đang đánh giá (để phân tích độ nhạy #10 có tác dụng; prototype
  đọc cache nên #10 không đổi kết quả).
- Không có tên/toạ độ HTX, Hub (RB-7): chỉ mã tham chiếu và mã Hub.
- Kết quả không mang thời điểm tính: cùng ảnh chụp → cùng byte kết quả.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from . import calendar as cal
from .flow import FlowHubInput, FlowSupplier, simulate_flow
from .jsmath import ceil_int, fmt_num, fmt_pct, fmt_tons, fmt_vi, js_round, jsum, num
from .params import SimulationParams, resolve

ENGINE_VERSION = "2026.09.24"
VEHICLE_HOURS_PER_YEAR = 3_000
MONTHS_PER_YEAR = 12

SOURCE_LABEL = {
    "routing_thuc_te": "Định tuyến thực tế",
    "tuyen_so_hoa_noi_bo": "Định tuyến trên tuyến số hóa nội bộ",
    "haversine_hieu_chinh": "Haversine hiệu chỉnh",
}

BASELINE_LABEL = {
    "no_hub": "No-Hub Baseline (tự tính, FN-17)",
    "scenario": "Kịch bản Hub khác",
    "manual": "Người dùng nhập tay",
    "thieu_baseline": "Thiếu baseline — chỉ có TCO",
}


class SnapshotError(ValueError):
    """Ảnh chụp thiếu dữ liệu bắt buộc (vd thiếu kết quả định tuyến của một cặp điểm)."""


def _f(v: Any) -> float:
    return float(v)


def _opt(v: Any) -> float | None:
    return None if v is None else float(v)


@dataclass(frozen=True)
class Distance:
    km: float
    source: str
    waterway_access: bool

    @property
    def label(self) -> str:
        return SOURCE_LABEL.get(self.source, self.source)


class Routes:
    """Tra kết quả định tuyến đã chốt trong ảnh chụp (FN-04 BR-01..BR-04)."""

    def __init__(self, routes: list[dict[str, Any]], params: SimulationParams):
        self._idx = {(r["from"], r["to"], r["mode"]): r for r in routes}
        self._params = params

    def _factor(self, mode: str) -> float:
        v = self._params.correction_factors.road if mode == "road" else self._params.correction_factors.waterway
        return v if v and v >= 1 else 1.0

    def distance(self, frm: str, to: str, mode: str) -> Distance:
        r = self._idx.get((frm, to, mode))
        if r is None:
            raise SnapshotError(f"ảnh chụp thiếu kết quả định tuyến {mode} {frm} → {to}")
        source = r["source"]
        km = _f(r["distance_km"])
        if source == "haversine_hieu_chinh" and r.get("straight_km") is not None:
            km = js_round(_f(r["straight_km"]) * self._factor(mode), 3)
        return Distance(km=km, source=source, waterway_access=bool(r.get("waterway_access", False)))

    def has_waterway_access(self, frm: str, to: str) -> bool:
        r = self._idx.get((frm, to, "waterway"))
        return bool(r and r.get("waterway_access"))


def _coop(ref: str) -> str:
    return f"coop:{ref}"


def _hub(ref: str) -> str:
    return f"hub:{ref}"


PLANT = "plant"


# =====================================================================
# FN-06 — sản lượng theo mùa vụ
# =====================================================================


def build_season_supply(rows: list[dict[str, Any]], sort_order: dict[str, int], p: SimulationParams) -> list[dict[str, Any]]:
    ordered = sorted(rows, key=lambda r: sort_order.get(r["season_key"], 0))
    out = []
    for r in ordered:
        paddy = _f(r["paddy_tons"])
        total = paddy * p.straw_to_paddy_ratio  # BR-01 (quyết định B3)
        collectable = total * p.collectable_ratio  # BR-02
        delivered = collectable * (1 - p.loss_ratio)  # BR-04
        out.append(
            {
                "season_key": r["season_key"],
                "total_available_tons": js_round(total),
                "collectable_tons": js_round(collectable),
                "delivered_tons": js_round(delivered),
                "paddy_tons": paddy,
                "planted_area_ha": _f(r["planted_area_ha"]),
            }
        )
    return out


def merge_seasons(items: list[dict[str, Any]]) -> list[dict[str, Any]]:
    acc: dict[str, dict[str, Any]] = {}
    for s in items:
        e = acc.get(s["season_key"])
        if e is None:
            acc[s["season_key"]] = dict(s)
            continue
        for k in ("total_available_tons", "collectable_tons", "delivered_tons", "paddy_tons", "planted_area_ha"):
            e[k] = js_round(e[k] + s[k])
    return list(acc.values())


# =====================================================================
# FN-09 / FN-10 / FN-11
# =====================================================================


def compute_sizing(collectable_annual: float, p: SimulationParams, simulated_peak: float | None) -> dict[str, Any]:
    heuristic = collectable_annual * p.peak_inventory_factor
    from_sim = simulated_peak is not None and simulated_peak > 0
    peak = simulated_peak if from_sim else heuristic
    assert peak is not None
    wh_cap = peak
    wh_area = wh_cap * p.warehouse_area_norm_m2_per_ton
    yard_area = wh_cap * p.yard_area_norm_m2_per_ton
    bale = max(1, ceil_int(collectable_annual / max(p.bale_press_capacity_tons_year, 1))) if collectable_annual > 0 else 0
    fork = max(1, ceil_int(collectable_annual / max(p.forklift_capacity_tons_year, 1))) if collectable_annual > 0 else 0
    if from_sim:
        notice = (
            f"Ước tính — Peak Inventory {fmt_vi(js_round(peak))} tấn lấy từ "
            "mô phỏng luồng rơm theo ngày (tồn kho cao nhất trong năm vận hành ổn định), "
            "không còn dùng hệ số tồn kho cao điểm — đóng rủi ro RS-06."
        )
    else:
        notice = (
            f"Ước tính — Peak Inventory suy ra từ hệ số tồn kho cao điểm {num(p.peak_inventory_factor)} "
            "(FN-09 BR-04, RS-06), chưa dùng mô hình tồn kho theo ngày."
        )
    return {
        "peak_inventory_tons": js_round(peak),
        "warehouse_capacity_tons": js_round(wh_cap),
        "warehouse_area_m2": js_round(wh_area),
        "yard_area_m2": js_round(yard_area),
        "bale_press_count": bale,
        "forklift_count": fork,
        "peak_inventory_source": "mo_phong_theo_ngay" if from_sim else "he_so_uoc_luong",
        "estimate_notice": notice,
    }


def compute_capex(sizing: dict[str, Any], p: SimulationParams) -> dict[str, float]:
    wh = sizing["warehouse_area_m2"] * p.warehouse_build_price_per_m2
    yard = sizing["yard_area_m2"] * p.yard_build_price_per_m2
    bale = sizing["bale_press_count"] * p.bale_press_unit_price
    fork = sizing["forklift_count"] * p.forklift_unit_price
    construction = wh + yard
    equipment = bale + fork
    other = p.capex_other if sizing["warehouse_capacity_tons"] > 0 else 0.0
    return {
        "warehouse_construction": js_round(wh),
        "yard_construction": js_round(yard),
        "construction": js_round(construction),
        "bale_press": js_round(bale),
        "forklift": js_round(fork),
        "equipment": js_round(equipment),
        "other": js_round(other),
        "total": js_round(construction + equipment + other),
    }


def compute_opex(sizing: dict[str, Any], capex: dict[str, float], land_mode: str, p: SimulationParams, processed: float) -> dict[str, float]:
    staff = p.staff_count * p.staff_salary_per_month * MONTHS_PER_YEAR
    utilities = p.utility_cost_per_year
    maintenance = capex["equipment"] * p.maintenance_rate  # BR-02: chỉ CAPEX thiết bị
    land = (sizing["warehouse_area_m2"] + sizing["yard_area_m2"]) * p.land_lease_price_per_m2_year if land_mode == "thue" else 0.0
    processing = processed * p.hub_processing_cost_per_ton
    other = 0.0
    total = staff + utilities + maintenance + land + processing + other
    return {
        "staff": js_round(staff),
        "utilities": js_round(utilities),
        "maintenance": js_round(maintenance),
        "land_lease": js_round(land),
        "processing": js_round(processing),
        "other_opex": js_round(other),
        "total": js_round(total),
        "handling_cost_annual": js_round(staff + maintenance + processing),  # BR-05 (B8)
        "warehouse_cost_annual": js_round(utilities + land + other),
    }


def build_cost_chain(collection: float, warehouse: float, handling: float, field_hub: float, hub_plant: float, delivered: float) -> dict[str, Any]:
    transport = field_hub + hub_plant
    total = collection + warehouse + handling + transport
    cpt = total / delivered if delivered > 0 else None  # BR-04: 0 → "Không xác định"
    return {
        "collection_cost": js_round(collection),
        "warehouse_cost": js_round(warehouse),
        "handling_cost": js_round(handling),
        "transportation_cost": js_round(transport),
        "transportation_field_hub": js_round(field_hub),
        "transportation_hub_plant": js_round(hub_plant),
        "total_delivered_logistics_cost": js_round(total),
        "cost_per_ton": None if cpt is None else js_round(cpt),
        "cost_per_ton_note": "chưa gồm khấu hao đầu tư",
    }


def _merge_sum(items: list[dict[str, Any]], keys: tuple[str, ...]) -> dict[str, float]:
    return {k: js_round(jsum(i[k] for i in items)) for k in keys}


CAPEX_KEYS = ("warehouse_construction", "yard_construction", "construction", "bale_press", "forklift", "equipment", "other", "total")
OPEX_KEYS = ("staff", "utilities", "maintenance", "land_lease", "processing", "other_opex", "total", "handling_cost_annual", "warehouse_cost_annual")


def _estimate_fleet(trips: int, lead_time: float) -> int:
    if trips <= 0:
        return 0
    cycle = max(lead_time * 2, 1)
    return max(1, ceil_int((trips * cycle) / VEHICLE_HOURS_PER_YEAR))


def _decide_mode(override: str, distance_km: float, frm: str, to: str, routes: Routes, p: SimulationParams) -> tuple[str, bool]:
    """FN-08 BR-04: mặc định đường bộ; gợi ý thủy khi vượt #27 VÀ tiếp giáp mạng thủy (#9); người dùng ghi đè."""
    exceeds = distance_km > p.road_feasible_distance_km
    has_water = routes.has_waterway_access(frm, to) if exceeds else False
    suggested = exceeds and has_water
    if override in ("road", "waterway"):
        return override, suggested
    return ("waterway" if suggested else "road"), suggested


# =====================================================================
# Kết quả một Hub
# =====================================================================


def _field_plant_leg(served: list[dict[str, Any]], flow: dict[str, Any] | None, p: SimulationParams) -> dict[str, Any]:
    total = jsum(c["collectable_tons"] for c in served)
    ratio = max(0.0, 1 - min(1.0, flow["received_from_field_tons"] / total)) if flow and total > 0 else 0.0
    tons = ton_km = lead_w = 0.0
    for c in served:
        t = c["collectable_tons"] * ratio
        tons += t
        ton_km += t * c["distance_to_plant_km"]
        lead_w += t * (c["distance_to_plant_km"] / max(p.waterway_speed_kmh, 1) + p.handling_hours_per_trip)
    cost = ton_km * p.waterway_freight_rate_per_ton_km
    lead = lead_w / tons if tons > 0 else 0.0
    trips = ceil_int(tons / max(p.boat_straw_payload_tons, 1)) if tons > 0 else 0
    return {
        "leg": "field_plant",
        "label": f"Ruộng → Nhà máy (ghe {fmt_num(p.boat_registered_tons)} tấn, chở thẳng)",
        "mode": "waterway",
        "mode_decided_by": "mac_dinh",
        "waterway_suggested": False,
        "tons": js_round(tons),
        "weighted_avg_distance_km": js_round(ton_km / tons, 3) if tons > 0 else 0,
        "ton_km": js_round(ton_km),
        "cost": js_round(cost),
        "trips": trips,
        "vehicles_required": _estimate_fleet(trips, lead),
        "lead_time_hours": js_round(lead, 2),
    }


def _field_hub_leg(hub: dict[str, Any], served: list[dict[str, Any]], routes: Routes, p: SimulationParams, flow: dict[str, Any] | None) -> dict[str, Any]:
    cost = ton_km = tons = lead_w = 0.0
    any_suggested = False
    water_legs = 0
    override = hub["mode_field_hub"]
    total = jsum(c["collectable_tons"] for c in served)
    ratio = min(1.0, flow["received_from_field_tons"] / total) if flow and total > 0 else 1.0
    hkey = _hub(hub["hub_ref"])
    for c in served:
        ckey = _coop(c["cooperative_ref"])
        mode, suggested = _decide_mode(override, c["distance_to_hub_km"], ckey, hkey, routes, p)
        if suggested:
            any_suggested = True
        if mode == "waterway":
            water_legs += 1
        dist = routes.distance(ckey, hkey, "waterway").km if mode == "waterway" else c["distance_to_hub_km"]
        rate = p.waterway_freight_rate_per_ton_km if mode == "waterway" else p.road_freight_rate_per_ton_km
        speed = p.waterway_speed_kmh if mode == "waterway" else p.road_speed_kmh
        t = c["collectable_tons"] * ratio
        cost += t * dist * rate
        ton_km += t * dist
        tons += t
        lead_w += t * (dist / max(speed, 1) + p.handling_hours_per_trip)
    if override in ("road", "waterway"):
        dominant = override
    else:
        dominant = "waterway" if water_legs > len(served) / 2 else "road"
    payload = p.boat_straw_payload_tons if dominant == "waterway" else p.truck_payload_tons
    if flow:
        # Prototype đọc `flow.boatTripsToHub` trên kết quả của MỘT Hub — trường không tồn tại
        # (trips = undefined, vehiclesRequired = null). Ý định rõ: số chuyến ghe chở phần rơm qua
        # Hub này. Sửa có chủ đích, ghi trong README (lệch golden đã biết).
        trips = ceil_int(tons / max(p.boat_straw_payload_tons, 1)) if tons > 0 else 0
    else:
        trips = ceil_int(tons / max(payload, 1)) if tons > 0 else 0
    lead = lead_w / tons if tons > 0 else 0.0
    if override != "auto":
        decided = "nguoi_dung_ghi_de"
    else:
        decided = "goi_y_he_thong" if any_suggested else "mac_dinh"
    return {
        "leg": "field_hub",
        "label": f"Ruộng → Hub (ghe {fmt_num(p.boat_registered_tons)} tấn)",
        "mode": dominant,
        "mode_decided_by": decided,
        "waterway_suggested": any_suggested,
        "tons": js_round(tons),
        "weighted_avg_distance_km": js_round(ton_km / tons, 3) if tons > 0 else 0,
        "ton_km": js_round(ton_km),
        "cost": js_round(cost),
        "trips": trips,
        "vehicles_required": _estimate_fleet(trips, lead),
        "lead_time_hours": js_round(lead, 2),
    }


def _hub_plant_leg(hub: dict[str, Any], road: Distance, routes: Routes, p: SimulationParams, flow: dict[str, Any] | None) -> dict[str, Any]:
    shipped = flow["shipped_to_plant_tons"] if flow else 0
    override = hub["mode_hub_plant"]
    hkey = _hub(hub["hub_ref"])
    mode, suggested = _decide_mode(override, road.km, hkey, PLANT, routes, p)
    dist = routes.distance(hkey, PLANT, "waterway").km if mode == "waterway" else road.km
    rate = p.waterway_freight_rate_per_ton_km if mode == "waterway" else p.road_freight_rate_per_ton_km
    speed = p.waterway_speed_kmh if mode == "waterway" else p.road_speed_kmh
    barge = hub["_barge_payload"]
    payload = barge if mode == "waterway" else p.truck_payload_tons
    cost = shipped * dist * rate
    if flow:
        trips = flow["full_barge_trips"] + flow["partial_barge_trips"]
    else:
        trips = ceil_int(shipped / max(payload, 1)) if shipped > 0 else 0
    lead = dist / max(speed, 1) + p.handling_hours_per_trip
    if override != "auto":
        decided = "nguoi_dung_ghi_de"
    else:
        decided = "goi_y_he_thong" if suggested else "mac_dinh"
    return {
        "leg": "hub_plant",
        "label": f"Hub → Nhà máy (sà lan {fmt_num(barge)} tấn, đầy tải)",
        "mode": mode,
        "mode_decided_by": decided,
        "waterway_suggested": suggested,
        "tons": js_round(shipped),
        "weighted_avg_distance_km": js_round(dist, 3),
        "ton_km": js_round(shipped * dist),
        "cost": js_round(cost),
        "trips": trips,
        "vehicles_required": _estimate_fleet(trips, lead),
        "lead_time_hours": js_round(lead, 2),
    }


def _hub_result(hub: dict[str, Any], served: list[dict[str, Any]], routes: Routes, p: SimulationParams, flow: dict[str, Any] | None) -> dict[str, Any]:
    warnings: list[str] = []
    code = hub["code"]
    seasons = merge_seasons([s for c in served for s in c["seasons"]])
    collectable = jsum(c["collectable_tons"] for c in served)
    delivered = jsum(c["delivered_tons"] for c in served)
    available = jsum(c["total_available_tons"] for c in served)
    radius = hub["_radius"]
    capacity = hub["_capacity"]

    if not served:
        warnings.append(f"{code}: không có vùng nguyên liệu phù hợp trong bán kính {num(radius)} km.")

    unit_collection = p.collection_labour_price_per_ton + p.collection_consumable_price_per_ton
    if unit_collection <= 0:
        warnings.append(f"{code}: chưa cấu hình đơn giá thu gom — không hiển thị Collection Cost.")
    collection = collectable * unit_collection  # FN-07

    hub_to_plant = routes.distance(_hub(hub["hub_ref"]), PLANT, "road")
    fp = _field_plant_leg(served, flow, p)
    fh = _field_hub_leg(hub, served, routes, p, flow)
    hp = _hub_plant_leg(hub, hub_to_plant, routes, p, flow)
    if hp["waterway_suggested"] and hp["mode"] == "road":
        warnings.append(
            f"{code}: chặng Hub→Nhà máy dài {fmt_num(hp['weighted_avg_distance_km'])} km vượt ngưỡng "
            f"{num(p.road_feasible_distance_km)} km — nên đánh giá phương án đường thủy, chi phí đường bộ có thể tăng cao."
        )

    sizing = compute_sizing(collectable, p, flow["peak_inventory_tons"] if flow else None)

    capacity_warning = None
    if flow and capacity > 0:
        if flow["days_at_capacity"] > 0:
            overflow_pct = (flow["overflow_tons"] / collectable) * 100 if collectable > 0 else 0.0
            msg = (
                f"Kho đầy {flow['days_at_capacity']} ngày/năm, phải bỏ lại {fmt_tons(flow['overflow_tons'])} tấn rơm "
                f"tại ruộng ({fmt_pct(overflow_pct)} sản lượng thu gom được) — sức chứa "
                f"{fmt_tons(capacity)} tấn chưa đủ."
            )
            capacity_warning = {"shortfall_pct": js_round(-overflow_pct, 2), "message": msg}
            warnings.append(f"{code}: {msg}")
        elif flow["utilization_pct"] < 60:
            idle = 100 - flow["utilization_pct"]
            msg = (
                f"Tồn kho cao điểm chỉ {fmt_tons(flow['peak_inventory_tons'])} tấn (ngày {flow['peak_inventory_day_label']}), "
                f"dùng {fmt_pct(flow['utilization_pct'])} sức chứa {fmt_tons(capacity)} tấn — "
                "sức chứa đang dư so với luồng rơm thực tế."
            )
            capacity_warning = {"shortfall_pct": js_round(idle, 2), "message": msg}
            warnings.append(f"{code}: {msg}")

    capex = compute_capex(sizing, p)
    opex = compute_opex(sizing, capex, hub["land_mode"], p, flow["received_from_field_tons"] if flow else 0)
    costs = build_cost_chain(collection, opex["warehouse_cost_annual"], opex["handling_cost_annual"], fp["cost"] + fh["cost"], hp["cost"], delivered)
    lead = js_round(fh["lead_time_hours"] + hp["lead_time_hours"], 2)

    return {
        "hub_ref": hub["hub_ref"],
        "scenario_hub_id": hub["scenario_hub_id"],
        "code": code,
        "service_radius_km": radius,
        "design_capacity_tons": capacity,
        "land_mode": hub["land_mode"],
        "cooperatives": served,
        "cooperative_count": len(served),
        "seasons": seasons,
        "total_available_tons": js_round(available),
        "collectable_tons": js_round(collectable),
        "delivered_tons": js_round(delivered),
        "capacity_warning": capacity_warning,
        "distance_to_plant_km": hub_to_plant.km,
        "distance_to_plant_source": hub_to_plant.source,
        "distance_to_plant_source_label": hub_to_plant.label,
        "legs": [fp, fh, hp],
        "flow": flow,
        "sizing": sizing,
        "capex": capex,
        "opex": opex,
        "costs": costs,
        "lead_time_hours": lead,
        "warnings": warnings,
    }


# =====================================================================
# FN-17 — No-Hub Baseline
# =====================================================================


def _empty_baseline(reason: str) -> dict[str, Any]:
    return {
        "available": False,
        "reason": reason,
        "cooperative_count": 0,
        "excluded_cooperatives": [],
        "excluded_supply_pct": 0,
        "collection_cost": 0,
        "transportation_cost": 0,
        "capex": 0,
        "warehouse_cost": 0,
        "handling_cost": 0,
        "total_delivered_logistics_cost": 0,
        "delivered_tons": 0,
        "baseline_cost_per_ton": None,
        "advisory_only": False,
        "legs": [],
    }


def compute_no_hub_baseline(served: list[dict[str, Any]], routes: Routes, p: SimulationParams) -> dict[str, Any]:
    # Ảnh chụp chỉ chứa HTX đã có toạ độ (nguồn cung loại HTX thiếu GPS trước khi định tuyến),
    # nên danh sách loại trừ của prototype ("Thiếu toạ độ GPS") luôn rỗng ở đây.
    usable = served
    if not usable:
        return _empty_baseline("Không có HTX nào đủ dữ liệu để tính No-Hub Baseline")
    unit_collection = p.collection_labour_price_per_ton + p.collection_consumable_price_per_ton
    collectable = jsum(c["collectable_tons"] for c in usable)
    collection = collectable * unit_collection
    transport = ton_km = lead_w = 0.0
    water_legs = 0
    for c in usable:
        ckey = _coop(c["cooperative_ref"])
        mode, _ = _decide_mode("auto", c["distance_to_plant_km"], ckey, PLANT, routes, p)
        if mode == "waterway":
            water_legs += 1
        dist = routes.distance(ckey, PLANT, "waterway").km if mode == "waterway" else c["distance_to_plant_km"]
        rate = p.waterway_freight_rate_per_ton_km if mode == "waterway" else p.road_freight_rate_per_ton_km
        speed = p.waterway_speed_kmh if mode == "waterway" else p.road_speed_kmh
        transport += c["collectable_tons"] * dist * rate
        ton_km += c["collectable_tons"] * dist
        lead_w += c["collectable_tons"] * (dist / max(speed, 1) + p.handling_hours_per_trip)
    delivered = collectable * (1 - p.loss_ratio)  # BR-02: V1 cùng hệ số #19
    total = collection + transport
    cpt = js_round(total / delivered) if delivered > 0 else None
    dominant = "waterway" if water_legs > len(usable) / 2 else "road"
    payload = p.barge_payload_tons if dominant == "waterway" else p.truck_payload_tons
    trips = ceil_int(collectable / max(payload, 1)) if collectable > 0 else 0
    lead = lead_w / collectable if collectable > 0 else 0.0
    return {
        "available": True,
        "cooperative_count": len(usable),
        "excluded_cooperatives": [],
        "excluded_supply_pct": 0,
        "collection_cost": js_round(collection),
        "transportation_cost": js_round(transport),
        "capex": 0,
        "warehouse_cost": 0,
        "handling_cost": 0,
        "total_delivered_logistics_cost": js_round(total),
        "delivered_tons": js_round(delivered),
        "baseline_cost_per_ton": cpt,
        "advisory_only": False,
        "legs": [
            {
                "leg": "field_plant",
                "label": "Ruộng → Nhà máy (không qua Hub)",
                "mode": dominant,
                "mode_decided_by": "mac_dinh",
                "waterway_suggested": water_legs > 0,
                "tons": js_round(collectable),
                "weighted_avg_distance_km": js_round(ton_km / collectable, 3) if collectable > 0 else 0,
                "ton_km": js_round(ton_km),
                "cost": js_round(transport),
                "trips": trips,
                "vehicles_required": _estimate_fleet(trips, lead),
                "lead_time_hours": js_round(lead, 2),
            }
        ],
    }


# =====================================================================
# FN-12 — TCO, ROI, Payback
# =====================================================================


def compute_financials(scn: dict[str, Any], p: SimulationParams, capex_total: float, opex_total: float, cost_per_ton: float | None, steady: float, baseline: dict[str, Any]) -> dict[str, Any]:
    years = int(scn.get("lifecycle_years") or 0) or p.lifecycle_years
    discounted = bool(scn.get("discounted"))
    rate = p.discount_rate

    base_cpt: float | None = None
    source = "thieu_baseline"
    mode = scn.get("baseline_mode", "no_hub")
    manual = _opt(scn.get("baseline_manual_cost_per_ton"))
    if mode == "manual" and manual is not None:
        base_cpt, source = manual, "manual"
    elif mode == "scenario" and scn.get("baseline_scenario_id"):
        other = _opt(scn.get("baseline_scenario_cost_per_ton"))
        if other is not None:
            base_cpt, source = other, "scenario"
    elif baseline["available"] and baseline["baseline_cost_per_ton"] is not None:
        base_cpt, source = float(baseline["baseline_cost_per_ton"]), "no_hub"

    yearly = []
    tco_opex = lifecycle_logistics = lifetime_tons = cumulative = 0.0
    payback: int | None = None
    steady_cpt = cost_per_ton if cost_per_ton is not None else 0.0
    for t in range(1, years + 1):
        delivered = steady * p.ramp_up_ratio if t == 1 else steady
        opex_nominal = opex_total * (1 + p.opex_escalation) ** (t - 1)
        df = 1 / (1 + rate) ** t if discounted else 1
        opex_present = opex_nominal * df
        tco_opex += opex_present
        lifecycle_logistics += steady_cpt * delivered * (1 + p.opex_escalation) ** (t - 1) * df
        lifetime_tons += delivered * df  # BR-05: mẫu số cũng chiết khấu
        savings_per_ton = base_cpt - cost_per_ton if base_cpt is not None and cost_per_ton is not None else 0.0
        savings = savings_per_ton * delivered * df
        cumulative += savings
        yearly.append(
            {
                "year": t,
                "delivered_tons": js_round(delivered),
                "opex": js_round(opex_present),
                "savings": js_round(savings),
                "cumulative_savings": js_round(cumulative),
            }
        )
        if payback is None and savings_per_ton > 0 and cumulative >= capex_total:
            payback = t

    tco = capex_total + tco_opex
    tco_per_ton = js_round(tco / lifetime_tons) if lifetime_tons > 0 else None
    lifecycle_cost = capex_total + lifecycle_logistics
    lifecycle_cpt = js_round(lifecycle_cost / lifetime_tons) if lifetime_tons > 0 else None

    roi: float | None = None
    status = "thieu_baseline"
    if base_cpt is not None and cost_per_ton is not None:
        spt = base_cpt - cost_per_ton
        roi = js_round(((spt * lifetime_tons - capex_total) / capex_total) * 100, 2) if capex_total > 0 else None
        if spt <= 0:
            status, payback = "khong_hoan_von", None  # BR-03
        elif payback is None:
            status = "khong_hoan_von_trong_ky"
        else:
            status = "hoan_von"

    return {
        "lifecycle_years": years,
        "discounted": discounted,
        "discount_rate_pct": js_round(rate * 100, 2),
        "opex_escalation_pct": js_round(p.opex_escalation * 100, 2),
        "ramp_up_pct": js_round(p.ramp_up_ratio * 100, 2),
        "capex": js_round(capex_total),
        "tco": js_round(tco),
        "lifetime_delivered_tons": js_round(lifetime_tons),
        "tco_per_ton": tco_per_ton,
        "lifecycle_cost": js_round(lifecycle_cost),
        "lifecycle_cost_per_ton": lifecycle_cpt,
        "roi_pct": roi,
        "payback_years": payback,
        "payback_status": status,
        "baseline_cost_per_ton": base_cpt,
        "baseline_source": source,
        "baseline_source_label": BASELINE_LABEL[source],
        "yearly_cashflow": yearly,
    }


# =====================================================================
# Kịch bản
# =====================================================================


def evaluate(snapshot: dict[str, Any], params: SimulationParams | None = None, skip_baseline: bool = False) -> dict[str, Any]:
    """Đánh giá một kịch bản từ ảnh chụp bất biến. `params` ghi đè dùng cho độ nhạy."""
    p = params or resolve(snapshot["params"])
    routes = Routes(snapshot.get("routes", []), p)
    scn = snapshot["scenario"]
    plant = scn.get("plant") or {}
    demand = _opt(plant.get("annual_demand_tons")) or p.plant_annual_demand_tons

    sort_order = {s["season_key"]: int(s.get("sort_order", 0)) for s in snapshot.get("seasons", [])}
    radius_idx = {(r["cooperative_ref"], r["hub_ref"]): _f(r["radius_km"]) for r in snapshot.get("radius", [])}
    hubs = sorted((dict(h) for h in snapshot.get("hubs", [])), key=lambda h: (int(h.get("sort_order", 0)), h.get("code", "")))
    for h in hubs:
        h["_radius"] = _f(h["service_radius_km"])
        h["_capacity"] = _f(h["design_capacity_tons"])
        h["_barge_payload"] = _f(h["barge_payload_tons"]) if h.get("barge_payload_tons") is not None else p.barge_small_payload_tons
    coops = snapshot.get("cooperatives", [])

    warnings: list[str] = []
    notes: list[str] = []

    # FN-05 BR-02: HTX thuộc bán kính nhiều Hub chỉ tính vào Hub GẦN NHẤT (theo FN-04).
    assignment: dict[str, tuple[str, float]] = {}
    for h in hubs:
        if h["_radius"] <= 0:
            warnings.append(f"{h['code']}: bán kính phục vụ = 0 — không có vùng nguyên liệu phù hợp.")
            continue
        for c in coops:
            rk = radius_idx.get((c["cooperative_ref"], h["hub_ref"]))
            if rk is None or rk > h["_radius"]:
                continue
            routed = routes.distance(_coop(c["cooperative_ref"]), _hub(h["hub_ref"]), "road").km
            cur = assignment.get(c["cooperative_ref"])
            if cur is None or routed < cur[1]:
                assignment[c["cooperative_ref"]] = (h["scenario_hub_id"], routed)

    served_by_hub: dict[str, list[dict[str, Any]]] = {}
    all_served: list[dict[str, Any]] = []
    for h in hubs:
        members = [c for c in coops if assignment.get(c["cooperative_ref"], ("", 0))[0] == h["scenario_hub_id"]]
        served = []
        for c in members:
            ckey = _coop(c["cooperative_ref"])
            to_hub = routes.distance(ckey, _hub(h["hub_ref"]), "road")
            to_plant = routes.distance(ckey, PLANT, "road")
            seasons = build_season_supply(c.get("seasons", []), sort_order, p)
            served.append(
                {
                    "cooperative_ref": c["cooperative_ref"],
                    "radius_distance_km": js_round(radius_idx[(c["cooperative_ref"], h["hub_ref"])], 3),
                    "distance_to_hub_km": to_hub.km,
                    "distance_to_hub_source": to_hub.source,
                    "distance_to_hub_source_label": to_hub.label,
                    "distance_to_plant_km": to_plant.km,
                    "distance_to_plant_source": to_plant.source,
                    "distance_to_plant_source_label": to_plant.label,
                    "member_count": int(c.get("member_count") or 0),
                    "planted_area_ha": jsum(s["planted_area_ha"] for s in seasons),
                    "seasons": seasons,
                    "total_available_tons": jsum(s["total_available_tons"] for s in seasons),
                    "collectable_tons": jsum(s["collectable_tons"] for s in seasons),
                    "delivered_tons": jsum(s["delivered_tons"] for s in seasons),
                }
            )
        all_served.extend(served)
        served_by_hub[h["scenario_hub_id"]] = served

    # Luồng rơm theo ngày: bài toán cấp mạng lưới (trần chở thẳng dùng chung) → chạy trước.
    calendar = cal.build(snapshot.get("seasons", []), snapshot.get("harvest_observations", {}) or {}, p.harvest_window_days)
    suppliers = [
        FlowSupplier(
            cooperative_ref=c["cooperative_ref"],
            hub_key=assignment.get(c["cooperative_ref"], (None, 0))[0],
            distance_to_hub_km=c["distance_to_hub_km"],
            distance_to_plant_km=c["distance_to_plant_km"],
            season_tons=[(s["season_key"], s["collectable_tons"]) for s in c["seasons"]],
        )
        for c in all_served
    ]
    flow = simulate_flow(
        suppliers,
        [
            FlowHubInput(
                key=h["scenario_hub_id"],
                code=h["code"],
                max_inventory_tons=h["_capacity"],  # công suất Hub = TỒN KHO TỐI ĐA
                distance_to_plant_km=routes.distance(_hub(h["hub_ref"]), PLANT, "road").km,
                barge_payload_tons=h["_barge_payload"],
            )
            for h in hubs
        ],
        demand,
        p,
        calendar,
    )
    flow_by_hub = {fh["key"]: fh for fh in flow["hubs"]}

    hub_results = []
    for h in hubs:
        hr = _hub_result(h, served_by_hub.get(h["scenario_hub_id"], []), routes, p, flow_by_hub.get(h["scenario_hub_id"]))
        hub_results.append(hr)
        warnings.extend(hr["warnings"])
    warnings.extend(flow["warnings"])
    if not hub_results:
        warnings.append("Kịch bản chưa có Hub nào — hãy gán ít nhất một Hub ứng viên.")

    # FN-11 AC-02: Cost/Ton kịch bản = tổng chi phí ÷ tổng Delivered, không bình quân các Hub.
    scenario_seasons = merge_seasons([s for hr in hub_results for s in hr["seasons"]])
    delivered = jsum(hr["delivered_tons"] for hr in hub_results)
    collectable = jsum(hr["collectable_tons"] for hr in hub_results)
    available = jsum(hr["total_available_tons"] for hr in hub_results)
    capex = _merge_sum([hr["capex"] for hr in hub_results], CAPEX_KEYS)
    opex = _merge_sum([hr["opex"] for hr in hub_results], OPEX_KEYS)
    cl = [hr["costs"] for hr in hub_results]
    costs = build_cost_chain(
        jsum(c["collection_cost"] for c in cl),
        jsum(c["warehouse_cost"] for c in cl),
        jsum(c["handling_cost"] for c in cl),
        jsum(c["transportation_field_hub"] for c in cl),
        jsum(c["transportation_hub_plant"] for c in cl),
        delivered,
    )

    baseline = _empty_baseline("Bỏ qua theo yêu cầu") if skip_baseline else compute_no_hub_baseline(all_served, routes, p)
    financial = compute_financials(scn, p, capex["total"], opex["total"], costs["cost_per_ton"], delivered, baseline)

    coverage = (delivered / demand) * 100 if demand > 0 else 0.0
    plant_warning = None
    if demand > 0 and delivered < demand:
        plant_warning = f"Kịch bản không đáp ứng nhu cầu nhà máy: thiếu {fmt_pct(100 - coverage)} ({fmt_tons(demand - delivered)} tấn/năm)."
        warnings.append(plant_warning)

    notes.extend(
        [
            "Cost per Ton chưa gồm khấu hao đầu tư — không dùng để xếp hạng kịch bản (FN-11 BR-03).",
            "Xếp hạng kịch bản dùng TCO per Ton (quyết định B2, FN-12 BR-04).",
            "Total Delivered Logistics Cost KHÔNG bao gồm giá mua rơm trả cho HTX/nông dân (quyết định B1).",
            "Khối lượng từng chặng KHÔNG còn suy từ hệ số mà lấy từ mô phỏng luồng rơm theo ngày: "
            f"{fmt_tons(flow['direct_field_plant_tons'])} tấn chở thẳng Ruộng→Nhà máy bằng ghe, "
            f"{fmt_tons(flow['field_hub_tons'])} tấn qua Hub, {fmt_tons(flow['hub_plant_tons'])} tấn xuất kho bằng sà lan.",
            f"Lịch thu hoạch: {flow['harvest_days']} ngày có rơm tại ruộng, {flow['no_harvest_days']} ngày không có — "
            "chính khoảng trống này là lý do tồn tại của Hub.",
            "KHOẢNG TRỐNG CÔNG THỨC (cần BA/Tài chính chốt): TCO theo FN-12 BR-01 = CAPEX + Σ OPEX, "
            "mà OPEX theo FN-10 BR-03 KHÔNG gồm Collection Cost và Transportation Cost. Vì vậy cột "
            "No-Hub Baseline (CAPEX = 0, OPEX = 0) sẽ có TCO per Ton = 0 và luôn đứng đầu bảng xếp hạng. "
            'Hệ thống bổ sung chỉ số "Chi phí vòng đời đầy đủ chuỗi / tấn" (CAPEX + Σ Total Delivered '
            "Logistics Cost) để so sánh đúng với No-Hub Baseline.",
            "V1 dùng mô hình chi phí tuyến tính theo tấn·km (FN-08 BR-06): không có phí cố định mỗi chuyến, "
            "phí tối thiểu hay chi phí chiều rỗng — lợi ích gom hàng của Hub do đó bị ước tính THẤP hơn thực tế.",
            "Công suất Hub được hiểu là TỒN KHO TỐI ĐA (tấn), không phải sản lượng thông qua hàng năm. "
            "Tồn kho cao điểm dùng để tính kho bãi và CAPEX nay lấy từ mô phỏng theo ngày, "
            "thay cho hệ số tồn kho cao điểm (#35) — đóng rủi ro RS-06 của BRD.",
            *flow["notes"],
        ]
    )

    unapproved = [dict(u) for u in p.unapproved]
    return {
        "engine_version": ENGINE_VERSION,
        "scenario_id": scn["scenario_id"],
        "scenario_code": scn.get("code", ""),
        "status": "tham_khao",
        "parameter_set_version": p.version,
        "plant": {"plant_ref": plant.get("plant_ref"), "annual_demand_tons": demand},
        "hubs": hub_results,
        "hub_count": len(hub_results),
        "seasons": scenario_seasons,
        "total_available_tons": js_round(available),
        "collectable_tons": js_round(collectable),
        "delivered_tons": js_round(delivered),
        "costs": costs,
        "capex": capex,
        "opex": opex,
        "financial": financial,
        "baseline": baseline,
        "flow": flow,
        "plant_demand_coverage_pct": js_round(coverage, 2),
        "plant_demand_warning": plant_warning,
        "unapproved_parameters": unapproved,
        # FN-01 BR-04: chỉ "Chính thức" được khi mọi tham số đã phê duyệt (Go kiểm lại từ ảnh chụp).
        "can_be_marked_official": bool(snapshot["params"].get("all_approved")) and not unapproved,
        "warnings": warnings,
        "notes": notes,
        "lineage": {
            "engine_version": ENGINE_VERSION,
            "parameter_set_version": p.version,
            "network_version": snapshot.get("network_version"),
            "input_counts": {"cooperatives": len(coops), "hubs": len(hubs), "routes": len(snapshot.get("routes", []))},
        },
    }


def normalize_numbers(obj: Any) -> Any:
    """Số nguyên dạng float → int để JSON gọn và ổn định (1.0 → 1)."""
    if isinstance(obj, dict):
        return {k: normalize_numbers(v) for k, v in obj.items()}
    if isinstance(obj, list):
        return [normalize_numbers(v) for v in obj]
    if isinstance(obj, float):
        return num(obj)
    return obj
