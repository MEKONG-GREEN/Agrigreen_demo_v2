// Quản trị KN giả lập: FN-AD-04.04 tạo tài khoản 2 vai trò (đăng nhập được, 2 ngữ cảnh), FN-AD-04.07 Menu các cấp
// (lý do bắt buộc, nhật ký attempted + completed, on_behalf_of, chặn tự rà soát bản mình nhập thay), FN-AD-04.03.
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { ApiProblem } from "@/api/problem";
import { createCycleApi, createKnAdminApi, supportReasonStore } from "@/kn/cycle/api";
import { provinceOrgRef } from "./data";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "./handler";
import { resetMockForms } from "./forms/routes";
import { KN_ACCOUNTS, KN_DEPARTMENTS, resetKnAccounts } from "./kn-accounts";
import { mockKnLogin, resetMockLoginAttempts } from "./kn";
import { resetKnAdmin } from "./kn-admin";
import { resetReportCycle } from "./kn-reports/store";

const ORIGINAL = KN_ACCOUNTS.map((a) => ({ ...a, assignments: a.assignments.map((x) => ({ ...x })) }));
const AG_REF = provinceOrgRef(1);

async function contextOf(personaId: string, assignmentId: string, dept: string | null = "trong_trot") {
  const r = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: { Authorization: `Bearer mock.${personaId}`, "Content-Type": "application/json", "Idempotency-Key": `ctx-${Math.random()}` },
      body: JSON.stringify({ assignment_id: assignmentId, department_code: dept }),
    }),
  );
  return ((await r.json()) as { context_id: string }).context_id;
}
const opts = (personaId: string, ctx: () => string | null, admin = false) => ({
  baseUrl: "http://mock",
  getAccessToken: () => `mock.${personaId}`,
  getContextId: ctx,
  fetch: admin ? mockAdminFetchNoDelay : mockFetchNoDelay,
});
const code = async (p: Promise<unknown>) => p.then(() => "ok", (e: unknown) => (e instanceof ApiProblem ? `${e.status} ${e.code}` : String(e)));

let now = Date.parse("2026-10-02T03:00:00Z");
beforeEach(() => {
  now = Date.parse("2026-10-02T03:00:00Z");
  resetMockForms();
  resetReportCycle(() => now);
  resetKnAdmin();
  resetMockLoginAttempts();
});
afterEach(() => {
  resetKnAccounts(ORIGINAL);
  supportReasonStore.set(null);
  for (const d of KN_DEPARTMENTS) if (d.code !== "trong_trot") d.status = "not_open";
});

