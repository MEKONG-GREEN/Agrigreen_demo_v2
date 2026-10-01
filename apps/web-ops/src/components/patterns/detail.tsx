"use client";

// Mẫu trang Detail (Design Sample "Screens / Detail"): liên kết quay lại, tiêu đề, thanh nút, các khối
// Nhãn – Giá trị, tab liên quan, khung Log bên phải (dòng thời gian: sự kiện, người, thời điểm).
// Log đọc từ API chuẩn `GET …/{id}/log` (kế hoạch §1 mục 5).
import Link from "next/link";
import { useId, useState, type ReactNode } from "react";
import { formatDateTime } from "@/lib/format";
import { Icon } from "../Icon";

export function BackLink({ href, children }: { href: string; children: ReactNode }) {
  return (
    <Link href={href} className="back-link">
      <Icon name="arrowLeft" size={15} />
      {children}
    </Link>
  );
}

export function ActionBar({ children }: { children: ReactNode }) {
  return (
    <div className="card action-bar" role="toolbar" aria-label="Thao tác">
      {children}
    </div>
  );
}

export interface LabelValue {
  label: string;
  value: ReactNode;
}

/** Khối "Nội dung": cặp Nhãn – Giá trị xếp 2 cột (thu về 1 cột trên màn hẹp). */
export function LabelValueBlock({ title, items }: { title?: string; items: LabelValue[] }) {
  return (
    <section className="lv-block">
      {title ? <h2>{title}</h2> : null}
      <dl className="lv-grid">
        {items.map((it) => (
          <div key={it.label} style={{ display: "contents" }}>
            <dt>{it.label}</dt>
            <dd>{it.value === null || it.value === undefined || it.value === "" ? "—" : it.value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

export interface RelatedTab {
  id: string;
  label: string;
  content: ReactNode;
}

/** Tab liên quan trong khối chính. */
export function RelatedTabs({ tabs, label = "Thông tin liên quan" }: { tabs: RelatedTab[]; label?: string }) {
  const base = useId();
  const [value, setValue] = useState(tabs[0]?.id ?? "");
  const current = tabs.find((t) => t.id === value) ?? tabs[0];
  return (
    <div>
      <div className="dtabs" role="tablist" aria-label={label}>
        {tabs.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            id={`${base}-${t.id}`}
            aria-selected={t.id === current?.id}
            aria-controls={`${base}-panel`}
            onClick={() => setValue(t.id)}
          >
            {t.label}
          </button>
        ))}
      </div>
      <div role="tabpanel" id={`${base}-panel`} aria-labelledby={`${base}-${current?.id ?? ""}`}>
        {current?.content}
      </div>
    </div>
  );
}

/** Một dòng Log (hình dạng API Log chuẩn của kế hoạch §1.5). */
export interface LogEntry {
  event: string;
  actor_display: string;
  at: string;
  changes?: { field: string; old: string | null; new: string | null }[];
  reason?: string | null;
}

export function LogPanel({ entries, title = "Log", empty = "Chưa có thay đổi nào." }: { entries: LogEntry[]; title?: string; empty?: string }) {
  return (
    <aside className="card log" aria-label={title}>
      <h2>{title}</h2>
      {entries.length ? (
        <ol className="log-list">
          {entries.map((e, i) => (
            <li key={`${e.at}-${i}`}>
              <b>{e.event}</b>
              <div className="meta">
                {e.actor_display} · {formatDateTime(e.at)}
              </div>
              {e.changes?.map((c) => (
                <div className="chg" key={c.field}>
                  {c.field}: {c.old ?? "—"} → {c.new ?? "—"}
                </div>
              ))}
              {e.reason ? <div className="chg">Lý do: {e.reason}</div> : null}
            </li>
          ))}
        </ol>
      ) : (
        <p className="mute">{empty}</p>
      )}
    </aside>
  );
}

/** Bố cục Detail: nội dung chính + khung Log bên phải. */
export function DetailLayout({ main, log }: { main: ReactNode; log: ReactNode }) {
  return (
    <div className="detail-grid">
      <div className="card detail-main">{main}</div>
      {log}
    </div>
  );
}
