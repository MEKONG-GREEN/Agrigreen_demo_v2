"use client";

import { useState } from "react";
import { idem, unwrap, unwrapAs } from "@/api/client";
import type { BuySideReconciliation, HtxConfirmation, RollCount, RollCountBook, Stage } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { ActionStatus, Chip, localToIso, nowLocalInput, Ref, Section, SelectField, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { isKg } from "@/lib/kg";
import { checkStage, ORDER_STATUS_LABEL, shortRef, STAGE_LABEL, STAGES, validReason } from "@/lib/rules";
import { PROBLEM_MESSAGES } from "@/api/problem";
import { useAsync } from "@/lib/useAsync";

function Book({ orderId, plots, open }: { orderId: string; plots: string[]; open: boolean }) {
  const { api } = useAuth();
  const book = useAsync(() => unwrapAs<RollCountBook>(api.GET("/v1/fieldops/orders/{id}/roll-counts", { params: { path: { id: orderId } } })), [api, orderId]);
  const conf = useAsync(async () => {
    const [c, r] = await Promise.all([
      unwrapAs<{ items: HtxConfirmation[] }>(api.GET("/v1/fieldops/orders/{id}/htx-confirmations", { params: { path: { id: orderId } } })),
      unwrap(api.GET("/v1/fieldops/reconciliation", { params: { query: { order_id: orderId } } })),
    ]);
    return { items: c.items, recon: (r.items ?? []) as BuySideReconciliation[] };
  }, [api, orderId]);
  const [plot, setPlot] = useState(plots[0] ?? "");
  const [stage, setStage] = useState<Stage>("bale");
  const [rolls, setRolls] = useState("");
  const [loose, setLoose] = useState("");
  const [disc, setDisc] = useState("");
  const [at, setAt] = useState(nowLocalInput());
  const [fix, setFix] = useState<RollCount | null>(null);
  const [fixReason, setFixReason] = useState("");
  const [cPlot, setCPlot] = useState(plots[0] ?? "");
  const [outcome, setOutcome] = useState<"confirmed" | "disputed">("confirmed");
  const [htxRolls, setHtxRolls] = useState("");
  const [cReason, setCReason] = useState("");
  const act = useAction();

  const total = (p: string, s: Stage) => book.data?.totals.find((t) => t.plot_ref === p && t.stage === s);
  const effStage = fix?.stage ?? stage;
  const effPlot = fix?.plot_ref ?? plot;
  const idx = STAGES.indexOf(effStage);
  const prev = idx > 0 ? total(effPlot, STAGES[idx - 1]!)?.rolls : undefined;
  const n = /^\d+$/.test(rolls) ? Number(rolls) : NaN;
  const chk = Number.isNaN(n) ? null : checkStage(effStage, n, prev, disc);
  const looseOk = n === 0 ? isKg(loose) : !loose;
  const canSubmit = chk?.ok === true && looseOk && (!fix || validReason(fixReason));
  const needsReason = !Number.isNaN(n) && prev !== undefined && n < prev;

  const submit = async () => {
    const body = {
      rolls: n,
      ...(n === 0 ? { loose_kg: loose.trim() } : {}),
      ...(needsReason ? { discrepancy_reason: disc.trim() } : {}),
      event_time: localToIso(at),
    };
    const r = fix
      ? await act.run(
          () => unwrapAs<unknown>(api.POST("/v1/fieldops/roll-counts/{id}/corrections", { params: { path: { id: fix.id! }, header: idem() }, body: { ...body, correction_reason: fixReason.trim() } })),
          "Đã ghi bản bù (RB-1) — bản gốc giữ nguyên",
        )
      : await act.run(
          () => unwrapAs<unknown>(api.POST("/v1/fieldops/orders/{id}/roll-counts", { params: { path: { id: orderId }, header: idem() }, body: { ...body, plot_ref: plot, stage } })),
          `Đã ghi ${STAGE_LABEL[stage]}: ${n} cuộn${stage === "boat_load" ? " — cập nhật đối soát chiều mua và công bố bảng kê HTX" : ""}`,
        );
    if (r) {
      setRolls("");
      setDisc("");
      setLoose("");
      setFix(null);
      setFixReason("");
      book.reload();
      conf.reload();
    }
  };
  const recon = conf.data?.recon.find((x) => x.plot_ref === cPlot);
  const confirm = async () => {
    const r = await act.run(
      () =>
        unwrapAs<unknown>(
          api.POST("/v1/fieldops/orders/{id}/htx-confirmations", {
            params: { path: { id: orderId }, header: idem() },
            body: {
              plot_ref: cPlot,
              outcome,
              event_time: new Date().toISOString(),
              ...(recon?.statement_revision ? { statement_revision: recon.statement_revision } : {}),
              ...(outcome === "disputed" ? { htx_rolls: Number(htxRolls), reason: cReason.trim() } : {}),
            },
          }),
        ),
      outcome === "confirmed" ? "Đã ghi HTX xác nhận số cuộn" : "Đã ghi HTX tranh chấp số cuộn",
    );
    if (r) {
      setCReason("");
      conf.reload();
    }
  };

  if (book.loading) return <Loading />;
  if (book.error) return <ProblemAlert problem={book.error} />;
  return (
    <div className="stack">
      <div className="tbl-wrap">
        <table className="tbl" aria-label="Số cuộn theo công đoạn">
          <thead>
            <tr>
              <th>Thửa</th>
              {STAGES.map((s) => (
                <th key={s} className="num">
                  {STAGE_LABEL[s]}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {plots.map((p) => (
              <tr key={p}>
                <td>
                  <Ref id={p} />
                </td>
                {STAGES.map((s) => {
                  const t = total(p, s);
                  return (
                    <td key={s} className="num" title={t?.discrepancy_reason ?? undefined}>
                      {t ? (t.rolls > 0 ? t.rolls.toLocaleString("vi-VN") : `${t.loose_kg} kg rời`) : "—"}
                      {t?.discrepancy_reason ? " *" : ""}
                    </td>
                  );
                })}
              </tr>
            ))}
            <tr>
              <td>
                <b>Tổng lệnh</b>
              </td>
              {STAGES.map((s) => (
                <td key={s} className="num">
                  <b>{(book.data?.order_totals[s] ?? 0).toLocaleString("vi-VN")}</b>
                </td>
              ))}
            </tr>
          </tbody>
        </table>
      </div>
      <details>
        <summary>Sổ đếm cuộn (cả bản bị bù)</summary>
        <div className="tbl-wrap">
          <table className="tbl" aria-label="Sổ đếm cuộn">
            <thead>
              <tr>
                <th>#</th>
                <th>Thửa</th>
                <th>Công đoạn</th>
                <th className="num">Cuộn</th>
                <th>Lý do chênh / bù</th>
                <th>Lúc</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {book.data?.counts.map((c) => {
                const superseded = book.data!.counts.some((x) => x.supersedes_id === c.id);
                return (
                  <tr key={c.id}>
                    <td>{c.seq}</td>
                    <td>
                      <Ref id={c.plot_ref} />
                    </td>
                    <td>{STAGE_LABEL[c.stage!]}</td>
                    <td className="num">{c.rolls}</td>
                    <td className="small">{c.correction_reason ?? c.discrepancy_reason ?? ""}</td>
                    <td>{formatDateTime(c.event_time)}</td>
                    <td>
                      {superseded ? (
                        <Chip status="superseded" />
                      ) : open ? (
                        <button type="button" className="btn btn-o btn-sm" onClick={() => setFix(c)} aria-label={`Bù số ${STAGE_LABEL[c.stage!]} thửa ${shortRef(c.plot_ref)}`}>
                          Bù số
                        </button>
                      ) : null}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </details>
      <ActionStatus a={act} />
      {open ? (
        <div className="panel stack">
          <b>{fix ? `Bản bù cho ${STAGE_LABEL[fix.stage!]} thửa ${shortRef(fix.plot_ref)} (${fix.rolls} cuộn)` : "Ghi số một công đoạn"}</b>
          <div className="grid3">
            {!fix ? (
              <>
                <SelectField label="Thửa" value={plot} onChange={setPlot} options={plots.map((p) => ({ value: p, label: shortRef(p) }))} />
                <SelectField label="Công đoạn" value={stage} onChange={(v) => setStage(v as Stage)} options={STAGES.map((s) => ({ value: s, label: STAGE_LABEL[s] }))} />
              </>
            ) : null}
            <TextField label="Số cuộn" value={rolls} onChange={setRolls} inputMode="numeric" hint={prev !== undefined ? `Công đoạn trước: ${prev} cuộn (FM-04)` : idx > 0 ? "Công đoạn trước chưa có số" : undefined} />
            {n === 0 ? <TextField label="Rơm rời (kg)" value={loose} onChange={setLoose} hint="Chỉ khi số cuộn = 0 (FM-09)" /> : null}
            <TextField label="Thời điểm" type="datetime-local" value={at} onChange={setAt} />
          </div>
          {needsReason ? <TextField label="Lý do chênh lệch" value={disc} onChange={setDisc} hint="Ít hơn công đoạn trước — bắt buộc ≥ 10 ký tự" /> : null}
          {fix ? <TextField label="Lý do bù" value={fixReason} onChange={setFixReason} hint="Bắt buộc ≥ 10 ký tự" /> : null}
          {chk && !chk.ok ? (
            <p className="pill-bad" role="alert">
              {PROBLEM_MESSAGES[chk.code]}
            </p>
          ) : null}
          <div className="row">
            <button type="button" className="btn btn-p btn-sm" disabled={!canSubmit || act.busy} onClick={() => void submit()}>
              {fix ? "Ghi bản bù" : "Ghi số cuộn"}
            </button>
            {fix ? (
              <button type="button" className="btn btn-o btn-sm" onClick={() => setFix(null)}>
                Huỷ bù
              </button>
            ) : null}
          </div>
        </div>
      ) : null}
      <Section title="Xác nhận của HTX (nhân viên MKG ghi thay ở G3)">
        <div className="stack">
          {conf.data?.items.length ? (
            <ul className="list-plain small" aria-label="Xác nhận HTX">
              {conf.data.items.map((c) => (
                <li key={c.id}>
                  Thửa <Ref id={c.plot_ref} /> · bảng kê v{c.statement_revision} · <Chip status={c.outcome === "confirmed" ? "match" : "mismatch"} label={c.outcome === "confirmed" ? "HTX xác nhận" : "HTX tranh chấp"} /> MKG {c.mkg_rolls} / HTX {c.htx_rolls}
                  {c.reason ? ` — ${c.reason}` : ""}
                </li>
              ))}
            </ul>
          ) : (
            <p className="small mute">Chưa có xác nhận.</p>
          )}
          <div className="grid3">
            <SelectField label="Thửa xác nhận" value={cPlot} onChange={setCPlot} options={plots.map((p) => ({ value: p, label: shortRef(p) }))} />
            <SelectField label="Kết quả HTX" value={outcome} onChange={(v) => setOutcome(v as typeof outcome)} options={[{ value: "confirmed", label: "Xác nhận đúng số MKG" }, { value: "disputed", label: "Tranh chấp số cuộn" }]} />
            {outcome === "disputed" ? <TextField label="Số cuộn HTX đếm" value={htxRolls} onChange={setHtxRolls} inputMode="numeric" /> : null}
          </div>
          {outcome === "disputed" ? <TextField label="Lý do tranh chấp" value={cReason} onChange={setCReason} hint="Ít nhất 10 ký tự" /> : null}
          <p className="small mute">{recon ? `Bảng kê HTX phiên bản ${recon.statement_revision}, MKG ${recon.mkg_rolls} cuộn — trạng thái ${recon.status}` : "Thửa chưa có số xếp ghe/xe — chưa có gì để xác nhận."}</p>
          <div className="row">
            <button type="button" className="btn btn-p btn-sm" disabled={act.busy || !recon || (outcome === "disputed" && (!/^\d+$/.test(htxRolls) || !validReason(cReason)))} onClick={() => void confirm()}>
              Ghi xác nhận HTX
            </button>
          </div>
        </div>
      </Section>
    </div>
  );
}

export default function RollCountsPage() {
  const { api } = useAuth();
  const orders = useAsync(async () => (await unwrap(api.GET("/v1/fieldops/orders", { params: { query: { limit: 200 } } }))).items ?? [], [api]);
  const usable = (orders.data ?? []).filter((o) => o.status !== "planned" && o.status !== "cancelled");
  const [sel, setSel] = useState("");
  const order = usable.find((o) => o.order_id === sel) ?? usable[0];
  return (
    <div className="stack">
      <PageHead
        title="Đếm cuộn & xác nhận HTX"
        subtitle="Cuộn rơm → tập kết → xếp ghe/xe: công đoạn trước phải có số (FM-03); bằng hoặc ít hơn kèm lý do (FM-04); bắt buộc số cuộn, kg rơm rời chỉ khi 0 cuộn (FM-09). Sửa bằng bản bù."
      />
      <Section title="Lệnh gom">
        {orders.loading ? <Loading /> : null}
        {orders.error ? <ProblemAlert problem={orders.error} /> : null}
        <SelectField
          label="Chọn lệnh gom"
          value={order?.order_id ?? ""}
          onChange={setSel}
          options={usable.map((o) => ({ value: o.order_id ?? "", label: `${shortRef(o.order_id)} · HTX ${shortRef(o.htx_ref)} · ${ORDER_STATUS_LABEL[o.status ?? ""]} · ${o.plot_refs?.length} thửa` }))}
        />
      </Section>
      {order ? (
        <Section title={`Số cuộn — lệnh ${shortRef(order.order_id)}`}>
          <Book key={order.order_id} orderId={order.order_id!} plots={order.plot_refs ?? []} open={order.status === "approved" || order.status === "assigned"} />
        </Section>
      ) : null}
    </div>
  );
}
