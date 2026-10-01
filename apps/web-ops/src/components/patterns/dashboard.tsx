"use client";

// Mẫu Dashboard (Design Sample "Screens / Dashboard"): "Chỉ số nóng" (thẻ số + % so kỳ trước + sparkline),
// "Chỉ số so sánh" (thanh tiến độ theo khu vực; kỳ này/kỳ trước), "Cảnh báo" (Khẩn / Cần chú ý / Thông tin).
import Link from "next/link";
import type { ReactNode } from "react";
import { formatDateTime } from "@/lib/format";
import { Icon } from "../Icon";

export function SectionTitle({ children, count, action }: { children: ReactNode; count?: number; action?: ReactNode }) {
  return (
    <h2 className="sec-title">
      {children}
      {count !== undefined ? <span className="count-pill">{count}</span> : null}
      {action}
    </h2>
  );
}

/** Đường sparkline (viewBox 100 × 40) — điểm chuẩn hóa theo min/max của dãy. */
export function sparkPath(values: readonly number[]): { line: string; area: string } {
  if (values.length < 2) return { line: "", area: "" };
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || 1;
  const pts = values.map((v, i) => [(i / (values.length - 1)) * 100, 36 - ((v - min) / span) * 28] as const);
  const line = pts.map(([x, y], i) => `${i ? "L" : "M"}${x.toFixed(1)} ${y.toFixed(1)}`).join(" ");
  return { line, area: `${line} L100 40 L0 40 Z` };
}

export type Trend = "up" | "down" | "neutral";

export function HotMetric({
  label,
  value,
  delta,
  trend = "up",
  series,
  tone,
}: {
  label: string;
  value: string;
  /** "+6,2% so kỳ trước". */
  delta?: string;
  trend?: Trend;
  series?: readonly number[];
  /** Màu sparkline: mặc định xanh; "bad" đỏ (vd sâu bệnh tăng), "info" xanh dương. */
  tone?: "ok" | "bad" | "info";
}) {
  const color = tone === "bad" ? "var(--danger)" : tone === "info" ? "var(--chart-blue)" : "var(--primary)";
  const fill = tone === "bad" ? "rgba(192,57,43,.12)" : tone === "info" ? "rgba(0,163,224,.12)" : "rgba(0,168,45,.12)";
  const sp = series ? sparkPath(series) : null;
  return (
    <div className="card hot">
      <div className="lb">{label}</div>
      <div className="vl">{value}</div>
      {delta ? <div className={`dl${trend === "down" ? " down" : trend === "neutral" ? " neutral" : ""}`}>{delta}</div> : null}
      {sp?.line ? (
        <svg viewBox="0 0 100 40" preserveAspectRatio="none" aria-hidden="true">
          <path d={sp.area} fill={fill} />
          <path d={sp.line} fill="none" stroke={color} strokeWidth="1.6" vectorEffect="non-scaling-stroke" />
        </svg>
      ) : null}
    </div>
  );
}

export interface ProgressItem {
  label: string;
  pct: number;
  detail?: string;
}

/** Màu thanh theo ngưỡng (tạm, chờ khách — Q-17/Q-18): ≥ 85 % xanh lá, < 40 % cam, còn lại xanh dương. */
export function progressColor(pct: number): string {
  if (pct >= 85) return "var(--primary)";
  if (pct < 40) return "var(--chart-orange)";
  return "var(--chart-blue)";
}

export function ProgressList({ title, items }: { title: string; items: ProgressItem[] }) {
  return (
    <section className="card cmp" aria-label={title}>
      <h3>{title}</h3>
      {items.map((it) => (
        <div className="prog-row" key={it.label}>
          <div className="prog-top">
            <span style={{ color: "var(--slate-900)" }}>{it.label}</span>
            <span>
              <b>{Math.round(it.pct)}%</b>
              {it.detail ? ` · ${it.detail}` : ""}
            </span>
          </div>
          <div className="prog-bar" role="progressbar" aria-label={it.label} aria-valuenow={Math.round(it.pct)} aria-valuemin={0} aria-valuemax={100}>
            <span style={{ width: `${Math.max(0, Math.min(100, it.pct))}%`, background: progressColor(it.pct) }} />
          </div>
        </div>
      ))}
    </section>
  );
}

