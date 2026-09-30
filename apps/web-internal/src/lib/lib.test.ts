import { describe, expect, it } from "vitest";
import { addKg, applyPct, formatPct, grams, isKg, isPct, kgOf, normKg, pctDiff, perUnit, subKg } from "./kg";
import { DEFAULT_LOSS_NORM, DEFAULT_TOLERANCE, IncompleteError, lossPctFor, reconcile, type ReconcileInput } from "./reconcile";
import { checkStage, DEFAULT_RISK_V2, deviceHealth, riskScore, validateRisk } from "./rules";
import { parseKgCell, parseWeightsCsv } from "./weights";
import { labelOf, recent, remember } from "./recent";

describe("kg — thập phân không float (R-10)", () => {
  it("đọc/ghi 3 chữ số, cộng trừ chính xác", () => {
    expect(grams("96800.5")).toBe(96_800_500n);
    expect(kgOf(-150n)).toBe("-0.150");
    expect(normKg("12")).toBe("12.000");
    expect(addKg("0.1", "0.2")).toBe("0.300");
    expect(subKg("93520.000", "93900.000")).toBe("-380.000");
    expect(isKg("1.2345")).toBe(false);
    expect(isPct("100")).toBe(true);
    expect(isPct("100.01")).toBe(false);
  });
  it("phần trăm làm tròn nửa ra xa 0 như backend", () => {
    expect(pctDiff("96800", "88000")).toBe("10.00");
    expect(pctDiff("93520", "93900")).toBe("-0.40");
    expect(applyPct("96800", "3.00")).toBe("2904.000");
    expect(perUnit("96800", 4400)).toBe("22.000");
    expect(formatPct("10.00", true)).toBe("+10,00 %");
  });
});

const base: ReconcileInput = {
  route: "HUB",
  basis: "per_tonne",
  hubIn: "96800.000",
  hubOut: "93900.000",
  customerIn: "93520.000",
  hubInMoisture: "17.50",
  rolls: { rolls: 4400, kgPerRoll: "20" },
  tolerance: DEFAULT_TOLERANCE,
  lossNorm: DEFAULT_LOSS_NORM,
};

describe("đối soát measurement.reconcile.v1 (mock = quy tắc backend)", () => {
  it("proto plant-weighing: ghe 4 400 cuộn cân 96,8 t → cờ +10 % so với ước 88 t (theo tấn, RB-6)", () => {
    const r = reconcile(base);
    expect(r.roll_check).toMatchObject({ status: "flag", rolls: 4400, estimate_kg: "88000.000", variance_pct: "10.00" });
    expect(r.yield_kg_per_roll).toBe("22.000");
    expect(r.internal).toMatchObject({ status: "normal", loss_kg: "2900.000", loss_norm_pct: "3.00", expected_loss_kg: "2904.000", abnormal_loss_kg: "0.000" });
    expect(r.commercial).toMatchObject({ status: "within", diff_pct: "-0.40" });
    expect(r.outcome).toBe("within_tolerance");
  });
  it("giá theo cuộn → kiểm cuộn chỉ tham khảo", () => {
    expect(reconcile({ ...base, basis: "per_bale" }).roll_check.status).toBe("informational");
  });
  it("chênh thương mại vượt cờ → mở tranh chấp; vượt 15 % → nghiêm trọng", () => {
    expect(reconcile({ ...base, customerIn: "88000.000" })).toMatchObject({ outcome: "dispute_opened", commercial: { status: "flag" } });
    expect(reconcile({ ...base, customerIn: "70000.000" }).commercial.status).toBe("critical");
  });
  it("hao hụt bất thường > 1 % HUB_IN → cảnh báo hao hụt; HUB_OUT > HUB_IN → gain", () => {
    const r = reconcile({ ...base, hubIn: "60000.000", hubOut: "58200.000", customerIn: "58100.000", hubInMoisture: "14.00" });
    expect(r.internal).toMatchObject({ status: "abnormal", expected_loss_kg: "600.000", abnormal_loss_kg: "1200.000" });
    expect(r.outcome).toBe("loss_alert");
    expect(reconcile({ ...base, hubOut: "97000.000", customerIn: "97000.000" }).internal.status).toBe("gain");
  });
  it("giao thẳng: chỉ CUSTOMER_IN, gộp; thiếu phiếu → IncompleteError", () => {
    const r = reconcile({ ...base, route: "DIRECT", hubIn: null, hubOut: null, customerIn: "45600.000", rolls: { rolls: 2280, kgPerRoll: "20" } });
    expect(r).toMatchObject({ outcome: "direct_aggregate", yield_aggregated: true, yield_kg_per_roll: "20.000" });
    expect(() => reconcile({ ...base, customerIn: null })).toThrow(IncompleteError);
  });
  it("định mức sấy theo khoảng ẩm [from, to), khoảng cuối nhận biên trên", () => {
    expect(lossPctFor(DEFAULT_LOSS_NORM, "15.00")).toBe("3.00");
    expect(lossPctFor(DEFAULT_LOSS_NORM, "14.99")).toBe("1.00");
    expect(lossPctFor(DEFAULT_LOSS_NORM, "100.00")).toBe("10.00");
    expect(lossPctFor(DEFAULT_LOSS_NORM, null)).toBe("3.00");
  });
});

