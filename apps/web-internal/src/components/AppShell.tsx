"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { roleLabel, type Principal } from "@/auth/principal";
import { config } from "@/config";
import { canSee, findNavItem, homeFor, needsMfa, visibleNav, type NavGroup } from "@/nav/nav";
import { Icon } from "./Icon";
import { Loading } from "./ui";

function initials(name: string): string {
  const parts = name.replace(/\(.*?\)/g, "").trim().split(/\s+/);
  return ((parts[0]?.[0] ?? "") + (parts[parts.length - 1]?.[0] ?? "")).toUpperCase() || "MG";
}

export function Sidebar({ groups, pathname, principal, onLogout, onNavigate }: { groups: NavGroup[]; pathname: string; principal: Principal; onLogout: () => void; onNavigate?: () => void }) {
  const roles = principal.roles.map(roleLabel).join(", ");
  return (
    <aside className="side" id="side-nav" aria-label="Điều hướng chính">
      <div className="side-brand">
        <div className="mark" aria-hidden="true">
          <Icon name="leaf" size={20} />
        </div>
        <div>
          <b>Mekong Green</b>
          <span>Web nội bộ · vùng MKG</span>
        </div>
      </div>
      <div className="side-user">
        <div className="av" aria-hidden="true">
          {initials(principal.displayName)}
        </div>
        <div style={{ minWidth: 0 }}>
          <b>{principal.displayName}</b>
          <span title={roles}>{principal.mfa ? "Đã xác thực OTP" : "Chưa xác thực OTP"}</span>
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
                  {i.admin ? <span className="sr-only"> (quản trị, cần OTP)</span> : null}
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

/** Màn hình quản trị khi phiên chưa qua OTP: không gọi bề mặt quản trị, hướng dẫn đăng nhập lại. */
function MfaGate({ onLogout }: { onLogout: () => void }) {
  return (
    <div className="card sec" role="alert" data-testid="mfa-gate">
      <h1 style={{ fontSize: 20, marginBottom: 6 }}>Cần xác thực hai yếu tố</h1>
      <p className="mute">
        Màn hình này dùng bề mặt quản trị của API nội bộ MKG — mọi thao tác bắt buộc phiên đã nhập mã OTP (máy chủ trả{" "}
        <code>mfa_required</code>). Đăng xuất rồi đăng nhập lại kèm mã OTP từ ứng dụng xác thực; nếu chưa cài OTP, liên hệ quản trị để
        được gửi liên kết cấu hình.
      </p>
      <p style={{ marginTop: 12 }}>
        <button type="button" className="btn btn-p btn-sm" onClick={onLogout}>
          Đăng xuất để đăng nhập lại có OTP
        </button>
      </p>
    </div>
  );
}

export function AppShell({ children }: { children: ReactNode }) {
  const { status, principal, logout } = useAuth();
  const pathname = usePathname() ?? "/";
  const router = useRouter();
  const [navOpen, setNavOpen] = useState(false);
  const isLogin = pathname.startsWith("/login");
  const item = findNavItem(pathname);
  const allowed = !item || canSee(item, principal);

  useEffect(() => {
    if (isLogin) return;
    if (status === "anonymous") router.replace(`/login?returnTo=${encodeURIComponent(pathname)}`);
  }, [status, isLogin, pathname, router]);

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
          <button type="button" className="icb" aria-label={navOpen ? "Đóng menu" : "Mở menu"} aria-expanded={navOpen} aria-controls="side-nav" onClick={() => setNavOpen((v) => !v)}>
            <Icon name="menu" />
          </button>
          <div className="crumb">
            <span>Mekong Green</span>
            <b>{item?.label ?? "Trang"}</b>
          </div>
          <div className="spacer" />
          {item?.admin ? <span className="chip c-info">Bề mặt quản trị · MFA</span> : null}
          {config.mock ? <span className="mock-badge">Dữ liệu giả lập</span> : null}
        </header>
        <main className="body" id="main" tabIndex={-1}>
          {!allowed ? (
            <div className="card sec" role="alert">
              <h1 style={{ fontSize: 20, marginBottom: 6 }}>Không có quyền truy cập</h1>
              <p className="mute">Tài khoản của bạn không được phân quyền cho màn hình này. Liên hệ quản trị nếu cần cấp quyền.</p>
              <p style={{ marginTop: 12 }}>
                <Link className="btn btn-o btn-sm" href={homeFor(principal)}>
                  Về trang chính
                </Link>
              </p>
            </div>
          ) : needsMfa(item, principal) ? (
            <MfaGate onLogout={() => void logout()} />
          ) : (
            children
          )}
        </main>
      </div>
    </div>
  );
}
