"use client";

// Cấp Xã — FN-XA-03.05 Phản hồi yêu cầu xác minh giá, FN-XA-03.03 Lịch sử báo giá của xã (BRD Báo giá v1.3; W11.75).
import Link from "next/link";
import { useState } from "react";
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

/** Lịch sử báo giá của xã (danh sách theo ngày, in phiếu lẻ / hàng loạt). */
export function HistoryScreen() {
  const ta = usePriceTicketApi();
  const today = vnToday();
  const [from, setFrom] = useState(daysAgo(today, 30));
  const [to, setTo] = useState(today);
  const [sel, setSel] = useState<string[]>([]);
  const [open, setOpen] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const list = useLoad(() => ta.history({ from, to }), [from, to]);
  const printable = (r: PriceHistoryRow) => !!r.ticket && r.view !== "missed" && r.view !== "deleted";
  const cols: Column<PriceHistoryRow>[] = [
    {
      key: "sel",
      header: "Chọn",
      cell: (r) => (
        <input
          type="checkbox"
          aria-label={`Chọn phiếu ngày ${formatDateVi(r.day)}`}
          disabled={!printable(r)}
          checked={!!r.ticket && sel.includes(r.ticket.id)}
          onChange={(e) => r.ticket && setSel(e.target.checked ? [...sel, r.ticket.id] : sel.filter((x) => x !== r.ticket!.id))}
        />
      ),
    },
    { key: "day", header: "Ngày báo giá", cell: (r) => formatDateVi(r.day) },
    { key: "pt", header: "Xã", cell: (r) => r.point.name },
    { key: "n", header: "Có giá / Không có hàng", cell: (r) => `${r.priced} / ${r.no_goods}` },
    { key: "adj", header: "Hiệu chỉnh", cell: (r) => (r.adjusted ? `${r.adjusted} dòng` : "—") },
    { key: "st", header: "Trạng thái", cell: (r) => <StatusPill tone={TICKET_VIEW_TONE[r.view]}>{TICKET_VIEW_LABEL[r.view]}</StatusPill> },
    {
      key: "act",
      header: "",
      cell: (r) =>
        r.ticket ? (
          <button type="button" className="btn" onClick={() => setOpen(r.ticket!.id)}>
            Xem
          </button>
        ) : null,
    },
  ];
  return (
    <>
      <PageHeader
        title="Lịch sử báo giá"
        actions={
          <Link className={`btn btn-primary${sel.length ? "" : " disabled"}`} aria-disabled={!sel.length} href={sel.length ? `/kn/bao-gia/in?${sel.map((s) => `ticket=${s}`).join("&")}` : "#"}>
            In phiếu ({sel.length})
          </Link>
        }
      >
        Phiếu báo giá của xã theo ngày (tối đa 12 tháng). Phiếu Không nộp, Đã xóa không in được.
      </PageHeader>
      <div className="row" style={{ marginBottom: 10 }}>
        <label htmlFor="pb-from">Từ ngày</label>
        <input id="pb-from" type="date" className="input" style={{ width: 170 }} value={from} onChange={(e) => e.target.value && setFrom(e.target.value)} />
        <label htmlFor="pb-to">Đến ngày</label>
        <input id="pb-to" type="date" className="input" style={{ width: 170 }} value={to} onChange={(e) => e.target.value && setTo(e.target.value)} />
      </div>
      <LoadState s={list}>
        {(d) => (
          <DataTable
            caption="Lịch sử báo giá"
            columns={cols}
            rows={d.rows.slice((page - 1) * 31, page * 31)}
            rowKey={(r) => `${r.day}:${r.ticket?.id ?? "none"}`}
            page={page}
            pageSize={31}
            total={d.rows.length}
            onPage={setPage}
            empty="Không có phiếu."
          />
        )}
      </LoadState>
      {open ? <TicketView id={open} onClose={() => setOpen(null)} /> : null}
    </>
  );
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
