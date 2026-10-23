import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { createWorkScheduleApi } from "@/kn/schedule/api";
import type { WsItemInput } from "@/kn/schedule/model";
import { mockFetchNoDelay } from "./handler";
import { callerOf, resetWorkSchedules, stableUuid, WS_ROSTER, type Result, type WorkScheduleStore, type WsCaller } from "./kn-schedules";
import { findPersona } from "./personas";

// Thứ Sáu 02/10/2026 10:00 giờ Việt Nam: tuần này 28/09, tuần sau 05/10, hạn đăng ký tuần sau 02/10 17:00.
const T0 = Date.parse("2026-10-02T03:00:00Z");
const NEXT = "2026-10-05";
const CUR = "2026-09-28";

const officer = (key: string) => WS_ROSTER.find((o) => o.actor_ref === stableUuid(key))!;
function asCaller(key: string, role: WsCaller["role"] = officer(key).level === "commune" ? "kn_xa" : "kn_tinh"): WsCaller {
  const o = officer(key);
  return {
    role,
    actor_ref: o.actor_ref,
    display: o.display,
    department_code: o.department_code,
    province_code: o.province_code,
    communes: o.communes,
    level: o.level,
    approver: o.mgmt_level === "Trưởng phòng",
  };
}

function body<T>(r: Result<T>): T {
  if (!r.ok) throw new Error(`${r.status} ${r.code}`);
  return r.body;
}
function code<T>(r: Result<T>): string {
  return r.ok ? "ok" : r.code;
}

const item = (day: string, extra: Partial<WsItemInput> = {}): WsItemInput => ({ day, session: "morning", start_time: "07:30", end_time: "10:00", content: "Thăm đồng lúa", commune_code: "91301", location: "Ấp 1", ...extra });

let now = T0;
let store: WorkScheduleStore;
const an = () => asCaller("kn-an");
const binh = () => asCaller("kn-binh");
const hoa = () => asCaller("ws-hoa");

beforeEach(() => {
  now = T0;
  store = resetWorkSchedules(() => now);
});

