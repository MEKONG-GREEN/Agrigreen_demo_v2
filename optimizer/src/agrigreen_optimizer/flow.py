"""Mô phỏng luồng rơm thực tế theo ngày — chuyển từ `simulateFlow` (flow.ts).

(1) Trong mùa thu hoạch: rơm ưu tiên chở THẲNG về nhà máy bằng ghe, trần bằng
    lượng tiêu thụ trong ngày; phần vượt về Hub, bị băm/nén (hao hụt riêng).
(2) Ngoài mùa: Hub → nhà máy bằng sà lan CHỞ ĐẦY TẢI; non tải chỉ khi "dọn kho
    cuối kỳ" (nhà máy sắp đứt nguyên liệu).
(3) Công suất Hub = TỒN KHO TỐI ĐA; rơm vượt sức chứa là rơm không thu gom được.

Chạy 2 năm liên tiếp theo bước ngày, chỉ ghi kết quả năm thứ hai (khởi động).
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

from .calendar import DAYS_PER_YEAR, SeasonCalendar, day_label, month_of_day
from .jsmath import ceil_int, fmt_vi, js_round, jsum, num
from .params import SimulationParams

PLANT_REORDER_DAYS = 5
MAX_DISPATCH_PER_DAY = 12


@dataclass
class FlowSupplier:
    cooperative_ref: str
    hub_key: str | None
    distance_to_hub_km: float
    distance_to_plant_km: float
    season_tons: list[tuple[str, float]]  # (season_key, collectable_tons)


@dataclass
class FlowHubInput:
    key: str
    code: str
    max_inventory_tons: float
    distance_to_plant_km: float
    barge_payload_tons: float


@dataclass
class _HubState:
    key: str
    code: str
    max_inventory_tons: float
    distance_to_plant_km: float
    barge_payload_tons: float
    inventory: float = 0.0
    received: float = 0.0
    stored: float = 0.0
    shipped: float = 0.0
    overflow: float = 0.0
    peak: float = 0.0
    peak_day: int = 1
    inventory_sum: float = 0.0
    days_at_capacity: int = 0
    full_trips: int = 0
    partial_trips: int = 0
    field_hub_ton_km: float = 0.0
    hub_plant_ton_km: float = 0.0
    curve: list[float] = field(default_factory=lambda: [0.0] * (DAYS_PER_YEAR + 1))


def _clamp01(v: float) -> float:
    return min(1.0, max(0.0, v))


def _fmt(v: float) -> str:
    return fmt_vi(v, 0)


def operating_calendar(operating_days: int) -> tuple[list[bool], int]:
    target = min(DAYS_PER_YEAR, max(1, int(js_round(operating_days))))
    off = DAYS_PER_YEAR - target
    flags = [True] * (DAYS_PER_YEAR + 1)
    count = DAYS_PER_YEAR
    if off > 0:
        for d in range(1, DAYS_PER_YEAR + 1):
            if ((d - 1) * off) // DAYS_PER_YEAR != (d * off) // DAYS_PER_YEAR:
                flags[d] = False
                count -= 1
    return flags, count


def _lookahead_direct(network: list[float], flags: list[bool], doy: int, daily: float, keep: float) -> float:
    total = 0.0
    for offset in range(1, PLANT_REORDER_DAYS + 1):
        day = ((doy - 1 + offset) % DAYS_PER_YEAR) + 1
        if not flags[day]:
            continue
        total += min(network[day] * keep, daily)
    return total


def _pick_hub(hubs: list[_HubState]) -> _HubState | None:
    best: _HubState | None = None
    for h in hubs:
        if h.inventory <= 1e-6:
            continue
        if best is None or h.inventory > best.inventory:
            best = h
    return best


def _windows(supply: list[float]) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    start, tons, days = 0, 0.0, 0

    def flush(end: int) -> None:
        nonlocal start, tons, days
        if not start:
            return
        out.append({"from_label": day_label(start), "to_label": day_label(end), "days": days, "tons": num(js_round(tons))})
        start, tons, days = 0, 0.0, 0

    for d in range(1, DAYS_PER_YEAR + 1):
        if supply[d] > 1e-9:
            if not start:
                start = d
            tons += supply[d]
            days += 1
        elif start:
            flush(d - 1)
    flush(DAYS_PER_YEAR)
    return out


def simulate_flow(
    suppliers_in: list[FlowSupplier],
    hubs_in: list[FlowHubInput],
    plant_annual_demand_tons: float,
    params: SimulationParams,
    calendar: dict[str, SeasonCalendar],
) -> dict[str, Any]:
    loss_keep = 1 - _clamp01(params.loss_ratio)
    process_keep = 1 - _clamp01(params.hub_processing_loss_ratio)

    supply_by_day: dict[str, list[float]] = {}
    network = [0.0] * (DAYS_PER_YEAR + 1)
    for s in suppliers_in:
        curve = [0.0] * (DAYS_PER_YEAR + 1)
        for season_key, tons in s.season_tons:
            season = calendar.get(season_key)
            if season is None or tons <= 0:
                continue
            for doy, w in season.days:
                curve[doy] += tons * w
        supply_by_day[s.cooperative_ref] = curve
        for d in range(1, DAYS_PER_YEAR + 1):
            network[d] += curve[d]

    suppliers = sorted(suppliers_in, key=lambda s: s.distance_to_plant_km)  # ổn định như Array.sort của V8
    hubs = [
        _HubState(
            key=h.key,
            code=h.code,
            max_inventory_tons=h.max_inventory_tons,
            distance_to_plant_km=h.distance_to_plant_km,
            barge_payload_tons=h.barge_payload_tons,
        )
        for h in hubs_in
    ]
    hub_by_key = {h.key: h for h in hubs}

    flags, op_count = operating_calendar(params.plant_operating_days)
    daily = plant_annual_demand_tons / op_count if op_count > 0 else 0.0
    reorder_point = daily * PLANT_REORDER_DAYS

    months = [
        {
            "month": i + 1,
            "label": f"Tháng {i + 1}",
            "harvest_tons": 0.0,
            "direct_to_plant_tons": 0.0,
            "to_hub_tons": 0.0,
            "hub_to_plant_tons": 0.0,
            "plant_consumed_tons": 0.0,
            "plant_unmet_tons": 0.0,
            "hub_inventory_end_tons": 0.0,
            "uncollected_tons": 0.0,
        }
        for i in range(12)
    ]

    plant_stock = 0.0
    plant_peak = 0.0
    direct_tons = field_hub_tons = hub_plant_tons = uncollected = 0.0
    direct_ton_km = plant_received = plant_consumed = plant_unmet = 0.0
    plant_unmet_days = 0
    harvest_days = 0

    for step in range(1, DAYS_PER_YEAR * 2 + 1):
        doy = ((step - 1) % DAYS_PER_YEAR) + 1
        record = step > DAYS_PER_YEAR
        row = months[month_of_day(doy) - 1]
        demand_today = daily if flags[doy] else 0.0

        if record and network[doy] > 1e-9:
            harvest_days += 1

        budget = demand_today
        for s in suppliers:
            available = supply_by_day[s.cooperative_ref][doy]
            if available <= 1e-9:
                continue
            if record:
                row["harvest_tons"] += available
            remaining = available
            if budget > 1e-9 and loss_keep > 0:
                arrive = min(remaining * loss_keep, budget)
                shipped = arrive / loss_keep
                remaining -= shipped
                budget -= arrive
                plant_stock += arrive
                if record:
                    direct_tons += shipped
                    direct_ton_km += shipped * s.distance_to_plant_km
                    plant_received += arrive
                    row["direct_to_plant_tons"] += shipped
            if remaining <= 1e-9:
                continue
            hub = hub_by_key.get(s.hub_key) if s.hub_key else None
            if hub is None or hub.max_inventory_tons <= 0 or process_keep <= 0:
                if record:
                    uncollected += remaining
                    row["uncollected_tons"] += remaining
                continue
            space = max(0.0, hub.max_inventory_tons - hub.inventory)
            stored = min(remaining * process_keep, space)
            accepted = stored / process_keep
            rejected = remaining - accepted
            hub.inventory += stored
            if record:
                hub.received += accepted
                hub.stored += stored
                hub.field_hub_ton_km += accepted * s.distance_to_hub_km
                field_hub_tons += accepted
                row["to_hub_tons"] += accepted
                if rejected > 1e-9:
                    hub.overflow += rejected
                    uncollected += rejected
                    row["uncollected_tons"] += rejected

        consumed = min(plant_stock, demand_today)
        plant_stock -= consumed
        if record:
            plant_consumed += consumed
            row["plant_consumed_tons"] += consumed
            shortfall = demand_today - consumed
            if shortfall > 1e-6:
                plant_unmet += shortfall
                plant_unmet_days += 1
                row["plant_unmet_tons"] += shortfall

        expected_direct = _lookahead_direct(network, flags, doy, daily, loss_keep)
        target_stock = max(daily, reorder_point - expected_direct)
        dispatched = 0
        while plant_stock < target_stock and dispatched < MAX_DISPATCH_PER_DAY:
            hub = _pick_hub(hubs)
            if hub is None:
                break
            payload = max(1.0, hub.barge_payload_tons)
            if hub.inventory >= payload:
                load = payload
                if record:
                    hub.full_trips += 1
            else:
                starving = plant_stock + expected_direct < daily
                if not starving:
                    break
                load = hub.inventory
                if record:
                    hub.partial_trips += 1
            hub.inventory -= load
            arrive = load * loss_keep
            plant_stock += arrive
            dispatched += 1
            if record:
                hub.shipped += load
                hub.hub_plant_ton_km += load * hub.distance_to_plant_km
                hub_plant_tons += load
                plant_received += arrive
                row["hub_to_plant_tons"] += load

        if record:
            for h in hubs:
                h.curve[doy] = h.inventory
                h.inventory_sum += h.inventory
                if h.inventory > h.peak:
                    h.peak = h.inventory
                    h.peak_day = doy
                if h.max_inventory_tons > 0 and h.inventory >= h.max_inventory_tons - 1e-6:
                    h.days_at_capacity += 1
            if plant_stock > plant_peak:
                plant_peak = plant_stock
            row["hub_inventory_end_tons"] = jsum(h.inventory for h in hubs)

    hub_results: list[dict[str, Any]] = []
    for h in hubs:
        warnings: list[str] = []
        utilization = (h.peak / h.max_inventory_tons) * 100 if h.max_inventory_tons > 0 else 0.0
        if h.days_at_capacity > 0:
            warnings.append(
                f"{h.code}: kho đầy {h.days_at_capacity} ngày trong năm, phải bỏ lại "
                f"{_fmt(h.overflow)} tấn rơm tại ruộng — cần tăng sức chứa hoặc xuất kho sớm hơn."
            )
        elif h.max_inventory_tons > 0 and utilization < 45:
            warnings.append(
                f"{h.code}: tồn kho cao điểm chỉ đạt {js_round(utilization, 1):.1f}% sức chứa "
                f"({_fmt(h.peak)}/{_fmt(h.max_inventory_tons)} tấn) — kho đang được đầu tư dư."
            )
        if h.partial_trips > 0:
            warnings.append(
                f"{h.code}: có {h.partial_trips} chuyến sà lan non tải (dọn kho cuối kỳ) — "
                "chi phí mỗi tấn của các chuyến này cao hơn định mức."
            )
        hub_results.append(
            {
                "key": h.key,
                "code": h.code,
                "max_inventory_tons": num(js_round(h.max_inventory_tons)),
                "barge_payload_tons": num(h.barge_payload_tons),
                "received_from_field_tons": num(js_round(h.received)),
                "stored_after_processing_tons": num(js_round(h.stored)),
                "processing_loss_tons": num(js_round(h.received - h.stored)),
                "shipped_to_plant_tons": num(js_round(h.shipped)),
                "peak_inventory_tons": num(js_round(h.peak)),
                "peak_inventory_day_label": day_label(h.peak_day),
                "average_inventory_tons": num(js_round(h.inventory_sum / DAYS_PER_YEAR)),
                "ending_inventory_tons": num(js_round(h.inventory)),
                "overflow_tons": num(js_round(h.overflow)),
                "days_at_capacity": h.days_at_capacity,
                "utilization_pct": num(js_round(utilization, 2)),
                "full_barge_trips": h.full_trips,
                "partial_barge_trips": h.partial_trips,
                "field_hub_ton_km": num(js_round(h.field_hub_ton_km)),
                "hub_plant_ton_km": num(js_round(h.hub_plant_ton_km)),
                "inventory_curve": [num(js_round(v)) for v in h.curve[1:]],
                "warnings": warnings,
            }
        )

    boat_payload = max(1.0, params.boat_straw_payload_tons)
    total_collectable = jsum(jsum(t for _, t in s.season_tons) for s in suppliers_in)
    warnings = [w for hr in hub_results for w in hr["warnings"]]
    if plant_unmet > 1e-3:
        warnings.append(
            f"Nhà máy thiếu nguyên liệu {plant_unmet_days} ngày trong năm "
            f"({_fmt(plant_unmet)} tấn) — mạng lưới chưa đủ nuôi nhà máy liên tục."
        )
    if uncollected > 1e-3:
        pct = (uncollected / max(total_collectable, 1)) * 100
        warnings.append(
            f"{_fmt(uncollected)} tấn rơm phải bỏ lại tại ruộng ({js_round(pct, 1):.1f}% sản lượng thu gom được) "
            "vì Hub đã đầy hoặc vùng nguyên liệu nằm ngoài mọi bán kính phục vụ."
        )

    return {
        "harvest_days": harvest_days,
        "no_harvest_days": DAYS_PER_YEAR - harvest_days,
        "harvest_windows": _windows(network),
        "calendar_sources": [
            {"season_key": c.season_key, "source": c.source, "source_label": c.source_label} for c in calendar.values()
        ],
        "plant_daily_demand_tons": num(js_round(daily, 2)),
        "plant_operating_days": op_count,
        "plant_received_tons": num(js_round(plant_received)),
        "plant_consumed_tons": num(js_round(plant_consumed)),
        "plant_unmet_tons": num(js_round(plant_unmet)),
        "plant_unmet_days": plant_unmet_days,
        "plant_peak_stock_tons": num(js_round(plant_peak)),
        "coverage_pct": num(js_round((plant_consumed / plant_annual_demand_tons) * 100, 2)) if plant_annual_demand_tons > 0 else 0,
        "direct_field_plant_tons": num(js_round(direct_tons)),
        "field_hub_tons": num(js_round(field_hub_tons)),
        "hub_plant_tons": num(js_round(hub_plant_tons)),
        "uncollected_tons": num(js_round(uncollected)),
        "uncollected_pct": num(js_round((uncollected / total_collectable) * 100, 2)) if total_collectable > 0 else 0,
        "direct_field_plant_ton_km": num(js_round(direct_ton_km)),
        "field_hub_ton_km": num(js_round(jsum(hr["field_hub_ton_km"] for hr in hub_results))),
        "hub_plant_ton_km": num(js_round(jsum(hr["hub_plant_ton_km"] for hr in hub_results))),
        "boat_trips_direct": ceil_int(direct_tons / boat_payload),
        "boat_trips_to_hub": ceil_int(field_hub_tons / boat_payload),
        "barge_trips_full": sum(hr["full_barge_trips"] for hr in hub_results),
        "barge_trips_partial": sum(hr["partial_barge_trips"] for hr in hub_results),
        "network_peak_inventory_tons": num(js_round(jsum(hr["peak_inventory_tons"] for hr in hub_results))),
        "hubs": hub_results,
        "months": [
            {
                "month": r["month"],
                "label": r["label"],
                **{
                    k: num(js_round(r[k]))
                    for k in (
                        "harvest_tons",
                        "direct_to_plant_tons",
                        "to_hub_tons",
                        "hub_to_plant_tons",
                        "plant_consumed_tons",
                        "plant_unmet_tons",
                        "hub_inventory_end_tons",
                        "uncollected_tons",
                    )
                },
            }
            for r in months
        ],
        "warnings": warnings,
        "notes": [
            f"Ghe {_fmt(params.boat_registered_tons)} tấn chở {_fmt(params.boat_straw_payload_tons)} tấn rơm rời — "
            "dùng cho cả chặng Ruộng đến Nhà máy và Ruộng đến Hub trong mùa thu hoạch.",
            "Rơm chở thẳng Ruộng đến Nhà máy bị giới hạn đúng bằng lượng tiêu thụ trong ngày của nhà máy; "
            "phần vượt được đưa về Hub để lưu trữ.",
            "Sà lan Hub đến Nhà máy chỉ rời bến khi đủ một chuyến đầy tải; chuyến non tải chỉ xảy ra khi "
            "dọn kho cuối kỳ và được đếm riêng.",
            "Số chuyến ghe tính trên tổng sản lượng cả năm (giả định ghe gom nhiều HTX trong một chuyến), "
            "không ép tròn theo từng HTX từng ngày.",
            "Mô hình chạy 2 năm liên tiếp và chỉ lấy năm thứ hai để tồn kho đầu kỳ phản ánh vận hành ổn định.",
            f"Tồn kho cao điểm tại bãi tập kết của nhà máy trong năm là {_fmt(plant_peak)} tấn — "
            "dùng để kiểm tra sức chứa đầu nhận của nhà máy.",
        ],
    }
