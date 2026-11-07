"use client";

// Cấp Xã — FN-XA-03.05 Phản hồi yêu cầu xác minh giá, FN-XA-03.03 Lịch sử báo giá của xã (BRD Báo giá v1.3; W11.75).
import Link from "next/link";
import { useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useAuth } from "@/auth/AuthProvider";
import { HistoryCalendar, historyPrintable } from "./HistoryCalendar";
import { historyFilters, historyHref, historyRangeProblem, shiftHistoryMonth, type HistoryFilters } from "./history-routes";
import historyStyles from "./HistoryCalendar.module.css";
import { Dialog } from "@/components/Dialog";
import { DataTable, PageHeader, StatusPill, type Column } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { formatDateVi } from "@/kn/grid/decimal";
import {
  ALERT_VIEW_LABEL,
  money,
  pctLabel,
  priceOf,
  TICKET_VIEW_LABEL,
  TICKET_VIEW_TONE,
  vnTime,
  vnToday,
  type PriceAlertRow,
  type PriceHistoryRow,
} from "./api";
import { SheetTable, usePriceTicketApi } from "./SheetLines";

function daysAgo(day: string, n: number): string {
  const d = new Date(`${day}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() - n);
  return d.toISOString().slice(0, 10);
}

/** Yêu cầu xác minh của xã (danh sách + phản hồi). */
export function VerifyScreen() {
  const ta = usePriceTicketApi();
  const today = vnToday();
  const [nonce, setNonce] = useState(0);
  const [onlyOpen, setOnlyOpen] = useState(true);
  const list = useLoad(() => ta.alerts({ from: daysAgo(today, 30), to: today, view: onlyOpen ? "requested" : undefined }), [onlyOpen, nonce]);
  const [pick, setPick] = useState<PriceAlertRow | null>(null);
  const [page, setPage] = useState(1);
  const cols: Column<PriceAlertRow>[] = [
    { key: "day", header: "Ngày báo giá", cell: (r) => formatDateVi(r.day) },
    { key: "item", header: "Mặt hàng", cell: (r) => `${r.item_name} ${r.variety} (${r.unit_name})` },
    { key: "ref", header: "Giá tham chiếu", cell: (r) => money(r.ref_price) },
    { key: "sent", header: "Giá đã gửi", cell: (r) => `${money(r.min)}–${money(r.max)}` },
    { key: "pct", header: "Chênh lệch", cell: (r) => `${r.direction === "high" ? "▲" : "▼"} ${pctLabel(r.change_pct)}` },
    { key: "msg", header: "Nội dung yêu cầu", cell: (r) => r.request?.message ?? r.message ?? "" },
    { key: "due", header: "Hạn phản hồi", cell: (r) => vnTime(r.respond_by) },
    { key: "st", header: "Trạng thái", cell: (r) => <StatusPill tone={r.view === "overdue" ? "bad" : r.view === "requested" ? "warn" : "neutral"}>{ALERT_VIEW_LABEL[r.view]}</StatusPill> },
    {
      key: "act",
      header: "",
      cell: (r) =>
        r.can_act ? (
          <button type="button" className="btn btn-primary" onClick={() => setPick(r)}>
            Phản hồi
          </button>
        ) : r.reply ? (
          <span className="muted">{r.reply.response === "propose" ? `Đề xuất ${money(r.reply.proposed_min)}–${money(r.reply.proposed_max)}` : "Giữ giá"}</span>
        ) : null,
    },
  ];
  return (
    <>
      <PageHeader title="Yêu cầu xác minh">Cấp Tỉnh đề nghị kiểm tra lại giá bất thường. Phản hồi được cả sau giờ khóa, trước hạn phản hồi.</PageHeader>
      <label className="row" style={{ marginBottom: 10 }}>
        <input type="checkbox" checked={onlyOpen} onChange={(e) => setOnlyOpen(e.target.checked)} />
        <span>Chỉ yêu cầu đang chờ phản hồi</span>
      </label>
      <LoadState s={list}>
        {(d) => (
          <DataTable
            caption="Yêu cầu xác minh"
            columns={cols}
            rows={d.alerts.slice((page - 1) * 20, page * 20)}
            rowKey={(r) => `${r.ticket_id}:${r.item_code}`}
            page={page}
            pageSize={20}
            total={d.alerts.length}
            onPage={setPage}
            empty="Không có yêu cầu xác minh."
          />
        )}
      </LoadState>
      {pick ? (
        <RespondDialog
          row={pick}
          onClose={() => setPick(null)}
          onDone={() => {
            setPick(null);
            setNonce((n) => n + 1);
          }}
        />
      ) : null}
    </>
  );
}

function RespondDialog({ row, onClose, onDone }: { row: PriceAlertRow; onClose: () => void; onDone: () => void }) {
  const ta = usePriceTicketApi();
  const act = useAction();
  const [choice, setChoice] = useState<"keep" | "propose">("keep");
  const [lo, setLo] = useState("");
  const [hi, setHi] = useState("");
  const [expl, setExpl] = useState("");
  const send = async () => {
    const min = choice === "propose" ? priceOf(lo) : null;
    const max = choice === "propose" ? priceOf(hi) : null;
    const r = await act.run(() =>
      ta.respond({
        ticket_id: row.ticket_id,
        item_code: row.item_code,
        choice,
        min: min === "bad" ? lo : min,
        max: max === "bad" ? hi : max,
        explanation: expl,
      }),
    );
    if (r) onDone();
  };
  return (
    <Dialog
      title={`Phản hồi xác minh — ${row.item_name}`}
      onClose={onClose}
      actions={
        <>
          <button type="button" className="btn" onClick={onClose}>
            Hủy
          </button>
          <button type="button" className="btn btn-primary" disabled={act.busy || !expl.trim()} onClick={() => void send()}>
            Gửi phản hồi
          </button>
        </>
      }
    >
      <p>
        Ngày {formatDateVi(row.day)} · giá tham chiếu {money(row.ref_price)} · đã gửi {money(row.min)}–{money(row.max)} ({pctLabel(row.change_pct)}) · hạn{" "}
        {vnTime(row.respond_by)}
      </p>
      <p className="muted">{row.request?.message ?? row.message}</p>
      <fieldset className="row">
        <label className="row" style={{ gap: 6 }}>
          <input type="radio" name="choice" checked={choice === "keep"} onChange={() => setChoice("keep")} /> Giữ giá đã gửi
        </label>
        <label className="row" style={{ gap: 6 }}>
          <input type="radio" name="choice" checked={choice === "propose"} onChange={() => setChoice("propose")} /> Đề xuất giá đúng
        </label>
      </fieldset>
      {choice === "propose" ? (
        <div className="row">
          <input className="input" aria-label="Giá đề xuất thấp nhất" placeholder="Thấp nhất" inputMode="decimal" value={lo} onChange={(e) => setLo(e.target.value)} />
          <input className="input" aria-label="Giá đề xuất cao nhất" placeholder="Cao nhất" inputMode="decimal" value={hi} onChange={(e) => setHi(e.target.value)} />
        </div>
      ) : null}
      <label htmlFor="pb-expl">Giải trình (bắt buộc)</label>
      <textarea id="pb-expl" className="input" rows={3} maxLength={1000} value={expl} onChange={(e) => setExpl(e.target.value)} />
      {act.view}
      <p className="muted">Phản hồi không đổi giá trên phiếu — chỉ Cấp Tỉnh hiệu chỉnh.</p>
    </Dialog>
  );
}

/** Lịch sử chỉ đọc; lịch chính thức và trạng thái do API history cung cấp (FN-XA-03.03 v2). */
export function HistoryScreen() {
  const auth = useAuth();
  const search = useSearchParams();
  const filters = historyFilters(search, vnToday());
  const scope = `${auth.principal?.tenantId}:${auth.principal?.subject}:${auth.kn.context?.context_id}:${auth.kn.context?.role}`;
  return <HistoryContent key={`${scope}:${filters.from}:${filters.to}:${filters.point}`} filters={filters} />;
}

function HistoryContent({ filters: f }: { filters: HistoryFilters }) {
  const ta = usePriceTicketApi();
  const router = useRouter();
  const [sel, setSel] = useState<string[]>([]);
  const [open, setOpen] = useState<string | null>(null);
  const [page, setPage] = useState({ status: f.status, value: 1 });
  const invalid = historyRangeProblem(f);
  const list = useLoad(invalid ? null : () => ta.history({ from: f.from, to: f.to, point: f.point || undefined }), [ta]);
  const rows = (list.data?.rows ?? []).filter(r => !f.status || r.view === f.status);
  const selected = sel.filter(id => rows.some(r => r.ticket?.id === id && historyPrintable(r)));
  const navigate = (change: Partial<HistoryFilters>) => router.push(historyHref({ ...f, ...change }), { scroll: false });
  const select = (r: PriceHistoryRow, checked: boolean) => r.ticket && setSel(ids => checked ? [...new Set([...ids, r.ticket!.id])] : ids.filter(id => id !== r.ticket!.id));
  const columns: Column<PriceHistoryRow>[] = [
    { key: "sel", header: "Chọn", cell: r => <input type="checkbox" aria-label={`Chọn phiếu ngày ${formatDateVi(r.day)} — ${TICKET_VIEW_LABEL[r.view]}`} disabled={!historyPrintable(r)} checked={!!r.ticket && selected.includes(r.ticket.id)} onChange={e => select(r, e.target.checked)} /> },
    { key: "day", header: "Ngày báo giá", cell: r => formatDateVi(r.day) },
    { key: "pt", header: "Xã", cell: r => r.point.name },
    { key: "n", header: "Mặt hàng cập nhật (Có giá / Không có hàng)", cell: r => `${r.priced + r.no_goods} (${r.priced} / ${r.no_goods})` },
    { key: "adj", header: "Hiệu chỉnh", cell: r => r.adjusted ? `${r.adjusted} dòng` : "—" },
    { key: "st", header: "Trạng thái", cell: r => <StatusPill tone={TICKET_VIEW_TONE[r.view]}>{TICKET_VIEW_LABEL[r.view]}</StatusPill> },
    { key: "act", header: "", cell: r => r.ticket ? <button type="button" className="btn" aria-label={`Xem phiếu ngày ${formatDateVi(r.day)} — ${TICKET_VIEW_LABEL[r.view]}`} onClick={() => setOpen(r.ticket!.id)}>Xem</button> : null },
  ];
  const currentPage = page.status === f.status ? Math.min(page.value, Math.max(1, Math.ceil(rows.length / 31))) : 1;
  return <>
    <PageHeader title="Lịch sử báo giá" actions={selected.length ? <Link className="btn btn-primary" href={`/kn/bao-gia/in?${new URLSearchParams(selected.map(id => ["ticket", id]))}`}>In phiếu ({selected.length})</Link> : <button type="button" className="btn btn-primary" disabled>In phiếu (0)</button>}>
      Phiếu báo giá của xã theo ngày (tối đa 12 tháng). Phiếu Không nộp, Đã xóa không in được.
    </PageHeader>
    <div className={historyStyles.toolbar} role="group" aria-label="Cách xem lịch sử báo giá">
      <button type="button" className="btn" aria-pressed={f.view === "list"} onClick={() => navigate({ view: "list" })}>Danh sách</button>
      <button type="button" className="btn" aria-pressed={f.view === "calendar"} onClick={() => navigate({ view: "calendar" })}>Lịch</button>
    </div>
    <div className={historyStyles.toolbar}>
      <label htmlFor="pb-from">Từ ngày</label><input id="pb-from" type="date" className="input" value={f.from} aria-invalid={!!invalid} onChange={e => e.target.value && navigate({ from: e.target.value })} />
      <label htmlFor="pb-to">Đến ngày</label><input id="pb-to" type="date" className="input" value={f.to} aria-invalid={!!invalid} onChange={e => e.target.value && navigate({ to: e.target.value })} />
      <label htmlFor="pb-status">Trạng thái</label><select id="pb-status" className="input" value={f.status} onChange={e => navigate({ status: e.target.value as HistoryFilters["status"] })}><option value="">Tất cả</option>{Object.entries(TICKET_VIEW_LABEL).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select>
    </div>
    {invalid ? <p role="alert">{invalid}</p> : <>
      {f.view === "calendar" && <div className={historyStyles.toolbar}>
        <button type="button" className="btn" aria-label="Tháng trước" disabled={f.month <= f.from.slice(0, 7)} onClick={() => navigate({ month: shiftHistoryMonth(f.month, -1) })}>‹</button>
        <h2 className={historyStyles.month} aria-live="polite">Tháng {f.month.slice(5)}/{f.month.slice(0, 4)}</h2>
        <button type="button" className="btn" aria-label="Tháng sau" disabled={f.month >= f.to.slice(0, 7)} onClick={() => navigate({ month: shiftHistoryMonth(f.month, 1) })}>›</button>
      </div>}
      <LoadState s={list}>{() => f.view === "calendar" ? <HistoryCalendar filters={f} today={vnToday()} rows={rows} selected={selected} onSelect={select} onOpen={setOpen} /> : <DataTable caption="Lịch sử báo giá" columns={columns} rows={rows.slice((currentPage - 1) * 31, currentPage * 31)} rowKey={r => `${r.day}:${r.ticket?.id ?? r.point.code}`} page={currentPage} pageSize={31} total={rows.length} onPage={value => setPage({ status: f.status, value })} empty="Không có phiếu phù hợp." />}</LoadState>
    </>}
    {open && <TicketView id={open} onClose={() => setOpen(null)} />}
  </>;
}

/** Chi tiết phiếu (chỉ xem) — hiệu chỉnh của Cấp Tỉnh, trao đổi xác minh. */
export function TicketView({ id, onClose }: { id: string; onClose: () => void }) {
  const ta = usePriceTicketApi();
  const sh = useLoad(() => ta.ticket(id), [id]);
  return (
    <Dialog title="Chi tiết phiếu báo giá" onClose={onClose}>
      <LoadState s={sh}>
        {(d) => (
          <>
            <p>
              {d.point.name} ({d.point.code}) · {formatDateVi(d.day)} · <StatusPill tone={TICKET_VIEW_TONE[d.view]}>{TICKET_VIEW_LABEL[d.view]}</StatusPill>
              {d.ticket?.source === "proxy" ? " · Nộp muộn – nhập thay" : ""}
            </p>
            <SheetTable sheet={d} />
            {d.lines
              .filter((l) => l.alert && l.alert.history.length > 1)
              .map((l) => (
                <div key={l.item_code} style={{ marginTop: 8 }}>
                  <strong>{l.name}</strong>:{" "}
                  {l.alert!.history.map((h) => `${ALERT_VIEW_LABEL[h.status]}${h.message ? ` — ${h.message}` : ""}${h.explanation ? ` — Xã: ${h.explanation}` : ""}`).join(" → ")}
                </div>
              ))}
          </>
        )}
      </LoadState>
    </Dialog>
  );
}