describe("sổ lịch công tác giả lập — trạng thái", () => {
  it("dữ liệu mẫu: tuần sau 2 lịch Chờ duyệt (một có trùng giờ), An và Phúc chưa đăng ký, Thu mới lưu nháp", () => {
    const list = body(store.list(binh(), { week_start: null, status: "pending", commune_code: null, owner_level: null }));
    expect(list.week_start).toBe(NEXT);
    expect(list.items.map((s) => s.owner_display)).toEqual(["Lý Thị Hoa", "Lê Thị Mai"]);
    expect(list.items[0]!.conflict_count).toBe(2);
    expect(list.items[0]!.items.filter((x) => x.conflict)).toHaveLength(2);
    const un = body(store.unregistered(binh(), NEXT, null)).items;
    expect(un.map((u) => [u.actor_display, u.status])).toEqual([
      ["Nguyễn Văn An", "none"],
      ["Huỳnh Văn Phúc", "none"],
      ["Đặng Thị Thu", "draft"],
      ["Trần Thị Bình", "none"],
    ]);
    const counts = body(store.counts(binh(), NEXT));
    expect(counts).toMatchObject({ pending: 2, draft: 1, approved: 0, unregistered: 4, pending_open_weeks: 2 });
    // Lịch tuần này của chính Trưởng phòng đang chờ: không tính vào số đếm của chính họ.
    expect(body(store.counts(binh(), CUR)).pending).toBe(1);
  });

  it("Xã: lưu nháp → gửi duyệt → Tỉnh yêu cầu chỉnh sửa (lý do ≥ 10) → sửa, gửi lại → duyệt; Log đủ sự kiện", () => {
    const mine0 = body(store.mine(an(), null));
    expect(mine0).toMatchObject({ week_start: NEXT, schedule: null, previous_available: true, locked: false, late_now: false });

    const d1 = body(store.save(an(), { week_start: NEXT, action: "draft", base_version: null, items: [item(NEXT, { commune_code: "91301" })] }));
    expect(d1.schedule).toMatchObject({ status: "draft", version: 1, item_count: 1 });
    expect(d1.can).toMatchObject({ edit: true, submit: true, approve: false });

    const s1 = body(store.save(an(), { week_start: NEXT, action: "submit", base_version: 1, items: d1.items }));
    expect(s1.schedule).toMatchObject({ status: "pending", version: 2, late: false, amendment: false });

    // Cùng phòng ban + tỉnh: Bình (Trưởng phòng) duyệt được; Hoa (Cấp Xã) không thấy.
    expect(body(store.get(binh(), s1.schedule.id)).can).toMatchObject({ approve: true, request_changes: true, edit: false });
    expect(code(store.get(hoa(), s1.schedule.id))).toBe("extension.schedule_not_found");

    expect(code(store.requestChanges(binh(), s1.schedule.id, { version: 2, reason: "ngắn" }))).toBe("validation_failed");
    const rc = body(store.requestChanges(binh(), s1.schedule.id, { version: 2, reason: "Bổ sung địa điểm cụ thể cho buổi sáng" }));
    expect(rc.schedule).toMatchObject({ status: "changes_requested", last_reason: "Bổ sung địa điểm cụ thể cho buổi sáng", reviewer_display: "Trần Thị Bình" });

    const mine1 = body(store.mine(an(), NEXT));
    expect(mine1.schedule?.schedule.last_reason).toContain("Bổ sung địa điểm");
    const s2 = body(store.save(an(), { week_start: NEXT, action: "submit", base_version: 2, items: [{ ...mine1.schedule!.items[0]!, location: "Ấp Mỹ Hòa" }] }));
    expect(s2.schedule.status).toBe("pending");

    const ap = body(store.approve(binh(), s2.schedule.id, { version: 3 }));
    expect(ap.schedule).toMatchObject({ status: "approved", approved_version: 3, amendment: false, last_reason: null });

    const log = body(store.logPage(an(), s2.schedule.id, null, 50)).items;
    expect(log.map((l) => l.event)).toEqual(["approved", "submitted", "change_requested", "submitted", "draft_saved"]);
    expect(log[2]!.reason).toBe("Bổ sung địa điểm cụ thể cho buổi sáng");
    expect(log[0]!.changes).toContainEqual({ field: "Trạng thái", old: "Chờ duyệt", new: "Đã duyệt" });
    expect(log[1]!.changes.some((c) => c.field.endsWith("Địa điểm") && c.new === "Ấp Mỹ Hòa")).toBe(true);
    // Phân trang Log theo seq (mới → cũ).
    const p1 = body(store.logPage(an(), s2.schedule.id, null, 2));
    expect(p1.items).toHaveLength(2);
    const p2 = body(store.logPage(an(), s2.schedule.id, p1.next_cursor, 10));
    expect(p2.items.map((l) => l.event)).toEqual(["change_requested", "submitted", "draft_saved"]);
  });

  it("điều chỉnh lịch đã duyệt: Chờ duyệt (amendment), bản đã duyệt giữ, mục khác bản đã duyệt có changed", () => {
    const cur = body(store.mine(an(), CUR)).schedule!;
    expect(cur.schedule.status).toBe("approved");
    const items = cur.items.map((x, i) => (i === 0 ? { ...x, content: "Kiểm tra rầy nâu (đổi giờ)", start_time: "08:00" } : x));
    const am = body(store.save(an(), { week_start: CUR, action: "submit", base_version: cur.schedule.version, items: [...items, item("2026-10-01", { session: "afternoon", start_time: null, end_time: null })] }));
    expect(am.schedule).toMatchObject({ status: "pending", amendment: true, approved_version: cur.schedule.version });
    expect(am.approved_items).toHaveLength(cur.items.length);
    expect(am.items.filter((x) => x.changed)).toHaveLength(2);
    expect(am.versions.at(-1)?.action).toBe("amended");
    // Nháp không được dùng khi đã gửi.
    expect(code(store.save(an(), { week_start: CUR, action: "draft", base_version: am.schedule.version, items }))).toBe("extension.schedule_state_conflict");
    const ap = body(store.approve(binh(), am.schedule.id, { version: am.schedule.version }));
    expect(ap.schedule).toMatchObject({ status: "approved", amendment: false, approved_version: am.schedule.version });
    expect(ap.items.every((x) => !x.changed)).toBe(true);
  });

  it("base_version lệch → 409 schedule_version_conflict; tuần đã kết thúc → 409 week_locked", () => {
    body(store.save(an(), { week_start: NEXT, action: "draft", base_version: null, items: [item(NEXT)] }));
    expect(code(store.save(an(), { week_start: NEXT, action: "draft", base_version: null, items: [item(NEXT)] }))).toBe("extension.schedule_version_conflict");
    expect(code(store.save(an(), { week_start: NEXT, action: "draft", base_version: 5, items: [item(NEXT)] }))).toBe("extension.schedule_version_conflict");
    const prev = "2026-09-21";
    expect(code(store.save(an(), { week_start: prev, action: "submit", base_version: 1, items: [item(prev)] }))).toBe("extension.week_locked");
    const locked = body(store.mine(an(), prev));
    expect(locked.locked).toBe(true);
    expect(locked.schedule?.can.edit).toBe(false);
    expect(body(store.logPage(an(), locked.schedule!.schedule.id, null, 50)).items[0]!.event).toBe("locked");
  });

  it("phân quyền: không tự duyệt (self_approval_forbidden), Chuyên viên không duyệt, Admin chỉ đọc, xã ngoài phạm vi bị từ chối", () => {
    const own = body(store.mine(binh(), CUR)).schedule!;
    expect(own.schedule.status).toBe("pending");
    expect(own.can.approve).toBe(false);
    expect(code(store.approve(binh(), own.schedule.id, { version: own.schedule.version }))).toBe("self_approval_forbidden");
    const mai = asCaller("ws-mai");
    expect(code(store.approve(mai, own.schedule.id, { version: own.schedule.version }))).toBe("extension.not_schedule_approver");
    const admin: WsCaller = { ...binh(), role: "admin", actor_ref: stableUuid("kn-admin"), approver: false, province_code: null };
    expect(store.get(admin, own.schedule.id).ok).toBe(true);
    expect(body(store.get(admin, own.schedule.id)).can).toEqual({ edit: false, submit: false, approve: false, request_changes: false });
    expect(code(store.save(admin, { week_start: NEXT, action: "draft", items: [] }))).toBe("forbidden");
    // Cán bộ xã chỉ ghi được xã phụ trách; không ghi SĐT/CCCD vào thành phần phối hợp (RB-3).
    const bad = store.save(an(), { week_start: NEXT, action: "draft", base_version: null, items: [item(NEXT, { commune_code: "91304", participants: "Anh Tư 0912 345 678" })] });
    expect(code(bad)).toBe("validation_failed");
    expect(!bad.ok && (bad.extra?.errors as { field: string }[]).map((e) => e.field)).toEqual(["items[0].commune_code", "items[0].participants"]);
    expect(code(store.save(an(), { week_start: NEXT, action: "draft", base_version: null, items: [item("2026-10-12")] }))).toBe("validation_failed");
    expect(code(store.unregistered(an(), NEXT, null))).toBe("forbidden");
  });

  it("duyệt hàng loạt: bỏ qua lịch của chính mình và lịch không còn Chờ duyệt", () => {
    const pending = body(store.list(binh(), { week_start: NEXT, status: "pending", commune_code: null, owner_level: null })).items.map((s) => s.id);
    const own = body(store.mine(binh(), CUR)).schedule!.schedule.id;
    const approvedCur = body(store.mine(an(), CUR)).schedule!.schedule.id;
    const r = body(store.bulkApprove(binh(), { ids: [...pending, own, approvedCur] }));
    expect(r.approved).toEqual(pending);
    expect(r.skipped).toEqual([
      { id: own, code: "self_approval_forbidden" },
      { id: approvedCur, code: "extension.schedule_state_conflict" },
    ]);
    expect(body(store.counts(binh(), NEXT))).toMatchObject({ pending: 0, approved: 2, pending_open_weeks: 0 });
  });

  it("trùng giờ → conflict + cảnh báo time_overlap; gửi sau hạn → Đăng ký muộn; sao chép tuần trước dời +7 ngày", () => {
    const copied = body(store.copyPrevious(an(), { week_start: NEXT }));
    expect(copied.schedule.status).toBe("draft");
    expect(copied.items.map((x) => x.day)).toEqual(["2026-10-05", "2026-10-06", "2026-10-06", "2026-10-07", "2026-10-08"]);
    const items = [...copied.items, item("2026-10-05", { start_time: "10:00", end_time: "11:30", content: "Họp thôn" })];
    now = Date.parse("2026-10-02T11:00:00Z"); // 18:00 Thứ Sáu — sau hạn đăng ký 17:00
    expect(body(store.mine(an(), NEXT)).late_now).toBe(true);
    const s = body(store.save(an(), { week_start: NEXT, action: "submit", base_version: copied.schedule.version, items }));
    expect(s.schedule.late).toBe(true);
    expect(s.items.filter((x) => x.conflict).map((x) => x.content)).toEqual(["Kiểm tra rầy nâu HTX Mỹ Hòa", "Họp thôn"]);
    expect(s.warnings.map((w) => w.code)).toEqual(["time_overlap", "late_registration"]);
    expect(body(store.logPage(an(), s.schedule.id, null, 10)).items.map((l) => l.event)).toEqual(["submitted", "draft_saved"]);
    // Tuần chưa có lịch trước → 404.
    expect(code(store.copyPrevious(an(), { week_start: "2026-10-19" }))).toBe("extension.schedule_not_found");
  });
});

