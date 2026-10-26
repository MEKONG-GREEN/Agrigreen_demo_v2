"use client";

// Thành phần giao diện dùng chung của các màn HCTH theo bố cục prototype `Khuyen_nong_so_HCTH.html` (W11.29): thẻ số
// liệu bấm được để lọc nhanh (`kpi()` của prototype) và lưới thẻ. Dùng lớp sẵn có `hot-grid`/`card hot` của khung —
// không thêm CSS toàn cục.
import type { ReactNode } from "react";

export function KpiGrid({ children, label }: { children: ReactNode; label?: string }) {
  return (
    <div className="hot-grid" role="group" aria-label={label ?? "Chỉ số"} style={{ marginBottom: 16 }}>
      {children}
    </div>
  );
}

/** Thẻ số liệu: bấm để lọc (aria-pressed khi đang lọc theo thẻ) hoặc mở danh sách. */
export function KpiTile({
  label,
  value,
  sub,
  active,
  onClick,
  ariaLabel,
}: {
  label: string;
  value: ReactNode;
  sub?: string;
  active?: boolean;
  onClick?: () => void;
  ariaLabel?: string;
}) {
  const style = {
    minHeight: 92,
    paddingBottom: 14,
    textAlign: "left" as const,
    font: "inherit",
    cursor: onClick ? "pointer" : "default",
    outline: active ? "2px solid var(--primary)" : undefined,
  };
  const body = (
    <>
      <span className="lb">{label}</span>
      <span className="vl">{value}</span>
      {sub ? <span className="dl neutral">{sub}</span> : null}
    </>
  );
  return onClick ? (
    <button type="button" className="card hot" style={style} onClick={onClick} aria-pressed={active} aria-label={ariaLabel}>
      {body}
    </button>
  ) : (
    <div className="card hot" style={style} aria-label={ariaLabel}>
      {body}
    </div>
  );
}
