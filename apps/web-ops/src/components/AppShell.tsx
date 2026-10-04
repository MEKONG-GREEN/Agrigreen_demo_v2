"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { unwrap, type ApiClient } from "@/api/client";
import { useAuth, type KnSessionState } from "@/auth/AuthProvider";
import { LEVEL_LABEL, roleLabel, type Principal } from "@/auth/principal";
import { config } from "@/config";
import type { MenuTree } from "@/kn/contract";
import { contextLabel, contextShortLabel, optionLabel, otherOptions } from "@/kn/session";
import { canSee, disabledModules, findNavItem, homeFor, visibleNav, type DisabledModules } from "@/nav/nav";
import { AccountMenu } from "@/shell/AccountMenu";
import { Breadcrumb, BreadcrumbTailProvider, useBreadcrumbTailValue, type Crumb } from "@/shell/Breadcrumb";
import { crumbsFor, legacyMenu, menuAllows, withExtraGroups } from "@/shell/menu";
import { Sidebar } from "@/shell/Sidebar";
import { useStoredValue, writeStoredValue } from "@/lib/useStoredValue";
import { Icon } from "./Icon";
import { NotificationBell } from "./NotificationBell";
import { Loading } from "./ui";
import { SupportBanner } from "@/kn/SupportBanner";

/** Trang không có khung (đăng nhập, chọn vai trò & phòng ban). */
export function isBareRoute(pathname: string): boolean {
  return pathname.startsWith("/login") || pathname.startsWith("/chon-vai-tro");
}

/** Menu đang dùng: menu cấu hình KN (P2) khi có ngữ cảnh, ngược lại menu cũ theo vai realm (nav.ts). */
export function activeMenu(kn: KnSessionState, principal: Principal | null, disabled: DisabledModules = NO_MODULES): MenuTree {
  const legacy = legacyMenu(visibleNav(principal, disabled));
  if (kn.status === "active" && kn.menu) {
    if (!config.knLegacyMenu) return kn.menu;
    const extra = legacy.groups.map((g) => ({ ...g, key: `extra-${g.key}`, label: `Chức năng khác · ${g.label.replace(/^\d+\.\s*/, "")}` }));
    return withExtraGroups(kn.menu, extra);
  }
  return legacy;
}

/**
 * Hiện màn hình được không (chỉ ẩn/hiện — backend vẫn kiểm từng lời gọi, R-55).
 * Bề mặt KN: đường /kn/* phải nằm trong menu của ngữ cảnh; màn hiện có ngoài /kn vẫn theo vai (canSee).
 */
export function isAllowed(pathname: string, kn: KnSessionState, menu: MenuTree, principal: Principal | null, disabled: DisabledModules = NO_MODULES): boolean {
  if (kn.status === "active" && pathname.startsWith("/kn")) return menuAllows(menu, pathname);
  const item = findNavItem(pathname);
  return !item || canSee(item, principal, disabled);
}

function legacyScopeText(p: Principal): string {
  if (p.level) return `${LEVEL_LABEL[p.level]}${p.unitName ? ` · ${p.unitName}` : ""}`;
  return p.roles[0] ? roleLabel(p.roles[0]) : "Chưa xác định phạm vi";
}

function useMediaQuery(query: string): boolean {
  const [match, setMatch] = useState(false);
  useEffect(() => {
    const mq = window.matchMedia?.(query);
    if (!mq) return;
    const on = () => setMatch(mq.matches);
    on();
    mq.addEventListener("change", on);
    return () => mq.removeEventListener("change", on);
  }, [query]);
  return match;
}

const COLLAPSE_KEY = "ag.shell.collapsed";

const NO_MODULES: DisabledModules = new Set<string>();

/**
 * W1.4 (R-MOD-08): module Ä‘Ã£ táº¯t cho Ä‘Æ¡n vá»‹ (GET /v1/me/modules) Ä‘á»ƒ áº©n mÃ n cá»§a chÃºng á»Ÿ menu cÅ© (nav.ts).
 * Menu KN (GET /v1/menu) Ä‘Ã£ lá»c á»Ÿ server. KhÃ´ng Ä‘á»c Ä‘Æ°á»£c â†’ khÃ´ng lá»c (backend váº«n tráº£ 403 module_not_enabled).
 */
