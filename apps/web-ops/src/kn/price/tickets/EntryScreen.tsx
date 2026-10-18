"use client";

// FN-XA-03.01 — Nhập / sửa báo giá theo ngày, trước giờ khóa (BRD Báo giá nông sản v1.3; W11.75, cap/price). Cán bộ Xã:
// mặt hàng của xã theo nhóm hàng, giá kỳ trước (CT-02), "Lấy giá kỳ trước", lưu nháp, gửi / gửi lại; dòng bất thường tô
// đỏ kèm ▲ / ▼ Δ% và ghi chú bắt buộc; sau giờ khóa / đã duyệt chỉ xem. Dùng được trên điện thoại (thẻ theo mặt hàng).
import Link from "next/link";
import { useState } from "react";
import { Dialog } from "@/components/Dialog";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { ProposeItemDialog, usePriceItemApi } from "@/kn/price/items/ItemsScreen";
import { lineInput, TICKET_VIEW_LABEL, TICKET_VIEW_TONE, type PriceSheet } from "./api";
import { countDrafts, draftsOf, fillFromRef, LockCountdown, SheetEditor, usePriceTicketApi, type Drafts } from "./SheetLines";

export function EntryScreen() {
  const ta = usePriceTicketApi();
  const mine = useLoad(() => ta.day({}), []);
  const [point, setPoint] = useState("");
  const rows = mine.data?.rows ?? [];
  const chosen = point || (rows.length === 1 ? rows[0]!.point.code : "");
  return (
    <>
      <PageHeader title="Nhập báo giá">
        Cập nhật giá thương lái thấp nhất, cao nhất cho mọi mặt hàng của xã (hoặc đánh dấu “Không có hàng”) và gửi trước giờ khóa.
      </PageHeader>
      <LoadState s={mine}>
        {(d) =>
          d.rows.length === 0 ? (
            <div className="alert alert-warn" role="status">
              Tài khoản chưa được gán xã báo giá cho ngày hôm nay — liên hệ Admin (Xã báo giá › Phân công).
            </div>
          ) : (
            <>
              {d.rows.length > 1 ? (
                <div className="row" style={{ marginBottom: 12 }}>
                  <label htmlFor="pb-point">Xã báo giá</label>
                  <select id="pb-point" className="input" style={{ width: 260 }} value={chosen} onChange={(e) => setPoint(e.target.value)}>
                    <option value="">— Chọn xã —</option>
                    {d.rows.map((r) => (
                      <option key={r.point.code} value={r.point.code}>
                        {r.point.name} ({r.point.code}) · {TICKET_VIEW_LABEL[r.view]}
                      </option>
                    ))}
                  </select>
                </div>
              ) : null}
              {chosen ? <EntrySheet key={chosen} point={chosen} /> : null}
            </>
          )
        }
      </LoadState>
    </>
  );
}

function EntrySheet({ point }: { point: string }) {
  const ta = usePriceTicketApi();
  const loaded = useLoad(() => ta.sheet({ point }), [point]);
  return <LoadState s={loaded}>{(d) => <EntryBody point={point} initial={d} />}</LoadState>;
}

