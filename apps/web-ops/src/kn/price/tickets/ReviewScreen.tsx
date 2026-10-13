"use client";

// Cấp Tỉnh — Kiểm duyệt & công bố (BRD Báo giá v1.3; W11.75): phiếu các xã trong ngày (Chi tiết báo giá FN-TI-02.02),
// cảnh báo biến động + yêu cầu xác minh + chốt (FN-TI-02.03), sửa phiếu: hiệu chỉnh / sửa ngày / xóa (FN-TI-02.04), nhập
// thay xã Không nộp (FN-TI-02.09), xuất Excel. Nút "Duyệt bảng giá" thêm ở W11.76.
import Link from "next/link";
import { useState } from "react";
import { Dialog } from "@/components/Dialog";
import { DataTable, PageHeader, StatusPill, type Column } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { formatDateVi } from "@/kn/grid/decimal";
import { saveBlob } from "@/lib/files";
import {
  ALERT_VIEW_LABEL,
  alertResolved,
  defaultRequestMessage,
  lineInput,
  money,
  pctLabel,
  priceOf,
  TICKET_VIEW_LABEL,
  TICKET_VIEW_TONE,
  vnTime,
  vnToday,
  type PriceAlertRow,
  type PriceSheet,
  type PriceTicketRow,
} from "./api";
import { countDrafts, draftsOf, fillFromRef, SheetEditor, SheetTable, usePriceTicketApi, type Drafts } from "./SheetLines";

type Tab = "tickets" | "alerts";

export function ReviewScreen() {
  const ta = usePriceTicketApi();
  const [day, setDay] = useState(vnToday());
  const [mine, setMine] = useState(true);
  const [tab, setTab] = useState<Tab>("tickets");
  const [nonce, setNonce] = useState(0);
  const bump = () => setNonce((n) => n + 1);
  const list = useLoad(() => ta.day({ day, mine }), [day, mine, nonce]);
  const alerts = useLoad(() => ta.alerts({ from: day, to: day, mine }), [day, mine, nonce]);
  const [open, setOpen] = useState<{ point: string; mode: "view" | "proxy" } | null>(null);
  const act = useAction();
  const exportXlsx = () =>
    act.run(async () => {
      const f = await ta.exportDay({ day, mine });
      saveBlob(f.blob, f.name);
    });
  return (
    <>
      <PageHeader
        title="Kiểm duyệt & công bố"
        badge={list.data ? <StatusPill tone={list.data.locked ? "info" : "warn"}>{list.data.locked ? `Đã khóa nhập ${vnTime(list.data.lock_at).slice(0, 5)}` : "Đang nhập"}</StatusPill> : null}
        actions={
          <button type="button" className="btn" disabled={act.busy} onClick={() => void exportXlsx()}>
            Xuất Excel
          </button>
        }
      >
        Phiếu báo giá của các xã, cảnh báo biến động và xác minh với Xã. Duyệt bảng giá ngày ở bước sau (khi mọi cảnh báo đã xử lý).
      </PageHeader>
      <div className="row" style={{ marginBottom: 12 }}>
        <label htmlFor="pr-day">Ngày báo giá</label>
        <input id="pr-day" type="date" className="input" style={{ width: 170 }} value={day} onChange={(e) => e.target.value && setDay(e.target.value)} />
        <label className="row" style={{ gap: 6 }}>
          <input type="checkbox" checked={mine} onChange={(e) => setMine(e.target.checked)} /> Chỉ xã được phân công
        </label>
      </div>
      {act.view}
      <LoadState s={list}>
        {(d) => {
          const n = (v: string) => d.rows.filter((r) => r.view === v).length;
          const open = d.rows.reduce((s, r) => s + r.open_alerts, 0);
          return (
            <div className="row" style={{ marginBottom: 8 }}>
              <StatusPill tone="ok">
                Đã gửi {n("submitted") + n("locked") + n("approved")} / {d.rows.length}
              </StatusPill>
              <StatusPill tone="warn">Nộp muộn {n("late")}</StatusPill>
              <StatusPill tone="bad">
                {d.locked ? "Không nộp" : "Chưa gửi"} {n("missed") + n("not_entered") + n("draft")}
              </StatusPill>
              <StatusPill tone={open ? "bad" : "ok"}>Cảnh báo chưa xử lý {open}</StatusPill>
            </div>
          );
        }}
      </LoadState>
      <div className="dtabs" role="tablist" aria-label="Kiểm duyệt">
        {(
          [
            { id: "tickets", label: "Phiếu các xã" },
            { id: "alerts", label: "Cảnh báo biến động" },
          ] as const
        ).map((t) => (
          <button key={t.id} type="button" role="tab" aria-selected={tab === t.id} onClick={() => setTab(t.id)}>
            {t.label}
          </button>
        ))}
      </div>
      {tab === "tickets" ? (
        <LoadState s={list}>{(d) => <TicketRows rows={d.rows} locked={d.locked} onOpen={(point, mode) => setOpen({ point, mode })} />}</LoadState>
      ) : (
        <LoadState s={alerts}>{(d) => <AlertRows rows={d.alerts} onChanged={bump} onOpen={(point) => setOpen({ point, mode: "view" })} />}</LoadState>
      )}
      {open ? (
        <TicketPanel
          point={open.point}
          day={day}
          proxy={open.mode === "proxy"}
          onClose={() => {
            setOpen(null);
            bump();
          }}
        />
      ) : null}
    </>
  );
}

