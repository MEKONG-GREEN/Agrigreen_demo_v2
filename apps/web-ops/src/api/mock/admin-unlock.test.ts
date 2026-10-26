// ADMIN-UNLOCK (#42/#61): backend giả lập pf/accounts (màn /quan-tri/tai-khoan) khớp hợp đồng máy chủ — login_locked (+ login_locked_until) chỉ có ở GET chi
// tiết, danh sách bỏ field; khóa đăng nhập sai giữ enabled=true; unlock xóa khóa, khóa tạm 15 phút tự hết (P0-VPS-OPS);
// không còn gì để mở → 409 accounts.state_conflict như máy chủ.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiProblem } from "@/api/problem";
import { createAdminApiClient, unwrap } from "../client";
import { newIdempotencyKey } from "../idempotency";
import { ACCOUNTS, lockAccountByBruteForce, resetAccountLoginLocks } from "./admin-ops";
import { mockAdminFetchNoDelay } from "./handler";
import { mockToken } from "./personas";

const T0 = Date.parse("2026-10-26T03:00:00Z");
const admin = (persona: string) => createAdminApiClient({ baseUrl: "https://admin.test", getAccessToken: () => mockToken(persona), fetch: mockAdminFetchNoDelay });
const key = () => ({ "Idempotency-Key": newIdempotencyKey() });
const reason = "Cán bộ gọi điện xác minh, quên mật khẩu đã đặt lại";
const code = async (p: Promise<unknown>) => p.then(() => "ok", (e: unknown) => (e instanceof ApiProblem ? `${e.status} ${e.code}` : String(e)));

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(T0);
  resetAccountLoginLocks();
});
afterEach(() => {
  vi.useRealTimers();
  resetAccountLoginLocks();
});

describe("pf/accounts giả lập (quan-tri/tai-khoan)", () => {
  it("chi tiết có login_locked; danh sách bỏ field; khóa sai mật khẩu giữ enabled, unlock xóa khóa và ghi nhật ký", async () => {
    const api = admin("setter");
    const target = ACCOUNTS.find((a) => a.user.enabled)!.user;
    const path = { params: { path: { id: target.id } } };
    const before = await unwrap(api.GET("/v1/accounts/users/{id}", path));
    expect(before.login_locked).toBe(false);
    expect(before).not.toHaveProperty("login_locked_until");

    lockAccountByBruteForce(target.id);
    const locked = await unwrap(api.GET("/v1/accounts/users/{id}", path));
    expect(locked).toMatchObject({ enabled: true, login_locked: true, login_locked_until: new Date(T0 + 15 * 60_000).toISOString() });
    const list = await unwrap(api.GET("/v1/accounts/users", { params: { query: {} } }));
    expect(list.items.every((u) => !("login_locked" in u) && !("login_locked_until" in u))).toBe(true);

    const ok = await unwrap(api.POST("/v1/accounts/users/{id}/unlock", { params: { path: { id: target.id }, header: key() }, body: { reason } }));
    expect(ok.account.enabled).toBe(true);
    const after = await unwrap(api.GET("/v1/accounts/users/{id}", path));
    expect(after.login_locked).toBe(false);
    const events = await unwrap(api.GET("/v1/accounts/users/{id}/events", path));
    expect(events.items.at(-1)).toMatchObject({ action: "unlocked", details: { prev_cause: "brute_force" } });
    // Không còn gì để mở → 409 như máy chủ (không ghi sự kiện rỗng).
    expect(await code(unwrap(api.POST("/v1/accounts/users/{id}/unlock", { params: { path: { id: target.id }, header: key() }, body: { reason } })))).toBe(
      "409 accounts.state_conflict",
    );
  });

  it("khóa tạm 15 phút tự hết: sau mốc chi tiết báo false, unlock → 409", async () => {
    const api = admin("setter");
    const target = ACCOUNTS.find((a) => a.user.enabled)!.user;
    lockAccountByBruteForce(target.id);
    vi.setSystemTime(T0 + 15 * 60_000 + 1);
    const d = await unwrap(api.GET("/v1/accounts/users/{id}", { params: { path: { id: target.id } } }));
    expect(d.login_locked).toBe(false);
    expect(d).not.toHaveProperty("login_locked_until");
    expect(await code(unwrap(api.POST("/v1/accounts/users/{id}/unlock", { params: { path: { id: target.id }, header: key() }, body: { reason } })))).toBe(
      "409 accounts.state_conflict",
    );
  });
});
