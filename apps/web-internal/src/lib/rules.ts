// Quy tắc hiển thị/kiểm trước trên form (backend vẫn là nơi quyết định, R-55) và dùng lại trong mock.
import type { ScaleDevice, Stage } from "@/api/types";

// ---------------------------------------------------------------------------
// Đếm cuộn theo công đoạn (proto FM-03, FM-04, FM-09)
// ---------------------------------------------------------------------------
export const STAGES: readonly Stage[] = ["bale", "gather", "boat_load"];
export const STAGE_LABEL: Record<Stage, string> = { bale: "Cuộn rơm", gather: "Tập kết", boat_load: "Xếp ghe/xe" };

export type StageCheck =
  | { ok: true; needsReason: boolean }
  | { ok: false; code: "fieldops.previous_stage_missing" | "fieldops.stage_exceeds_previous" | "fieldops.discrepancy_reason_required" };

/**
 * FM-03: công đoạn trước phải có số. FM-04: bằng công đoạn trước, hoặc ít hơn kèm lý do (≥ 10 ký tự); nhiều hơn → từ chối.
 * `previous` = số hiệu lực của công đoạn liền trước cùng thửa (undefined = chưa có).
 */
export function checkStage(stage: Stage, rolls: number, previous: number | undefined, reason = ""): StageCheck {
  const idx = STAGES.indexOf(stage);
  if (idx === 0) return { ok: true, needsReason: false };
  if (previous === undefined) return { ok: false, code: "fieldops.previous_stage_missing" };
  if (rolls > previous) return { ok: false, code: "fieldops.stage_exceeds_previous" };
  if (rolls < previous) {
    if (reason.trim().length < 10) return { ok: false, code: "fieldops.discrepancy_reason_required" };
    return { ok: true, needsReason: true };
  }
  return { ok: true, needsReason: false };
}

// ---------------------------------------------------------------------------
// Điểm rủi ro xuống cấp hub.risk.v2 (không FEFO)
// ---------------------------------------------------------------------------
export interface RiskParamsV2 {
  age_horizon_days: string;
  age_weight: string;
  moisture_warn_pct: string;
  moisture_span_pct: string;
  moisture_weight: string;
  alert_saturation: string;
  alert_weight: string;
  humidity_warn_pct: string;
  humidity_crit_pct: string;
  temp_warn_c: string;
  temp_crit_c: string;
  crit_alert_weight: number;
}

/** Mặc định TẠM (prototype mekong-green, chờ khách C-4) — domain.DefaultRiskV2. */
export const DEFAULT_RISK_V2: RiskParamsV2 = {
  age_horizon_days: "90",
  age_weight: "40",
  moisture_warn_pct: "18",
  moisture_span_pct: "10",
  moisture_weight: "35",
  alert_saturation: "10",
  alert_weight: "25",
  humidity_warn_pct: "18",
  humidity_crit_pct: "22",
  temp_warn_c: "40",
  temp_crit_c: "55",
  crit_alert_weight: 1,
};

export const RISK_KEYS = Object.keys(DEFAULT_RISK_V2) as (keyof RiskParamsV2)[];

const clamp01 = (x: number) => Math.min(1, Math.max(0, x));

/** Điểm 0..100, 1 chữ số (chỉ hiển thị/mock; backend dùng decimal). */
export function riskScore(p: RiskParamsV2, ageDays: number, moisture: number | null, alerts = 0): string {
  let s = clamp01(ageDays / Number(p.age_horizon_days)) * Number(p.age_weight);
  const warn = Number(p.moisture_warn_pct);
  if (moisture !== null && moisture > warn) s += clamp01((moisture - warn) / Number(p.moisture_span_pct)) * Number(p.moisture_weight);
  if (alerts > 0) s += clamp01(alerts / Number(p.alert_saturation)) * Number(p.alert_weight);
  return (Math.round(s * 10) / 10).toFixed(1);
}

