import { beforeEach, describe, expect, it } from "vitest";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { MAX_ATTEMPTS, mockKnLogin, resetMockLoginAttempts } from "@/api/mock/kn";
import { KN_ACCOUNTS, MOCK_KN_PASSWORD } from "@/api/mock/kn-accounts";
import { mockToken } from "@/api/mock/personas";
import { createApiClient, unwrap } from "@/api/client";
import { createKnApi } from "./client";
import type { components } from "@/api/generated/application";
import { CONTEXT_HEADER, type RoleAssignment, type SessionContext } from "./contract";
import { autoSelection, contextLabel, contextShortLabel, enterableOptions, initials, isValidSelection, otherOptions } from "./session";

const acc = (id: string) => KN_ACCOUNTS.find((a) => a.id === id)!;

describe("quy tắc chọn vai trò & phòng ban (FN-CH-00.01 B3)", () => {
  it("1 vai trò + 1 phòng ban đang áp dụng → vào thẳng; nhiều → màn chọn", () => {
    expect(autoSelection(acc("kn-quan").assignments)).toEqual({ assignment_id: "asg-quan-tw", department_code: "trong_trot" });
    expect(autoSelection(acc("kn-admin").assignments)).toEqual({ assignment_id: "asg-admin", department_code: null });
    expect(autoSelection(acc("kn-an").assignments)).toBeNull();
    expect(autoSelection(acc("kn-binh").assignments)).toBeNull();
    expect(enterableOptions(acc("kn-loc").assignments)).toEqual([]);
  });

  it("phòng ban chưa mở không vào được; lựa chọn đã lưu kiểm lại", () => {
    const an = acc("kn-an").assignments;
    expect(enterableOptions(an)).toHaveLength(3); // Xã·Trồng trọt, Tỉnh·Trồng trọt, Admin
    expect(isValidSelection(an, { assignment_id: "asg-an-xa", department_code: "chan_nuoi_thuy_san" })).toBe(false);
    expect(isValidSelection(an, { assignment_id: "asg-an-xa", department_code: "trong_trot" })).toBe(true);
  });

  it("nhãn ngữ cảnh và các cặp khác cho menu tài khoản", () => {
    const ctx = { assignment_id: "asg-binh-tinh", department_code: "trong_trot", role: "kn_tinh" as const, department_name: "Phòng Trồng trọt" };
    expect(contextLabel(ctx)).toBe("Cấp Tỉnh · Phòng Trồng trọt");
    expect(contextShortLabel(ctx)).toBe("Cấp Tỉnh · Trồng trọt");
    expect(contextLabel({ role: "admin", department_name: null })).toBe("Admin · Tất cả phòng ban");
    expect(otherOptions(acc("kn-binh").assignments, ctx).map((o) => o.assignment.assignment_id)).toEqual(["asg-binh-xa"]);
    expect(initials("Nguyễn Văn An")).toBe("NA");
    expect(initials("admin.mekong")).toBe("AD");
  });
});

describe("đăng nhập mật khẩu giả lập", () => {
  beforeEach(() => resetMockLoginAttempts());

  it("email hoặc SĐT; sai → còn n lần; sai 5 lần → khóa tạm", () => {
    expect(mockKnLogin("0912345603", MOCK_KN_PASSWORD)).toEqual({ ok: true, personaId: "kn-quan" });
    expect(mockKnLogin("AN.NV@angiang.gov.vn", MOCK_KN_PASSWORD)).toEqual({ ok: true, personaId: "kn-an" });
    for (let i = 1; i < MAX_ATTEMPTS; i++) expect(mockKnLogin("an.nv@angiang.gov.vn", "sai")).toEqual({ ok: false, error: "invalid_credentials", remaining: MAX_ATTEMPTS - i });
    const locked = mockKnLogin("an.nv@angiang.gov.vn", "sai");
    expect(locked.ok).toBe(false);
    expect(!locked.ok && locked.error).toBe("locked");
    // Đang khóa: đúng mật khẩu vẫn bị từ chối.
    expect(mockKnLogin("an.nv@angiang.gov.vn", MOCK_KN_PASSWORD).ok).toBe(false);
    // Tài khoản không tồn tại: cùng thông báo (không lộ tài khoản có thật).
    expect(mockKnLogin("khong.co@x.vn", "x")).toEqual({ ok: false, error: "invalid_credentials", remaining: MAX_ATTEMPTS - 1 });
  });
});

