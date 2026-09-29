import { describe, expect, it } from "vitest";
import { createAdminApiClient, createApiClient, unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { mockToken } from "@/api/mock/personas";
import { ApiProblem } from "@/api/problem";
import type { ParamVersion } from "@/api/types";
import { AREA_SYNC_OUTCOME, staleMessage } from "@/cgh/AreaFreshness";
import { HEX_COLOR_ERROR, PERCENT_ERROR } from "@/gis/legend";
import { COLOR_ROWS, displayValuesByKey, FULL_KEY, isDisplayKey, pendingByKey, sourceLabel, thresholdPlan, WARN_KEY } from "./displayConfig";
import { dayBound, dlqAlerts, dlqLabel, SYNC_STATUS, type SyncIntegration } from "./syncMonitor";

const app = (p: string) => createApiClient({ baseUrl: "https://api.test", getAccessToken: () => mockToken(p), fetch: mockFetchNoDelay });
const admin = (p: string) => createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken(p), fetch: mockAdminFetchNoDelay });
const hdr = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

async function problemOf(p: Promise<unknown>): Promise<ApiProblem> {
  try {
    await p;
  } catch (e) {
    if (e instanceof ApiProblem) return e;
    throw e;
  }
  throw new Error("expected problem");
}

describe("quy chuẩn hiển thị — màn quản trị (GIS US-CFG-01/02)", () => {
  it("GIS US-CFG-01: đủ 11 khóa màu (6 giai đoạn, 4 mức lấp đầy, viền chín rộ); lọc khóa hiển thị", () => {
    expect(COLOR_ROWS).toHaveLength(11);
    expect(COLOR_ROWS.find((r) => r.key === "gis.stage_color:chin_ro")?.label).toBe("Chín rộ / đang thu hoạch");
    expect(isDisplayKey("gis.capacity_color:full")).toBe(true);
    expect(isDisplayKey(WARN_KEY)).toBe(true);
    expect(isDisplayKey("extension.task_sla_hours")).toBe(false);
    expect(sourceLabel({ source: "approved", version: 3 })).toBe("Đã duyệt v.3");
    expect(sourceLabel({ source: "default", version: 0 })).toBe("Mặc định");
  });

  it("GIS US-CFG-01: chờ duyệt gom theo khóa hiển thị, bỏ khóa khác và bản không ở trạng thái submitted", () => {
    const v = (key: string, status: ParamVersion["status"], version: number) => ({ id: `${key}${version}`, key, status, version, value: null, text: "#010203" }) as unknown as ParamVersion;
    const m = pendingByKey([v("gis.stage_color:sap_thu", "submitted", 2), v("gis.stage_color:sap_thu", "submitted", 1), v("gis.stage_color:da_thu", "draft", 1), v("cgh.x.y", "submitted", 1)]);
    expect(Array.from(m.keys())).toEqual(["gis.stage_color:sap_thu"]);
    expect(m.get("gis.stage_color:sap_thu")!.map((x) => x.version)).toEqual([1, 2]);
  });

  it("GIS US-CFG-01 AC-2: thứ tự đề xuất bộ ngưỡng — nâng thì Đã đầy trước, hạ thì Sắp đầy trước; không đổi → rỗng", () => {
    expect(thresholdPlan({ warn: "60", full: "85" }, { warn: "70", full: "90" }).map((s) => s.key)).toEqual([FULL_KEY, WARN_KEY]);
    expect(thresholdPlan({ warn: "60", full: "85" }, { warn: "50", full: "80" }).map((s) => s.key)).toEqual([WARN_KEY, FULL_KEY]);
    expect(thresholdPlan({ warn: "60", full: "85" }, { warn: "60.0", full: "85" })).toEqual([]);
    expect(thresholdPlan({ warn: "60", full: "85" }, { warn: "70", full: "85" })).toEqual([{ key: WARN_KEY, value: "70" }]);
  });

  it("GIS US-CFG-01: mock display-config — mặc định nhúng + ngưỡng đã duyệt; màu HEX sai → 422 trường text đúng thông điệp", async () => {
    const cfg = await unwrap(app("xa").GET("/v1/masterdata/display-config"));
    const byKey = displayValuesByKey(cfg);
    expect(byKey.get("gis.stage_color:chin_ro")).toMatchObject({ value: "#D6452F", source: "default", version: 0 });
    expect(byKey.get(WARN_KEY)).toMatchObject({ value: "60", source: "approved", default: "80" });
    const bad = await problemOf(
      unwrap(admin("setter").POST("/v1/masterdata/params/versions", { params: hdr(), body: { key: "gis.stage_color:da_thu", text: "#12345G", effective_from: "2026-09-30", source_doc: "VB thử" } })),
    );
    expect(bad.code).toBe("validation_failed");
    expect(bad.fieldErrors).toContainEqual({ field: "text", reason: HEX_COLOR_ERROR });
  });

  it("GIS US-CFG-01 AC-2: ngưỡng không tăng dần → masterdata.param_percent_thresholds; ngoài 0–100 → lỗi trường", async () => {
    const order = await problemOf(
      unwrap(admin("setter").POST("/v1/masterdata/params/versions", { params: hdr(), body: { key: WARN_KEY, value: "90", unit: "%", effective_from: "2026-09-30", source_doc: "VB thử" } })),
    );
    expect(order.code).toBe("masterdata.param_percent_thresholds");
    expect(order.message).toBe(PERCENT_ERROR);
    const range = await problemOf(
      unwrap(admin("setter").POST("/v1/masterdata/params/versions", { params: hdr(), body: { key: FULL_KEY, value: "101", unit: "%", effective_from: "2026-09-30", source_doc: "VB thử" } })),
    );
    expect(range.fieldErrors).toContainEqual({ field: "value", reason: PERCENT_ERROR });
  });

  it("GIS US-CFG-02: màu mới áp dụng khi được người khác duyệt; khôi phục mặc định = phiên bản nháp mới; khóa lạ → 404", async () => {
    const draft = await unwrap(
      admin("setter").POST("/v1/masterdata/params/versions", {
        params: hdr(),
        body: { key: "gis.stage_color:sinh_truong", text: "#00aa11", effective_from: new Date(Date.now() - 1000).toISOString(), source_doc: "VB thử" },
      }),
    );
    expect(draft).toMatchObject({ text: "#00AA11", value: null, status: "draft" });
    await unwrap(admin("setter").POST("/v1/masterdata/params/versions/{id}/submit", { params: { path: { id: draft.id }, ...hdr() } }));
    await unwrap(admin("approver").POST("/v1/masterdata/params/versions/{id}/approve", { params: { path: { id: draft.id }, ...hdr() }, body: {} }));
    const cfg = await unwrap(app("xa").GET("/v1/masterdata/display-config"));
    expect(cfg.stage_colors.sinh_truong).toMatchObject({ value: "#00AA11", source: "approved" });
    const restore = await unwrap(admin("setter").POST("/v1/masterdata/params/restore-default", { params: hdr(), body: { key: "gis.stage_color:sinh_truong" } }));
    expect(restore).toMatchObject({ text: "#2E9E4F", status: "draft", version: draft.version + 1 });
    const none = await problemOf(unwrap(admin("setter").POST("/v1/masterdata/params/restore-default", { params: hdr(), body: { key: "extension.task_sla_hours" } })));
    expect(none.code).toBe("masterdata.param_no_default");
    expect(none.status).toBe(404);
  });
});

