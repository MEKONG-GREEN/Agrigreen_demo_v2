"use client";

// Mẫu trang List (BRD §4.0 mục 3, Design Sample "Screens / List"): tiêu đề + nút, thanh bộ lọc dạng thẻ
// có "Đặt lại", bảng có STT, nhãn trạng thái, biểu tượng xem/sửa/xóa, phân trang số trang.
import Link from "next/link";
import type { ReactNode } from "react";
import { Icon } from "../Icon";

export interface FilterOption {
  value: string;
  label: string;
}

/** Thẻ lọc "Nhãn  Giá trị ▾" (select gốc phủ trong suốt → bàn phím, trình đọc màn hình dùng được). */
export function FilterChip({
  label,
  value,
  options,
  onChange,
  defaultValue = "",
}: {
  label: string;
  value: string;
  options: FilterOption[];
  onChange: (v: string) => void;
  /** Giá trị "Tất cả"; khác giá trị này → thẻ tô xanh (đã lọc). */
  defaultValue?: string;
}) {
  const current = options.find((o) => o.value === value)?.label ?? value;
  return (
    <span className={`fchip${value !== defaultValue ? " is-set" : ""}`}>
      <span aria-hidden="true">{label}</span>
      <b aria-hidden="true">{current}</b>
      <Icon name="chevronDown" size={14} />
      <select aria-label={label} value={value} onChange={(e) => onChange(e.target.value)}>
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </span>
  );
}

export function SearchBox({ value, onChange, placeholder = "Tìm kiếm" }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  return (
    <label className="fsearch">
      <Icon name="search" size={16} />
      <span className="sr-only">{placeholder}</span>
      <input type="search" value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} />
    </label>
  );
}

export function FilterBar({ children, onReset }: { children: ReactNode; onReset?: () => void }) {
  return (
    <div className="card filters" role="search" aria-label="Bộ lọc">
      {children}
      {onReset ? (
        <button type="button" className="freset" onClick={onReset}>
          <Icon name="refresh" size={15} />
          Đặt lại
        </button>
      ) : null}
    </div>
  );
}

export type PillTone = "neutral" | "ok" | "warn" | "bad" | "info";

export function StatusPill({ tone = "neutral", children }: { tone?: PillTone; children: ReactNode }) {
  return <span className={`pill${tone === "neutral" ? "" : ` pill-${tone}`}`}>{children}</span>;
}

/** Biểu tượng xem / sửa / xóa của một dòng. Cấp Quốc gia: không truyền edit/remove (chỉ xem). */
export function RowActions({
  label,
  viewHref,
  editHref,
  onDelete,
}: {
  /** Tên dòng cho nhãn trợ năng: "Xem HTX Mỹ Hòa". */
  label: string;
  viewHref?: string;
  editHref?: string;
  onDelete?: () => void;
}) {
  return (
    <span className="row-acts">
      {viewHref ? (
        <Link href={viewHref} aria-label={`Xem ${label}`} title="Xem">
          <Icon name="eye" size={17} />
        </Link>
      ) : null}
      {editHref ? (
        <Link href={editHref} aria-label={`Sửa ${label}`} title="Sửa">
          <Icon name="edit" size={16} />
        </Link>
      ) : null}
      {onDelete ? (
        <button type="button" className="del" aria-label={`Xóa ${label}`} title="Xóa" onClick={onDelete}>
          <Icon name="trash" size={16} />
        </button>
      ) : null}
    </span>
  );
}

/** Số trang hiển thị: 1 2 3 … 10 (luôn có trang đầu, cuối, lân cận trang hiện tại). */
export function pageItems(current: number, total: number): (number | "gap")[] {
  if (total <= 7) return Array.from({ length: total }, (_, i) => i + 1);
  const set = new Set<number>([1, total, current - 1, current, current + 1]);
  if (current <= 3) [2, 3].forEach((n) => set.add(n));
  if (current >= total - 2) [total - 1, total - 2].forEach((n) => set.add(n));
  const nums = [...set].filter((n) => n >= 1 && n <= total).sort((a, b) => a - b);
  const out: (number | "gap")[] = [];
  nums.forEach((n, i) => {
    if (i > 0 && n - nums[i - 1]! > 1) out.push("gap");
    out.push(n);
  });
  return out;
}

