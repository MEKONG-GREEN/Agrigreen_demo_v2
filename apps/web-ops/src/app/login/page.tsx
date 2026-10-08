"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { LOCK_MINUTES } from "@/api/mock/kn";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { useAuth } from "@/auth/AuthProvider";
import { safeReturnTo } from "@/auth/oidc";
import { Icon } from "@/components/Icon";
import { config } from "@/config";
import { canSee, findNavItem, homeFor } from "@/nav/nav";
import { BrandMark } from "@/shell/BrandMark";
import { BRAND } from "@/shell/brand";
import { LoginCard, type LoginError } from "@/shell/LoginCard";
import { menuAllows } from "@/shell/menu";
import { purgeLegacyIdentifier, rememberIdentifier, useRememberedIdentifier } from "@/lib/rememberLogin";

function LoginInner() {
  const { status, principal, kn, login, loginWithPassword, loginAsPersona } = useAuth();
  const router = useRouter();
  const params = useSearchParams();
  const returnTo = safeReturnTo(params.get("returnTo"));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<LoginError | null>(() =>
    params.get("error") ? { kind: "message", text: "Không hoàn tất được đăng nhập. Vui lòng thử lại hoặc liên hệ quản trị viên." } : null,
  );
  // undefined trước hydrate → chờ để thẻ nhận đúng giá trị ban đầu.
  // Chỉ email, chỉ khi đã tích "Ghi nhớ đăng nhập", ở sessionStorage (src/lib/rememberLogin.ts).
  const remembered = useRememberedIdentifier();
  useEffect(purgeLegacyIdentifier, []);

  useEffect(() => {
    if (status !== "authenticated" || !principal) return;
    if (kn.status === "choose") {
      router.replace(`/chon-vai-tro?returnTo=${encodeURIComponent(returnTo)}`);
      return;
    }
    if (kn.status === "active" && kn.context && kn.menu) {
      router.replace(returnTo !== "/" && menuAllows(kn.menu, returnTo) ? returnTo : kn.context.default_route);
      return;
    }
    const item = findNavItem(returnTo);
    const allowed = returnTo !== "/" && (!item || canSee(item, principal));
    router.replace(allowed ? returnTo : homeFor(principal));
  }, [status, principal, kn, returnTo, router]);

  const submit = async (v: { identifier: string; password: string; remember: boolean }) => {
    setBusy(true);
    setError(null);
    rememberIdentifier(v.identifier, v.remember);
    try {
      if (config.mock) {
        const r = await loginWithPassword(v.identifier, v.password);
        if (!r.ok) setError(r.error === "locked" ? { kind: "locked", minutes: LOCK_MINUTES } : { kind: "invalid", remaining: r.remaining });
        setBusy(false);
        return;
      }
      await login(returnTo, v.identifier);
    } catch {
      setError({ kind: "message", text: "Không kết nối được máy chủ đăng nhập. Vui lòng thử lại sau." });
      setBusy(false);
    }
  };

  return (
    <div className="auth-bg">
      <main className="login">
        <section className="login-brand" aria-label="Giới thiệu hệ thống">
          <div className="brand-row">
            <BrandMark />
            {BRAND.productName}
          </div>
          <div className="login-hero">
            <span className="sys-tag">{BRAND.systemTag}</span>
            <p className="sr-only">Tên sản phẩm, logo và ảnh nền là tạm, chờ bộ nhận diện của TTKNQG.</p>
            <h2>{BRAND.loginTitle}</h2>
            <p>{BRAND.loginLead}</p>
          </div>
          <p className="login-copy">
            © {BRAND.year} {BRAND.orgFull}
          </p>
        </section>
        <section className="login-side">
          {remembered !== undefined ? (
            <LoginCard
              mode={config.mock ? "password" : "oidc"}
              busy={busy || status === "loading"}
              error={error}
              initialIdentifier={remembered ?? ""}
              initialRemember={!!remembered}
              onSubmit={(v) => void submit(v)}
            />
          ) : null}
          {config.mock ? (
            <div className="persona-card">
              <div className="alert alert-info">
                <Icon name="info" />
                <span>
                  Chế độ <b style={{ display: "inline" }}>dữ liệu giả lập</b> (NEXT_PUBLIC_API_MOCK=1): không gọi Keycloak hay backend. Tài khoản KN v7.7
                  (vd an.nv@angiang.gov.vn) đăng nhập bằng mật khẩu thử ở README; hoặc chọn một vai thử dưới đây.
                </span>
              </div>
              <h2>Vai thử (giao diện hiện có)</h2>
              <div className="stack" style={{ gap: 6 }} role="list">
                {MOCK_PERSONAS.map((p) => (
                  <div role="listitem" key={p.id}>
                    <button type="button" className="persona" onClick={() => void loginAsPersona(p.id)}>
                      <b>{p.title}</b>
                      <span className="mute small">{p.description}</span>
                    </button>
                  </div>
                ))}
              </div>
            </div>
          ) : null}
        </section>
      </main>
    </div>
  );
}

export default function LoginPage() {
  return (
    <Suspense fallback={null}>
      <LoginInner />
    </Suspense>
  );
}
