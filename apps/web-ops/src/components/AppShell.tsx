"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { LEVEL_LABEL, type Principal } from "@/auth/principal";
import { config } from "@/config";
import { canSee, findNavItem, homeFor, visibleNav, type NavGroup } from "@/nav/nav";
import { Icon } from "./Icon";
import { Loading } from "./ui";

function initials(name: string): string {
  const parts = name.replace(/\(.*?\)/g, "").trim().split(/\s+/);
  return ((parts[0]?.[0] ?? "") + (parts[parts.length - 1]?.[0] ?? "")).toUpperCase() || "AG";
}

export function Sidebar({
  groups,
  pathname,
  principal,
  onLogout,
  onNavigate,
}: {
  groups: NavGroup[];
  pathname: string;
  principal: Principal;
  onLogout: () => void;
  onNavigate?: () => void;
}) {
  const scopeText = principal.level ? `${LEVEL_LABEL[principal.level]}${principal.unitName ? ` · ${principal.unitName}` : ""}` : "Chưa xác định phạm vi";
  return (
    <aside className="side" id="side-nav" aria-label="Điều hướng chính">
      <div className="side-brand">
        <div className="mark" aria-hidden="true">
          <Icon name="leaf" size={20} />
        </div>
        <div>
          <b>AgriGreen</b>
          <span>Web vận hành khuyến nông</span>
        </div>
      </div>
      <div className="side-user">
        <div className="av" aria-hidden="true">
          {initials(principal.displayName)}
        </div>
        <div style={{ minWidth: 0 }}>
          <b>{principal.displayName}</b>
          <span>{scopeText}</span>
        </div>
      </div>
      <nav className="nav">
        {groups.map((g, gi) => (
          <div className="nav-grp" key={g.label} role="group" aria-labelledby={`nav-grp-${gi}`}>
            <div className="nav-lbl" id={`nav-grp-${gi}`}>
              {g.label}
            </div>
            {g.items.map((i) => {
              const active = findNavItem(pathname)?.href === i.href;
              return (
                <Link key={i.href} href={i.href} aria-current={active ? "page" : undefined} onClick={onNavigate}>
                  <Icon name={i.icon} />
                  {i.label}
                </Link>
              );
            })}
          </div>
        ))}
      </nav>
      <div className="side-foot">
        <button type="button" onClick={onLogout}>
          <Icon name="logout" />
          Đăng xuất
        </button>
      </div>
    </aside>
  );
}

export function AppShell({ children }: { children: ReactNode }) {
  const { status, principal, logout, scopeWarning } = useAuth();
  const pathname = usePathname() ?? "/";
  const router = useRouter();
  const [navOpen, setNavOpen] = useState(false);
  const isLogin = pathname.startsWith("/login");
  const item = findNavItem(pathname);
  const allowed = !item || canSee(item, principal);

  useEffect(() => {
    if (isLogin) return;
    if (status === "anonymous") {
      router.replace(`/login?returnTo=${encodeURIComponent(pathname)}`);
    } else if (status === "authenticated" && pathname === "/" && !allowed) {
      // Cục KTHT vào "/" → cổng CGH (BRD CGH FN-11 B1).
      router.replace(homeFor(principal));
    }
  }, [status, isLogin, pathname, router, allowed, principal]);

  if (isLogin) return <>{children}</>;
  if (status !== "authenticated" || !principal) {
    return (
      <main style={{ display: "grid", placeItems: "center", minHeight: "100vh" }}>
        <Loading label="Đang kiểm tra phiên đăng nhập…" />
      </main>
    );
  }

  const groups = visibleNav(principal);
  return (
    <div className={`shell${navOpen ? " nav-open" : ""}`}>
      <a href="#main" className="skip-link">
        Bỏ qua điều hướng
      </a>
      <Sidebar groups={groups} pathname={pathname} principal={principal} onLogout={() => void logout()} onNavigate={() => setNavOpen(false)} />
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
          <div className="crumb">
            <span>{principal.orgName ?? "AgriGreen"}</span>
            <b>{item?.label ?? "Trang"}</b>
          </div>
          <div className="spacer" />
          {config.mock ? <span className="mock-badge">Dữ liệu giả lập</span> : null}
        </header>
        <main className="body" id="main" tabIndex={-1}>
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
                <Link className="btn btn-o btn-sm" href={homeFor(principal)}>
                  Về trang chính
                </Link>
              </p>
            </div>
          )}
        </main>
      </div>
    </div>
  );
}