describe("client P1/P2 (GET /v1/menu theo ngữ cảnh) + header X-AG-Context", () => {
  it("chọn ngữ cảnh → menu theo vai trò; mọi lời gọi sau mang X-AG-Context; /v1/me theo ngữ cảnh", async () => {
    let ctxId: string | null = null;
    const seen: (string | null)[] = [];
    const spy = (r: Request) => {
      seen.push(r.headers.get(CONTEXT_HEADER));
      return mockFetchNoDelay(r);
    };
    const token = mockToken("kn-an");
    const kn = createKnApi({ baseUrl: "http://x", getAccessToken: () => token, getContextId: () => ctxId, fetch: spy });
    const api = createApiClient({ baseUrl: "http://x", getAccessToken: () => token, getContextId: () => ctxId, fetch: spy });

    const { assignments } = await kn.listContexts();
    expect(assignments.map((a) => a.role)).toEqual(["kn_xa", "kn_tinh", "admin"]);
    expect((await unwrap(api.GET("/v1/me"))).roles).toEqual(["kn_user"]);

    const ctx = await kn.selectContext({ assignment_id: "asg-an-xa", department_code: "trong_trot" });
    expect(ctx.default_route).toBe("/kn/nhiem-vu/cong-viec");
    ctxId = ctx.context_id;
    const menu = await kn.menu();
    expect(menu.groups.map((g) => g.label)).toEqual(["Điều hành", "Báo cáo", "Dữ liệu"]);
    // Số đếm tính từ sổ giả lập vòng đời báo cáo (P4): công việc còn phải gửi của 2 xã phụ trách.
    expect((await kn.badges()).counts["extension.report_tasks.todo"]).toBeGreaterThan(0);
    const me = await unwrap(api.GET("/v1/me"));
    expect(me.roles).toContain("kn_officer");
    expect(me.scope.units).toEqual(["91301", "91302"]);
    expect(seen.at(-1)).toBe(ctx.context_id);
  });

  it("phòng ban chưa mở → 409; vai thử cũ → danh sách rỗng (giữ menu cũ)", async () => {
    const kn = createKnApi({ baseUrl: "http://x", getAccessToken: () => mockToken("kn-an"), getContextId: () => null, fetch: mockFetchNoDelay });
    await expect(kn.selectContext({ assignment_id: "asg-an-xa", department_code: "chan_nuoi_thuy_san" })).rejects.toMatchObject({ status: 409 });
    const legacy = createKnApi({ baseUrl: "http://x", getAccessToken: () => mockToken("tinh"), getContextId: () => null, fetch: mockFetchNoDelay });
    expect((await legacy.listContexts()).assignments).toEqual([]);
  });

  it("header X-AG-Context lạ hoặc của người khác → 403 accounts.context_invalid (như middleware P1)", async () => {
    const an = createKnApi({ baseUrl: "http://x", getAccessToken: () => mockToken("kn-an"), getContextId: () => null, fetch: mockFetchNoDelay });
    const ctx = await an.selectContext({ assignment_id: "asg-an-xa", department_code: "trong_trot" });
    expect(ctx.support_session_id).toBeNull();
    const binh = createKnApi({ baseUrl: "http://x", getAccessToken: () => mockToken("kn-binh"), getContextId: () => ctx.context_id, fetch: mockFetchNoDelay });
    await expect(binh.listContexts()).rejects.toMatchObject({ status: 403 });
    const junk = createKnApi({ baseUrl: "http://x", getAccessToken: () => mockToken("kn-an"), getContextId: () => "khong-phai-uuid", fetch: mockFetchNoDelay });
    await expect(junk.listContexts()).rejects.toMatchObject({ status: 403 });
  });

  it("kiểu sinh từ OpenAPI của P1 gán được vào hợp đồng web (KNAssignment, KNSessionContext)", () => {
    // Kiểm ở thời điểm biên dịch (typecheck): server trả thừa trường được, không thiếu trường web dùng.
    const asg = (x: components["schemas"]["KNAssignment"]): RoleAssignment => x;
    const ctx = (x: components["schemas"]["KNSessionContext"]): SessionContext => x;
    expect([typeof asg, typeof ctx]).toEqual(["function", "function"]);
  });

  it("backend thật chưa có P1 → 404 (AuthProvider giữ đăng nhập một vai)", async () => {
    const kn = createKnApi({
      baseUrl: "http://x",
      getAccessToken: () => "t",
      getContextId: () => null,
      fetch: async () => new Response(JSON.stringify({ type: "about:blank", title: "Not found", status: 404, code: "not_found" }), { status: 404, headers: { "Content-Type": "application/problem+json" } }),
    });
    await expect(kn.listContexts()).rejects.toMatchObject({ status: 404 });
  });
});
