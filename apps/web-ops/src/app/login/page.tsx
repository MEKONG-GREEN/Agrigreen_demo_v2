"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { MOCK_PERSONAS } from "@/api/mock/personas";
import { useAuth } from "@/auth/AuthProvider";
import { safeReturnTo } from "@/auth/oidc";
import { Icon } from "@/components/Icon";
import { config } from "@/config";
import { canSee, findNavItem, homeFor } from "@/nav/nav";

function LoginInner() {
  const { status, principal, login, loginAsPersona } = useAuth();
  const router = useRouter();
  const params = useSearchParams();
  const returnTo = safeReturnTo(params.get("returnTo"));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (status === "authenticated" && principal) {
      const item = findNavItem(returnTo);
      const allowed = returnTo !== "/" && (!item || canSee(item, principal));
      router.replace(allowed ? returnTo : homeFor(principal));
    }
  }, [status, principal, returnTo, router]);

  const start = async () => {
    setBusy(true);
    setError(null);
    try {
      await login(returnTo);
    } catch {
      setError("Không kết nối được máy chủ đăng nhập. Vui lòng thử lại sau.");
      setBusy(false);
    }
  };

  return (
    <main className="login">
      <section className="login-hero" aria-hidden="false">
        <div className="row" style={{ marginBottom: "auto" }}>
          <Icon name="leaf" size={28} />
          <b style={{ fontSize: 20 }}>AgriGreen</b>
        </div>
        <h1>Khuyến nông số — điều hành từ Trung ương đến từng xã</h1>
        <div className="rule" />
        <p>
          Theo dõi diện tích triển khai, HTX, thửa đã số hoá, nhiệm vụ hỗ trợ và cân đối cơ giới hoá theo phạm vi được
          phân quyền.
        </p>
      </section>
      <section className="login-form">
        <h2>Đăng nhập</h2>
        {config.mock ? (
          <>
            <div className="alert alert-info">
              <Icon name="info" />
              <span>
                Chế độ <b style={{ display: "inline" }}>dữ liệu giả lập</b> (NEXT_PUBLIC_API_MOCK=1): không gọi Keycloak hay
                backend. Chọn một vai để xem giao diện theo phạm vi.
              </span>
            </div>
            <div className="stack" style={{ gap: 8 }} role="list">
              {MOCK_PERSONAS.map((p) => (
                <div role="listitem" key={p.id}>
                  <button type="button" className="persona" onClick={() => void loginAsPersona(p.id)}>
                    <b>{p.title}</b>
                    <span className="mute small">{p.description}</span>
                  </button>
                </div>
              ))}
            </div>
          </>
        ) : (
          <>
            <p className="mute">
              Đăng nhập bằng tài khoản AgriGreen qua máy chủ xác thực (Keycloak). Tài khoản quản trị cần mã OTP.
            </p>
            {error ? (
              <div className="alert alert-bad" role="alert">
                <Icon name="alert" />
                <span>{error}</span>
              </div>
            ) : null}
            <button type="button" className="btn btn-p" onClick={() => void start()} disabled={busy || status === "loading"}>
              {busy ? "Đang chuyển tới trang đăng nhập…" : "Đăng nhập"}
            </button>
            <p className="mute small">Chưa có tài khoản? Liên hệ quản trị đơn vị để được gửi liên kết kích hoạt.</p>
          </>
        )}
      </section>
    </main>
  );
}

export default function LoginPage() {
  return (
    <Suspense fallback={null}>
      <LoginInner />
    </Suspense>
  );
}