function useDisabledModules(api: ApiClient, enabled: boolean): DisabledModules {
  const [disabled, setDisabled] = useState<DisabledModules>(NO_MODULES);
  useEffect(() => {
    if (!enabled) return;
    let alive = true;
    unwrap(api.GET("/v1/me/modules"))
      .then((r) => {
        if (alive) setDisabled(disabledModules(r.items));
      })
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, [api, enabled]);
  return disabled;
}

function TopCrumbs({ base }: { base: Crumb[] }) {
  const tail = useBreadcrumbTailValue();
  return <Breadcrumb items={[...base, ...tail]} />;
}

export function AppShell({ children }: { children: ReactNode }) {
  const { status, principal, logout, scopeWarning, kn, selectContext, changePassword, api } = useAuth();
  const pathname = usePathname() ?? "/";
  const router = useRouter();
  const [navOpen, setNavOpen] = useState(false);
  const prefCollapsed = useStoredValue(COLLAPSE_KEY) === "1";
  // Thông báo trong menu tài khoản gắn với trang hiện tại (đổi trang → ẩn).
  const [noteState, setNoteState] = useState<{ path: string; text: string } | null>(null);
  // 960–1279 px (máy tính bảng ngang): tự thu gọn còn icon; < 960 px: ngăn kéo.
  const tablet = useMediaQuery("(min-width: 960px) and (max-width: 1279px)");
  const bare = isBareRoute(pathname);

  const toggleCollapse = useCallback(() => writeStoredValue(COLLAPSE_KEY, prefCollapsed ? "0" : "1"), [prefCollapsed]);

  const disabled = useDisabledModules(api, status === "authenticated");
  const menu = useMemo(() => activeMenu(kn, principal, disabled), [kn, principal, disabled]);
  const allowed = isAllowed(pathname, kn, menu, principal, disabled);

  useEffect(() => {
    if (bare) return;
    if (status === "anonymous") {
      router.replace(`/login?returnTo=${encodeURIComponent(pathname)}`);
    } else if (status === "authenticated") {
      if (kn.status === "choose") router.replace(`/chon-vai-tro?returnTo=${encodeURIComponent(pathname)}`);
      else if (kn.status === "active" && kn.context && pathname === "/") router.replace(kn.context.default_route);
      // Cục KTHT vào "/" → cổng CGH (BRD CGH FN-11 B1).
      else if (kn.status === "legacy" && pathname === "/" && !allowed) router.replace(homeFor(principal, disabled));
    }
  }, [status, bare, pathname, router, allowed, principal, kn.status, kn.context, disabled]);

  const note = noteState?.path === pathname ? noteState.text : null;

  if (bare) return <>{children}</>;
  if (status !== "authenticated" || !principal || kn.status === "choose") {
    return (
      <main style={{ display: "grid", placeItems: "center", minHeight: "100vh" }}>
        <Loading label="Đang kiểm tra phiên đăng nhập…" />
      </main>
    );
  }

  const ctx = kn.status === "active" ? kn.context : null;
  const others = ctx ? otherOptions(kn.assignments, ctx) : [];
  const crumbs = crumbsFor(menu, pathname);
  // Nhãn nhóm menu cũ có số thứ tự ("1. Điều hành") — breadcrumb bỏ số.
  const baseCrumbs: Crumb[] = crumbs.length ? crumbs.map((label) => ({ label: label.replace(/^\d+\.\s*/, "") })) : [{ label: findNavItem(pathname)?.label ?? "Trang" }];
  const home = ctx?.default_route ?? homeFor(principal, disabled);
  const collapsed = !navOpen && (tablet || prefCollapsed);

  return (
    <BreadcrumbTailProvider>
      <div className={`shell${navOpen ? " nav-open" : ""}${collapsed ? " is-collapsed" : ""}`}>
        <a href="#main" className="skip-link">
          Bỏ qua điều hướng
        </a>
        <Sidebar
          menu={menu}
          pathname={pathname}
          badges={kn.badges}
          collapsed={collapsed}
          onToggleCollapse={tablet ? undefined : toggleCollapse}
          onNavigate={() => setNavOpen(false)}
        />
        <div className="main">
          <header className="top">
            <button
              type="button"
              className="icb"
              aria-label={navOpen ? "Đóng menu" : "Mở menu"}
              aria-expanded={navOpen}
              aria-controls="side-nav"
              onClick={() => setNavOpen((v) => !v)}
            >
              <Icon name="menu" />
            </button>
            <TopCrumbs base={baseCrumbs} />
            <div className="spacer" />
            {config.mock ? <span className="mock-badge">Dữ liệu giả lập</span> : null}
            <NotificationBell />
            <AccountMenu
              displayName={principal.displayName}
              subtitle={ctx ? contextShortLabel(ctx) : legacyScopeText(principal)}
              current={ctx ? contextLabel(ctx) : legacyScopeText(principal)}
              others={others.map((o) => ({ key: `${o.assignment.assignment_id}|${o.department?.code ?? ""}`, label: optionLabel(o) }))}
              onSwitch={async (key) => {
                const [assignment_id = "", dept = ""] = key.split("|");
                const next = await selectContext({ assignment_id, department_code: dept || null });
                router.push(next.default_route);
              }}
              onChangePassword={() => {
                if (config.mock) setNoteState({ path: pathname, text: "Chế độ giả lập: bản thật đổi mật khẩu ở trang xác thực (Keycloak)." });
                else void changePassword();
              }}
              onLogout={() => void logout()}
              note={note}
            />
          </header>
          <main className="body" id="main" tabIndex={-1}>
            {ctx?.support_session_id ? <SupportBanner /> : null}
            {scopeWarning ? (
              <div className="alert alert-warn" role="status" style={{ marginBottom: 16 }}>
                <Icon name="info" />
                <span>{scopeWarning}</span>
              </div>
            ) : null}
            {allowed ? (
              children
            ) : (
              <div className="card sec" role="alert">
                <h1 style={{ fontSize: 20, marginBottom: 6 }}>Không có quyền truy cập</h1>
                <p className="mute">Tài khoản của bạn không được phân quyền cho màn hình này. Liên hệ quản trị nếu cần cấp quyền.</p>
                <p style={{ marginTop: 12 }}>
                  <Link className="btn btn-o btn-sm" href={home}>
                    Về trang chính
                  </Link>
                </p>
              </div>
            )}
          </main>
        </div>
      </div>
    </BreadcrumbTailProvider>
  );
}