describe("giám sát đồng bộ (GIS US-SYNC-01/02)", () => {
  const it0 = (o: Partial<SyncIntegration>) => ({ consumer: "c", source: "s", status: "ok", processed_count: 1, last_processed_at: null, last_event_type: null, failure_count: 0, dead_lettered_count: 0, last_failure_at: null, last_error_code: null, dlq_depth: 0, ...o }) as SyncIntegration;

  it("GIS US-SYNC-01: nhãn trạng thái, DLQ không đo được, cảnh báo khi DLQ > 0 hoặc có sự kiện vào hàng chờ lỗi", () => {
    expect(SYNC_STATUS.ok.label).toBe("Bình thường");
    expect(SYNC_STATUS.error.label).toBe("Lỗi");
    expect(SYNC_STATUS.idle.label).toBe("Chưa có dữ liệu");
    expect(dlqLabel(null)).toBe("không đo được");
    expect(dlqLabel(2)).toBe("2");
    expect(dlqAlerts([it0({ consumer: "a" }), it0({ consumer: "b", dlq_depth: 1 }), it0({ consumer: "c", dlq_depth: null, dead_lettered_count: 2 })]).map((x) => x.consumer)).toEqual(["b", "c"]);
    expect(dayBound("2026-09-01", false)).toBe("2026-09-01T00:00:00+07:00");
    expect(dayBound("2026-09-01", true)).toBe("2026-09-01T23:59:59+07:00");
    expect(dayBound("", true)).toBeUndefined();
  });

  it("GIS US-SYNC-01/02: mock — lỗi + DLQ ở luồng App HTX → CGH, lọc trạng thái, nhật ký lọc nguồn + phân trang; vai khác 403", async () => {
    const all = await unwrap(admin("setter").GET("/v1/sync/integrations", { params: { query: {} } }));
    const area = all.items.find((i) => i.consumer === "pf.mechanization.area_sync")!;
    expect(area).toMatchObject({ status: "error", dlq_depth: 1 });
    expect(area.dead_lettered_count).toBeGreaterThan(0);
    expect(all.items.some((i) => i.status === "idle" && i.dlq_depth === null)).toBe(true);
    const errs = await unwrap(admin("setter").GET("/v1/sync/integrations", { params: { query: { status: "error" } } }));
    expect(errs.items.every((i) => i.status === "error")).toBe(true);
    const log = await unwrap(admin("setter").GET("/v1/sync/integrations/log", { params: { query: { consumer: "pf.geo.hub_valve", limit: 5 } } }));
    expect(log.items).toHaveLength(5);
    expect(log.items.every((l) => l.consumer === "pf.geo.hub_valve")).toBe(true);
    expect(log.next_cursor).not.toBeNull();
    expect((await problemOf(unwrap(admin("approver").GET("/v1/sync/integrations", { params: { query: {} } })))).status).toBe(403);
  });
});