describe("quản trị KN giả lập", () => {
  it("FN-AD-04.04: tạo tài khoản 2 vai trò → Log, cảnh báo, trùng SĐT bị chặn; đăng nhập được, 2 ngữ cảnh", async () => {
    const adminCtx = await contextOf("kn-admin", "asg-admin", null);
    const adm = createKnAdminApi(opts("kn-admin", () => adminCtx, true));
    const list = await adm.users();
    expect(list.items.map((u) => u.email)).toContain("an.nv@angiang.gov.vn");
    const body = {
      full_name: "Võ Thị Hạnh",
      email: "hanh.vt@angiang.gov.vn",
      phone: "0912 345 699",
      assignments: [
        { role: "kn_xa" as const, org_ref: AG_REF, department_codes: ["trong_trot"], mgmt_level: "chuyen_vien", commune_codes: ["91307"] },
        { role: "kn_tinh" as const, org_ref: AG_REF, department_codes: ["trong_trot"], mgmt_level: "chuyen_vien" },
      ],
    };
    const created = await adm.createKnUser(body);
    expect(created.assignments).toHaveLength(2);
    expect(await code(adm.createKnUser({ ...body, email: "khac@angiang.gov.vn" }))).toBe("409 accounts.duplicate_identity");
    expect(await code(adm.createKnUser({ ...body, phone: undefined, email: "x@angiang.gov.vn", assignments: [{ role: "kn_xa", org_ref: "00000000-0000-7000-8000-000000000000", department_codes: ["trong_trot"] }] }))).toBe(
      "409 accounts.prerequisite_missing",
    );
    const noCommune = await adm.createKnUser({ full_name: "Cán bộ mới", email: "moi@angiang.gov.vn", assignments: [{ role: "kn_xa", org_ref: AG_REF, department_codes: ["trong_trot"] }] });
    expect(noCommune.warnings.map((w) => w.code)).toEqual(["commune_unassigned"]);
    const log = await adm.log(created.user.id);
    expect(log.items.map((l) => l.event)).toEqual(["account.activation_sent", "account.created"]);
    // Đăng nhập bằng SĐT, có 2 vai trò → màn chọn vai trò.
    const login = mockKnLogin("0912345699", "KhuyenNong@2026");
    expect(login.ok).toBe(true);
    const pid = login.ok ? login.personaId : "";
    const ctxs = await mockFetchNoDelay(new Request("http://mock/v1/me/contexts", { headers: { Authorization: `Bearer mock.${pid}` } }));
    expect(((await ctxs.json()) as { assignments: { role: string }[] }).assignments.map((a) => a.role)).toEqual(["kn_xa", "kn_tinh"]);
    // Cán bộ mới phụ trách Châu Phong: có công việc khi Tỉnh phát hành.
    const lock = await code(adm.lock(created.user.id, "Nghỉ phép dài hạn, khóa tạm thời"));
    expect(lock).toBe("ok");
    expect((await adm.log(created.user.id)).items[0]).toMatchObject({ event: "account.locked", reason: "Nghỉ phép dài hạn, khóa tạm thời" });
  });

  it("FN-AD-04.07: Menu các cấp — lý do bắt buộc, nhật ký hỗ trợ, on_behalf_of, không tự rà soát bản mình nhập thay", async () => {
    const adminCtx = await contextOf("kn-admin", "asg-admin", null);
    const adm = createKnAdminApi(opts("kn-admin", () => adminCtx, true));
    const users = (await adm.users()).items;
    const an = users.find((u) => u.email === "an.nv@angiang.gov.vn")!;
    const asg = (await adm.assignments(an.id)).items.find((a) => a.role === "kn_xa")!;
    expect(await code(adm.startSupport({ target_assignment_id: asg.assignment_id, department_code: "trong_trot", reason: "ngắn" }))).toBe("422 validation_failed");
    const sc = await adm.startSupport({ target_assignment_id: asg.assignment_id, department_code: "trong_trot", reason: "Cán bộ xã mất mạng, nhập thay T01" });
    expect(sc).toMatchObject({ role: "kn_xa", on_behalf_of: an.id });
    expect(sc.support_session_id).toBeTruthy();
    const asXa = createCycleApi(opts("kn-admin", () => sc.context_id));
    const task = (await asXa.tasks({ status: "not_started", form_code: "T04" })).items[0]!;
    expect(await code(asXa.submit(task.id, { kind: "submitted", rows: [{ input: { K: "10" } }] }))).toBe("422 accounts.support_reason_required");
    supportReasonStore.set("Cán bộ xã mất mạng, nhập thay T01");
    const r = await asXa.submit(task.id, { kind: "submitted", rows: [{ input: { K: "10" } }] });
    expect(r.submission.on_behalf_of).toBe(an.id);
    expect(r.submission.support_reason).toBe("Cán bộ xã mất mạng, nhập thay T01");
    const log = (await adm.supportLog()).items;
    expect(log.map((l) => [l.phase, l.function])).toEqual([
      ["completed", "POST /v1/extension/report-tasks/:id/submissions"],
      ["attempted", "POST /v1/extension/report-tasks/:id/submissions"],
    ]);
    expect(log[0]).toMatchObject({ actor_display: "admin.mekong", acted_role: "kn_xa", reason: "Cán bộ xã mất mạng, nhập thay T01", status_code: 201 });
    // Cùng Admin vào vai Cấp Tỉnh: không được yêu cầu điều chỉnh/xác nhận bản mình nhập thay (R-56).
    const tinhSc = await adm.startSupport({ role: "kn_tinh", org_ref: AG_REF, department_code: "trong_trot", reason: "Hỗ trợ rà soát cho Cấp Tỉnh" });
    const asTinh = createCycleApi(opts("kn-admin", () => tinhSc.context_id));
    expect(await code(asTinh.confirm(task.id))).toBe("403 self_approval_forbidden");
    // Thoát hỗ trợ: ngữ cảnh hết hiệu lực.
    await adm.endSupport(sc.support_session_id!);
    expect(await code(asXa.tasks())).toBe("403 accounts.context_invalid");
  });

  it("FN-AD-04.03: đổi trạng thái phòng ban có Log; ngưng cần lý do; phòng ban mở hiện ngay ở bước 2 đăng nhập", async () => {
    const adminCtx = await contextOf("kn-admin", "asg-admin", null);
    const adm = createKnAdminApi(opts("kn-admin", () => adminCtx, true));
    const depts = (await adm.departments()).items;
    expect(depts).toHaveLength(6);
    const cn = depts.find((d) => d.code === "chan_nuoi_thuy_san")!;
    expect(await code(adm.departmentStatus(cn.id, "suspended"))).toBe("422 validation_failed");
    const r = await adm.departmentStatus(cn.id, "active");
    expect(r.department.status_label).toBe("Đang áp dụng");
    expect((await adm.departmentLog(cn.id)).items[0]).toMatchObject({ event: "department.status_changed", changes: [{ field: "status", old: "Chưa mở", new: "Đang áp dụng" }] });
    // Lộc (Cấp Xã · Chăn nuôi) chọn được ngữ cảnh ngay.
    expect(await contextOf("kn-loc", "asg-loc-xa", "chan_nuoi_thuy_san")).toMatch(/^[0-9a-f-]{36}$/);
    const tree = await adm.orgTree();
    expect(tree.items[0]!.children[0]!.children.map((g) => g.name)).toEqual(["Khu vực Long Xuyên", "Khu vực Châu Đốc"]);
  });
});
