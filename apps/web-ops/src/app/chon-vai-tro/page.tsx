"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useState } from "react";
import { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { safeReturnTo } from "@/auth/oidc";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import type { SelectContextRequest } from "@/kn/contract";
import { initials } from "@/kn/session";
import { BrandMark } from "@/shell/BrandMark";
import { BRAND } from "@/shell/brand";
import { RoleContextPicker } from "@/shell/RoleContextPicker";

/** FN-CH-00.01 B3 — Chọn vai trò và phòng ban (Hình 3b). Đổi lại sau trong menu tài khoản. */
function ChooseInner() {
  const { status, principal, kn, selectContext, logout } = useAuth();
  const router = useRouter();
  const returnTo = safeReturnTo(useSearchParams().get("returnTo"));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);

  useEffect(() => {
    if (status === "anonymous") router.replace("/login");
    // Tài khoản không có vai trò KN (vai thử cũ, backend chưa có P1) → không cần màn này.
    else if (status === "authenticated" && kn.status === "legacy") router.replace("/");
  }, [status, kn.status, router]);

  if (status !== "authenticated" || !principal || kn.status === "legacy") {
    return (
      <main style={{ display: "grid", placeItems: "center", minHeight: "100vh" }}>
        <Loading label="Đang kiểm tra phiên đăng nhập…" />
      </main>
    );
  }

  const choose = async (sel: SelectContextRequest) => {
    setBusy(true);
    setProblem(null);
    try {
      const ctx = await selectContext(sel);
      // Vào trang mặc định của vai trò (FN-CH-00.01); trang cũ ngoài /kn được mở lại nếu có.
      router.replace(returnTo !== "/" && !returnTo.startsWith("/kn") && !returnTo.startsWith("/chon-vai-tro") ? returnTo : ctx.default_route);
    } catch (e) {
      setProblem(e instanceof ApiProblem ? e : null);
      setBusy(false);
    }
  };

  return (
    <div className="auth-bg">
      <div className="choose">
        <header className="choose-top">
          <div className="brand-row">
            <BrandMark />
            {BRAND.productName}
          </div>
          <div className="choose-user">
            <span className="acct-av" aria-hidden="true">
              {initials(principal.displayName)}
            </span>
            <div>
              <b>{principal.displayName}</b>
              <span>{kn.assignments.length} vai trò</span>
            </div>
          </div>
        </header>
        <main className="choose-body">
          <h1>Chọn vai trò và phòng ban</h1>
          <p className="lead">Tài khoản của bạn có nhiều vai trò. Chọn vai trò, rồi chọn phòng ban để vào hệ thống. Có thể đổi lại trong menu tài khoản.</p>
          {problem ? <ProblemAlert problem={problem} /> : null}
          {busy ? <Loading label="Đang vào hệ thống…" /> : null}
          <RoleContextPicker assignments={kn.assignments} onSelect={(s) => void choose(s)} busy={busy} />
          <p style={{ marginTop: 18 }}>
            <Link
              href="/login"
              style={{ color: "#fff" }}
              onClick={(e) => {
                e.preventDefault();
                void logout();
              }}
            >
              Đăng nhập bằng tài khoản khác
            </Link>
          </p>
        </main>
      </div>
    </div>
  );
}

export default function ChooseContextPage() {
  return (
    <Suspense fallback={null}>
      <ChooseInner />
    </Suspense>
  );
}
