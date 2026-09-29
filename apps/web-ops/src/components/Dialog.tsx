"use client";

import { useEffect, useId, useRef, type ReactNode } from "react";

/**
 * Hộp thoại mô-đun theo WAI-ARIA (role="dialog", aria-modal): Esc đóng, focus vào phần tử đầu tiên,
 * trả focus về nút mở khi đóng. Dùng cho thao tác có lý do (sửa/hủy vụ, yêu cầu bổ sung, vô hiệu hoá HTX).
 */
export function Dialog({ title, onClose, children, actions }: { title: string; onClose: () => void; children: ReactNode; actions?: ReactNode }) {
  const id = useId();
  const ref = useRef<HTMLDivElement>(null);
  const closeRef = useRef(onClose);
  useEffect(() => {
    closeRef.current = onClose;
  });
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    const first = ref.current?.querySelector<HTMLElement>("input, textarea, select, button:not([data-dlg-close])");
    first?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") closeRef.current();
    };
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      prev?.focus?.();
    };
  }, []);
  return (
    <div className="dlg-back" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dlg" role="dialog" aria-modal="true" aria-labelledby={`${id}-t`} ref={ref}>
        <h2 id={`${id}-t`}>{title}</h2>
        {children}
        <div className="dlg-actions">
          {actions}
          <button type="button" className="btn btn-o btn-sm" onClick={onClose} data-dlg-close>
            Đóng
          </button>
        </div>
      </div>
    </div>
  );
}
