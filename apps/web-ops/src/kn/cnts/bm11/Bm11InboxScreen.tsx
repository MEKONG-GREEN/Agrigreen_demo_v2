"use client";

// Hộp "Báo cáo đến" (W11.45, BRD CNTS FN-16 Hình 45; Q-046 giả định: BM-11 gửi để LƯU): phòng nhận (HCTH — báo cáo tuần;
// KHTC — tháng, quý, năm; Ban Giám đốc) xem báo cáo Phòng CNTS đã gửi tới mình, bấm "Đã nhận", xuất Word. Số liệu cho KHTC
// tổng hợp KHÔNG đọc từ hộp này (chỉ qua phiếu Báo cáo cho KHTC + nguồn đã chốt — W11.11).
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { dt, LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { Bm11Detail } from "./Bm11Detail";
import { createBm11Api, TYPE_LABEL, type Bm11 } from "./api";

export function Bm11InboxScreen() {
  const { api } = useAuth();
  const bapi = useMemo(() => (api ? createBm11Api(api) : null), [api]);
  const [open, setOpen] = useState<Bm11 | null>(null);
  const list = useLoad(bapi ? () => bapi.inbox() : null, [bapi]);
  const act = useAction();
  if (!bapi) return null;
  return (
    <div className="page">
      <PageHeader title="Báo cáo đến">
        <p className="mute">Báo cáo định kỳ Phòng Chăn nuôi – Thủy sản đã duyệt và gửi tới phòng. Bấm “Đã nhận” khi đã tiếp nhận.</p>
      </PageHeader>
      {act.view}
      {open ? (
        <>
          <button className="btn btn-o btn-sm" onClick={() => { setOpen(null); list.reload(); }}>‹ Danh sách</button>
          <Bm11Detail key={open.id + ":" + open.seq} v={open} bapi={bapi} act={act} onChange={setOpen} />
        </>
      ) : (
        <LoadState s={list}>
          {(d) =>
            d.items.length === 0 ? (
              <p className="mute">Chưa có báo cáo gửi đến.</p>
            ) : (
              <table className="dt" aria-label="Báo cáo đến">
                <thead><tr><th>Phòng gửi</th><th>Loại</th><th>Kỳ</th><th>Gửi lúc</th><th /></tr></thead>
                <tbody>
                  {d.items.map((r) => (
                    <tr key={r.id}>
                      <td>Chăn nuôi – Thủy sản</td>
                      <td>{TYPE_LABEL[r.type]}</td>
                      <td>{r.label}</td>
                      <td>{dt(r.last_at)} {r.last_event === "received" ? <StatusPill tone="ok">Có nơi đã nhận</StatusPill> : null}</td>
                      <td>
                        <button className="btn btn-o btn-sm" disabled={act.busy}
                          onClick={async () => { const v = await act.run(() => bapi.get(r.id)); if (v) setOpen(v); }}>
                          Mở
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )
          }
        </LoadState>
      )}
    </div>
  );
}