describe("HTTP giả lập: ngữ cảnh KN (X-AG-Context), Idempotency-Key, số đếm menu", () => {
  async function clientFor(personaId: string, assignmentId: string) {
    const ctxRes = await mockFetchNoDelay(
      new Request("http://mock/v1/me/context", {
        method: "POST",
        headers: { Authorization: `Bearer mock.${personaId}`, "Content-Type": "application/json", "Idempotency-Key": `ctx-${personaId}-${Math.random()}` },
        body: JSON.stringify({ assignment_id: assignmentId, department_code: "trong_trot" }),
      }),
    );
    const ctx = (await ctxRes.json()) as { context_id: string };
    const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${personaId}`, getContextId: () => ctx.context_id, fetch: mockFetchNoDelay });
    return { api, ws: createWorkScheduleApi(api), contextId: ctx.context_id };
  }

  beforeEach(() => {
    resetWorkSchedules(() => T0);
  });

  it("gọi lặp cùng Idempotency-Key → cùng kết quả, không thêm phiên bản", async () => {
    const { ws } = await clientFor("kn-an", "asg-an-xa");
    const req = { week_start: NEXT, action: "draft" as const, base_version: null, items: [item(NEXT)] };
    const a = await ws.save(req, "idem-ws-0001");
    const b = await ws.save(req, "idem-ws-0001");
    expect(b.schedule.id).toBe(a.schedule.id);
    expect(b.schedule.version).toBe(1);
    expect((await ws.mine(NEXT)).schedule?.schedule.version).toBe(1);
    await expect(ws.save({ ...req, base_version: null }, "idem-ws-0002")).rejects.toMatchObject({ code: "extension.schedule_version_conflict", status: 409 });
  });

  it("Cấp Quốc gia không có quyền; số đếm menu Cấp Tỉnh lấy từ sổ lịch", async () => {
    const tw = await clientFor("kn-quan", "asg-quan-tw");
    await expect(tw.ws.mine()).rejects.toMatchObject({ status: 403 });
    const tinh = await clientFor("kn-binh", "asg-binh-tinh");
    const badges = await tinh.api.GET("/v1/me/menu-badges" as never);
    expect((badges.data as unknown as { counts: Record<string, number> }).counts["extension.work_schedules.pending"]).toBe(2);
    const xa = await clientFor("kn-an", "asg-an-xa");
    const mine = await xa.ws.mine();
    expect(mine.week_start).toBe(NEXT);
    // Tên xã và HTX của tài khoản KN giả lập (địa điểm mục công tác).
    const coops = await xa.api.GET("/v1/cooperatives", { params: { query: { admin_unit: "91302" } } });
    expect(coops.data?.items.map((c) => c.name)).toEqual(["HTX Bình Đức"]);
    const unit = await xa.api.GET("/v1/masterdata/admin-units/{code}", { params: { path: { code: "91301" } } });
    expect(unit.data?.name).toBe("Mỹ Hòa Hưng");
  });

  it("vai thử cũ (không có ngữ cảnh P1): kn_officer → Cấp Xã, kn_province_admin → Cấp Tỉnh", () => {
    const xa = callerOf(findPersona("xa")!, null);
    const tinh = callerOf(findPersona("tinh")!, null);
    expect(xa.ok && xa.body.role).toBe("kn_xa");
    expect(tinh.ok && tinh.body).toMatchObject({ role: "kn_tinh", province_code: "82", approver: true });
    expect(callerOf(findPersona("tw")!, null).ok).toBe(false);
  });

  it("xuất Excel/PDF trả tệp có tên theo tuần", async () => {
    const { ws } = await clientFor("kn-binh", "asg-binh-tinh");
    const x = await ws.exportFile({ week_start: NEXT, format: "xlsx" });
    expect(x.name).toBe(`lich-cong-tac-${NEXT}-gia-lap.csv`);
    expect(await x.blob.text()).toContain("Lý Thị Hoa");
    const p = await ws.exportFile({ week_start: NEXT, format: "pdf" });
    expect((await p.blob.text()).startsWith("%PDF-1.4")).toBe(true);
  });
});