export function rangeText(page: number, pageSize: number, total: number): string {
  if (total === 0) return "Không có dòng nào";
  const from = (page - 1) * pageSize + 1;
  const to = Math.min(page * pageSize, total);
  return `Hiển thị ${from}–${to} trên ${total.toLocaleString("vi-VN")}`;
}

export function Pagination({ page, pageSize, total, onPage }: { page: number; pageSize: number; total: number; onPage: (p: number) => void }) {
  const pages = Math.max(1, Math.ceil(total / pageSize));
  return (
    <div className="pager">
      <span aria-live="polite">{rangeText(page, pageSize, total)}</span>
      <nav aria-label="Phân trang">
        <ol>
          <li>
            <button type="button" aria-label="Trang trước" disabled={page <= 1} onClick={() => onPage(page - 1)}>
              ‹
            </button>
          </li>
          {pageItems(page, pages).map((p, i) =>
            p === "gap" ? (
              <li key={`g${i}`} className="gap" aria-hidden="true">
                …
              </li>
            ) : (
              <li key={p}>
                <button type="button" aria-current={p === page ? "page" : undefined} aria-label={`Trang ${p}`} onClick={() => onPage(p)}>
                  {p}
                </button>
              </li>
            ),
          )}
          <li>
            <button type="button" aria-label="Trang sau" disabled={page >= pages} onClick={() => onPage(page + 1)}>
              ›
            </button>
          </li>
        </ol>
      </nav>
    </div>
  );
}

export interface Column<T> {
  key: string;
  header: string;
  cell: (row: T) => ReactNode;
  className?: string;
}

/**
 * Bảng danh sách có cột STT do hệ thống đánh (R-A5, theo trang) và phân trang số trang.
 * Không truyền `onPage` → bảng không phân trang (STT từ 1), dùng thay `<table className="dt">` tự viết.
 */
export function DataTable<T>({
  caption,
  columns,
  rows,
  rowKey,
  page = 1,
  pageSize = rows.length,
  total = rows.length,
  onPage,
  empty = "Không có dữ liệu phù hợp bộ lọc.",
}: {
  caption: string;
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  page?: number;
  pageSize?: number;
  total?: number;
  onPage?: (p: number) => void;
  empty?: ReactNode;
}) {
  return (
    <div className="card dt-card">
      <div className="tbl-wrap">
        <table className="dt">
          <caption className="sr-only">{caption}</caption>
          <thead>
            <tr>
              <th scope="col">STT</th>
              {columns.map((c) => (
                <th key={c.key} scope="col" className={c.className}>
                  {c.header}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.length ? (
              rows.map((r, i) => (
                <tr key={rowKey(r)}>
                  <td className="stt">{(page - 1) * pageSize + i + 1}</td>
                  {columns.map((c) => (
                    <td key={c.key} className={c.className}>
                      {c.cell(r)}
                    </td>
                  ))}
                </tr>
              ))
            ) : (
              <tr>
                <td colSpan={columns.length + 1} className="empty">
                  {empty}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {onPage ? <Pagination page={page} pageSize={pageSize} total={total} onPage={onPage} /> : null}
    </div>
  );
}

/** Tiêu đề trang + nút thao tác bên phải (nút chính xanh). */
export function PageHeader({ title, badge, actions, children }: { title: ReactNode; badge?: ReactNode; actions?: ReactNode; children?: ReactNode }) {
  return (
    <div className="pg-head">
      <div>
        <h1>
          {title} {badge}
        </h1>
        {children ? <p>{children}</p> : null}
      </div>
      {actions ? <div className="row">{actions}</div> : null}
    </div>
  );
}