export interface PeriodCompareItem {
  label: string;
  current: number;
  previous: number;
}

export function deltaPct(current: number, previous: number): number {
  return previous === 0 ? 0 : ((current - previous) / previous) * 100;
}

export function formatDelta(pct: number): string {
  const s = Math.abs(pct).toLocaleString("vi-VN", { minimumFractionDigits: 1, maximumFractionDigits: 1 });
  return `${pct > 0 ? "+" : pct < 0 ? "−" : ""}${s}%`;
}

export function PeriodCompare({ title, items }: { title: string; items: PeriodCompareItem[] }) {
  const max = Math.max(1, ...items.flatMap((i) => [i.current, i.previous]));
  return (
    <section className="card cmp" aria-label={title}>
      <h3>
        {title}
        <span className="cmp-legend">
          <span>
            <i style={{ background: "var(--primary)" }} />
            Kỳ này
          </span>
          <span>
            <i style={{ background: "var(--slate-300)" }} />
            Kỳ trước
          </span>
        </span>
      </h3>
      {items.map((it) => {
        const d = deltaPct(it.current, it.previous);
        return (
          <div className="pcmp-row" key={it.label}>
            <span>{it.label}</span>
            <span className="pcmp-bars" aria-hidden="true">
              <span style={{ width: `${(it.current / max) * 100}%`, background: "var(--primary)" }} />
              <span style={{ width: `${(it.previous / max) * 100}%`, background: "var(--slate-300)" }} />
            </span>
            <span className={`d${d < 0 ? " down" : ""}`}>{formatDelta(d)}</span>
          </div>
        );
      })}
    </section>
  );
}

export type AlertLevel = "urgent" | "attention" | "info";

export const ALERT_LEVEL_LABEL: Record<AlertLevel, string> = { urgent: "Khẩn", attention: "Cần chú ý", info: "Thông tin" };
const ALERT_PILL: Record<AlertLevel, string> = { urgent: "pill-bad", attention: "pill-warn", info: "pill-info" };
const ALERT_ICON = { urgent: "xCircle", attention: "alert", info: "info" } as const;

export interface DashboardAlert {
  id: string;
  level: AlertLevel;
  title: string;
  description?: string;
  at: string;
  href?: string;
}

export function AlertList({ title = "Cảnh báo", alerts, allHref }: { title?: string; alerts: DashboardAlert[]; allHref?: string }) {
  return (
    <section aria-label={title}>
      <SectionTitle count={alerts.length} action={allHref ? <Link className="link-all" href={allHref}>Xem tất cả</Link> : undefined}>
        {title}
      </SectionTitle>
      <div className="card alerts">
        {alerts.length ? (
          alerts.map((a) => (
            <div className="alert-row" key={a.id}>
              <span className={`alert-ic ${a.level}`} aria-hidden="true">
                <Icon name={ALERT_ICON[a.level]} />
              </span>
              <div style={{ minWidth: 0 }}>
                <b>{a.title}</b>
                <span className={`pill ${ALERT_PILL[a.level]}`}>{ALERT_LEVEL_LABEL[a.level]}</span>
                {a.description ? <div className="desc">{a.description}</div> : null}
              </div>
              <time dateTime={a.at}>{formatDateTime(a.at)}</time>
              {a.href ? (
                <Link className="btn btn-o btn-sm" href={a.href} aria-label={`Xem: ${a.title}`}>
                  Xem
                </Link>
              ) : (
                <span />
              )}
            </div>
          ))
        ) : (
          <p className="empty">Không có cảnh báo.</p>
        )}
      </div>
    </section>
  );
}