function TicketRows({ rows, locked, onOpen }: { rows: PriceTicketRow[]; locked: boolean; onOpen: (point: string, mode: "view" | "proxy") => void }) {
  const [page, setPage] = useState(1);
  const cols: Column<PriceTicketRow>[] = [
    { key: "pt", header: "Xã báo giá", cell: (r) => `${r.point.name} (${r.point.code})` },
    { key: "rg", header: "Nhóm vùng", cell: (r) => r.point.region_name },
    { key: "xa", header: "Cán bộ Xã", cell: (r) => r.point.officer_name ?? (r.point.officer_ref ? "—" : "Chưa có cán bộ Xã") },
    { key: "tinh", header: "Cán bộ Tỉnh", cell: (r) => r.point.owner_name ?? (r.point.owner_ref ? "—" : "Chưa phân công") },
    { key: "n", header: "Có giá / KCH / Tổng", cell: (r) => `${r.priced} / ${r.no_goods} / ${r.items}` },
    { key: "al", header: "▲ / ▼", cell: (r) => (r.high + r.low ? `${r.high} / ${r.low}` : "—") },
    { key: "open", header: "Chưa xử lý", cell: (r) => r.open_alerts || "—" },
    { key: "st", header: "Trạng thái", cell: (r) => <StatusPill tone={TICKET_VIEW_TONE[r.view]}>{TICKET_VIEW_LABEL[r.view]}</StatusPill> },
    { key: "at", header: "Gửi lúc", cell: (r) => vnTime(r.ticket?.submitted_at) },
    {
      key: "act",
      header: "",
      cell: (r) =>
        r.ticket && r.view !== "missed" ? (
          <button type="button" className="btn" onClick={() => onOpen(r.point.code, "view")}>
            Xem / sửa
          </button>
        ) : locked ? (
          <button type="button" className="btn btn-primary" onClick={() => onOpen(r.point.code, "proxy")}>
            Nhập thay
          </button>
        ) : null,
    },
  ];
  return (
    <DataTable
      caption="Phiếu các xã"
      columns={cols}
      rows={rows.slice((page - 1) * 30, page * 30)}
      rowKey={(r) => r.point.code}
      page={page}
      pageSize={30}
      total={rows.length}
      onPage={setPage}
      empty="Không có dữ liệu phù hợp."
    />
  );
}

