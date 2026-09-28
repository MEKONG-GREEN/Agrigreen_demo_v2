// GIẢ LẬP pf/mechanization (Bản đồ CGH) — hình dạng theo contracts/openapi/application.yaml
// (/v1/mechanization/balance-runs, /coverage, /dashboard). Kết quả cân đối là "đã lưu theo vụ":
// sinh một lần, ổn định. Số thập phân tính bằng số nguyên đã nhân tỉ lệ rồi đổi ra chuỗi (R-10).
import type { AppGet, MechCoverageItem, MechCoverageStatus, MechGroup, MechRun } from "../types";
import { COMMUNES, COOPERATIVES, mulberry32, PROVINCES, type MockCooperative } from "./data";

const STAGES = [
  { stage: "lam_dat", machine: "MLD", norm: 120 },
  { stage: "gieo_sa", machine: "MGS", norm: 100 },
  { stage: "cham_soc", machine: "MPT", norm: 260 },
  { stage: "thu_hoach", machine: "MGD", norm: 90 },
] as const;

const STATUS_LABEL: Record<MechCoverageStatus, string> = {
  sufficient: "Đủ",
  attention: "Cần chú ý",
  shortage: "Thiếu",
  surplus: "Thừa",
  no_data: "Chưa có dữ liệu",
  no_norm: "Chưa cấu hình định mức",
  unclassified: "Chưa cấu hình ngưỡng cảnh báo",
};
const STATUS_COLOR: Record<MechCoverageStatus, string> = {
  sufficient: "#1F7A38",
  attention: "#9C6414",
  shortage: "#A3372A",
  surplus: "#2A78D6",
  no_data: "#9AA5A0",
  no_norm: "#9AA5A0",
  unclassified: "#9AA5A0",
};
/** Thứ tự "thấp nhất" để lấy mức của đơn vị = mức của khâu thấp nhất. */
const RANK: Record<MechCoverageStatus, number> = { shortage: 0, attention: 1, sufficient: 2, surplus: 3, unclassified: 4, no_norm: 5, no_data: 6 };

const fmt4 = (x: number) => `${Math.floor(x / 10000)}.${String(x % 10000).padStart(4, "0")}`;
const fmt2 = (x: number) => `${Math.floor(x / 100)}.${String(x % 100).padStart(2, "0")}`;

/** Số chuỗi thập phân → số nguyên ×10^4 (không float). */
function scaled4(s: string): number {
  const [i = "0", f = ""] = s.split(".");
  return Number(i) * 10000 + Number((f + "0000").slice(0, 4));
}

function statusOf(pctX100: number | null): MechCoverageStatus {
  if (pctX100 === null) return "no_data";
  if (pctX100 >= 12000) return "surplus";
  if (pctX100 >= 8500) return "sufficient";
  if (pctX100 >= 6000) return "attention";
  return "shortage";
}

interface Cell {
  requiredX4: number;
  requiredWhole: number;
  capacity: number;
  machines: number;
  pctX100: number | null;
}

/** Gộp các ô CÙNG khâu (nhiều HTX): nhu cầu/năng lực là tổng, mức đáp ứng tính lại từ tổng. */
function sumCells(cells: Cell[]): Cell {
  const withData = cells.filter((c) => c.pctX100 !== null);
  const requiredX4 = withData.reduce((s, c) => s + c.requiredX4, 0);
  const capacity = withData.reduce((s, c) => s + c.capacity, 0);
  return {
    requiredX4,
    requiredWhole: withData.reduce((s, c) => s + c.requiredWhole, 0),
    capacity,
    machines: withData.reduce((s, c) => s + c.machines, 0),
    pctX100: requiredX4 === 0 ? null : Math.round((capacity * 100_000_000) / requiredX4),
  };
}

