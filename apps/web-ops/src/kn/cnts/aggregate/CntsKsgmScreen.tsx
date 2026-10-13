"use client";

// BM-02 — KSGM tuần, tháng (W11.44, BRD CNTS v3.7 UR-TINH-14, FN-16): số theo cơ sở giết mổ tự cộng từ BM-01 đã gửi + dòng
// điều chỉnh (chỉ ghi thêm, có lý do; không ghi đè số tự cộng), "Thiếu n ngày", dòng điều chỉnh "cần xem lại" khi BM-01 đổi,
// chốt / mở lại tuần KSGM (số công bố cho KHTC), phiên bản tệp chia sẻ Chi cục (Excel).
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, num, useAction, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import { AGG_STATE, adjustmentErrors, createCntsAggApi, type Basis, type CntsBM02 } from "./api";

export function CntsKsgmScreen() {
  const { api, kn } = useAuth();
  const writer = kn.context?.role === "kn_tinh";
  const capi = useMemo(() => (api ? createCntsAggApi(api) : null), [api]);
  const [periodType, setPeriodType] = useState<"ksgm_week" | "month">("ksgm_week");
  const [key, setKey] = useState("");
  const [basis, setBasis] = useState<Basis>("submitted");
  const data = useLoad(capi ? () => capi.bm02({ period_type: periodType, key: key || undefined, basis }) : null, [capi, periodType, key, basis]);
  const act = useAction();
  if (!capi) return null;

  return (
    <div className="page">
      <PageHeader title="KSGM tuần, tháng (BM-02)">
        <p className="mute">Tự cộng từ báo cáo KSGM hằng ngày (BM-01) đã gửi + dòng điều chỉnh. Tuần KSGM: 4 tuần mỗi tháng theo lịch kỳ.</p>
      </PageHeader>
      {act.view}
      <div className="toolbar" role="group" aria-label="Chọn kỳ">
        <select className="select" aria-label="Loại kỳ" value={periodType} onChange={(e) => { setPeriodType(e.target.value as "ksgm_week" | "month"); setKey(""); }}>
          <option value="ksgm_week">Tuần KSGM</option>
          <option value="month">Tháng</option>
        </select>
        <button className="btn btn-o btn-sm" disabled={!data.data?.prev_key} onClick={() => setKey(data.data?.prev_key ?? "")}>‹ Kỳ trước</button>
        <strong aria-label="Kỳ đang xem">{data.data?.label ?? "…"}</strong>
        <button className="btn btn-o btn-sm" disabled={!data.data?.next_key} onClick={() => setKey(data.data?.next_key ?? "")}>Kỳ sau ›</button>
        <select className="select" aria-label="Số liệu" value={basis} onChange={(e) => setBasis(e.target.value as Basis)}>
          <option value="submitted">Số đã gửi</option>
          <option value="closed">Số đã chốt (kỳ nguồn)</option>
        </select>
      </div>
      <LoadState s={data}>{(v) => <Bm02Body key={v.period_key + v.basis} v={v} writer={writer} act={act} capi={capi} reload={data.reload} />}</LoadState>
    </div>
  );
}