function AlertRows({ rows, onChanged, onOpen }: { rows: PriceAlertRow[]; onChanged: () => void; onOpen: (point: string) => void }) {
  const ta = usePriceTicketApi();
  const act = useAction();
  const [sel, setSel] = useState<string[]>([]);
  const [ask, setAsk] = useState<string | null>(null);
  const [decide, setDecide] = useState<{ row: PriceAlertRow; decision: "keep" | "exclude" } | null>(null);
  const [note, setNote] = useState("");
  const [page, setPage] = useState(1);
  const key = (r: PriceAlertRow) => `${r.ticket_id}:${r.item_code}`;
  const askable = (r: PriceAlertRow) => r.can_act && (r.view === "provisional" || r.view === "pending" || r.view === "responded");
  const decidable = (r: PriceAlertRow) => r.can_act && (r.view === "pending" || r.view === "responded" || r.view === "overdue");
  const chosen = rows.filter((r) => sel.includes(key(r)));
  const cols: Column<PriceAlertRow>[] = [
    {
      key: "sel",
      header: "Chọn",
      cell: (r) => (
        <input
          type="checkbox"
          aria-label={`Chọn ${r.item_name} ${r.point.name}`}
          disabled={!askable(r)}
          checked={sel.includes(key(r))}
          onChange={(e) => setSel(e.target.checked ? [...sel, key(r)] : sel.filter((x) => x !== key(r)))}
        />
      ),
    },
    { key: "item", header: "Mặt hàng", cell: (r) => `${r.item_name} ${r.variety}` },
    { key: "grp", header: "Nhóm hàng", cell: (r) => r.group_name },
    { key: "pt", header: "Xã", cell: (r) => r.point.name },
    { key: "ref", header: "Giá kỳ trước", cell: (r) => money(r.ref_price) },
    { key: "p", header: "Giá kỳ này", cell: (r) => money(r.price) },
    { key: "pct", header: "Chênh lệch", cell: (r) => <span className="pb-flag">{`${r.direction === "high" ? "▲" : "▼"} ${pctLabel(r.change_pct)}`}</span> },
    { key: "note", header: "Ghi chú của Xã", cell: (r) => r.note ?? "" },
    {
      key: "st",
      header: "Trạng thái",
      cell: (r) => <StatusPill tone={alertResolved(r.view) ? "ok" : r.view === "overdue" ? "bad" : "warn"}>{ALERT_VIEW_LABEL[r.view]}</StatusPill>,
    },
    { key: "due", header: "Hạn phản hồi", cell: (r) => vnTime(r.respond_by) },
    {
      key: "reply",
      header: "Phản hồi của Xã",
      cell: (r) =>
        r.reply ? `${r.reply.response === "propose" ? `Đề xuất ${money(r.reply.proposed_min)}–${money(r.reply.proposed_max)}` : "Giữ giá"} — ${r.reply.explanation ?? ""}` : "—",
    },
    {
      key: "act",
      header: "",
      cell: (r) => (
        <div className="row" style={{ gap: 4 }}>
          {r.can_act && !alertResolved(r.view) ? (
            <button type="button" className="btn" onClick={() => onOpen(r.point.code)}>
              Hiệu chỉnh
            </button>
          ) : null}
          {decidable(r) ? (
            <>
              <button type="button" className="btn" onClick={() => setDecide({ row: r, decision: "keep" })}>
                Giữ nguyên
              </button>
              <button type="button" className="btn" onClick={() => setDecide({ row: r, decision: "exclude" })}>
                Loại
              </button>
            </>
          ) : null}
        </div>
      ),
    },
  ];
  const sendAsk = async () => {
    const r = await act.run(
      () => ta.request({ alerts: chosen.map((x) => ({ ticket_id: x.ticket_id, item_code: x.item_code })), message: ask ?? "" }),
      (w) => `Đã gửi yêu cầu xác minh ${w.written} cảnh báo.`,
    );
    if (r) {
      setAsk(null);
      setSel([]);
      onChanged();
    }
  };
  const sendDecide = async () => {
    if (!decide) return;
    const r = await act.run(
      () => ta.decide({ ticket_id: decide.row.ticket_id, item_code: decide.row.item_code, decision: decide.decision, note }),
      decide.decision === "keep" ? "Đã giữ nguyên." : "Đã loại khỏi bảng giá.",
    );
    if (r) {
      setDecide(null);
      setNote("");
      onChanged();
    }
  };
  return (
    <>
      {act.view}
      <div className="row" style={{ marginBottom: 8 }}>
        <button
          type="button"
          className="btn btn-primary"
          disabled={!chosen.length}
          onClick={() => setAsk(chosen.length === 1 ? defaultRequestMessage(chosen[0]!.item_name, chosen[0]!.point.name, chosen[0]!.change_pct) : "Giá chênh lệch bất thường so với giá tham chiếu, đề nghị xã kiểm tra lại")}
        >
          Yêu cầu xác minh ({chosen.length})
        </button>
      </div>
      <DataTable
        caption="Cảnh báo biến động"
        columns={cols}
        rows={rows.slice((page - 1) * 30, page * 30)}
        rowKey={key}
        page={page}
        pageSize={30}
        total={rows.length}
        onPage={setPage}
        empty="Không có cảnh báo."
      />
      {ask !== null ? (
        <Dialog
          title="Yêu cầu xác minh"
          onClose={() => setAsk(null)}
          actions={
            <>
              <button type="button" className="btn" onClick={() => setAsk(null)}>
                Hủy
              </button>
              <button type="button" className="btn btn-primary" disabled={act.busy || !ask.trim()} onClick={() => void sendAsk()}>
                Gửi
              </button>
            </>
          }
        >
          <label htmlFor="pr-ask">Nội dung (bắt buộc)</label>
          <textarea id="pr-ask" className="input" rows={3} maxLength={1000} value={ask} onChange={(e) => setAsk(e.target.value)} />
          {act.view}
        </Dialog>
      ) : null}
      {decide ? (
        <Dialog
          title={decide.decision === "keep" ? "Giữ nguyên giá" : "Loại khỏi bảng giá"}
          onClose={() => setDecide(null)}
          actions={
            <>
              <button type="button" className="btn" onClick={() => setDecide(null)}>
                Hủy
              </button>
              <button type="button" className="btn btn-primary" disabled={act.busy || !note.trim()} onClick={() => void sendDecide()}>
                Xác nhận
              </button>
            </>
          }
        >
          <p>
            {decide.row.item_name} tại {decide.row.point.name}: {money(decide.row.ref_price)} → {money(decide.row.price)} ({pctLabel(decide.row.change_pct)})
          </p>
          <label htmlFor="pr-note">{decide.decision === "keep" ? "Ghi chú (bắt buộc)" : "Lý do (bắt buộc)"}</label>
          <textarea
            id="pr-note"
            className="input"
            rows={3}
            maxLength={1000}
            value={note}
            placeholder={decide.row.view === "overdue" ? "Xã không phản hồi" : ""}
            onChange={(e) => setNote(e.target.value)}
          />
          {act.view}
        </Dialog>
      ) : null}
    </>
  );
}

