"use client";

// Bảng giá ngày trên màn Kiểm duyệt & công bố (W11.76; BRD Báo giá v1.3 FN-TI-02.05 Duyệt, FN-TI-02.06 Mở duyệt): nhãn
// trạng thái, tóm tắt kỳ, "Xem trước bảng giá", "Duyệt bảng giá" (vô hiệu kèm lý do / số cảnh báo còn lại; giữ khóa
// "Đang duyệt" tới khi xác nhận / hủy), "Mở duyệt" (lý do bắt buộc), "Hủy mở duyệt", phiên bản đã duyệt, nhật ký.
import { useState } from "react";
import { Dialog } from "@/components/Dialog";
import { DataTable, StatusPill, type Column } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import {
  BOARD_ACTION_LABEL,
  BOARD_STATE_LABEL,
  BOARD_STATE_TONE,
  money,
  vnTime,
  type PriceBoard,
  type PriceBoardAction,
  type PriceBoardLine,
} from "./api";
import { usePriceTicketApi } from "./SheetLines";

const lineCols: Column<PriceBoardLine>[] = [
  { key: "grp", header: "Nhóm hàng", cell: (r) => r.group_name },
  { key: "item", header: "Mặt hàng", cell: (r) => r.item_name },
  { key: "unit", header: "ĐVT", cell: (r) => r.unit_name },
  { key: "min", header: "Giá thấp nhất", cell: (r) => money(r.min) },
  { key: "max", header: "Giá cao nhất", cell: (r) => money(r.max) },
  { key: "avg", header: "Giá trung bình", cell: (r) => money(r.avg) },
  { key: "n", header: "Số xã", cell: (r) => r.points },
];

export function BoardLines({ caption, rows }: { caption: string; rows: PriceBoardLine[] }) {
  return (
    <DataTable
      caption={caption}
      columns={lineCols}
      rows={rows}
      rowKey={(r) => r.item_code}
      page={1}
      pageSize={Math.max(rows.length, 1)}
      total={rows.length}
      onPage={() => undefined}
      empty="Không có dòng giá."
    />
  );
}

