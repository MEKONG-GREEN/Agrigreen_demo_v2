"use client";

// Dải cảnh báo vàng "Đang hỗ trợ thay …" (FN-AD-04.07, Hình 28): mọi màn hình trong chế độ hỗ trợ; lý do gửi kèm
// mọi thao tác ghi (header X-AG-Reason, ≥ 10 ký tự) và được ghi vào nhật ký hỗ trợ của Admin.
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { contextLabel } from "./session";

export function SupportBanner() {
  const { kn, exitSupport, setSupportReason } = useAuth();
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  if (!kn.context?.support_session_id) return null;
  const reason = kn.support?.reason ?? "";
  const short = [...reason.trim()].length < 10;
  return (
    <div className="alert alert-warn support-banner" role="status" aria-label="Chế độ hỗ trợ" style={{ marginBottom: 16, alignItems: "center", flexWrap: "wrap", gap: 10 }}>
      <Icon name="support" />
      <span style={{ flex: "1 1 320px" }}>
        <b>Đang hỗ trợ thay {kn.support?.target ?? "người dùng"}</b> · {contextLabel(kn.context)}. Mọi thao tác trên dữ liệu nghiệp vụ được ghi nhật ký riêng của Admin.
      </span>
      <label className="row" style={{ gap: 6 }}>
        <span className="small">Lý do thao tác*</span>
        <input className="input" style={{ minWidth: 260 }} value={reason} aria-invalid={short} onChange={(e) => setSupportReason(e.target.value)} />
      </label>
      <button
        type="button"
        className="btn btn-o btn-sm"
        disabled={busy}
        onClick={async () => {
          setBusy(true);
          try {
            await exitSupport();
            router.push("/kn/quan-tri/menu-cac-cap");
          } finally {
            setBusy(false);
          }
        }}
      >
        Thoát chế độ hỗ trợ
      </button>
    </div>
  );
}