/** Phiếu của một xã một ngày: xem, hiệu chỉnh, sửa ngày, xóa, nhật ký; hoặc nhập thay. */
function TicketPanel({ point, day, proxy, onClose }: { point: string; day: string; proxy: boolean; onClose: () => void }) {
  const ta = usePriceTicketApi();
  const sh = useLoad(() => ta.sheet({ point, day }), [point, day]);
  return (
    <Dialog title={proxy ? "Nhập thay báo giá — Nộp muộn" : "Phiếu báo giá"} onClose={onClose}>
      <LoadState s={sh}>{(d) => (proxy || (!d.ticket && d.can.proxy) ? <ProxyForm initial={d} onDone={onClose} /> : <Review initial={d} onDone={onClose} />)}</LoadState>
    </Dialog>
  );
}

function ProxyForm({ initial, onDone }: { initial: PriceSheet; onDone: () => void }) {
  const ta = usePriceTicketApi();
  const [sheet, setSheet] = useState(initial);
  const [drafts, setDrafts] = useState<Drafts>(() => draftsOf(initial));
  const [reason, setReason] = useState(initial.ticket?.reason ?? "");
  const act = useAction();
  const c = countDrafts(sheet, drafts);
  const save = async (submit: boolean) => {
    const r = await act.run(() =>
      ta.proxy({
        point: sheet.point.code,
        day: sheet.day,
        version: sheet.ticket?.version ?? 0,
        submit,
        reason,
        lines: sheet.lines.map((l) => lineInput(l.item_code, drafts[l.item_code]!)),
      }),
    );
    if (r) {
      if (submit) onDone();
      else {
        setSheet(r);
        setDrafts(draftsOf(r));
      }
    }
  };
  if (!sheet.can.proxy) return <p>Không nhập thay được: chỉ xã Không nộp, sau giờ khóa, bảng giá chưa duyệt, xã thuộc phân công của bạn hoặc chưa phân công.</p>;
  return (
    <>
      <p>
        {sheet.point.name} ({sheet.point.code}) · {formatDateVi(sheet.day)} · Có giá {c.priced} · Không có hàng {c.noGoods} · Còn trống {c.blank} / {c.total}
      </p>
      <label htmlFor="px-reason">Lý do nộp muộn (bắt buộc)</label>
      <input id="px-reason" className="input" maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Xã gửi số liệu qua Zalo / điện thoại…" />
      <div className="row" style={{ margin: "8px 0" }}>
        <button type="button" className="btn" onClick={() => setDrafts(fillFromRef(sheet, drafts))}>
          Lấy giá kỳ trước
        </button>
        <button type="button" className="btn" disabled={act.busy || !reason.trim() || c.invalid > 0} onClick={() => void save(false)}>
          Lưu nháp
        </button>
        <button type="button" className="btn btn-primary" disabled={act.busy || !reason.trim() || c.blank > 0 || c.invalid > 0} onClick={() => void save(true)}>
          Lưu — Nộp muộn
        </button>
      </div>
      {act.view}
      <SheetEditor sheet={sheet} drafts={drafts} onChange={(code, d) => setDrafts((x) => ({ ...x, [code]: d }))} />
    </>
  );
}

