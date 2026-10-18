// W11.02 — đóng khoảng trống Trồng trọt (giả lập + client web): nạp Tỉnh giao từ tệp (UR-TI-02), di chuyển Nút xã
// (FN-AD-04.02 B3), nạp Excel tài khoản nhiều vai trò + tab phiên/lịch sử đăng nhập (FN-AD-04.04), cảnh báo PII ô tự do (X-02).
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { ApiProblem } from "@/api/problem";
import { RawFile, createCycleApi, createKnAdminApi } from "@/kn/cycle/api";
import { detectPii } from "@/kn/pii";
import { provinceOrgRef } from "./data";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "./handler";
import { resetMockForms } from "./forms/routes";
import { KN_ACCOUNTS, resetKnAccounts } from "./kn-accounts";
import { resetKnAdmin } from "./kn-admin";
import { resetReportCycle } from "./kn-reports/store";

const ORIGINAL = KN_ACCOUNTS.map((a) => ({ ...a, assignments: a.assignments.map((x) => ({ ...x })) }));

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
const opts = (personaId: string, ctx: string, admin = false) => ({
  baseUrl: "http://mock",
  getAccessToken: () => `mock.${personaId}`,
  getContextId: () => ctx,
  fetch: admin ? mockAdminFetchNoDelay : mockFetchNoDelay,
});
const code = async (p: Promise<unknown>) => p.then(() => "ok", (e: unknown) => (e instanceof ApiProblem ? `${e.status} ${e.code}` : String(e)));

let now = Date.parse("2026-10-02T03:00:00Z");
beforeEach(() => {
  now = Date.parse("2026-10-02T03:00:00Z");
  resetMockForms();
  resetReportCycle(() => now);
  resetKnAdmin();
});
afterEach(() => resetKnAccounts(ORIGINAL));

describe("W11.02", () => {
  it("UR-TI-02: nạp Tỉnh giao từ CSV → giá trị theo mẫu/xã, dòng lỗi có lý do; phát hành với provincial_source excel", async () => {
    const tinh = createCycleApi(opts("kn-binh", await contextOf("kn-binh", "asg-binh-tinh")));
    const t01 = (await tinh.reportForms("weekly")).items.find((f) => f.code === "T01")!;
    const csv = "Mã xã,Tên xã,T01 D (ha),Ghi chú\n91301,Xã 1,126,\n91302,Xã 2,abc,\n,thiếu,1,\n";
    const r = await tinh.provincialImport({ report_type: "weekly", from_date: "2026-09-28", template_ids: [t01.template_id] }, new RawFile(csv, "text/csv; charset=utf-8"));
    expect(r.provincial_values[t01.template_id]).toEqual({ "91301": { D: "126" } });
    expect([r.accepted, r.rejected, r.ignored_columns]).toEqual([1, 2, ["Ghi chú"]]);
    expect(r.rows.filter((x) => x.status === "rejected").map((x) => x.reason)).toEqual(["T01 cột D: phải là số không âm", "Thiếu mã xã"]);
    expect(await code(tinh.provincialImport({ report_type: "weekly", template_ids: [t01.template_id] }, new RawFile("Tên,Khác\n1,2\n", "text/csv")))).toBe("422 extension.provincial_import_invalid");
    const issued = await tinh.issue({
      report_type: "weekly",
      template_ids: [t01.template_id],
      scope: { mode: "communes", communes: ["91301", "91302"] },
      from_date: "2026-09-28",
      to_date: "2026-10-04",
      season_label: "Đợt nạp Tỉnh giao",
      provincial_values: r.provincial_values,
      provincial_source: "excel",
    });
    expect(issued.batch.task_count).toBeGreaterThan(0);
  });

  it("FN-AD-04.02 B3: di chuyển Nút xã sang tỉnh khác (lý do ≥ 10 ký tự); Nút cấp 1/2 không di chuyển", async () => {
    const adm = createKnAdminApi(opts("kn-admin", await contextOf("kn-admin", "asg-admin", null), true));
    const tree = await adm.orgTree();
    const ag = tree.items[0]!.children[0]!;
    const other = tree.items[0]!.children[1]!;
    const xa = ag.children[0]!.children[0]!;
    expect(await code(adm.moveOrg(xa.ref!, other.ref!, "ngắn"))).toBe("422 validation_failed");
    expect(await code(adm.moveOrg(ag.ref!, other.ref!, "Sáp nhập đơn vị hành chính 2025"))).toBe("422 party.invalid_hierarchy");
    await adm.moveOrg(xa.ref!, other.ref!, "Sáp nhập đơn vị hành chính 2025");
    const after = await adm.orgTree();
    expect(after.items[0]!.children[1]!.children.map((n) => n.ref)).toEqual([xa.ref]);
    expect(await code(adm.moveOrg(xa.ref!, other.ref!, "Sáp nhập đơn vị hành chính 2025"))).toBe("409 party.org_state_conflict");
    expect(provinceOrgRef(2)).toBe(other.ref);
  });

  it("FN-AD-04.04: nạp Excel tài khoản nhiều vai (cùng Email = một tài khoản), tab phiên/lịch sử đăng nhập", async () => {
    const adm = createKnAdminApi(opts("kn-admin", await contextOf("kn-admin", "asg-admin", null), true));
    const csv = [
      "Họ tên,Email,SĐT,Vai trò,Mã Nút,Phòng ban,Cấp quản lý,Xã phụ trách",
      "Đỗ Văn Nam,nam.dv@angiang.gov.vn,0912345777,Cấp Xã,TTKN-AG,trong_trot,Chuyên viên,91307",
      "Đỗ Văn Nam,nam.dv@angiang.gov.vn,,Cấp Tỉnh,TTKN-AG,trong_trot,,",
      "Lê Thị Oanh,oanh.lt@angiang.gov.vn,,Thủ quỹ,TTKN-AG,trong_trot,,",
    ].join("\n");
    const r = await adm.importKnUsers(new RawFile(csv, "text/csv; charset=utf-8"));
    expect([r.accounts_created, r.created, r.rejected]).toEqual([1, 2, 1]);
    expect(r.rows[2]).toMatchObject({ row: 4, status: "rejected", reason: "Vai trò không hợp lệ (Cấp Quốc gia, Cấp Tỉnh, Cấp Xã)" });
    const id = r.rows[0]!.user_id!;
    expect(r.rows[1]!.user_id).toBe(id);
    expect((await adm.assignments(id)).items.map((a) => a.role).sort()).toEqual(["kn_tinh", "kn_xa"]);
    const s = await adm.sessions(id);
    expect(s.available).toBe(true);
    expect(s.history.map((h) => h.type)).toEqual(["LOGIN", "LOGIN_ERROR"]);
  });

  it("X-02: nhận ra SĐT, email, CCCD trong ô tự do (không trả giá trị)", () => {
    expect(detectPii("Liên hệ anh Ba 0912 345 678")).toEqual(["phone"]);
    expect(detectPii("gửi ba@vi-du.vn")).toEqual(["email"]);
    expect(detectPii("CCCD 087123456789")).toEqual(["national_id"]);
    expect(detectPii("+84912345678")).toEqual(["phone"]);
    expect(detectPii("Diện tích 126,5 ha, 3 HTX, năm 2026")).toEqual([]);
    expect(detectPii("")).toEqual([]);
  });
});
