"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { getUserManager, safeReturnTo } from "@/auth/oidc";
import { Icon } from "@/components/Icon";
import { Loading } from "@/components/ui";
import { config } from "@/config";

/** redirect_uri của OIDC: đổi code + code_verifier (PKCE) lấy token, rồi quay về trang cũ. */
export default function LoginCallbackPage() {
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);
  const done = useRef(false);

  useEffect(() => {
    if (done.current) return; // StrictMode gọi effect hai lần; code chỉ đổi được một lần
    done.current = true;
    if (config.mock) {
      router.replace("/login");
      return;
    }
    getUserManager()
      .signinRedirectCallback()
      .then((user) => {
        const state = user.state as { returnTo?: string } | undefined;
        // Xoá code/state khỏi URL và lịch sử.
        router.replace(safeReturnTo(state?.returnTo));
      })
      .catch(() => {
        setError("Không hoàn tất được đăng nhập (mã xác thực hết hạn hoặc không hợp lệ).");
      });
  }, [router]);

  return (
    <main style={{ display: "grid", placeItems: "center", minHeight: "100vh", padding: 16 }}>
      {error ? (
        <div className="card sec" role="alert" style={{ maxWidth: 480 }}>
          <div className="row" style={{ color: "var(--red)", marginBottom: 8 }}>
            <Icon name="alert" />
            <b>{error}</b>
          </div>
          <Link className="btn btn-p" href="/login">
            Đăng nhập lại
          </Link>
        </div>
      ) : (
        <Loading label="Đang hoàn tất đăng nhập…" />
      )}
    </main>
  );
}
