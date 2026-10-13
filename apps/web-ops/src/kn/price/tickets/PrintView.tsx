"use client";

// FN-XA-03.02 — In phiếu báo giá theo mẫu File 2 (BRD Báo giá v1.3; W11.75): xem trước khổ A4, "In" (trình duyệt — cũng
// lưu được PDF), "Tải PDF" / "Tải Excel" (máy chủ). Giá in là giá cán bộ Xã gửi; dòng hiệu chỉnh "Tỉnh hiệu chỉnh: …";
// dòng đã loại "Không đưa vào bảng giá"; nhiều phiếu → mỗi phiếu một trang.
import { useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { Loading } from "@/components/ui";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { formatDateVi } from "@/kn/grid/decimal";
import { saveBlob } from "@/lib/files";
import { byGroup, money, type PriceSheet, type PriceSheetLine } from "./api";
import { usePriceTicketApi } from "./SheetLines";

/** Ghi chú in của một dòng (BR-02 FN-XA-03.02). */
export function printNote(l: PriceSheetLine): string {
  const out: string[] = [];
  if (l.no_goods) out.push("Không có hàng");
  if (l.note) out.push(l.note);
  if (l.adj_min) out.push(`Tỉnh hiệu chỉnh: ${money(l.adj_min)}${l.adj_max !== l.adj_min ? `–${money(l.adj_max)}` : ""}`);
  if (l.alert?.view === "excluded") out.push("Không đưa vào bảng giá");
  return out.join("; ");
}

export function TicketPrint({ sheet }: { sheet: PriceSheet }) {
  let stt = 0;
  const label = !sheet.locked ? "Chưa khóa" : sheet.ticket?.source === "proxy" ? "Nộp muộn – nhập thay" : "";
  return (
    <article className="pb-print" aria-label={`Phiếu báo giá ${sheet.point.name} ${sheet.day}`}>
      <div style={{ textAlign: "center" }}>
        <div style={{ fontWeight: 700 }}>PHIẾU BÁO GIÁ NÔNG SẢN</div>
        <div>
          Ngày {formatDateVi(sheet.day)}
          {label ? ` — ${label}` : ""}
        </div>
      </div>
      <p>
        Xã báo giá: <strong>{sheet.point.name}</strong> ({sheet.point.code}) · Nhóm vùng: {sheet.point.region_name}
        <br />
        Cán bộ Xã: {sheet.point.officer_name ?? "………………………"}
      </p>
      <table>
        <thead>
          <tr>
            <th>STT</th>
            <th>Mặt hàng</th>
            <th>ĐVT</th>
            <th>Giá thấp nhất</th>
            <th>Giá cao nhất</th>
            <th>Ghi chú</th>
          </tr>
        </thead>
        <tbody>
          {byGroup(sheet.lines).map((g) => [
            <tr key={g.code}>
              <td colSpan={6} style={{ fontWeight: 700 }}>
                {g.name}
              </td>
            </tr>,
            ...g.rows.map((l) => (
              <tr key={l.item_code}>
                <td>{++stt}</td>
                <td>
                  {l.name} {l.variety}
                </td>
                <td>{l.unit_name}</td>
                <td style={{ textAlign: "right" }}>{l.no_goods ? "" : money(l.min)}</td>
                <td style={{ textAlign: "right" }}>{l.no_goods ? "" : money(l.max)}</td>
                <td>{printNote(l)}</td>
              </tr>
            )),
          ])}
        </tbody>
      </table>
      <div className="pb-sign">
        <div>
          <strong>Xác nhận của Phòng TTTT</strong>
          <br />
          (ký, ghi rõ họ tên)
        </div>
        <div>
          <strong>Người lập phiếu</strong>
          <br />
          (ký, ghi rõ họ tên)
        </div>
      </div>
    </article>
  );
}

function PrintInner() {
  const ta = usePriceTicketApi();
  const ids = useSearchParams().getAll("ticket");
  const sheets = useLoad(() => Promise.all(ids.map((id) => ta.ticket(id))), [ids.join(",")]);
  const act = useAction();
  const download = (format: "pdf" | "xlsx") =>
    act.run(async () => {
      const f = await ta.print(ids, format);
      saveBlob(f.blob, f.name);
    });
  return (
    <>
      <div className="row pb-noprint" style={{ margin: "10px 0" }}>
        <button type="button" className="btn btn-primary" onClick={() => window.print()}>
          In
        </button>
        <button type="button" className="btn" disabled={act.busy} onClick={() => void download("pdf")}>
          Tải PDF
        </button>
        <button type="button" className="btn" disabled={act.busy} onClick={() => void download("xlsx")}>
          Tải Excel
        </button>
      </div>
      <div className="pb-noprint">{act.view}</div>
      {ids.length === 0 ? <p>Chưa chọn phiếu.</p> : null}
      <LoadState s={sheets}>
        {(list) => (
          <>
            {list.map((s) =>
              s.view === "missed" || s.view === "deleted" ? (
                <p key={s.ticket?.id ?? s.day} className="alert alert-warn pb-noprint">
                  Phiếu {s.point.name} ngày {formatDateVi(s.day)} {s.view === "deleted" ? "đã xóa" : "không nộp"} — không in được.
                </p>
              ) : (
                <TicketPrint key={s.ticket?.id ?? s.day} sheet={s} />
              ),
            )}
          </>
        )}
      </LoadState>
    </>
  );
}

export function PrintView() {
  return (
    <Suspense fallback={<Loading />}>
      <PrintInner />
    </Suspense>
  );
}