describe("đếm cuộn theo công đoạn (proto FM-03, FM-04)", () => {
  it("công đoạn đầu luôn được; công đoạn sau cần số trước", () => {
    expect(checkStage("bale", 5800, undefined)).toEqual({ ok: true, needsReason: false });
    expect(checkStage("gather", 5700, undefined)).toEqual({ ok: false, code: "fieldops.previous_stage_missing" });
  });
  it("nhiều hơn công đoạn trước → từ chối; ít hơn cần lý do ≥ 10 ký tự", () => {
    expect(checkStage("gather", 5900, 5800)).toEqual({ ok: false, code: "fieldops.stage_exceeds_previous" });
    expect(checkStage("gather", 5700, 5800, "ướt")).toEqual({ ok: false, code: "fieldops.discrepancy_reason_required" });
    expect(checkStage("gather", 5700, 5800, "100 cuộn bị ướt để lại")).toEqual({ ok: true, needsReason: true });
    expect(checkStage("boat_load", 5700, 5700)).toEqual({ ok: true, needsReason: false });
  });
});

describe("hub.risk.v2 và sức khoẻ thiết bị", () => {
  it("điểm rủi ro: tuổi + ẩm vượt ngưỡng + cảnh báo, bão hoà 1", () => {
    expect(riskScore(DEFAULT_RISK_V2, 45, 23, 0)).toBe("37.5"); // 0.5×40 + 0.5×35
    expect(riskScore(DEFAULT_RISK_V2, 200, 40, 20)).toBe("100.0");
    expect(riskScore(DEFAULT_RISK_V2, 0, 18, 0)).toBe("0.0");
  });
  it("kiểm tham số: Σ trọng số = 100, ngưỡng tăng dần", () => {
    expect(validateRisk(DEFAULT_RISK_V2)).toBeNull();
    expect(validateRisk({ ...DEFAULT_RISK_V2, age_weight: "50" })).toMatch(/100/);
    expect(validateRisk({ ...DEFAULT_RISK_V2, temp_crit_c: "30" })).toMatch(/nhiệt độ/);
  });
  it("mất tín hiệu, đệm đầy, lệch đồng hồ", () => {
    const now = Date.parse("2026-09-30T10:00:00Z");
    const ok = deviceHealth({ status: "active", health: { last_seen_at: "2026-09-30T09:58:00Z", buffer_pct: "3.50", clock_skew_ms: 120 } }, now);
    expect(ok.level).toBe("ok");
    const bad = deviceHealth({ status: "active", health: { last_seen_at: "2026-09-30T06:50:00Z", buffer_pct: "64.00", clock_skew_ms: 2400 } }, now);
    expect(bad.level).toBe("bad");
    expect(bad.issues).toHaveLength(3);
    expect(deviceHealth({ status: "revoked", health: {} }, now).level).toBe("unknown");
  });
});

describe("nhập số cân khách (CSV)", () => {
  const lot = "0192f003-0000-7000-8000-000000000001";
  it("đọc tiêu đề, số kiểu Việt Nam, dấu chấm phẩy", () => {
    const r = parseWeightsCsv(`lot_ref;mass_kg;weighed_at;customer_ticket_no\n${lot};93.520,5;2026-09-25T08:00:00+07:00;PC-001`);
    expect(r.errors).toEqual([]);
    expect(r.rows[0]).toMatchObject({ lot_ref: lot, mass_kg: "93520.500", customer_ticket_no: "PC-001" });
    expect(parseKgCell("96800")).toBe("96800.000");
    expect(parseKgCell("-5")).toBeNull();
  });
  it("một dòng sai → báo lỗi theo dòng; chặn ô công thức", () => {
    const r = parseWeightsCsv(`${lot},abc,2026-09-25T08:00:00Z\n=cmd|' /C calc'!A0`);
    expect(r.errors.map((e) => e.line)).toEqual([1, 2]);
  });
});

describe("mã dùng gần đây", () => {
  it("nhớ nhãn, đưa mục mới lên đầu, không trùng", () => {
    remember("transfer", "t-1", "A → B");
    remember("transfer", "t-2", "C → D");
    remember("transfer", "t-1");
    expect(recent("transfer").map((x) => x.id)).toEqual(["t-1", "t-2"]);
    expect(labelOf("transfer", "t-1")).toBe("A → B");
  });
});
