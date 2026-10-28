"use client";

// BM-02 — KSGM tuần, tháng (W11.44, BRD CNTS v3.7 UR-TINH-14, FN-16): số theo cơ sở giết mổ tự cộng từ BM-01 đã gửi + dòng
// điều chỉnh (chỉ ghi thêm, có lý do; không ghi đè số tự cộng), "Thiếu n ngày", dòng điều chỉnh "cần xem lại" khi BM-01 đổi,
// chốt / mở lại tuần KSGM (số công bố cho KHTC), phiên bản tệp chia sẻ Chi cục (Excel).
// BF-SWEEP-CNTS (#101): bảng danh sách có STT (DataTable); bảng BM-02 giữ bố cục biểu báo cáo (dòng cộng theo xã, dòng tổng).
// Ô điều chỉnh dùng NumberField (được âm: cộng/trừ), lý do mở lại nhập trong pop-up ngắn thay `window.prompt`.
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { NumberField, SelectField, TextField } from "@/components/form";
import { DataTable, PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, dt, num, useAction, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import { ReasonDialog } from "../ReasonDialog";
import { AGG_STATE, DELTA_RULE, adjustmentErrors, createCntsAggApi, normalizeDeltas, type Basis, type CntsBM02 } from "./api";

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
  const [reopening, setReopening] = useState<string | null>(null);
  const facilities = v.rows.map((r) => ({ ref: r.facility_ref, name: r.facility_name }));
  const aggOf = (k: string) => v.aggregates.find((a) => a.key === k);
  const submitAdj = async () => {
    const e = adjustmentErrors(deltas, reason);
    if (!adjWeek) e.week = "Bắt buộc nhập.";
    if (!adjFac) e.facility = "Bắt buộc nhập.";
    setErrs(e);
    if (Object.keys(e).length) return;
    const clean = normalizeDeltas(deltas);
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
        <DataTable
          caption="Chốt tuần KSGM"
          rows={v.weeks}
          rowKey={(w) => w.key}
          columns={[
            { key: "key", header: "Tuần KSGM", cell: (w) => w.key },
            { key: "range", header: "Khoảng ngày", cell: (w) => `${w.from} – ${w.to}` },
            {
              key: "st",
              header: "Trạng thái",
              cell: (w) => {
                const a = aggOf(w.key);
                const st = AGG_STATE[a?.state ?? "none"];
                return (
                  <>
                    <StatusPill tone={st[1]}>{st[0]}</StatusPill>
                    {a?.changed ? <StatusPill tone="warn">Nguồn đã thay đổi</StatusPill> : null}
                  </>
                );
              },
            },
            {
              key: "closes",
              header: "Lần chốt",
              cell: (w) => {
                const a = aggOf(w.key);
                return `${a?.close_count ?? 0}${a?.last_at ? ` · ${dt(a.last_at)}` : ""}`;
              },
            },
            {
              key: "act",
              header: "",
              cell: (w) => {
                const a = aggOf(w.key);
                return (
                  <>
                    {writer && a?.state !== "closed" ? (
                      <button className="btn btn-primary btn-sm" disabled={act.busy}
                        onClick={async () => (await act.run(() => capi.close("CNTS-BM02", w.key), `Đã chốt ${w.key}.`)) && reload()}>Chốt</button>
                    ) : null}
                    {writer && a?.state === "closed" ? (
                      <button className="btn btn-o btn-sm" disabled={act.busy} onClick={() => setReopening(w.key)}>Mở lại</button>
                    ) : null}
                  </>
                );
              },
            },
          ]}
        />
        {reopening ? (
          <ReasonDialog
            title={`Mở lại ${reopening}`}
            label="Lý do mở lại"
            confirmLabel="Mở lại"
            busy={act.busy}
            onClose={() => setReopening(null)}
            onConfirm={async (r) => {
              if (await act.run(() => capi.reopen("CNTS-BM02", reopening, r), `Đã mở lại ${reopening}.`)) {
                setReopening(null);
                reload();
              }
            }}
          />
        ) : null}
      </section>

      <section className="card sec">
        <h2>Số lượng KSGM theo cơ sở</h2>
        {/* Biểu BM-02: giữ bảng báo cáo (dòng cộng theo xã, dòng tổng cộng) như biểu gốc — không phải danh sách. */}
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
          <DataTable
            caption="Dòng điều chỉnh"
            rows={v.adjustments}
            rowKey={(a) => a.id}
            columns={[
              { key: "week", header: "Tuần KSGM", cell: (a) => a.week_key },
              { key: "fac", header: "Cơ sở", cell: (a) => a.facility_name },
              ...v.species.map((s) => ({
                key: `s:${s.key}`,
                header: s.label,
                className: "num",
                cell: (a: (typeof v.adjustments)[number]) => (a.deltas[s.key] && a.deltas[s.key] !== "0" ? num(a.deltas[s.key], 3) : ""),
              })),
              { key: "reason", header: "Lý do", cell: (a) => a.reason },
              { key: "who", header: "Người, thời điểm", cell: (a) => `${a.actor_name} ${dt(a.at)}` },
              { key: "stale", header: "", cell: (a) => (a.stale ? <StatusPill tone="bad">BM-01 đã đổi — xem lại</StatusPill> : null) },
            ]}
          />
        )}
        {writer ? (
          <form className="ws-two" aria-label="Thêm dòng điều chỉnh" onSubmit={(e) => { e.preventDefault(); void submitAdj(); }}>
            <SelectField
              label="Tuần KSGM"
              required
              value={adjWeek}
              onChange={setAdjWeek}
              error={errs.week}
              options={v.weeks.map((w) => ({ value: w.key, label: w.label }))}
            />
            <SelectField
              label="Cơ sở"
              required
              value={adjFac}
              onChange={setAdjFac}
              error={errs.facility}
              placeholder="— Chọn cơ sở —"
              options={facilities.map((f) => ({ value: f.ref, label: f.name }))}
            />
            {v.species.map((s) => (
              <NumberField
                key={s.key}
                label={`Điều chỉnh ${s.label}`}
                hint="Cộng/trừ; số âm để giảm"
                rule={DELTA_RULE}
                value={deltas[s.key] ?? ""}
                onChange={(x) => setDeltas({ ...deltas, [s.key]: x })}
                onBlurError={(m) => setErrs((e) => ({ ...e, [s.key]: m ?? "" }))}
                error={errs[s.key]}
              />
            ))}
            <TextField label="Lý do điều chỉnh" required value={reason} onChange={setReason} maxLength={500} error={errs.reason ?? errs.deltas} hint="10–500 ký tự" />
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
        <DataTable
          caption="Phiên bản đã chia sẻ"
          rows={shares}
          rowKey={(s) => s.id}
          columns={[
            { key: "ver", header: "Phiên bản", cell: (s) => `v${s.version}` },
            { key: "at", header: "Thời điểm", cell: (s) => dt(s.at) },
            { key: "basis", header: "Số liệu", cell: (s) => (s.basis === "closed" ? "Đã chốt" : "Đã gửi") },
            { key: "note", header: "Ghi chú", cell: (s) => s.note || "—" },
            { key: "who", header: "Người tạo", cell: (s) => s.actor_name || "—" },
            {
              key: "dl",
              header: "",
              cell: (s) => (
                <button className="btn btn-o btn-sm" disabled={act.busy} onClick={() => void act.run(() => download(s.id, s.version))}>
                  Tải Excel
                </button>
              ),
            },
          ]}
        />
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
