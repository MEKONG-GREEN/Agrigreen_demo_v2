// ADMIN-UNLOCK (#42/#61): quản trị KN giả lập khớp hợp đồng máy chủ — login_locked (+ login_locked_until) chỉ có ở GET
// chi tiết, danh sách bỏ field; khóa lấy từ bộ đếm đăng nhập giả lập (sai 5 lần → khóa tạm 15 phút, P0-VPS-OPS), giữ
// enabled=true; Admin mở khóa → đăng nhập lại được ngay; không còn gì để mở → 409 accounts.state_conflict.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiProblem } from "@/api/problem";
import { createKnAdminApi } from "@/kn/cycle/api";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "./handler";
import { MAX_ATTEMPTS, mockKnLogin, resetMockLoginAttempts } from "./kn";
import { MOCK_KN_PASSWORD } from "./kn-accounts";
import { resetKnAdmin } from "./kn-admin";

const T0 = Date.parse("2026-10-26T03:00:00Z");
const reason = "Cán bộ gọi điện xác minh, quên mật khẩu đã đặt lại";
const code = async (p: Promise<unknown>) => p.then(() => "ok", (e: unknown) => (e instanceof ApiProblem ? `${e.status} ${e.code}` : String(e)));

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(T0);
  resetKnAdmin();
  resetMockLoginAttempts();
});
afterEach(() => {
  vi.useRealTimers();
  resetMockLoginAttempts();
});

describe("Quản trị KN giả lập — khóa do đăng nhập sai", () => {
  async function adminCtx() {
    const r = await mockFetchNoDelay(
      new Request("http://mock/v1/me/context", {
        method: "POST",
        headers: { Authorization: "Bearer mock.kn-admin", "Content-Type": "application/json", "Idempotency-Key": `ctx-${Math.random()}` },
        body: JSON.stringify({ assignment_id: "asg-admin", department_code: null }),
      }),
    );
    return ((await r.json()) as { context_id: string }).context_id;
  }

  it("sai mật khẩu 5 lần → chi tiết login_locked + giờ tự mở; Admin mở khóa → đăng nhập lại được ngay", async () => {
    const ctx = await adminCtx();
    const adm = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => ctx, fetch: mockAdminFetchNoDelay });
    const an = (await adm.users()).items.find((u) => u.email === "an.nv@angiang.gov.vn")!;
    expect(an).not.toHaveProperty("login_locked");
    expect((await adm.user(an.id)).login_locked).toBe(false);
    // Chưa khóa gì → unlock là xung đột trạng thái.
    expect(await code(adm.unlock(an.id, reason))).toBe("409 accounts.state_conflict");

    for (let i = 0; i < MAX_ATTEMPTS; i++) mockKnLogin("an.nv@angiang.gov.vn", "sai-mat-khau");
    expect(mockKnLogin("an.nv@angiang.gov.vn", MOCK_KN_PASSWORD).ok).toBe(false);
    const locked = await adm.user(an.id);
    expect(locked).toMatchObject({ enabled: true, login_locked: true, login_locked_until: new Date(T0 + 15 * 60_000).toISOString() });
    expect((await adm.users()).items.find((u) => u.id === an.id)).not.toHaveProperty("login_locked");

    expect(await code(adm.unlock(an.id, "ngắn"))).toBe("422 validation_failed");
    expect(await code(adm.unlock(an.id, reason))).toBe("ok");
    expect((await adm.user(an.id)).login_locked).toBe(false);
    expect((await adm.log(an.id)).items[0]).toMatchObject({ event: "account.unlocked", reason });
    expect(mockKnLogin("an.nv@angiang.gov.vn", MOCK_KN_PASSWORD).ok).toBe(true);
  });

  it("khóa tạm tự hết sau 15 phút: chi tiết báo false", async () => {
    const ctx = await adminCtx();
    const adm = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => ctx, fetch: mockAdminFetchNoDelay });
    const an = (await adm.users()).items.find((u) => u.email === "an.nv@angiang.gov.vn")!;
    for (let i = 0; i < MAX_ATTEMPTS; i++) mockKnLogin("0912345601", "sai-mat-khau");
    expect((await adm.user(an.id)).login_locked).toBe(true);
    vi.setSystemTime(T0 + 15 * 60_000 + 1);
    const d = await adm.user(an.id);
    expect(d.login_locked).toBe(false);
    expect(d).not.toHaveProperty("login_locked_until");
  });
});
