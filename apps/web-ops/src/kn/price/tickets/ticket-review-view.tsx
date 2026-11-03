import type { LogEntry } from "@/components/patterns/detail";
import { LogPanel } from "@/components/patterns/detail";
import { ALERT_VIEW_LABEL, money, type PriceTicketLog } from "./api";
const OP: Record<string, string> = { save: "Lưu nháp", submit: "Gửi báo giá", proxy_save: "Lưu nháp nhập thay", proxy_submit: "Nộp muộn", correct: "Hiệu chỉnh", redate: "Sửa ngày", delete: "Xóa mềm" };
const pair = (lo: string | null, hi: string | null) => lo === null ? "Không có giá" : `${money(lo)} – ${money(hi)}`;
/** All actor/time/version values below come from ledger API; line versions are per-item, not header versions. */
export function reviewLogEntries(log: PriceTicketLog): LogEntry[] {
  const entries: LogEntry[] = [];
  const heads = [...log.ticket].sort((a, b) => a.version - b.version);
  for (let i = 0; i < heads.length; i++) {
    const h = heads[i]!, prev = heads[i - 1];
    entries.push({ event: `${OP[h.op] ?? h.op} · phiếu v${h.version}`, actor_display: h.actor_name || h.actor_ref, at: h.at, reason: h.reason,
      changes: prev ? [{ field: "Ngày phiếu", old: prev.day, new: h.day }, { field: "Trạng thái", old: prev.status, new: h.status }].filter(x => x.old !== x.new) : [] });
  }
  const lines = [...log.lines].sort((a, b) => a.item_code.localeCompare(b.item_code) || a.version - b.version);
  const previous = new Map<string, PriceTicketLog["lines"][number]>();
  for (const l of lines) {
    const prev = previous.get(l.item_code);
    const before = prev ? pair(prev.adj_min ?? prev.min, prev.adj_max ?? prev.max) : null;
    const after = pair(l.adj_min ?? l.min, l.adj_max ?? l.max);
    entries.push({ event: `${l.item_code} · dòng v${l.version}${l.adj_min !== null ? " · Hiệu chỉnh" : " · Giá Xã gửi"}`, actor_display: l.actor_name || l.actor_ref, at: l.at, reason: l.adj_reason ?? l.note,
      changes: [{ field: "Giá sử dụng", old: before, new: after }, { field: "Giá Xã gửi (lưu nguyên)", old: prev ? pair(prev.min, prev.max) : null, new: pair(l.min, l.max) }] });
    previous.set(l.item_code, l);
  }
  for (const a of log.alerts) { const e = a.entry; entries.push({ event: `${a.item_code} · xác minh v${e.version} · ${ALERT_VIEW_LABEL[e.status] ?? e.status}`, actor_display: a.actor_name || e.actor_ref, at: e.at, reason: [e.message, e.explanation].filter(Boolean).join(" · ") || null,
    changes: e.proposed_min !== null ? [{ field: "Giá Xã đề xuất", old: null, new: pair(e.proposed_min, e.proposed_max) }] : [] }); }
  return entries.sort((a, b) => b.at.localeCompare(a.at));
}
export function TicketReviewLog({ log }: { log: PriceTicketLog }) { return <LogPanel title="Nhật ký phiếu" entries={reviewLogEntries(log)} empty="Chưa có phiên bản nhật ký." />; }
export function ReviewDateField({ value, onChange, error, disabled }: { value: string; onChange: (v: string) => void; error?: string; disabled?: boolean }) {
  return <div className="field"><label htmlFor="review204-day">Ngày phiếu mới <span className="req">*</span></label><input id="review204-day" type="date" className="input" required disabled={disabled} value={value} onChange={e => onChange(e.target.value)} aria-invalid={!!error} aria-describedby={error ? "review204-day-error" : undefined} />{error && <span id="review204-day-error" className="err" role="alert">{error}</span>}</div>;
}
