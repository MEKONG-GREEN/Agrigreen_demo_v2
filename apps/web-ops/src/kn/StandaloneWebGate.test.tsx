import { render, screen } from "@testing-library/react";
import { useEffect } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { knMenuFor } from "@/api/mock/kn";
import type * as ConfigModule from "@/config";
import type { AuthContextValue, KnSessionState } from "@/auth/AuthProvider";
import { buildPrincipal } from "@/auth/principal";
import type { SessionContext } from "@/kn/contract";
import { findNavItem } from "@/nav/nav";
import { activeMenu, AppShell, isAllowed, isBareRoute } from "@/components/AppShell";

const state = vi.hoisted(() => ({ path: "/", replace: vi.fn(), push: vi.fn(), auth: {} as Partial<AuthContextValue> }));
vi.mock("next/navigation", () => ({ usePathname: () => state.path, useRouter: () => ({ replace: state.replace, push: state.push }) }));
vi.mock("@/auth/AuthProvider", () => ({ useAuth: () => state.auth }));
vi.mock("@/components/NotificationBell", () => ({ NotificationBell: () => null }));
vi.mock("@/config", async (importOriginal) => {
  const original = await importOriginal<typeof ConfigModule>();
  // An obsolete legacy setting must never grant an active KN session another surface.
  return { ...original, config: { ...original.config, knLegacyMenu: true } };
});

const menu = knMenuFor("kn_tinh", "trong_trot");
const principal = buildPrincipal({ sub: "multi-role", name: "Cán bộ nhiều vai", realm_access: { roles: ["platform_admin", "kn_user", "kn_province_admin", "coop_manager"] } }, null);
const context: SessionContext = {
  context_id: "kn-session", assignment_id: "kn-assignment", role: "kn_tinh", department_code: "trong_trot",
  department_name: "Trồng trọt", org_ref: "province", org_name: "Tỉnh", communes: [],
  default_route: "/kn/trang-chu", expires_at: new Date(Date.now() + 3_600_000).toISOString(),
};
const active: KnSessionState = { status: "active", assignments: [], context, menu, badges: null };
const legacy: KnSessionState = { status: "legacy", assignments: [], context: null, menu: null, badges: null };
const legacyPages = ["/", "/ban-do", "/ban-do-gis", "/nhiem-vu", "/chi-dao", "/danh-ba", "/bao-cao", "/htx", "/thu-vien"];

beforeEach(() => {
  state.path = "/";
  state.replace.mockClear();
  state.push.mockClear();
  state.auth = {
    status: "authenticated", principal, kn: active, scopeWarning: null,
    api: { GET: vi.fn().mockResolvedValue({ data: { items: [] } }) } as unknown as AuthContextValue["api"],
    logout: vi.fn(), selectContext: vi.fn(), changePassword: vi.fn(),
  };
});

describe("KN-STANDALONE-2: phiên KN chỉ mở bề mặt KN", () => {
  it.each([...legacyPages, "/htx/dashboard", "/htx/mua-vu", "/quan-tri/tham-so", "/dong-bo", "/mrv", "/unknown", "/kn-evil", "/login-evil", "/chon-vai-tro-evil", "/thong-bao-evil", "/khong-co-quyen-evil"])("active KN từ chối %s kể cả tài khoản nhiều vai", (path) => {
    expect(isAllowed(path, active, menu, principal)).toBe(false);
  });

  it.each(["/login", "/login/callback", "/login/lien-he", "/chon-vai-tro", "/thong-bao", "/khong-co-quyen"])("giữ trang dùng chung %s", (path) => {
    expect(isAllowed(path, active, menu, principal)).toBe(true);
  });

  it("/kn theo menu đang chọn; không tự mở cả namespace", () => {
    expect(isAllowed("/kn/nhiem-vu/phat-hanh", active, menu, principal)).toBe(true);
    expect(isAllowed("/kn/nhiem-vu/cong-viec", active, menu, principal)).toBe(false);
    expect(isAllowed("/kn/unknown", active, menu, principal)).toBe(false);
  });

  it("menu active chỉ dùng registry KN dù có setting legacy cũ", () => {
    expect(activeMenu(active, principal)).toBe(menu);
  });

  it("nav legacy không còn cấp TW/Tỉnh/Xã ở chín trang ngoài BRD", () => {
    for (const path of legacyPages) {
      const item = findNavItem(path)!;
      expect(item.levels).toBeDefined();
      for (const level of ["tw", "tinh", "xa"]) expect(item.levels).not.toContain(level);
    }
  });

  it("phân theo phiên active, không khóa vai HTX/MKG chỉ vì tài khoản có role KN", () => {
    for (const path of legacyPages) expect(isAllowed(path, legacy, activeMenu(legacy, principal), principal)).toBe(true);
  });

  it.each(["/login-evil", "/loginning", "/chon-vai-tro-evil", "/chon-vai-tro/extra"])("%s không được bypass khung bằng prefix", (path) => {
    expect(isBareRoute(path)).toBe(false);
  });

  it.each(["/", "/htx", "/chi-dao", "/login-evil"])("direct load %s từ chối trước khi mount hoặc đọc dữ liệu màn legacy", (path) => {
    state.path = path;
    const readData = vi.fn();
    function LegacyPage() { useEffect(() => { readData(); }, []); return <h1>Màn legacy</h1>; }
    render(<AppShell><LegacyPage /></AppShell>);
    expect(screen.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
    expect(screen.queryByRole("heading", { name: "Màn legacy" })).not.toBeInTheDocument();
    expect(readData).not.toHaveBeenCalled();
    expect(state.replace).not.toHaveBeenCalled();
  });

  it("đổi cùng tài khoản từ phiên MKG sang KN gỡ màn legacy, đổi lại khôi phục đúng quyền", () => {
    state.path = "/chi-dao";
    state.auth.kn = legacy;
    const readData = vi.fn();
    function LegacyPage() { useEffect(() => { readData(); }, []); return <h1>Màn legacy</h1>; }
    const { rerender } = render(<AppShell><LegacyPage /></AppShell>);
    expect(readData).toHaveBeenCalledTimes(1);
    state.auth.kn = active;
    rerender(<AppShell><LegacyPage /></AppShell>);
    expect(screen.getByRole("heading", { name: "Không có quyền truy cập" })).toBeVisible();
    expect(screen.queryByRole("heading", { name: "Màn legacy" })).not.toBeInTheDocument();
    expect(readData).toHaveBeenCalledTimes(1);
    state.auth.kn = legacy;
    rerender(<AppShell><LegacyPage /></AppShell>);
    expect(screen.getByRole("heading", { name: "Màn legacy" })).toBeVisible();
    expect(readData).toHaveBeenCalledTimes(2);
  });
});
