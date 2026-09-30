"use client";

import { useState } from "react";
import { idem, unwrapAs } from "@/api/client";
import type { CustomerImportResult } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { ActionStatus, Chip, Provisional, Ref, Section, SelectField, TextField, useAction } from "@/components/kit";
import { PageHead } from "@/components/ui";
import { addKg, formatKg } from "@/lib/kg";
import { validReason } from "@/lib/rules";
import { parseWeightsCsv, sha256Hex } from "@/lib/weights";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export default function CustomerImportPage() {
  const { api } = useAuth();
  const [channel, setChannel] = useState<"file" | "api" | "manual">("file");
  const [customer, setCustomer] = useState("");
  const [evidence, setEvidence] = useState("");
  const [reason, setReason] = useState("");
  const [csv, setCsv] = useState("");
  const [sha, setSha] = useState("");
  const [result, setResult] = useState<CustomerImportResult | null>(null);
  const act = useAction();
  const parsed = csv.trim() ? parseWeightsCsv(csv) : null;
  const needEvidence = channel !== "api";
  const ok = !!parsed && parsed.errors.length === 0 && (!needEvidence || UUID.test(evidence)) && (channel !== "manual" || validReason(reason)) && (!customer || UUID.test(customer));

  const onFile = async (f: File | undefined) => {
    if (!f) return;
    const buf = await f.arrayBuffer();
    setCsv(new TextDecoder("utf-8").decode(buf));
    setSha(await sha256Hex(buf));
  };
  const submit = async () => {
    if (!parsed) return;
    const r = await act.run(
      () =>
        unwrapAs<CustomerImportResult>(
          api.POST("/v1/measurement/customer-weight-imports", {
            params: { header: idem() },
            body: {
              channel,
              rows: parsed.rows,
              ...(customer ? { customer_ref: customer } : {}),
              ...(evidence ? { evidence_ref: evidence } : {}),
              ...(sha && channel === "file" ? { file_sha256: sha } : {}),
              ...(reason.trim() ? { reason: reason.trim() } : {}),
            },
          }),
        ),
      (x) => `Đã nhập ${x.import.rows} dòng → ${x.import.ticket_ids.length} phiếu CUSTOMER_IN, ${x.reconciliations.length} lô đã đối soát tự động`,
    );
    if (r) {
      setResult(r);
      setCsv("");
      setSha("");
    }
  };

  return (
    <div className="stack">
      <PageHead title="Nhập số cân khách" subtitle="Mỗi dòng → một phiếu CUSTOMER_IN trong cùng giao dịch; một dòng sai → cả lần bị từ chối. Tối đa 200 dòng." actions={<Provisional what="C-5" />} />
      <Section title="Lần nhập">
        <div className="stack">
          <div className="grid3">
            <SelectField
              label="Kênh nhận"
              value={channel}
              onChange={(v) => setChannel(v as typeof channel)}
              options={[
                { value: "file", label: "Tệp của khách (CSV)" },
                { value: "api", label: "API hệ cân khách" },
                { value: "manual", label: "Nhập tay có bằng chứng" },
              ]}
            />
            <TextField label="Mã khách hàng" value={customer} onChange={setCustomer} hint="UUID (tuỳ chọn) — danh tính khách không bao giờ qua van" />
            {needEvidence ? <TextField label="Mã tệp bằng chứng" value={evidence} onChange={setEvidence} hint="Bắt buộc với tệp và nhập tay" /> : null}
          </div>
          {channel === "manual" ? <TextField label="Lý do nhập tay" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" /> : null}
          <div className="field">
            <label htmlFor="weights-file">Tệp CSV</label>
            <input id="weights-file" type="file" accept=".csv,text/csv" onChange={(e) => void onFile(e.target.files?.[0])} />
            {sha ? <span className="small mono">SHA-256 {sha}</span> : null}
          </div>
          <TextField
            label="Bảng số cân (CSV)"
            value={csv}
            onChange={(v) => {
              setCsv(v);
              setSha("");
            }}
            multiline
            hint="Cột: lot_ref, mass_kg, weighed_at, route, moisture_pct, customer_ticket_no — có thể kèm dòng tiêu đề"
          />
          {parsed ? (
            parsed.errors.length ? (
              <div className="alert alert-bad" role="alert">
                <ul style={{ margin: 0, paddingLeft: 18 }}>
                  {parsed.errors.map((e, i) => (
                    <li key={i}>
                      {e.line ? `Dòng ${e.line}: ` : ""}
                      {e.reason}
                    </li>
                  ))}
                </ul>
              </div>
            ) : (
              <p className="small" role="status">
                {parsed.rows.length} dòng hợp lệ · tổng {formatKg(addKg("0", ...parsed.rows.map((r) => r.mass_kg)))}
              </p>
            )
          ) : null}
          <div className="row">
            <button type="button" className="btn btn-p btn-sm" disabled={!ok || act.busy} onClick={() => void submit()}>
              Nhập số cân
            </button>
          </div>
          <ActionStatus a={act} />
        </div>
      </Section>
      {result ? (
        <Section title="Kết quả đối soát tự động">
          <div className="tbl-wrap">
            <table className="tbl" aria-label="Đối soát sau khi nhập">
              <thead>
                <tr>
                  <th>Lô</th>
                  <th>Kết quả</th>
                  <th>Thương mại</th>
                  <th>Cuộn ↔ khối lượng</th>
                  <th>Tranh chấp</th>
                </tr>
              </thead>
              <tbody>
                {result.reconciliations.map((r) => (
                  <tr key={r.id}>
                    <td>
                      <Ref id={r.lot_ref} />
                    </td>
                    <td>
                      <Chip status={r.outcome} />
                    </td>
                    <td>
                      <Chip status={r.commercial?.status} /> {r.commercial?.diff_pct ?? ""}
                    </td>
                    <td>
                      <Chip status={r.roll_check?.status} />
                    </td>
                    <td>
                      <Ref id={r.dispute_id} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      ) : null}
    </div>
  );
}