/** Nhóm theo khâu: tổng nhu cầu/năng lực; mức = mức của khâu thấp nhất. */
function group(cells: Cell[]): MechGroup {
  const withData = cells.filter((c) => c.pctX100 !== null);
  if (withData.length === 0) {
    return { required: "0.0000", required_whole: 0, capacity: 0, machines_total: 0, coverage_pct: null, status: "no_data", status_label: STATUS_LABEL.no_data, color: STATUS_COLOR.no_data };
  }
  const requiredX4 = withData.reduce((s, c) => s + c.requiredX4, 0);
  const capacity = withData.reduce((s, c) => s + c.capacity, 0);
  const lowest = withData.reduce((m, c) => ((c.pctX100 ?? 0) < (m.pctX100 ?? 0) ? c : m));
  const status = statusOf(lowest.pctX100);
  return {
    required: fmt4(requiredX4),
    required_whole: withData.reduce((s, c) => s + c.requiredWhole, 0),
    capacity,
    machines_total: withData.reduce((s, c) => s + c.machines, 0),
    coverage_pct: lowest.pctX100 === null ? null : fmt2(lowest.pctX100),
    status,
    status_label: STATUS_LABEL[status],
    color: STATUS_COLOR[status],
  };
}

interface CoopResult {
  coop: MockCooperative;
  cells: Record<string, Cell>;
}

interface StoredRun {
  run: MechRun;
  results: CoopResult[];
}

function compute(seasonCode: string, seasonYear: number, seq: number, seed: number): StoredRun {
  const r = mulberry32(seed);
  const results: CoopResult[] = COOPERATIVES.filter((c) => c.api.active).map((coop) => {
    const noData = coop.province_code === "80";
    const areaX4 = scaled4(coop.api.area_ha ?? "0");
    const cells: Record<string, Cell> = {};
    const coopFactor = 0.5 + r() * 0.95;
    for (const s of STAGES) {
      if (noData) {
        cells[s.stage] = { requiredX4: 0, requiredWhole: 0, capacity: 0, machines: 0, pctX100: null };
        continue;
      }
      const requiredX4 = Math.round(areaX4 / s.norm);
      const requiredWhole = Math.ceil(requiredX4 / 10000);
      // Năng lực quanh nhu cầu theo hệ số của HTX (± chút theo khâu) để có đủ Thiếu / Cần chú ý / Đủ / Thừa.
      const capacity = Math.max(1, Math.round((requiredX4 / 10000) * (coopFactor + (r() - 0.5) * 0.12)));
      const machines = capacity + Math.floor(r() * 2);
      const pctX100 = requiredX4 === 0 ? null : Math.round((capacity * 100_000_000) / requiredX4); // % × 100
      cells[s.stage] = { requiredX4, requiredWhole, capacity, machines, pctX100 };
    }
    return { coop, cells };
  });
  const counts: Record<string, number> = {};
  for (const res of results) {
    const g = group(Object.values(res.cells));
    counts[g.status] = (counts[g.status] ?? 0) + 1;
  }
  const param = (key: string, value: string) => ({ key, value, version: 1, source_doc: "BRD Bản đồ CGH v1.3 — QT-02 (giả lập)" });
  const startMonth = seasonCode === "DX" ? "11" : seasonCode === "HT" ? "04" : "08";
  return {
    results,
    run: {
      id: `01927b3e-4a11-7000-8000-${String(seasonYear * 10 + seq).padStart(12, "0")}`,
      season_code: seasonCode,
      season_year: seasonYear,
      season_key: `${seasonCode}-${seasonYear}`,
      reference_date: `${seasonYear}-${startMonth}-01`,
      rule_version: "QT-01/QT-02 v1",
      pilot_stages: STAGES.map((s) => s.stage),
      thresholds: {
        sufficient: param("cgh.coverage.sufficient_min_pct", "85"),
        attention: param("cgh.coverage.attention_min_pct", "60"),
        surplus: param("cgh.coverage.surplus_min_pct", "120"),
      },
      result_count: results.length * STAGES.length,
      status_counts: counts,
      missing_norms: [],
      computed_at: new Date(Date.now() - (seq === 2 ? 1 : 150) * 86_400_000).toISOString(),
      seq,
    },
  };
}

/** Lần cân đối đã lưu — mới nhất trước. */
export const RUNS: StoredRun[] = [compute("HT", 2026, 2, 2026042), compute("DX", 2025, 1, 2025111)];

export function findRun(q: { run_id?: string | null; season_code?: string | null; season_year?: number | null }): StoredRun | undefined {
  if (q.run_id) return RUNS.find((x) => x.run.id === q.run_id);
  return RUNS.find((x) => (!q.season_code || x.run.season_code === q.season_code) && (!q.season_year || x.run.season_year === q.season_year));
}

