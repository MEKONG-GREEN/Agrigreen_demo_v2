import type { ReactNode } from "react";
import { Icon, type IconName } from "./Icon";

export function Loading({ label = "Đang tải dữ liệu…" }: { label?: string }) {
  return (
    <div className="loading" role="status" aria-live="polite">
      <span className="spinner" aria-hidden="true" />
      {label}
    </div>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>;
}

export function PageHead({ title, subtitle, actions }: { title: string; subtitle?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="pg-head">
      <div>
        <h1>{title}</h1>
        {subtitle ? <p>{subtitle}</p> : null}
      </div>
      {actions ? <div className="row">{actions}</div> : null}
    </div>
  );
}

export type KpiColor = "green" | "blue" | "orange" | "violet" | "red";

export function KpiTile({
  label,
  value,
  unit,
  footer,
  color,
  icon,
  missingHint,
}: {
  label: string;
  value: string | null;
  unit?: string;
  footer?: ReactNode;
  color: KpiColor;
  icon: IconName;
  /** Chú thích khi thiếu dữ liệu (BRD CGH FN-11: hiện "—" + tooltip). */
  missingHint?: string;
}) {
  const missing = value === null || value === "—";
  return (
    <div className={`kpi ${color}`} role="group" aria-label={label}>
      <div className="lb">{label}</div>
      <div className="vl">
        {missing ? (
          <span title={missingHint ?? "Chưa có dữ liệu tính đến thời điểm hiện tại"}>
            —<span className="sr-only">{missingHint ?? "Chưa có dữ liệu tính đến thời điểm hiện tại"}</span>
          </span>
        ) : (
          <>
            {value}
            {unit ? <small>{unit}</small> : null}
          </>
        )}
      </div>
      {footer ? <div className="ft">{footer}</div> : null}
      <div className="ic" aria-hidden="true">
        <Icon name={icon} size={20} />
      </div>
    </div>
  );
}

export function LoadMore({ hasMore, loading, onClick }: { hasMore: boolean; loading: boolean; onClick: () => void }) {
  if (!hasMore) return null;
  return (
    <div style={{ textAlign: "center", padding: 12 }}>
      <button type="button" className="btn btn-o btn-sm" onClick={onClick} disabled={loading}>
        {loading ? "Đang tải…" : "Tải thêm"}
      </button>
    </div>
  );
}
