// pf/extension — GET /v1/extension/dashboard (US-DASH-01). Route chỉ trả MÃ đơn vị con;
// tên lấy từ pf/masterdata. Số thập phân giữ nguyên chuỗi (R-10) — chỉ định dạng khi hiển thị.
import type { ApiClient } from "../client";
import { unwrap } from "../client";
import type { AdminUnit, ExtDashboard, ExtKPI } from "../types";
import { listChildUnits, unitResolver } from "./units";

export interface DashboardChild {
  unit: string;
  name: string;
  kpi: ExtKPI;
}

export interface DashboardView {
  raw: ExtDashboard;
  unitName: string | null;
  children: DashboardChild[];
}

export async function loadDashboard(api: ApiClient, query: { unit?: string; period?: string } = {}): Promise<DashboardView> {
  const raw = await unwrap(api.GET("/v1/extension/dashboard", { params: { query } }));
  let units: AdminUnit[] = [];
  if (raw.children.length > 0) {
    try {
      units = await listChildUnits(api, raw.level === "national" ? null : raw.unit);
    } catch {
      units = []; // thiếu tên thì hiện mã — không chặn cả dashboard
    }
  }
  const names = new Map(units.map((u) => [u.code, u.name]));
  let unitName: string | null = null;
  if (raw.unit) {
    try {
      unitName = (await unitResolver(api).get(raw.unit))?.name ?? null;
    } catch {
      unitName = null;
    }
  }
  return {
    raw,
    unitName,
    children: raw.children.map((c) => ({ unit: c.unit, name: names.get(c.unit) ?? c.unit, kpi: c.kpi })),
  };
}
