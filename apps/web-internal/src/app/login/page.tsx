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
      router.replace(returnTo !== "/" && (!item || canSee(item, principal)) ? returnTo : homeFor(principal));
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
      <section className="login-hero">
        <div className="row" style={{ marginBottom: "auto" }}>
          <Icon name="leaf" size={28} />
          <b style={{ fontSize: 20 }}>Mekong Green</b>
        </div>
        <h1>Vận hành nội bộ — từ ruộng tới nhà máy</h1>
        <div className="rule" />
        <p>Lệnh gom và số cuộn, cân ba điểm HUB_IN → HUB_OUT → CUSTOMER_IN, đối soát hao hụt và dung sai, tồn kho theo lô xuất theo rủi ro.</p>
      </section>
      <section className="login-form">
        <h2>Đăng nhập</h2>
        {config.mock ? (
          <>
            <div className="alert alert-info">
              <Icon name="info" />
              <span>
                Chế độ <b style={{ display: "inline" }}>dữ liệu giả lập</b> (NEXT_PUBLIC_API_MOCK=1): không gọi Keycloak hay api-internal. Chọn
                một vai để thử.
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
            <p className="mute">Đăng nhập bằng tài khoản Mekong Green qua máy chủ xác thực (Keycloak). Màn hình quản trị cần mã OTP.</p>
            {error ? (
              <div className="alert alert-bad" role="alert">
                <Icon name="alert" />
                <span>{error}</span>
              </div>
            ) : null}
            <button type="button" className="btn btn-p" onClick={() => void start()} disabled={busy || status === "loading"}>
              {busy ? "Đang chuyển tới trang đăng nhập…" : "Đăng nhập"}
            </button>
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