function item(
  base: Pick<MechCoverageItem, "unit_code" | "cooperative_ref" | "name" | "province_code" | "commune_code">,
  results: CoopResult[],
  stage: string | null,
): MechCoverageItem {
  const stageKeys = stage ? [stage] : STAGES.map((s) => s.stage);
  const stages: Record<string, MechGroup> = {};
  const perStage = stageKeys.map((k) => sumCells(results.map((r) => r.cells[k]).filter((c): c is Cell => !!c)));
  stageKeys.forEach((k, i) => (stages[k] = group([perStage[i]!])));
  const overall = group(perStage);
  const one = results.length === 1 ? results[0] : undefined;
  const source = one ? (one.coop.province_code === "80" ? null : one.coop.api.ref.endsWith("3") ? "manual" : "app_htx") : null;
  return {
    ...base,
    cooperatives: results.length,
    // G-10: Mã HTX và nhãn nguồn diện tích — chỉ ở cấp HTX.
    cooperative_code: base.cooperative_ref ? (one?.coop.api.code ?? null) : null,
    area_source_label: source === "manual" ? "[Nhập tay]" : source === "app_htx" ? "[App HTX]" : null,
    area_source: source,
    area_locked: false,
    overall,
    stages,
  };
}

type Coverage = AppGet<"/v1/mechanization/coverage">;
type Dashboard = AppGet<"/v1/mechanization/dashboard">;

export function coverage(stored: StoredRun, q: { level: string; province_code?: string | null; commune_code?: string | null; farm_stage?: string | null }): Coverage {
  const stage = q.farm_stage && STAGES.some((s) => s.stage === q.farm_stage) ? q.farm_stage : null;
  let res = stored.results;
  if (q.province_code) res = res.filter((r) => r.coop.province_code === q.province_code);
  if (q.commune_code) res = res.filter((r) => r.coop.api.admin_unit_code === q.commune_code);
  let items: MechCoverageItem[];
  if (q.level === "cooperative") {
    items = res.map((r) =>
      item({ unit_code: null, cooperative_ref: r.coop.api.ref, name: r.coop.api.name, province_code: r.coop.province_code, commune_code: r.coop.api.admin_unit_code }, [r], stage),
    );
  } else if (q.level === "commune") {
    items = COMMUNES.filter((c) => !q.province_code || c.province_code === q.province_code).map((c) =>
      item({ unit_code: c.code, cooperative_ref: null, name: c.name, province_code: c.province_code, commune_code: c.code }, res.filter((r) => r.coop.api.admin_unit_code === c.code), stage),
    );
  } else {
    items = PROVINCES.map((p) =>
      item({ unit_code: p.code, cooperative_ref: null, name: `Tỉnh ${p.name}`, province_code: p.code, commune_code: null }, res.filter((r) => r.coop.province_code === p.code), stage),
    );
  }
  const all = item({ unit_code: null, cooperative_ref: null, name: "Toàn vùng", province_code: null, commune_code: null }, res, stage);
  return {
    run: stored.run,
    level: q.level,
    summary: all.overall,
    stages: all.stages,
    items,
    legend: (["shortage", "attention", "sufficient", "surplus", "no_data"] as const).map((s) => ({ status: s, label: STATUS_LABEL[s], color: STATUS_COLOR[s] })),
  };
}

export function dashboard(stored: StoredRun): Dashboard {
  const coops = stored.results.map((r) =>
    item({ unit_code: null, cooperative_ref: r.coop.api.ref, name: r.coop.api.name, province_code: r.coop.province_code, commune_code: r.coop.api.admin_unit_code }, [r], null),
  );
  const byStatus: Record<string, number> = {};
  for (const c of coops) byStatus[c.overall.status] = (byStatus[c.overall.status] ?? 0) + 1;
  const priority = coops
    .filter((c) => c.overall.coverage_pct !== null)
    .sort((a, b) => RANK[a.overall.status] - RANK[b.overall.status] || scaled4(a.overall.coverage_pct ?? "0") - scaled4(b.overall.coverage_pct ?? "0"))
    .slice(0, 12);
  return {
    kpis: {
      total_machines: coops.reduce((s, c) => s + c.overall.machines_total, 0),
      total_cooperatives: coops.length,
      cooperatives_sufficient: coops.filter((c) => c.overall.status === "sufficient" || c.overall.status === "surplus").length,
      cooperatives_shortage: coops.filter((c) => c.overall.status === "shortage").length,
    },
    cooperatives_by_status: byStatus,
    priority,
    run: stored.run,
  };
}
