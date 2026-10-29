"use client";
import { useState } from "react";
import { SelectField } from "@/components/form";
import { useAction } from "@/kn/cycle/common";
import { type KhtcPeriod } from "../periods/api";
import { type PreviewInput, type TemplateApi, type TemplateDefinition, type TemplatePreview } from "./api";
import { FIELD_LABELS, SOURCE_LABELS } from "./template";

export function TemplatePreviewPanel({ api, definition, periods }: { api: TemplateApi; definition: TemplateDefinition; periods: KhtcPeriod[] }) {
  const [selected, setSelected] = useState("");
  const [scenario, setScenario] = useState<PreviewInput["scenario"]>("actual");
  const [result, setResult] = useState<{ value: TemplatePreview; definition: string } | null>(null);
  const act = useAction();
  const period = periods.find((p) => p.id === selected) ?? periods[0];
  const current = JSON.stringify(definition);
  const preview = result?.definition === current ? result.value : null;
  const render = async () => {
    const fingerprint = current;
    const value = await act.run(() => api.preview(definition.key, { definition, period_id: period?.id ?? "00000000-0000-0000-0000-000000000000", scenario }));
    if (value) setResult({ value, definition: fingerprint });
  };
  return <section className="card sec" aria-label="Xem trước mẫu báo cáo">
    <h2>Xem trước theo kỳ báo cáo</h2>
    <SelectField label="Kỳ xem trước" value={period?.id ?? ""} onChange={(v) => { setSelected(v); setResult(null); }} options={periods.map((p) => ({ value: p.id, label: p.name }))} placeholder="Kỳ mới nhất phù hợp" />
    <SelectField label="Tình huống" value={scenario ?? "actual"} onChange={(v) => { setScenario(v as PreviewInput["scenario"]); setResult(null); }} options={[
      { value: "actual", label: "Trạng thái dữ liệu thật" }, { value: "normal", label: "Mô phỏng bình thường" }, { value: "no_send", label: "Mô phỏng chưa gửi" }, { value: "unclosed", label: "Mô phỏng chưa chốt" }, { value: "no_activity", label: "Mô phỏng không hoạt động" }, { value: "provisional", label: "Mô phỏng Tạm tính" },
    ]} />
    <button type="button" className="btn" disabled={act.busy} onClick={() => void render()}>Xem trước toàn mẫu</button>
    {act.view}
    {result && !preview ? <p role="status">Nội dung đã thay đổi. Bấm Xem trước để cập nhật.</p> : null}
    {preview ? <article aria-label="Nội dung xem trước">
      <h3>{preview.context.period_name}</h3>
      {preview.scenario !== "actual" && preview.scenario !== "" ? <p className="alert alert-warn">Đang xem tình huống mô phỏng.</p> : null}
      {preview.document.provisional.length ? <div className="alert alert-warn"><b>Tạm tính</b><ul>{preview.document.provisional.map((p, i) => <li key={i}>{p}</li>)}</ul></div> : null}
      {preview.document.nodes.map((node) => <section key={node.id} style={{ marginInlineStart: `${node.depth * 12}px` }}>
        <h3>{node.number}. {node.title}</h3>
        {node.rows.map((row, i) => <div key={`${row.ref}:${i}`}>
          {row.preamble ? <p><strong>{row.preamble}</strong></p> : null}
          <p style={{ whiteSpace: "pre-wrap" }}>{row.tokens.map((token, j) => <span key={j} title={token.source ? `${SOURCE_LABELS[token.source as keyof typeof SOURCE_LABELS] ?? "Trạng thái nguồn"}: ${FIELD_LABELS[token.field ?? ""] ?? ""}${token.locked ? " — khóa" : ""}` : undefined} style={token.source ? { background: token.locked ? "#e4f0fa" : "#fff3cc", borderRadius: 3 } : undefined}>{token.text}</span>)}</p>
        </div>)}
      </section>)}
    </article> : null}
  </section>;
}