function EntryBody({ point, initial }: { point: string; initial: PriceSheet }) {
  const ta = usePriceTicketApi();
  const [sheet, setSheet] = useState<PriceSheet>(initial);
  const [drafts, setDrafts] = useState<Drafts>(() => draftsOf(initial));
  const [confirm, setConfirm] = useState(false);
  const [propose, setPropose] = useState(false);
  const [tried, setTried] = useState(false);
  const act = useAction();
  const c = countDrafts(sheet, drafts);
  const editable = sheet.can.enter;
  const submitted = sheet.ticket?.status === "submitted";
  const requested = sheet.lines.filter((l) => l.alert?.view === "requested").length;
  const save = async (submit: boolean) => {
    setTried(submit);
    const body = {
      point,
      day: sheet.day,
      version: sheet.ticket?.version ?? 0,
      submit,
      lines: sheet.lines.map((l) => lineInput(l.item_code, drafts[l.item_code]!)),
    };
    const r = await act.run(() => ta.save(body), submit ? "Đã gửi báo giá." : "Đã lưu nháp.");
    if (r) {
      setSheet(r);
      setDrafts(draftsOf(r));
      setConfirm(false);
    }
  };
  return (
    <>
      <div className="row" style={{ marginBottom: 8 }}>
        <strong>
          {sheet.point.name} ({sheet.point.code})
        </strong>
        <span>Ngày báo giá {sheet.day.split("-").reverse().join("/")}</span>
        <LockCountdown lockAt={sheet.lock_at} now={sheet.now} locked={sheet.locked} />
        <StatusPill tone={TICKET_VIEW_TONE[sheet.view]}>{TICKET_VIEW_LABEL[sheet.view]}</StatusPill>
      </div>
      {!editable ? (
        <div className="alert alert-info" role="status">
          {sheet.locked ? "Đã khóa nhập — chỉ xem; phản hồi yêu cầu xác minh ở mục “Yêu cầu xác minh”." : "Không mở nhập cho ngày này."}
        </div>
      ) : null}
      {requested ? (
        <div className="alert alert-info" role="status">
          Có {requested} yêu cầu xác minh của Cấp Tỉnh — sửa giá và gửi lại trước giờ khóa, hoặc{" "}
          <Link href="/kn/bao-gia/xac-minh">phản hồi</Link>.
        </div>
      ) : null}
      {c.abnormal ? (
        <div className="alert alert-bad" role="status">
          {c.abnormal} mặt hàng cao / thấp bất thường so với kỳ trước{c.needNote ? ` — ${c.needNote} dòng cần ghi chú biến động` : ""}.
        </div>
      ) : null}
      {editable && c.blank ? (
        <div className="alert alert-warn" role="status">
          Còn {c.blank} mặt hàng trống — nhập giá hoặc đánh dấu “Không có hàng” để gửi được.
        </div>
      ) : null}
      {act.view}
      <div className="row pb-noprint" style={{ margin: "10px 0" }}>
        <span>
          Có giá <strong>{c.priced}</strong> · Không có hàng <strong>{c.noGoods}</strong> · Còn trống <strong>{c.blank}</strong> / {c.total}
        </span>
        {editable ? (
          <>
            <button type="button" className="btn" onClick={() => setDrafts(fillFromRef(sheet, drafts))}>
              Lấy giá kỳ trước
            </button>
            {!submitted ? (
              <button type="button" className="btn" disabled={act.busy || c.invalid > 0} onClick={() => void save(false)}>
                Lưu nháp
              </button>
            ) : null}
            <button
              type="button"
              className="btn btn-primary"
              disabled={act.busy || c.blank > 0 || c.needNote > 0 || c.invalid > 0}
              onClick={() => setConfirm(true)}
            >
              {submitted ? "Gửi lại báo giá" : "Gửi báo giá"}
            </button>
          </>
        ) : null}
        <button type="button" className="btn" onClick={() => setPropose(true)}>
          Đề xuất mặt hàng
        </button>
        {sheet.ticket && sheet.view !== "missed" ? (
          <Link className="btn" href={`/kn/bao-gia/in?ticket=${sheet.ticket.id}`}>
            In phiếu báo giá
          </Link>
        ) : null}
      </div>
      <SheetEditor
        sheet={sheet}
        drafts={drafts}
        readOnly={!editable}
        showMissing={tried}
        onChange={(code, d) => setDrafts((x) => ({ ...x, [code]: d }))}
      />
      {confirm ? (
        <Dialog
          title="Xác nhận gửi báo giá"
          onClose={() => setConfirm(false)}
          actions={
            <>
              <button type="button" className="btn" onClick={() => setConfirm(false)}>
                Hủy
              </button>
              <button type="button" className="btn btn-primary" disabled={act.busy} onClick={() => void save(true)}>
                Xác nhận gửi
              </button>
            </>
          }
        >
          <p>
            Gửi {c.total} mặt hàng: có giá {c.priced}, Không có hàng {c.noGoods}
            {c.abnormal ? `, ${c.abnormal} dòng bất thường (đã ghi chú)` : ""}. Trước giờ khóa vẫn sửa và gửi lại được.
          </p>
        </Dialog>
      ) : null}
      {propose ? <Propose point={point} onClose={() => setPropose(false)} /> : null}
    </>
  );
}

/** "Đề xuất mặt hàng" (FN-XA-03.01 luồng phụ) — dùng lại hộp thoại của W11.73. */
function Propose({ point, onClose }: { point: string; onClose: () => void }) {
  const pa = usePriceItemApi();
  const cat = useLoad(() => pa.catalog(), []);
  return <LoadState s={cat}>{(c) => <ProposeItemDialog pa={pa} cat={c} point={point} onClose={onClose} onDone={onClose} />}</LoadState>;
}
