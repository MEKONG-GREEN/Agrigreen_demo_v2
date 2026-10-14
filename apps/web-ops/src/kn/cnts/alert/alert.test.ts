import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetCntsAlerts } from "@/api/mock/kn-cnts-alert";
import { ApiProblem } from "@/api/problem";
import { createAlertApi, findingsOf, reasonOf, reminderErrors, type FormReminder } from "./api";

function client(personaId: string) {
  return createAlertApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${personaId}`, getContextId: () => null, fetch: mockFetchNoDelay }));
}

const code = async (p: Promise<unknown>) => p.then(() => "ok", (e: unknown) => (e instanceof ApiProblem ? e.problem.code : String(e)));

const reminder = (patch: Partial<FormReminder> = {}): FormReminder => ({
  before_due_hours: 24, on_overdue: true, repeat_daily: true, repeat_at: "08:00", late_streak: 2, channels: ["in_app"], message: "Nhắc", ...patch,
});

describe("Cảnh báo CNTS — hàm phía máy khách", () => {
  it("kiểm cấu hình nhắc (UR-TINH-08)", () => {
    expect(reminderErrors(reminder())).toEqual({});
    expect(reminderErrors(reminder({ repeat_at: "25:00" })).repeat_at).toBeDefined();
    expect(reminderErrors(reminder({ before_due_hours: 200 })).before_due_hours).toBeDefined();
    expect(reminderErrors(reminder({ late_streak: 13 })).late_streak).toBeDefined();
  });
  it("đọc vi phạm và lý do từ detail", () => {
    const a = { detail: { summary: "Heo tăng 20%", findings: [{ field: "tong_dan_heo", label: "Heo", reason: "tăng 20%" }] } };
    expect(findingsOf(a)).toHaveLength(1);
    expect(reasonOf(a)).toBe("Heo tăng 20%");
    expect(findingsOf({ detail: {} })).toEqual([]);
  });
});

describe("Cảnh báo CNTS — backend giả lập /v1/extension/cnts/alerts*, /alert-config*", () => {
  beforeEach(() => resetCntsAlerts());

  it("Tỉnh giao, khóa lạc quan, đóng có kết quả; cấu hình nhắc có phiên bản", async () => {
    const tinh = client("kn-binh");
    const l = await tinh.list({ status: "open" });
    expect(l.counts.urgent_open).toBe(1);
    const a = l.items.find((x) => x.kind === "outbreak")!;
    expect(a.can_assign).toBe(true);
    const people = await tinh.assignees(a.commune_code);
    const d = await tinh.assign(a.alert_ref, a.seq, people[0]!.actor.ref);
    expect(d.status).toBe("assigned");
    expect(await code(tinh.assign(a.alert_ref, a.seq, people[0]!.actor.ref))).toBe("extension.cnts_alert_version_conflict");
    expect(await code(tinh.close(a.alert_ref, d.seq, "ngắn"))).toBe("extension.cnts_alert_invalid");
    const c = await tinh.close(a.alert_ref, d.seq, "Đã khoanh vùng, tiêu hủy theo quy định");
    expect(c.status).toBe("closed");
    expect(c.history.map((h) => h.action)).toEqual(["raised", "assigned", "closed"]);

    const cfg = await tinh.config();
    expect(cfg.forms.find((f) => f.code === "CNTS-BM01")?.reminder.late_streak).toBe(3);
    expect(await code(tinh.putForm("CNTS-BM04", reminder({ repeat_at: "9h" })))).toBe("extension.cnts_alert_config_invalid");
    const saved = await tinh.putForm("CNTS-BM04", reminder({ before_due_hours: 48, channels: ["email"] }));
    const bm04 = saved.forms.find((f) => f.code === "CNTS-BM04")!;
    expect(bm04).toMatchObject({ custom: true, version: 1 });
    expect(bm04.reminder.channels).toEqual(["in_app", "email"]);
    expect(await code(tinh.putKind("herd_change", { sla_hours: 24, urgent: true }))).toBe("extension.cnts_alert_config_invalid");
    expect((await tinh.putKind("water_quality", { sla_hours: 12, urgent: true })).kinds.find((k) => k.kind.code === "water_quality")?.level).toBe("urgent");
  });
});
