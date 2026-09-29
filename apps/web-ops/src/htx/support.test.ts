import { describe, expect, it } from "vitest";
import type { ExtTask, SupportRequestStaff } from "@/api/types";
import { gpsLabel, photoLabel, summarizeSupport } from "./support";

function req(over: Partial<SupportRequestStaff> & { status?: ExtTask["status"]; assignee?: string | null }): SupportRequestStaff {
  const { status = "assigned", assignee = "01927b3e-0ff1-7000-8000-000000000001", ...rest } = over;
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
    assignment_rule: "commune_on_duty",
    task: { status, assignee_ref: assignee } as ExtTask,
    ...rest,
  };
}

describe("yêu cầu hỗ trợ cho cán bộ (HTX US-SUPPORT-01)", () => {
  it("nhãn ảnh", () => {
    expect(photoLabel(0)).toBe("Không có ảnh");
    expect(photoLabel(3)).toBe("3 ảnh");
  });

  it("AC-1: thiếu GPS gắn nhãn “Thiếu vị trí GPS”; có GPS hiện độ chính xác", () => {
    expect(gpsLabel({ gps_missing: true, labels: ["Thiếu vị trí GPS"], gps_accuracy_m: null })).toEqual({ text: "Thiếu vị trí GPS", missing: true });
    expect(gpsLabel({ gps_missing: false, labels: [], gps_accuracy_m: "8" })).toEqual({ text: "Có vị trí GPS (±8 m)", missing: false });
    expect(gpsLabel({ gps_missing: false, labels: [], gps_accuracy_m: null }).text).toBe("Có vị trí GPS");
  });

  it("KN US-TASK-03: tổng hợp chưa xử lý, khẩn, thiếu GPS, chưa giao", () => {
    const s = summarizeSupport([
      req({ urgency: "khan", status: "new", assignee: null }),
      req({ gps_missing: true, status: "in_progress" }),
      req({ urgency: "khan", status: "resolved" }),
    ]);
    expect(s).toEqual({ total: 3, open: 2, urgentOpen: 1, gpsMissing: 1, unassigned: 1 });
    expect(summarizeSupport([])).toEqual({ total: 0, open: 0, urgentOpen: 0, gpsMissing: 0, unassigned: 0 });
  });
});
