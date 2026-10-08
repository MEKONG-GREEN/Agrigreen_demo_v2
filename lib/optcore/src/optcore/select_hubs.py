"""THỬ NGHIỆM — chọn tập Hub bằng MILP bài toán vị trí cơ sở (facility location).

Không có trong prototype (prototype chỉ ĐÁNH GIÁ kịch bản người dùng dựng). Mô
hình rút gọn, dùng để GỢI Ý tập Hub rồi chạy lại `evaluate` đầy đủ cho tập đó:

    min  Σ_j F_j·y_j + Σ_ij c_ij·S_i·x_ij + Σ_i c_i0·S_i·x_i0
    s.t. Σ_j x_ij + x_i0 = 1                  ∀i   (mỗi HTX: qua một Hub hoặc chở thẳng)
         x_ij ≤ y_j                           ∀i,j (chỉ qua Hub mở, trong bán kính)
         Σ_i S_i·x_ij ≤ T_j·y_j               ∀j   (thông lượng ≈ sức chứa ÷ hệ số tồn kho #35)
         1 ≤ Σ_j y_j ≤ max_hubs
         y ∈ {0,1}, x ∈ [0,1]

F_j: chi phí cố định năm = CAPEX(sức chứa)/vòng đời + nhân sự + điện + bảo trì;
c_ij: VNĐ/tấn thu gom→Hub→nhà máy (+ băm/nén); c_i0: VNĐ/tấn chở thẳng (FN-17).
Bỏ qua trần chở thẳng theo ngày của mô hình luồng rơm — vì vậy chỉ là gợi ý.
Giải bằng HiGHS (`highspy`, MIT) — phụ thuộc tùy chọn `[milp]`.
"""

from __future__ import annotations

import copy
from typing import Any

from .engine import PLANT, Routes, _coop, _decide_mode, _hub, build_season_supply, compute_capex, compute_sizing, evaluate
from .params import resolve


class SolverUnavailable(RuntimeError):
    pass


def _leg_cost_per_ton(frm: str, to: str, road_km: float, override: str, routes: Routes, p: Any) -> float:
    mode, _ = _decide_mode(override, road_km, frm, to, routes, p)
    if mode == "waterway":
        return routes.distance(frm, to, "waterway").km * p.waterway_freight_rate_per_ton_km
    return road_km * p.road_freight_rate_per_ton_km


def select_hubs(snapshot: dict[str, Any], max_hubs: int | None = None) -> dict[str, Any]:
    try:
        import highspy
    except ImportError as exc:  # pragma: no cover - phụ thuộc tùy chọn
        raise SolverUnavailable("chế độ select_hubs cần gói tùy chọn highspy (pip install .[milp])") from exc

    p = resolve(snapshot["params"])
    routes = Routes(snapshot.get("routes", []), p)
    hubs = snapshot.get("hubs", [])
    coops = snapshot.get("cooperatives", [])
    order = {s["season_key"]: int(s.get("sort_order", 0)) for s in snapshot.get("seasons", [])}
    radius = {(r["cooperative_ref"], r["hub_ref"]): float(r["radius_km"]) for r in snapshot.get("radius", [])}
    k = max_hubs or len(hubs)
    years = int(snapshot["scenario"].get("lifecycle_years") or p.lifecycle_years)

    supply = {}
    for c in coops:
        seasons = build_season_supply(c.get("seasons", []), order, p)
        supply[c["cooperative_ref"]] = sum(s["collectable_tons"] for s in seasons)
    in_radius = {ref for (ref, hub_ref), rk in radius.items() if any(h["hub_ref"] == hub_ref and rk <= float(h["service_radius_km"]) for h in hubs)}
    supply = {ref: s for ref, s in supply.items() if s > 0 and ref in in_radius}
    unit_collection = p.collection_labour_price_per_ton + p.collection_consumable_price_per_ton

    h = highspy.Highs()
    h.setOptionValue("output_flag", False)
    y: dict[str, Any] = {}
    fixed: dict[str, float] = {}
    for hub in hubs:
        cap = float(hub["design_capacity_tons"])
        sizing = compute_sizing(cap / max(p.peak_inventory_factor, 1e-9), p, cap)
        capex = compute_capex(sizing, p)
        fixed[hub["hub_ref"]] = (
            capex["total"] / max(years, 1)
            + p.staff_count * p.staff_salary_per_month * 12
            + p.utility_cost_per_year
            + capex["equipment"] * p.maintenance_rate
        )
        y[hub["hub_ref"]] = h.addBinary(obj=fixed[hub["hub_ref"]])

    x: dict[tuple[str, str], Any] = {}
    x0: dict[str, Any] = {}
    for ref, s in supply.items():
        ckey = _coop(ref)
        plant_road = routes.distance(ckey, PLANT, "road").km
        direct = unit_collection + _leg_cost_per_ton(ckey, PLANT, plant_road, "auto", routes, p)
        x0[ref] = h.addVariable(lb=0, ub=1, obj=direct * s)
        for hub in hubs:
            rk = radius.get((ref, hub["hub_ref"]))
            if rk is None or rk > float(hub["service_radius_km"]):
                continue
            hkey = _hub(hub["hub_ref"])
            to_hub = routes.distance(ckey, hkey, "road").km
            hub_plant = routes.distance(hkey, PLANT, "road").km
            c = (
                unit_collection
                + _leg_cost_per_ton(ckey, hkey, to_hub, hub.get("mode_field_hub", "auto"), routes, p)
                + _leg_cost_per_ton(hkey, PLANT, hub_plant, hub.get("mode_hub_plant", "auto"), routes, p)
                + p.hub_processing_cost_per_ton
            )
            x[(ref, hub["hub_ref"])] = h.addVariable(lb=0, ub=1, obj=c * s)

    for ref in supply:
        terms = [x0[ref]] + [v for (r, _), v in x.items() if r == ref]
        h.addConstr(sum(terms[1:], terms[0]) == 1)
    for (ref, hub_ref), v in x.items():
        h.addConstr(v - y[hub_ref] <= 0)
    for hub in hubs:
        cap_t = float(hub["design_capacity_tons"]) / max(p.peak_inventory_factor, 1e-9)
        terms = [supply[r] * v for (r, hr), v in x.items() if hr == hub["hub_ref"]]
        if terms:
            h.addConstr(sum(terms[1:], terms[0]) - cap_t * y[hub["hub_ref"]] <= 0)
    if y:
        ys = list(y.values())
        h.addConstr(sum(ys[1:], ys[0]) <= k)
        h.addConstr(sum(ys[1:], ys[0]) >= 1)  # chọn ít nhất một Hub — "không Hub" đã có cột No-Hub Baseline
    h.setMinimize()
    h.run()
    status = h.modelStatusToString(h.getModelStatus())
    sol = h.getSolution().col_value
    chosen = [hub for hub in hubs if sol[y[hub["hub_ref"]].index] > 0.5] if y else []
    objective = h.getInfo().objective_function_value

    reduced = copy.deepcopy(snapshot)
    reduced["hubs"] = copy.deepcopy(chosen)
    result = evaluate(reduced)
    return {
        "experimental": True,
        "solver": "highs",
        "solver_status": status,
        "max_hubs": k,
        "selected_hub_refs": [hub["hub_ref"] for hub in chosen],
        "objective_annual_vnd": objective,
        "note": "Gợi ý từ MILP rút gọn (bỏ qua trần chở thẳng theo ngày); kết quả đầy đủ tính lại bằng evaluate.",
        "result": result,
    }
