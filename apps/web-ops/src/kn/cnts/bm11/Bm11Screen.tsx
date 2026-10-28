"use client";

// Báo cáo định kỳ BM-11 — Cấp Tỉnh Phòng CNTS (W11.45, BRD CNTS v3.7 UR-TINH-16, FN-16; Hình 25 "Báo cáo định kỳ", Hình 26
// "Lãnh đạo phòng duyệt báo cáo"): danh sách theo loại (tuần/tháng/quý/năm) + lọc "Chờ duyệt" cho lãnh đạo; lập báo cáo của kỳ
// (tính thử phần tự động: số liệu, "Tổng hợp từ n/N xã", lý do chưa lập được — quy tắc 13); chi tiết soạn/trình/duyệt.
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { SelectField, TextField } from "@/components/form";
import { dt, LoadState, num, useAction, useLoad } from "@/kn/cycle/common";
import { Bm11Detail } from "./Bm11Detail";
import { createBm11Api, KEY_HINT, periodKeyError, STATUS_LABEL, TYPE_LABEL, type Bm11, type Bm11Preview, type Bm11Status, type Bm11Type } from "./api";

const TYPES: Bm11Type[] = ["week", "month", "quarter", "year"];

export function Bm11Screen() {
  const { api, kn } = useAuth();
  const bapi = useMemo(() => (api ? createBm11Api(api) : null), [api]);
  const leader = !!kn.context?.manager;
  const [type, setType] = useState<Bm11Type | "">("");
  const [status, setStatus] = useState<Bm11Status | "">(leader ? "pending" : "");
  const [open, setOpen] = useState<Bm11 | null>(null);
  const [creating, setCreating] = useState(false);
  const list = useLoad(bapi ? () => bapi.list({ type: type || undefined, status: status || undefined }) : null, [bapi, type, status]);
  const act = useAction();
  if (!bapi) return null;
  const show = async (id: string) => {
    const r = await act.run(() => bapi.get(id));
    if (r) setOpen(r);
  };
  return (
    <div className="page">
      <PageHeader title="Báo cáo định kỳ (BM-11)"
        actions={list.data?.can_create && !open ? <button className="btn btn-primary" onClick={() => setCreating(true)}>Lập báo cáo</button> : null}>
        <p className="mute">
          Báo cáo tuần, tháng, quý, năm của Phòng Chăn nuôi – Thủy sản: phần tự động lấy từ số liệu xã đã nộp/đã chốt (không sửa tay),
          phần viết do cán bộ soạn; Lãnh đạo phòng duyệt thì tự gửi Ban Giám đốc, HCTH (tuần) hoặc KHTC (tháng, quý, năm).
        </p>
      </PageHeader>
      {act.view}
      {open ? (
        <>
          <button className="btn btn-o btn-sm" onClick={() => { setOpen(null); list.reload(); }}>‹ Danh sách</button>
          <Bm11Detail key={open.id + ":" + open.seq} v={open} bapi={bapi} act={act} onChange={setOpen} />
        </>
      ) : (
        <>
          {creating ? <CreatePanel bapi={bapi} act={act} onClose={() => setCreating(false)} onCreated={(r) => { setCreating(false); setOpen(r); }} /> : null}
          <div className="toolbar" role="group" aria-label="Lọc báo cáo">
            <select className="select" aria-label="Loại báo cáo" value={type} onChange={(e) => setType(e.target.value as Bm11Type | "")}>
              <option value="">Mọi loại</option>
              {TYPES.map((t) => <option key={t} value={t}>{TYPE_LABEL[t]}</option>)}
            </select>
            <select className="select" aria-label="Trạng thái" value={status} onChange={(e) => setStatus(e.target.value as Bm11Status | "")}>
              <option value="">Mọi trạng thái</option>
              {(Object.keys(STATUS_LABEL) as Bm11Status[]).map((s) => <option key={s} value={s}>{STATUS_LABEL[s][0]}</option>)}
            </select>
          </div>
          <LoadState s={list}>
            {(d) =>
              d.items.length === 0 ? (
                <p className="mute">{status === "pending" ? "Không có báo cáo chờ duyệt." : "Chưa có báo cáo nào."}</p>
              ) : (
                <table className="dt" aria-label="Báo cáo định kỳ">
                  <thead><tr><th>Loại</th><th>Kỳ</th><th>Trạng thái</th><th>Nơi nhận</th><th>Cập nhật</th><th /></tr></thead>
                  <tbody>
                    {d.items.map((r) => (
                      <tr key={r.id}>
                        <td>{TYPE_LABEL[r.type]}</td>
                        <td>{r.label}</td>
                        <td><StatusPill tone={STATUS_LABEL[r.status][1]}>{STATUS_LABEL[r.status][0]}</StatusPill></td>
                        <td>{r.recipients.map((x) => ({ bgd: "BGĐ", hcth: "HCTH", khtc: "KHTC" })[x] ?? x).join(", ")}</td>
                        <td>{dt(r.last_at)}</td>
                        <td><button className="btn btn-o btn-sm" disabled={act.busy} onClick={() => show(r.id)}>Mở</button></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )
            }
          </LoadState>
        </>
      )}
    </div>
  );
}

function CreatePanel({ bapi, act, onClose, onCreated }: {
  bapi: ReturnType<typeof createBm11Api>;
  act: ReturnType<typeof useAction>;
  onClose: () => void;
  onCreated: (r: Bm11) => void;
}) {
  const [type, setType] = useState<Bm11Type>("week");
  const [key, setKey] = useState("");
  const [pv, setPv] = useState<Bm11Preview | null>(null);
  const keyErr = periodKeyError(type, key);
  const preview = async () => {
    if (keyErr) return;
    const r = await act.run(() => bapi.preview(type, key.trim()));
    setPv(r);
  };
  return (
    <section className="card sec" aria-label="Lập báo cáo">
      <h2>Lập báo cáo</h2>
      <div className="grid2">
        <SelectField label="Loại báo cáo" value={type} onChange={(x) => { setType(x as Bm11Type); setPv(null); }}
          options={TYPES.map((t) => ({ value: t, label: TYPE_LABEL[t] }))} />
        <TextField label="Kỳ" value={key} onChange={(x) => { setKey(x); setPv(null); }} placeholder={KEY_HINT[type]} error={keyErr ?? undefined}
          hint="Để trống = kỳ vừa kết thúc" />
      </div>
      <div className="toolbar">
        <button className="btn btn-o" disabled={act.busy || !!keyErr} onClick={preview}>Tính thử số liệu</button>
        <button className="btn btn-primary" disabled={act.busy || !pv?.ready || !!pv?.existing_id}
          onClick={async () => {
            const r = await act.run(() => bapi.create(type, pv!.period_key), "Đã lập báo cáo.");
            if (r) onCreated(r);
          }}>
          Lập báo cáo
        </button>
        <button className="btn btn-o" onClick={onClose}>Đóng</button>
      </div>
      {pv ? (
        <div className="stack">
          <p>
            <strong>{pv.label}</strong> · {pv.communes} · hạn nộp tuần cuối {dt(pv.due_at)} · mức nộp tối thiểu {pv.min_pct}% ·{" "}
            {pv.ready ? <StatusPill tone="ok">Lập được</StatusPill> : <StatusPill tone="warn">Chưa lập được</StatusPill>}
          </p>
          {pv.existing_id ? <p className="warn">Kỳ này đã có báo cáo — mở báo cáo trong danh sách.</p> : null}
          {pv.reasons.map((r) => <p key={r} className="warn">{r}</p>)}
          {pv.coverage.filter((c) => c.missing_names.length).map((c) => (
            <p key={c.form} className="mute">Xã chưa nộp {c.form}: {c.missing_names.join(", ")}</p>
          ))}
          <p>
            Khai thác {num(pv.fishery.catch_cur, 3)} tấn (lũy kế {num(pv.fishery.catch_ytd, 3)}), nuôi trồng {num(pv.fishery.aqua_cur, 3)} tấn ·
            Tổng đàn heo {num(pv.lines.find((l) => l.key === "tong_dan_heo")?.value, 0)} con
          </p>
        </div>
      ) : null}
    </section>
  );
}
