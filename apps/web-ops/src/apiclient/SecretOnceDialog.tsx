"use client";

import { useEffect, useId, useRef, useState } from "react";
import { Icon } from "@/components/Icon";
import { formatDateTime } from "@/lib/format";

export interface SecretOnce {
  /** agk_<prefix>_<bí mật> — chỉ có ở phản hồi 201 đầu tiên (ADR-0022 §4). */
  apiKey: string;
  keyPrefix: string;
  expiresAt: string;
  /** Khi xoay: hạn chồng lấn của khóa cũ. */
  previousGraceUntil?: string | null;
  clientName: string;
}

/**
 * Hộp thoại hiện bí mật khóa API MỘT lần (ADR-0022 §4, R-92).
 * - Bí mật chỉ nằm trong props của hộp thoại đang mở; nơi gọi xóa nó khỏi state khi `onConfirm`.
 * - Không lưu vào localStorage/sessionStorage, không ghi log, không đặt vào URL hay thuộc tính title.
 * - Không đóng bằng Esc hay bấm nền: chỉ đóng bằng xác nhận rõ ràng “Tôi đã lưu khóa”.
 */
export function SecretOnceDialog({ secret, onConfirm }: { secret: SecretOnce; onConfirm: () => void }) {
  const id = useId();
  const ref = useRef<HTMLDivElement>(null);
  const [copied, setCopied] = useState<"idle" | "ok" | "fail">("idle");

  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    ref.current?.querySelector<HTMLElement>("[data-secret-copy]")?.focus();
    const onKey = (e: KeyboardEvent) => {
      // Esc KHÔNG đóng: mất bí mật là không lấy lại được (chỉ có thể phát hành khóa mới).
      if (e.key === "Escape") e.preventDefault();
    };
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      prev?.focus?.();
    };
  }, []);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(secret.apiKey);
      setCopied("ok");
    } catch {
      setCopied("fail");
    }
  };

  return (
    <div className="dlg-back">
      <div className="dlg" role="dialog" aria-modal="true" aria-labelledby={`${id}-t`} aria-describedby={`${id}-d`} ref={ref} data-testid="secret-once">
        <h2 id={`${id}-t`}>Khóa API mới — chỉ hiển thị một lần</h2>
        <div className="alert alert-warn" role="alert" id={`${id}-d`}>
          <Icon name="lock" />
          <span>
            <b>Sao chép và lưu khóa này ngay.</b> Hệ thống chỉ lưu mã băm, không lưu khóa — đóng hộp thoại là không xem lại được. Giao khóa cho{" "}
            {secret.clientName} qua kênh đã thỏa thuận (Q5-8), không gửi qua email thường hay mạng xã hội.
          </span>
        </div>
        <div className="field" style={{ marginTop: 12 }}>
          <label htmlFor={`${id}-k`}>Khóa API (header X-API-Key)</label>
          <input
            id={`${id}-k`}
            className="input link-once"
            readOnly
            value={secret.apiKey}
            autoComplete="off"
            spellCheck={false}
            data-secret="api-key"
            onFocus={(e) => e.currentTarget.select()}
          />
        </div>
        <dl className="kv small" style={{ marginTop: 8 }}>
          <dt>Tiền tố (an toàn để ghi log)</dt>
          <dd>
            <code>{secret.keyPrefix}</code>
          </dd>
          <dt>Hết hạn</dt>
          <dd>{formatDateTime(secret.expiresAt)}</dd>
          {secret.previousGraceUntil ? (
            <>
              <dt>Khóa cũ còn dùng được tới</dt>
              <dd>{formatDateTime(secret.previousGraceUntil)}</dd>
            </>
          ) : null}
        </dl>
        <p className="small" role="status" aria-live="polite">
          {copied === "ok" ? "Đã sao chép khóa vào bộ nhớ tạm." : copied === "fail" ? "Trình duyệt không cho sao chép — chọn ô khóa và sao chép thủ công." : ""}
        </p>
        <div className="dlg-actions">
          <button type="button" className="btn btn-o btn-sm" onClick={() => void copy()} data-secret-copy>
            Sao chép khóa
          </button>
          <button type="button" className="btn btn-p btn-sm" onClick={onConfirm}>
            Tôi đã lưu khóa
          </button>
        </div>
      </div>
    </div>
  );
}
