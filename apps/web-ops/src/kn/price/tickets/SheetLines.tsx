"use client";

// Dòng giá của phiếu báo giá (W11.75): bảng soạn (Xã nhập — FN-XA-03.01, Cấp Tỉnh nhập thay — FN-TI-02.09) và bảng xem
// (giá Xã gửi, giá hiệu chỉnh, cảnh báo — FN-TI-02.02, 02.04). Trên điện thoại mỗi mặt hàng là một thẻ (globals.css .pb-line).
import { useEffect, useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { StatusPill } from "@/components/patterns/list";
import {
  ALERT_VIEW_LABEL,
  byGroup,
  createPriceTicketApi,
  draftOf,
  draftProblem,
  evalDraft,
  filled,
  money,
  pctLabel,
  priceOf,
  type Draft,
  type PriceSheet,
  type PriceSheetLine,
  type PriceTicketApi,
} from "./api";

export function usePriceTicketApi(): PriceTicketApi {
  const { api } = useAuth();
  return useMemo(() => createPriceTicketApi(api), [api]);
}

export type Drafts = Record<string, Draft>;

/** Bản soạn từ phiếu máy chủ. */
export function draftsOf(sh: PriceSheet): Drafts {
  const out: Drafts = {};
  for (const l of sh.lines) out[l.item_code] = draftOf(l);
  return out;
}

/** Đếm Có giá / Không có hàng / Còn trống, số dòng bất thường, dòng bất thường thiếu ghi chú. */
export function countDrafts(sh: PriceSheet, d: Drafts) {
  let priced = 0;
  let noGoods = 0;
  let blank = 0;
  let abnormal = 0;
  let needNote = 0;
  let invalid = 0;
  for (const l of sh.lines) {
    const x = d[l.item_code] ?? draftOf(l);
    if (draftProblem(x)) invalid++;
    if (x.noGoods) noGoods++;
    else if (filled(x)) priced++;
    else blank++;
    const ev = evalDraft(x, l);
    if (ev?.direction) {
      abnormal++;
      if (!x.note.trim()) needNote++;
    }
  }
  return { priced, noGoods, blank, abnormal, needNote, invalid, total: sh.lines.length };
}

/** "Lấy giá kỳ trước": điền giá tham chiếu vào ô còn trống, không ghi đè ô đã nhập (BR-05, AC-06). */
export function fillFromRef(sh: PriceSheet, d: Drafts): Drafts {
  const out = { ...d };
  for (const l of sh.lines) {
    const x = out[l.item_code] ?? draftOf(l);
    if (!l.ref || x.noGoods || priceOf(x.min) !== null || priceOf(x.max) !== null) continue;
    out[l.item_code] = { ...x, min: l.ref.min, max: l.ref.max };
  }
  return out;
}

/** Nhãn ▲ / ▼ ± Δ%. */
export function Flag({ direction, pct }: { direction: string; pct: string }) {
  if (direction === "high") return <span className="pb-flag">▲ Cao bất thường {pctLabel(pct)}</span>;
  if (direction === "low") return <span className="pb-flag">▼ Thấp bất thường {pctLabel(pct)}</span>;
  return <span className="muted">{pctLabel(pct)}</span>;
}

function RefCell({ l }: { l: PriceSheetLine }) {
  if (!l.ref) return <span className="muted">Giá mới</span>;
  return (
    <span title={`Ngày ${l.ref.day}`}>
      {money(l.ref.mid)}
      {l.ref.min !== l.ref.max ? (
        <span className="muted" style={{ fontSize: 12 }}>
          {" "}
          ({money(l.ref.min)}–{money(l.ref.max)})
        </span>
      ) : null}
    </span>
  );
}

/** Bảng soạn giá — mỗi mặt hàng một dòng, gom theo nhóm hàng. */
export function SheetEditor({
  sheet,
  drafts,
  onChange,
  readOnly,
  showMissing,
}: {
  sheet: PriceSheet;
  drafts: Drafts;
  onChange: (code: string, d: Draft) => void;
  readOnly?: boolean;
  showMissing?: boolean;
}) {
  return (
    <div>
      {byGroup(sheet.lines).map((g) => (
        <section key={g.code} aria-label={g.name}>
          <div className="pb-group">{g.name}</div>
          <div className="pb-line pb-head" aria-hidden="true">
            <span>Mặt hàng</span>
            <span>Giá kỳ trước</span>
            <span>Giá thấp nhất</span>
            <span>Giá cao nhất</span>
            <span>Không có hàng</span>
            <span>Ghi chú biến động</span>
          </div>
          {g.rows.map((l) => {
            const d = drafts[l.item_code] ?? draftOf(l);
            const ev = evalDraft(d, l);
            const bad = draftProblem(d);
            const miss = showMissing && !filled(d);
            const id = `pb-${l.item_code}`;
            return (
              <div key={l.item_code} className={`pb-line${ev?.direction || bad ? " pb-bad" : miss ? " pb-miss" : ""}`} data-testid={id}>
                <div className="pb-item">
                  <strong>{l.name}</strong> <span className="muted">{l.variety}</span> · {l.unit_name}{" "}
                  {l.kind === "basic" ? <StatusPill tone="info">Cơ bản</StatusPill> : null}
                  {ev?.direction ? (
                    <div>
                      <Flag direction={ev.direction} pct={ev.pct} /> so kỳ trước
                    </div>
                  ) : null}
                  {bad ? <div className="pb-flag">{bad}</div> : null}
                  {l.alert && l.alert.view !== "cancelled" ? (
                    <div style={{ fontSize: 12.5 }}>Cảnh báo: {ALERT_VIEW_LABEL[l.alert.view]}</div>
                  ) : null}
                </div>
                <div className="pb-ref">
                  <span className="muted" style={{ fontSize: 12 }}>
                    Kỳ trước:{" "}
                  </span>
                  <RefCell l={l} />
                </div>
                <input
                  className="input"
                  aria-label={`Giá thấp nhất ${l.name}`}
                  id={`${id}-min`}
                  inputMode="decimal"
                  placeholder="Thấp nhất"
                  value={d.min}
                  disabled={readOnly || d.noGoods}
                  onChange={(e) => onChange(l.item_code, { ...d, min: e.target.value })}
                />
                <input
                  className="input"
                  aria-label={`Giá cao nhất ${l.name}`}
                  inputMode="decimal"
                  placeholder="Cao nhất"
                  value={d.max}
                  disabled={readOnly || d.noGoods}
                  onChange={(e) => onChange(l.item_code, { ...d, max: e.target.value })}
                />
                <label className="row" style={{ gap: 6 }}>
                  <input
                    type="checkbox"
                    checked={d.noGoods}
                    disabled={readOnly}
                    aria-label={`Không có hàng ${l.name}`}
                    onChange={(e) => onChange(l.item_code, { ...d, noGoods: e.target.checked, min: e.target.checked ? "" : d.min, max: e.target.checked ? "" : d.max })}
                  />
                  <span>Không có hàng</span>
                </label>
                <input
                  className="input pb-note"
                  aria-label={`Ghi chú biến động ${l.name}`}
                  placeholder={ev?.direction ? "Bắt buộc với dòng bất thường" : "Ghi chú"}
                  value={d.note}
                  maxLength={500}
                  disabled={readOnly}
                  aria-invalid={!!ev?.direction && !d.note.trim()}
                  onChange={(e) => onChange(l.item_code, { ...d, note: e.target.value })}
                />
              </div>
            );
          })}
        </section>
      ))}
    </div>
  );
}

/** Bảng xem phiếu (Cấp Tỉnh, lịch sử): giá Xã gửi, giá hiệu chỉnh, Δ%, cảnh báo. */
export function SheetTable({ sheet, onPick }: { sheet: PriceSheet; onPick?: (l: PriceSheetLine) => void }) {
  return (
    <div className="tbl-wrap">
      <table className="dt">
        <caption className="sr-only">Dòng giá của phiếu</caption>
        <thead>
          <tr>
            <th scope="col">Nhóm hàng</th>
            <th scope="col">Mặt hàng</th>
            <th scope="col">ĐVT</th>
            <th scope="col">Giá tham chiếu</th>
            <th scope="col">Thấp nhất</th>
            <th scope="col">Cao nhất</th>
            <th scope="col">Δ%</th>
            <th scope="col">Hiệu chỉnh</th>
            <th scope="col">Ghi chú</th>
            <th scope="col">Cảnh báo</th>
          </tr>
        </thead>
        <tbody>
          {sheet.lines.map((l) => {
            const red = l.eval?.direction === "high" || l.eval?.direction === "low";
            return (
              <tr key={l.item_code} style={red ? { background: "var(--danger-bg)" } : undefined} onClick={onPick ? () => onPick(l) : undefined}>
                <td>{l.group_name}</td>
                <td>
                  {l.name} <span className="muted">{l.variety}</span>
                </td>
                <td>{l.unit_name}</td>
                <td>{l.ref ? money(l.ref.mid) : <span className="muted">Giá mới</span>}</td>
                <td>{l.no_goods ? "Không có hàng" : money(l.min)}</td>
                <td>{l.no_goods ? "" : money(l.max)}</td>
                <td>{l.eval ? <Flag direction={l.eval.direction} pct={l.eval.change_pct} /> : "—"}</td>
                <td>
                  {l.adj_min ? (
                    <span title={l.adj_reason ?? ""}>
                      {money(l.adj_min)}–{money(l.adj_max)}
                    </span>
                  ) : (
                    "—"
                  )}
                </td>
                <td>{l.note ?? ""}</td>
                <td>{l.alert && l.alert.view !== "cancelled" ? ALERT_VIEW_LABEL[l.alert.view] : "—"}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

/** "Khóa lúc 12:00 · còn 1 giờ 20 phút" — đếm ngược theo đồng hồ máy chủ lúc tải (now) + thời gian trôi trên trình duyệt. */
export function LockCountdown({ lockAt, now, locked }: { lockAt: string | null; now: string; locked: boolean }) {
  const [t0] = useState(() => Date.now());
  const [nowMs, setNowMs] = useState(t0);
  useEffect(() => {
    const t = setInterval(() => setNowMs(Date.now()), 30_000);
    return () => clearInterval(t);
  }, []);
  if (!lockAt) return <StatusPill tone="neutral">Không phải ngày báo giá</StatusPill>;
  const hhmm = new Date(Date.parse(lockAt) + 7 * 3_600_000).toISOString().slice(11, 16);
  const left = Date.parse(lockAt) - (Date.parse(now) + (nowMs - t0));
  if (locked || left <= 0) return <StatusPill tone="info">Đã khóa nhập {hhmm}</StatusPill>;
  const m = Math.floor(left / 60_000);
  return (
    <StatusPill tone="warn">
      Khóa lúc {hhmm} · còn {m >= 60 ? `${Math.floor(m / 60)} giờ ` : ""}
      {m % 60} phút
    </StatusPill>
  );
}