/** Khối bảng giá ngày — onChanged: tải lại phiếu / cảnh báo sau khi đổi trạng thái (phiếu chuyển "Đã duyệt"). */
export function BoardPanel({ day, nonce, onChanged }: { day: string; nonce: number; onChanged: () => void }) {
  const ta = usePriceTicketApi();
  const [own, setOwn] = useState(0);
  const board = useLoad(() => ta.board(day), [day, nonce, own]);
  // D11 (CEO): chỉ cán bộ có cờ Quản trị nghiệp vụ báo giá duyệt / mở duyệt — người khác chỉ xem.
  const status = useLoad(() => ta.status(), []);
  const steward = status.data?.can_steward ?? false;
  const act = useAction();
  const [dialog, setDialog] = useState<"preview" | "confirm" | "reopen" | "log" | null>(null);
  const [reason, setReason] = useState("");
  const run = async (b: PriceBoard, action: PriceBoardAction, ok: string, next: typeof dialog = null) => {
    const r = await act.run(() => ta.boardOp(day, action, { seq: b.seq, reason: reason.trim() || undefined }), ok);
    setOwn((n) => n + 1);
    if (r) {
      setDialog(next);
      setReason("");
      onChanged();
    }
    return r;
  };
  return (
    <section className="card" aria-label="Bảng giá ngày" style={{ marginBottom: 12, padding: 12 }}>
      <LoadState s={board}>
        {(b) => {
          const s = b.summary;
          return (
            <>
              <div className="row" style={{ justifyContent: "space-between", flexWrap: "wrap", gap: 8 }}>
                <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
                  <strong>Bảng giá ngày</strong>
                  <StatusPill tone={BOARD_STATE_TONE[b.state]}>
                    {BOARD_STATE_LABEL[b.state]}
                    {b.version ? ` (v${b.version})` : ""}
                  </StatusPill>
                  {b.state === "approving" ? <span>Đang duyệt bởi {b.locked_by_name ?? "cán bộ khác"}</span> : null}
                  {b.published?.reopened ? <StatusPill tone="warn">Đang cập nhật — các vai khác vẫn thấy v{b.published.version}</StatusPill> : null}
                </div>
                <div className="row" style={{ gap: 6, flexWrap: "wrap" }}>
                  <button type="button" className="btn" disabled={!b.preview.length} onClick={() => setDialog("preview")}>
                    Xem trước bảng giá
                  </button>
                  {!steward ? (
                    <span>Chỉ Quản trị nghiệp vụ báo giá duyệt / mở duyệt</span>
                  ) : b.state === "approved" ? (
                    <button type="button" className="btn" disabled={!b.can.reopen || act.busy} onClick={() => setDialog("reopen")}>
                      Mở duyệt
                    </button>
                  ) : b.state === "approving" && b.can.unlock ? (
                    <>
                      <button type="button" className="btn" disabled={act.busy} onClick={() => void run(b, "unlock", "Đã hủy duyệt.")}>
                        Hủy duyệt
                      </button>
                      <button type="button" className="btn btn-primary" disabled={act.busy} onClick={() => setDialog("confirm")}>
                        Xác nhận duyệt
                      </button>
                    </>
                  ) : (
                    <button
                      type="button"
                      className="btn btn-primary"
                      disabled={!b.can.approve || act.busy}
                      title={b.blockers.join("; ")}
                      onClick={() => void run(b, "lock", "", "confirm")}
                    >
                      {b.summary && b.summary.open_alerts > 0 ? `Duyệt bảng giá (còn ${b.summary.open_alerts} cảnh báo)` : "Duyệt bảng giá"}
                    </button>
                  )}
                  {steward && b.can.cancel_reopen ? (
                    <button type="button" className="btn" disabled={act.busy} onClick={() => void run(b, "cancel-reopen", "Đã hủy mở duyệt.")}>
                      Hủy mở duyệt
                    </button>
                  ) : null}
                  {b.log.length ? (
                    <button type="button" className="btn" onClick={() => setDialog("log")}>
                      Nhật ký
                    </button>
                  ) : null}
                </div>
              </div>
              {s ? (
                <div className="row" style={{ gap: 6, flexWrap: "wrap", marginTop: 8 }} aria-label="Tóm tắt kỳ">
                  <StatusPill tone="neutral">Xã phải báo giá {s.points}</StatusPill>
                  <StatusPill tone="ok">Đúng hạn {s.submitted}</StatusPill>
                  <StatusPill tone="warn">Nộp muộn {s.late}</StatusPill>
                  <StatusPill tone="bad">
                    {b.locked ? "Không nộp" : "Chưa gửi"} {s.missed}
                  </StatusPill>
                  <StatusPill tone="neutral">Dòng giá {s.price_lines}</StatusPill>
                  <StatusPill tone="neutral">Đã loại {s.excluded}</StatusPill>
                </div>
              ) : null}
              {b.state !== "approved" && b.blockers.length ? (
                <p className="alert alert-warn" role="note" style={{ marginTop: 8 }}>
                  Chưa duyệt được: {b.blockers.join("; ")}.
                </p>
              ) : null}
              {b.published ? (
                <p style={{ marginTop: 8 }}>
                  Đã công bố v{b.published.version} lúc {vnTime(b.published.approved_at)}
                  {b.published.week_key ? ` — tuần ${b.published.week_key}` : ""}.
                </p>
              ) : null}
              {act.view}
              {dialog === "preview" ? (
                <Dialog
                  title={`Xem trước bảng giá ${day}`}
                  onClose={() => setDialog(null)}
                  actions={
                    <button type="button" className="btn" onClick={() => setDialog(null)}>
                      Đóng
                    </button>
                  }
                >
                  <BoardLines caption="Bảng giá tổng hợp (chưa duyệt)" rows={b.preview} />
                  {b.published ? <BoardLines caption={`Phiên bản đã duyệt v${b.published.version}`} rows={b.published.lines} /> : null}
                </Dialog>
              ) : null}
              {dialog === "confirm" && b.state === "approving" ? (
                <Dialog
                  title="Xác nhận duyệt bảng giá"
                  onClose={() => void run(b, "unlock", "Đã hủy duyệt.")}
                  actions={
                    <>
                      <button type="button" className="btn" disabled={act.busy} onClick={() => void run(b, "unlock", "Đã hủy duyệt.")}>
                        Hủy
                      </button>
                      <button
                        type="button"
                        className="btn btn-primary"
                        disabled={act.busy}
                        onClick={() => void run(b, "approve", b.version ? `Đã duyệt lại — phiên bản v${b.version + 1}.` : "Đã duyệt bảng giá.")}
                      >
                        Duyệt
                      </button>
                    </>
                  }
                >
                  <p>
                    Duyệt bảng giá ngày {day}: {b.preview.length} mặt hàng, {s ? s.submitted + s.late : 0} phiếu. Bảng giá được công bố cho Cấp TW, Cấp Xã
                    {b.version ? ` (thay phiên bản v${b.version})` : ""}. Giữ khóa tới {vnTime(b.lock_until)}.
                  </p>
                  <BoardLines caption="Bảng giá sẽ công bố" rows={b.preview} />
                  {act.view}
                </Dialog>
              ) : null}
              {dialog === "reopen" ? (
                <Dialog
                  title="Mở duyệt bảng giá"
                  onClose={() => setDialog(null)}
                  actions={
                    <>
                      <button type="button" className="btn" onClick={() => setDialog(null)}>
                        Hủy
                      </button>
                      <button type="button" className="btn btn-primary" disabled={act.busy || !reason.trim()} onClick={() => void run(b, "reopen", "Đã mở duyệt.")}>
                        Xác nhận
                      </button>
                    </>
                  }
                >
                  <p>
                    Trong thời gian mở duyệt, Cấp TW, Cấp Xã vẫn thấy phiên bản v{b.version} (nhãn &quot;Đang cập nhật&quot;). Chỉ mở duyệt ngày báo giá trong {b.reopen_days} ngày gần nhất.
                  </p>
                  <label htmlFor="pb-reason">Lý do (bắt buộc)</label>
                  <textarea id="pb-reason" className="input" rows={3} maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
                  {act.view}
                </Dialog>
              ) : null}
              {dialog === "log" ? (
                <Dialog
                  title="Nhật ký bảng giá"
                  onClose={() => setDialog(null)}
                  actions={
                    <button type="button" className="btn" onClick={() => setDialog(null)}>
                      Đóng
                    </button>
                  }
                >
                  <ul>
                    {b.log.map((l) => (
                      <li key={l.seq}>
                        {vnTime(l.at)} — {BOARD_ACTION_LABEL[l.action]}
                        {l.action === "approve" ? ` v${l.version}` : ""} — {l.actor_name ?? l.actor_ref}
                        {l.reason ? `: ${l.reason}` : ""}
                      </li>
                    ))}
                  </ul>
                </Dialog>
              ) : null}
            </>
          );
        }}
      </LoadState>
    </section>
  );
}
