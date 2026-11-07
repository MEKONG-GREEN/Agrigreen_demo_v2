import { StatusPill } from "@/components/patterns/list";
import { formatDateVi } from "@/kn/grid/decimal";
import { TICKET_VIEW_LABEL, TICKET_VIEW_TONE, type PriceHistoryRow } from "./api";
import { historyMonthDays, type HistoryFilters } from "./history-routes";
import styles from "./HistoryCalendar.module.css";

export function historyPrintable(row: PriceHistoryRow): boolean {
  return !!row.ticket && row.view !== "missed" && row.view !== "deleted";
}
export function HistoryCalendar({ filters: f, today, rows, selected, onSelect, onOpen }: {
  filters: HistoryFilters; today: string; rows: PriceHistoryRow[]; selected: string[];
  onSelect: (row: PriceHistoryRow, checked: boolean) => void; onOpen: (id: string) => void;
}) {
  const byDay = new Map<string, PriceHistoryRow[]>();
  for (const row of rows) byDay.set(row.day, [...(byDay.get(row.day) ?? []), row]);
  return <section aria-label="Lịch báo giá tháng">
    <div className={styles.weekdays} aria-hidden="true">{["Thứ Hai", "Thứ Ba", "Thứ Tư", "Thứ Năm", "Thứ Sáu", "Thứ Bảy", "Chủ nhật"].map(d => <span key={d}>{d}</span>)}</div>
    <ol className={styles.days}>
      {historyMonthDays(f.month).map((day, i) => !day ? <li key={`pad-${i}`} className={styles.padding} aria-hidden="true" /> :
        <li key={day} className={`${styles.day}${day < f.from || day > f.to ? ` ${styles.outside}` : ""}`}>
          <time dateTime={day} aria-current={day === today ? "date" : undefined}>{formatDateVi(day)}</time>
          {(day < f.from || day > f.to) ? <p className="muted">Ngoài khoảng chọn</p> : (byDay.get(day) ?? []).map(r =>
            <div className={styles.entry} key={r.ticket?.id ?? `${r.day}:${r.point.code}`}>
              {r.ticket ? <button type="button" className="btn" aria-label={`Xem phiếu ngày ${formatDateVi(day)} — ${TICKET_VIEW_LABEL[r.view]}`} onClick={() => onOpen(r.ticket!.id)}>{r.point.name}</button> : <span>{r.point.name}</span>}
              <span>{r.priced + r.no_goods} mặt hàng cập nhật</span>
              <StatusPill tone={TICKET_VIEW_TONE[r.view]}>{TICKET_VIEW_LABEL[r.view]}</StatusPill>
              {r.adjusted > 0 && <span>{r.adjusted} dòng hiệu chỉnh</span>}
              <label><input type="checkbox" aria-label={`Chọn phiếu ngày ${formatDateVi(day)} — ${TICKET_VIEW_LABEL[r.view]}`} disabled={!historyPrintable(r)} checked={!!r.ticket && selected.includes(r.ticket.id)} onChange={e => onSelect(r, e.target.checked)} />Chọn in</label>
            </div>)}
        </li>)}
    </ol>
    {!rows.some(r => r.day.startsWith(f.month)) && <p role="status">Không có phiếu phù hợp trong tháng.</p>}
  </section>;
}