/** Lỗi kiểm bộ tham số rủi ro (như RiskParams.Validate) — null nếu hợp lệ. */
export function validateRisk(p: RiskParamsV2): string | null {
  const n = (k: keyof RiskParamsV2) => Number(p[k]);
  if (RISK_KEYS.some((k) => !Number.isFinite(n(k)))) return "Mọi tham số phải là số";
  if (n("age_horizon_days") <= 0 || n("moisture_span_pct") <= 0 || n("alert_saturation") <= 0) return "Thời hạn tuổi, khoảng ẩm và ngưỡng bão hoà cảnh báo phải > 0";
  if (n("age_weight") + n("moisture_weight") + n("alert_weight") !== 100) return "Tổng trọng số tuổi + ẩm + cảnh báo phải bằng 100";
  if (n("humidity_crit_pct") <= n("humidity_warn_pct") || n("humidity_crit_pct") > 100) return "Ngưỡng độ ẩm kho: cảnh báo < nguy hiểm ≤ 100";
  if (n("temp_crit_c") <= n("temp_warn_c")) return "Ngưỡng nhiệt độ nguy hiểm phải lớn hơn ngưỡng cảnh báo";
  if (!Number.isInteger(p.crit_alert_weight) || p.crit_alert_weight < 1 || p.crit_alert_weight > 10) return "Hệ số cảnh báo nguy hiểm 1..10";
  return null;
}

// ---------------------------------------------------------------------------
// Sức khoẻ thiết bị cân (do ingest báo): lần cuối thấy, đệm %, lệch đồng hồ
// ---------------------------------------------------------------------------
export type HealthLevel = "ok" | "warn" | "bad" | "unknown";
export interface HealthView {
  level: HealthLevel;
  issues: string[];
}
/** Ngưỡng hiển thị (không phải tham số nghiệp vụ): mất tín hiệu > 15 phút, đệm ≥ 50 % cảnh báo / ≥ 80 % nguy, lệch > 2 s. */
export const HEALTH_LIMITS = { staleMinutes: 15, bufferWarn: 50, bufferBad: 80, skewWarnMs: 2000 } as const;

export function deviceHealth(d: Pick<ScaleDevice, "status" | "health">, now: number = Date.now()): HealthView {
  const h = d.health;
  if (d.status !== "active") return { level: "unknown", issues: [] };
  if (!h?.last_seen_at) return { level: "unknown", issues: ["Chưa nhận lô số đo nào"] };
  const issues: string[] = [];
  let level: HealthLevel = "ok";
  const bump = (l: HealthLevel) => {
    if (l === "bad" || (l === "warn" && level === "ok")) level = l;
  };
  const mins = (now - Date.parse(h.last_seen_at)) / 60_000;
  if (mins > HEALTH_LIMITS.staleMinutes) {
    issues.push(`Mất tín hiệu ${Math.round(mins)} phút`);
    bump("bad");
  }
  const buf = h.buffer_pct === null || h.buffer_pct === undefined ? null : Number(h.buffer_pct);
  if (buf !== null && buf >= HEALTH_LIMITS.bufferBad) {
    issues.push(`Đệm gần đầy ${buf} %`);
    bump("bad");
  } else if (buf !== null && buf >= HEALTH_LIMITS.bufferWarn) {
    issues.push(`Đệm ${buf} %`);
    bump("warn");
  }
  const skew = h.clock_skew_ms ?? null;
  if (skew !== null && Math.abs(skew) > HEALTH_LIMITS.skewWarnMs) {
    issues.push(`Lệch đồng hồ ${skew} ms`);
    bump("warn");
  }
  return { level, issues };
}

// ---------------------------------------------------------------------------
// Phân tách nhiệm vụ (R-56): gợi ý trên giao diện — máy chủ vẫn chặn (403 self_approval_forbidden)
// ---------------------------------------------------------------------------
export function isSelf(actor: string | null | undefined, proposer: string | null | undefined): boolean {
  return !!actor && !!proposer && actor === proposer;
}

/** Lý do tối thiểu 10 ký tự (sau khi bỏ khoảng trắng). */
export function validReason(s: string | undefined | null, min = 10): boolean {
  return (s ?? "").trim().length >= min;
}

/** Rút gọn UUID để hiển thị ("…a1b2c3"). */
export function shortRef(id: string | null | undefined): string {
  if (!id) return "—";
  return `…${id.replace(/-/g, "").slice(-6)}`;
}

export const ORDER_STATUS_LABEL: Record<string, string> = {
  planned: "Chờ duyệt",
  approved: "Đã duyệt",
  assigned: "Đã giao tổ",
  closed: "Đã đóng",
  cancelled: "Đã huỷ",
};

export const VERSION_STATE_LABEL: Record<string, string> = {
  pending: "Chờ duyệt",
  approved: "Đã duyệt",
  rejected: "Từ chối",
  superseded: "Bị thay",
  active: "Hiệu lực",
  revoked: "Đã thu hồi",
};

export const PRODUCT_LABEL: Record<string, string> = {
  straw_bale: "Rơm cuộn",
  straw_loose: "Rơm rời",
  dried_straw: "Rơm đã sấy",
  compressed_bale: "Kiện nén",
};