function Bm02Body({ v, writer, act, capi, reload }: {
  v: CntsBM02;
  writer: boolean;
  act: ReturnType<typeof useAction>;
  capi: ReturnType<typeof createCntsAggApi>;
  reload: () => void;
}) {
  const [adjWeek, setAdjWeek] = useState(v.weeks[0]?.key ?? "");
  const [adjFac, setAdjFac] = useState("");
  const [deltas, setDeltas] = useState<Record<string, string>>({});
  const [reason, setReason] = useState("");
  const [errs, setErrs] = useState<Record<string, string>>({});
  const facilities = v.rows.map((r) => ({ ref: r.facility_ref, name: r.facility_name }));
  const aggOf = (k: string) => v.aggregates.find((a) => a.key === k);
  const askReason = () => {
    const r = window.prompt("Lý do mở lại (10–500 ký tự)");
    return r && r.trim().length >= 10 ? r.trim() : null;
  };
  const submitAdj = async () => {
    const e = adjustmentErrors(deltas, reason);
    if (!adjFac) e.facility = "Chọn cơ sở";
    setErrs(e);
    if (Object.keys(e).length) return;
    const clean = Object.fromEntries(Object.entries(deltas).filter(([, x]) => x.trim()));
    if (await act.run(() => capi.adjust({ week_key: adjWeek, facility_ref: adjFac, deltas: clean, reason: reason.trim() }), "Đã thêm dòng điều chỉnh.")) {
      setDeltas({});
      setReason("");
      reload();
    }
  };
  return (
    <>
      <section className="card sec">
        <h2>Tình trạng</h2>
        <p>
          Kỳ ngày BM-01 đã chốt: {v.sources.closed}/{v.sources.count} · Cơ sở có báo cáo: {v.units_reported}/{v.units_total} · Số liệu:{" "}
          {v.basis === "closed" ? "đã chốt" : "đã gửi"}
        </p>
        <table className="dt" aria-label="Chốt tuần KSGM">
          <thead>
            <tr><th>Tuần KSGM</th><th>Khoảng ngày</th><th>Trạng thái</th><th>Lần chốt</th><th /></tr>
          </thead>
          <tbody>
            {v.weeks.map((w) => {
              const a = aggOf(w.key);
              const st = AGG_STATE[a?.state ?? "none"];
              return (
                <tr key={w.key}>
                  <td>{w.key}</td>
                  <td>{w.from} – {w.to}</td>
                  <td><StatusPill tone={st[1]}>{st[0]}</StatusPill>{a?.changed ? <StatusPill tone="warn">Nguồn đã thay đổi</StatusPill> : null}</td>
                  <td>{a?.close_count ?? 0}{a?.last_at ? ` · ${dt(a.last_at)}` : ""}</td>
                  <td>
                    {writer && a?.state !== "closed" ? (
                      <button className="btn btn-primary btn-sm" disabled={act.busy}
                        onClick={async () => (await act.run(() => capi.close("CNTS-BM02", w.key), `Đã chốt ${w.key}.`)) && reload()}>Chốt</button>
                    ) : null}
                    {writer && a?.state === "closed" ? (
                      <button className="btn btn-o btn-sm" disabled={act.busy}
                        onClick={async () => { const r = askReason(); if (r && (await act.run(() => capi.reopen("CNTS-BM02", w.key, r), `Đã mở lại ${w.key}.`))) reload(); }}>Mở lại</button>
                    ) : null}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </section>

      <section className="card sec">
        <h2>Số lượng KSGM theo cơ sở</h2>
        <table className="dt" aria-label="Bảng BM-02">
          <thead>
            <tr>
              <th>Xã/phường</th><th>Cơ sở</th><th>Mã số GS / GC</th>
              {v.species.map((s) => <th key={s.key} className="num">{s.label}</th>)}
              <th>Số ngày có báo cáo</th><th>Ghi chú</th>
            </tr>
          </thead>
          <tbody>
            {v.rows.map((r, i) => {
              const showCommune = i === 0 || v.rows[i - 1]!.commune_code !== r.commune_code;
              const sub = v.communes.find((c) => c.commune_code === r.commune_code);
              const last = v.rows.filter((x) => x.commune_code === r.commune_code).at(-1) === r;
              return [
                <tr key={r.facility_ref}>
                  <td>{showCommune ? r.commune_name : ""}</td>
                  <td>{r.facility_name}</td>
                  <td>{[r.stamps_gs.join("; "), r.stamps_gc.join("; ")].filter(Boolean).join(" / ") || "—"}</td>
                  {v.species.map((s) => (
                    <td key={s.key} className="num" title={r.adjust[s.key] !== "0" ? `Tự cộng ${num(r.auto[s.key], 3)}, điều chỉnh ${num(r.adjust[s.key], 3)}` : undefined}>
                      {num(r.total[s.key], 3)}
                    </td>
                  ))}
                  <td>{r.days_reported}/{r.days_expected}</td>
                  <td>
                    {r.missing_days > 0 ? <StatusPill tone="warn">Thiếu {r.missing_days} ngày</StatusPill> : null}
                    {r.stale ? <StatusPill tone="bad">Điều chỉnh cần xem lại</StatusPill> : null}
                  </td>
                </tr>,
                last && sub ? (
                  <tr key={r.facility_ref + ":sub"} className="row-sub">
                    <td colSpan={3}><strong>Cộng {sub.commune_name}</strong></td>
                    {v.species.map((s) => <td key={s.key} className="num"><strong>{num(sub.total[s.key], 3)}</strong></td>)}
                    <td colSpan={2} />
                  </tr>
                ) : null,
              ];
            })}
          </tbody>
          <tfoot>
            <tr>
              <td colSpan={3}><strong>Tổng cộng</strong></td>
              {v.species.map((s) => <td key={s.key} className="num"><strong>{num(v.total[s.key], 3)}</strong></td>)}
              <td colSpan={2} />
            </tr>
          </tfoot>
        </table>
      </section>

      <section className="card sec">
        <h2>Dòng điều chỉnh</h2>
        {v.adjustments.length === 0 ? <p className="mute">Chưa có dòng điều chỉnh.</p> : (
          <table className="dt" aria-label="Dòng điều chỉnh">
            <thead>
              <tr><th>Tuần KSGM</th><th>Cơ sở</th>{v.species.map((s) => <th key={s.key} className="num">{s.label}</th>)}<th>Lý do</th><th>Người, thời điểm</th><th /></tr>
            </thead>
            <tbody>
              {v.adjustments.map((a) => (
                <tr key={a.id}>
                  <td>{a.week_key}</td>
                  <td>{a.facility_name}</td>
                  {v.species.map((s) => <td key={s.key} className="num">{a.deltas[s.key] && a.deltas[s.key] !== "0" ? num(a.deltas[s.key], 3) : ""}</td>)}
                  <td>{a.reason}</td>
                  <td>{a.actor_name} {dt(a.at)}</td>
                  <td>{a.stale ? <StatusPill tone="bad">BM-01 đã đổi — xem lại</StatusPill> : null}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {writer ? (
          <form className="ws-two" aria-label="Thêm dòng điều chỉnh" onSubmit={(e) => { e.preventDefault(); void submitAdj(); }}>
            <label className="field">
              <span>Tuần KSGM</span>
              <select className="select" value={adjWeek} onChange={(e) => setAdjWeek(e.target.value)}>
                {v.weeks.map((w) => <option key={w.key} value={w.key}>{w.label}</option>)}
              </select>
            </label>
            <label className="field">
              <span>Cơ sở</span>
              <select className="select" value={adjFac} onChange={(e) => setAdjFac(e.target.value)}>
                <option value="">— Chọn cơ sở —</option>
                {facilities.map((f) => <option key={f.ref} value={f.ref}>{f.name}</option>)}
              </select>
              {errs.facility ? <span className="err">{errs.facility}</span> : null}
            </label>
            {v.species.map((s) => (
              <label key={s.key} className="field">
                <span>{s.label} (cộng/trừ)</span>
                <input className="input" inputMode="decimal" aria-label={`Điều chỉnh ${s.label}`} value={deltas[s.key] ?? ""}
                  onChange={(e) => setDeltas({ ...deltas, [s.key]: e.target.value })} />
                {errs[s.key] ? <span className="err">{errs[s.key]}</span> : null}
              </label>
            ))}
            <label className="field">
              <span>Lý do</span>
              <input className="input" aria-label="Lý do điều chỉnh" value={reason} onChange={(e) => setReason(e.target.value)} maxLength={500} />
              {errs.reason ? <span className="err">{errs.reason}</span> : null}
              {errs.deltas ? <span className="err">{errs.deltas}</span> : null}
            </label>
            <button className="btn btn-primary" type="submit" disabled={act.busy}>Thêm dòng điều chỉnh</button>
          </form>
        ) : null}
      </section>

      <ShareSection
        shares={v.shares}
        writer={writer}
        act={act}
        create={() => capi.share("CNTS-BM02", v.period_type, v.period_key)}
        download={(id, ver) => capi.download(id).then((b) => saveBlob(b, `bm02-${v.period_key}-v${ver}.xlsx`))}
        reload={reload}
      />
    </>
  );
}

/** Phiên bản tệp chia sẻ Chi cục (dùng chung BM-02, BM-18). */
export function ShareSection({ shares, writer, act, create, download, reload }: {
  shares: { id: string; version: number; at: string; basis: string; note: string; actor_name: string }[];
  writer: boolean;
  act: ReturnType<typeof useAction>;
  create: () => Promise<{ replayed: boolean; share: { version: number } }>;
  download: (id: string, version: number) => Promise<void>;
  reload: () => void;
}) {
  return (
    <section className="card sec">
      <h2>Chia sẻ Chi cục Chăn nuôi và Thú y</h2>
      <p className="mute">Mỗi lần số liệu thay đổi sau khi đã chia sẻ, tệp mới ghi số phiên bản, ngày giờ và ghi chú thay đổi so với bản đã gửi.</p>
      {shares.length === 0 ? <p className="mute">Chưa chia sẻ.</p> : (
        <table className="dt" aria-label="Phiên bản đã chia sẻ">
          <thead>
            <tr><th>Phiên bản</th><th>Thời điểm</th><th>Số liệu</th><th>Ghi chú</th><th>Người tạo</th><th /></tr>
          </thead>
          <tbody>
            {shares.map((s) => (
              <tr key={s.id}>
                <td>v{s.version}</td>
                <td>{dt(s.at)}</td>
                <td>{s.basis === "closed" ? "Đã chốt" : "Đã gửi"}</td>
                <td>{s.note || "—"}</td>
                <td>{s.actor_name || "—"}</td>
                <td><button className="btn btn-o btn-sm" disabled={act.busy} onClick={() => void act.run(() => download(s.id, s.version))}>Tải Excel</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {writer ? (
        <button className="btn btn-primary" disabled={act.busy}
          onClick={async () => (await act.run(create, (r) => (r.replayed ? `Số liệu không đổi — giữ phiên bản v${r.share.version}.` : `Đã tạo phiên bản v${r.share.version}.`))) && reload()}>
          Tạo tệp chia sẻ
        </button>
      ) : null}
    </section>
  );
}