function Review({ initial, onDone }: { initial: PriceSheet; onDone: () => void }) {
  const ta = usePriceTicketApi();
  const [sheet, setSheet] = useState(initial);
  const [mode, setMode] = useState<"view" | "correct" | "redate" | "delete" | "log">("view");
  const [adj, setAdj] = useState<Record<string, { min: string; max: string }>>({});
  const [reason, setReason] = useState("");
  const [newDay, setNewDay] = useState("");
  const act = useAction();
  const t = sheet.ticket;
  const tid = t?.id;
  const log = useLoad(mode === "log" && tid ? () => ta.log(tid) : null, [mode, tid]);
  if (!t) return <p>Xã chưa có phiếu cho ngày này.</p>;
  const doCorrect = async () => {
    const lines = Object.entries(adj)
      .filter(([, v]) => v.min.trim() && v.max.trim())
      .map(([item_code, v]) => {
        const lo = priceOf(v.min);
        const hi = priceOf(v.max);
        return { item_code, min: typeof lo === "string" && lo !== "bad" ? lo : v.min, max: typeof hi === "string" && hi !== "bad" ? hi : v.max };
      });
    const r = await act.run(() => ta.correct(t.id, { version: t.version, reason, lines }), "Đã lưu hiệu chỉnh.");
    if (r) {
      setSheet(r);
      setAdj({});
      setReason("");
      setMode("view");
    }
  };
  const doRedate = async () => {
    const r = await act.run(() => ta.redate(t.id, { version: t.version, day: newDay, reason }), "Đã sửa ngày phiếu.");
    if (r) onDone();
  };
  const doDelete = async () => {
    const r = await act.run(() => ta.remove(t.id, { version: t.version, reason }), "Đã xóa phiếu.");
    if (r) onDone();
  };
  return (
    <>
      <p>
        {sheet.point.name} ({sheet.point.code}) · {formatDateVi(sheet.day)} · <StatusPill tone={TICKET_VIEW_TONE[sheet.view]}>{TICKET_VIEW_LABEL[sheet.view]}</StatusPill>
        {t.source === "proxy" ? ` · Nộp muộn – nhập thay${t.actor_name ? ` bởi ${t.actor_name}` : ""}` : ""} · gửi {vnTime(t.submitted_at)}
      </p>
      <div className="row" style={{ marginBottom: 8 }}>
        {sheet.can.correct ? (
          <button type="button" className="btn" onClick={() => setMode("correct")}>
            Hiệu chỉnh
          </button>
        ) : null}
        {sheet.can.redate ? (
          <button type="button" className="btn" onClick={() => setMode("redate")}>
            Sửa ngày phiếu
          </button>
        ) : null}
        {sheet.can.delete ? (
          <button type="button" className="btn" onClick={() => setMode("delete")}>
            Xóa phiếu
          </button>
        ) : null}
        <button type="button" className="btn" onClick={() => setMode(mode === "log" ? "view" : "log")}>
          Nhật ký
        </button>
        <Link className="btn" href={`/kn/bao-gia/in?ticket=${t.id}`}>
          In phiếu
        </Link>
      </div>
      {act.view}
      {mode === "correct" ? (
        <div className="card" style={{ padding: 10, marginBottom: 8 }}>
          <p className="muted">Nhập giá mới cho dòng cần hiệu chỉnh — giá cán bộ Xã gửi được giữ để in phiếu, đối chiếu.</p>
          {sheet.lines
            .filter((l) => l.min && !l.no_goods)
            .map((l) => {
              const v = adj[l.item_code] ?? { min: "", max: "" };
              const set = (x: Partial<typeof v>) => setAdj({ ...adj, [l.item_code]: { ...v, ...x } });
              return (
                <div key={l.item_code} className="row" style={{ marginBottom: 4 }}>
                  <span style={{ minWidth: 180 }}>
                    {l.name} <span className="muted">({money(l.min)}–{money(l.max)})</span>
                  </span>
                  <input className="input" style={{ width: 120 }} aria-label={`Giá mới thấp nhất ${l.name}`} value={v.min} onChange={(e) => set({ min: e.target.value })} />
                  <input className="input" style={{ width: 120 }} aria-label={`Giá mới cao nhất ${l.name}`} value={v.max} onChange={(e) => set({ max: e.target.value })} />
                  {l.alert?.reply?.response === "propose" ? (
                    <button type="button" className="btn" onClick={() => set({ min: l.alert!.reply!.proposed_min ?? "", max: l.alert!.reply!.proposed_max ?? "" })}>
                      Dùng giá Xã đề xuất
                    </button>
                  ) : null}
                </div>
              );
            })}
          <label htmlFor="rv-reason">Lý do (bắt buộc)</label>
          <input id="rv-reason" className="input" maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
          <div className="row" style={{ marginTop: 8 }}>
            <button type="button" className="btn" onClick={() => setMode("view")}>
              Hủy
            </button>
            <button type="button" className="btn btn-primary" disabled={act.busy || !reason.trim()} onClick={() => void doCorrect()}>
              Lưu thay đổi
            </button>
          </div>
        </div>
      ) : null}
      {mode === "redate" || mode === "delete" ? (
        <div className="card" style={{ padding: 10, marginBottom: 8 }}>
          {mode === "redate" ? (
            <>
              <label htmlFor="rv-day">Ngày phiếu đúng</label>
              <input id="rv-day" type="date" className="input" style={{ width: 170 }} value={newDay} onChange={(e) => setNewDay(e.target.value)} />
            </>
          ) : (
            <p>Xóa mềm phiếu sai / trùng — phiếu “Đã xóa” không tính vào bảng giá, thống kê.</p>
          )}
          <label htmlFor="rv-reason2">Lý do (bắt buộc)</label>
          <input id="rv-reason2" className="input" maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
          <div className="row" style={{ marginTop: 8 }}>
            <button type="button" className="btn" onClick={() => setMode("view")}>
              Hủy
            </button>
            <button
              type="button"
              className="btn btn-primary"
              disabled={act.busy || !reason.trim() || (mode === "redate" && !newDay)}
              onClick={() => void (mode === "redate" ? doRedate() : doDelete())}
            >
              {mode === "redate" ? "Sửa ngày" : "Xóa phiếu"}
            </button>
          </div>
        </div>
      ) : null}
      {mode === "log" ? (
        <LoadState s={log}>
          {(lg) => (
            <ul>
              {lg.ticket.map((h) => (
                <li key={`t${h.version}`}>
                  {vnTime(h.at)} · {h.actor_name ?? h.actor_ref.slice(0, 8)} · {h.op}
                  {h.reason ? ` — ${h.reason}` : ""}
                </li>
              ))}
              {lg.lines
                .filter((l) => l.adj_min)
                .map((l) => (
                  <li key={`l${l.item_code}${l.version}`}>
                    {vnTime(l.at)} · {l.actor_name ?? l.actor_ref.slice(0, 8)} · {l.item_code}: {money(l.min)}–{money(l.max)} → {money(l.adj_min)}–{money(l.adj_max)} — {l.adj_reason}
                  </li>
                ))}
              {lg.alerts.map((a) => (
                <li key={`a${a.item_code}${a.entry.version}`}>
                  {vnTime(a.entry.at)} · {a.actor_name ?? a.entry.actor_ref.slice(0, 8)} · {a.item_code}: {ALERT_VIEW_LABEL[a.entry.status]}
                  {a.entry.message ? ` — ${a.entry.message}` : ""}
                  {a.entry.explanation ? ` — Xã: ${a.entry.explanation}` : ""}
                </li>
              ))}
            </ul>
          )}
        </LoadState>
      ) : null}
      <SheetTable sheet={sheet} />
    </>
  );
}
