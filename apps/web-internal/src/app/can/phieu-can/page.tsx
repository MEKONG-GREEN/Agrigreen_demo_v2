"use client";

import { useState } from "react";
import { idem, unwrapAs } from "@/api/client";
import type { LotTickets, ManualTicketResult, WeighTicket } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ActionStatus, Chip, localToIso, nowLocalInput, Provisional, Ref, RefPicker, Section, SelectField, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { ReconView } from "@/components/ReconView";
import { Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { formatKg, isKg, isPct } from "@/lib/kg";
import { validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

const POINTS = ["HUB_IN", "HUB_OUT", "CUSTOMER_IN"] as const;
const SOURCE_LABEL: Record<string, string> = { edge: "Cân biên (ký số)", customer_integration: "Hệ cân khách", manual_evidence: "Nhập tay có bằng chứng" };
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export default function TicketsPage() {
  const { api, principal } = useAuth();
  const [lot, setLot] = useState("");
  const data = useAsync(async () => (UUID.test(lot) ? unwrapAs<LotTickets>(api.GET("/v1/measurement/lots/{id}/tickets", { params: { path: { id: lot } } })) : null), [api, lot]);
  const [point, setPoint] = useState<(typeof POINTS)[number]>("CUSTOMER_IN");
  const [route, setRoute] = useState<"HUB" | "DIRECT">("HUB");
  const [mass, setMass] = useState("");
  const [moist, setMoist] = useState("");
  const [at, setAt] = useState(nowLocalInput());
  const [evidence, setEvidence] = useState("");
  const [reason, setReason] = useState("");
  const [corr, setCorr] = useState("");
  const [result, setResult] = useState<ManualTicketResult | null>(null);
  const act = useAction();
  const canWrite = hasRole(principal, ROLES.scaleSupervisor);
  const effectiveAtPoint = data.data?.tickets.find((t) => t.point === point && t.effective);
  const ok = isKg(mass) && (!moist || isPct(moist)) && UUID.test(evidence) && validReason(reason) && (!effectiveAtPoint || validReason(corr));

  const submit = async () => {
    const r = await act.run(
      () =>
        unwrapAs<ManualTicketResult>(
          api.POST("/v1/measurement/lots/{id}/tickets", {
            params: { path: { id: lot }, header: idem() },
            body: {
              point,
              route,
              mass_kg: mass.trim(),
              ...(moist.trim() ? { moisture_pct: moist.trim(), moisture_state: "as_received" as const } : {}),
              weighed_at: localToIso(at),
              evidence_ref: evidence.trim(),
              reason: reason.trim(),
              ...(effectiveAtPoint ? { supersedes_id: effectiveAtPoint.id, correction_reason: corr.trim() } : {}),
            },
          }),
        ),
      (x) => `Đã ghi phiếu ${x.ticket.point} ${formatKg(x.ticket.mass_kg, 3)}${x.reconciliation ? " — lô đủ phiếu, đã đối soát cùng giao dịch" : ""}`,
    );
    if (r) {
      setResult(r);
      setMass("");
      setCorr("");
      data.reload();
    }
  };

  const mass3 = data.data?.mass;
  return (
    <div className="stack">
      <PageHead title="Phiếu cân theo lô" subtitle="Ba điểm cân HUB_IN → HUB_OUT → CUSTOMER_IN (giao thẳng: chỉ CUSTOMER_IN). Phiếu chỉ ghi thêm; sửa bằng phiếu bù có lý do." />
      <Section title="Chọn lô">
        <RefPicker label="Mã lô" kind="lot" value={lot} onChange={setLot} />
      </Section>
      {data.loading && lot ? <Loading /> : null}
      {data.error ? <ProblemAlert problem={data.error} /> : null}
      {data.data ? (
        <Section title="Khối lượng hiệu lực ba điểm">
          <div className="stack">
            <div className="grid3">
              {POINTS.map((p) => {
                const m = p === "HUB_IN" ? mass3?.hub_in : p === "HUB_OUT" ? mass3?.hub_out : mass3?.customer_in;
                return (
                  <div className="kpi green" key={p} role="group" aria-label={p}>
                    <div className="lb">{p}</div>
                    <div className="vl">{m ? formatKg(m.mass_kg) : "—"}</div>
                    <div className="ft">{m ? formatDateTime(m.event_time) : "Chưa có phiếu"}</div>
                  </div>
                );
              })}
            </div>
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Phiếu cân của lô">
                <thead>
                  <tr>
                    <th>Điểm</th>
                    <th>Nguồn</th>
                    <th className="num">Khối lượng</th>
                    <th className="num">Ẩm</th>
                    <th>Cửa sổ / bằng chứng</th>
                    <th>Quy tắc</th>
                    <th>Hiệu lực</th>
                    <th>Cân lúc</th>
                  </tr>
                </thead>
                <tbody>
                  {data.data.tickets.map((t: WeighTicket) => (
                    <tr key={t.id}>
                      <td>
                        {t.point}
                        {t.route === "DIRECT" ? " (thẳng)" : ""}
                      </td>
                      <td>{SOURCE_LABEL[t.source ?? ""] ?? t.source}</td>
                      <td className="num">{formatKg(t.mass_kg, 3)}</td>
                      <td className="num">{t.moisture_pct ?? "—"}</td>
                      <td className="small">
                        {t.window ? `${t.window.device_id}#${t.window.epoch} · ${t.window.counter_from}–${t.window.counter_to}` : <>bằng chứng <Ref id={t.evidence_ref} /></>}
                        {t.supersedes_id ? (
                          <>
                            {" "}
                            · bù <Ref id={t.supersedes_id} />
                          </>
                        ) : null}
                      </td>
                      <td className="mono">{t.rule_version}</td>
                      <td>{t.effective ? <Chip status="active" label="Hiệu lực" /> : <Chip status="superseded" />}</td>
                      <td>{formatDateTime(t.event_time)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </Section>
      ) : null}
      {data.data && canWrite ? (
        <Section title="Phiếu nhập tay có bằng chứng" actions={<Provisional what="C-5" />}>
          <div className="stack">
            <p className="small mute">
              CUSTOMER_IN khi chưa tích hợp hệ cân khách (C-5); HUB_IN/HUB_OUT chỉ khi cân hỏng (Q-MKG-04). Lô đủ phiếu → đối soát ngay; vượt dung sai → mở tranh chấp.
            </p>
            <div className="grid3">
              <SelectField label="Điểm cân" value={point} onChange={(v) => setPoint(v as typeof point)} options={POINTS.map((p) => ({ value: p, label: p }))} />
              <SelectField label="Tuyến" value={route} onChange={(v) => setRoute(v as "HUB" | "DIRECT")} options={[{ value: "HUB", label: "Qua hub" }, { value: "DIRECT", label: "Giao thẳng" }]} />
              <TextField label="Khối lượng tịnh (kg)" value={mass} onChange={setMass} inputMode="decimal" />
              <TextField label="Độ ẩm khi cân (%)" value={moist} onChange={setMoist} inputMode="decimal" />
              <TextField label="Thời điểm cân" type="datetime-local" value={at} onChange={setAt} />
              <TextField label="Mã tệp bằng chứng" value={evidence} onChange={setEvidence} hint="UUID ảnh phiếu giấy / biên bản trong kho tệp" />
            </div>
            <TextField label="Lý do nhập tay" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
            {effectiveAtPoint ? (
              <TextField label="Lý do bù phiếu" value={corr} onChange={setCorr} hint={`Điểm ${point} đã có phiếu hiệu lực ${formatKg(effectiveAtPoint.mass_kg, 3)} — phiếu mới là phiếu bù (≥ 10 ký tự)`} />
            ) : null}
            <div className="row">
              <button type="button" className="btn btn-p btn-sm" disabled={!ok || act.busy} onClick={() => void submit()}>
                {effectiveAtPoint ? "Ghi phiếu bù" : "Ghi phiếu cân tay"}
              </button>
            </div>
            <ActionStatus a={act} />
            {result?.reconciliation ? <ReconView r={result.reconciliation} /> : null}
          </div>
        </Section>
      ) : null}
    </div>
  );
}
