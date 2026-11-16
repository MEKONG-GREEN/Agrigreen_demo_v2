import { describe, expect, it } from "vitest";
import type { CoopSupportRequest } from "@/api/types";
import { gpsLabel, photoLabel, summarizeSupport } from "./support";

function req(over: Partial<CoopSupportRequest>): CoopSupportRequest {
  return {
    id: "r",
    household_ref: "h",
    plot_ref: null,
    cooperative_ref: "c",
    commune_code: "30001",
    province_code: "89",
    category: "dich_hai",
    urgency: "binh_thuong",
    description: "Rầy nâu",
    photo_refs: [],
    photo_count: 0,
    gps_missing: false,
    gps_accuracy_m: "8",
    labels: [],
    event_time: "2026-09-15T03:00:00Z",
    received_at: "2026-09-15T03:00:00Z",
    legacy_task_status: null,
    ...over,
  };
}

describe("yêu cầu hỗ trợ cho quản lý HTX (HTX US-SUPPORT-01)", () => {
  it("nhãn ảnh", () => {
    expect(photoLabel(0)).toBe("Không có ảnh");
    expect(photoLabel(3)).toBe("3 ảnh");
  });

  it("AC-1: thiếu GPS gắn nhãn “Thiếu vị trí GPS”; có GPS hiện độ chính xác", () => {
    expect(gpsLabel({ gps_missing: true, labels: ["Thiếu vị trí GPS"], gps_accuracy_m: null })).toEqual({ text: "Thiếu vị trí GPS", missing: true });
    expect(gpsLabel({ gps_missing: false, labels: [], gps_accuracy_m: "8" })).toEqual({ text: "Có vị trí GPS (±8 m)", missing: false });
    expect(gpsLabel({ gps_missing: false, labels: [], gps_accuracy_m: null }).text).toBe("Có vị trí GPS");
  });

  it("tổng hợp số yêu cầu, khẩn, thiếu GPS", () => {
    const s = summarizeSupport([req({ urgency: "khan" }), req({ gps_missing: true }), req({ urgency: "khan", legacy_task_status: "resolved" })]);
    expect(s).toEqual({ total: 3, urgent: 2, gpsMissing: 1 });
    expect(summarizeSupport([])).toEqual({ total: 0, urgent: 0, gpsMissing: 0 });
  });
});