describe("độ tươi dữ liệu App HTX trên Bản đồ CGH (CGH FN-07)", () => {
  it("CGH FN-07: nhãn kết quả QT-03 và thông điệp dự phòng", () => {
    expect(AREA_SYNC_OUTCOME.applied.label).toBe("Đã áp");
    expect(AREA_SYNC_OUTCOME.kept_locked.label).toBe("Giữ bản nhập tay đã khóa");
    expect(AREA_SYNC_OUTCOME.stale_ignored.label).toBe("Bỏ qua (cũ hơn)");
    expect(staleMessage({ stale: false, message: null, threshold_hours: "48" })).toBeNull();
    expect(staleMessage({ stale: true, message: null, threshold_hours: "48" })).toMatch(/đang dùng dữ liệu nhập tay dự phòng/);
  });

  it("CGH FN-07 AC-03: mock — feed quá 48 giờ → stale + message; nhật ký chỉ cho Admin CGH", async () => {
    const s = await unwrap(app("cuc").GET("/v1/mechanization/area-sync-status", { params: { query: { season_code: "HT", season_year: 2026 } } }));
    expect(s).toMatchObject({ stale: true, fallback_manual: true, threshold_hours: "48" });
    expect(s.message).toMatch(/đang dùng dữ liệu nhập tay dự phòng\.$/);
    expect(s.items.length).toBeGreaterThan(0);
    expect((await problemOf(unwrap(app("cuc").GET("/v1/mechanization/area-sync-log", { params: { query: {} } })))).status).toBe(403);
    const log = await unwrap(app("setter").GET("/v1/mechanization/area-sync-log", { params: { query: { season_code: "HT", season_year: 2026, source: "manual" } } }));
    expect(log.items.length).toBeGreaterThan(0);
    expect(log.items.every((l) => l.source === "manual")).toBe(true);
  });
});
