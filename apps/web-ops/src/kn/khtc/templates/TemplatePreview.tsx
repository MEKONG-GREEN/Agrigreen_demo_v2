"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { SelectField } from "@/components/form";
import { useAction } from "@/kn/cycle/common";
import { type KhtcPeriod } from "../periods/api";
import { type PreviewInput, type TemplateApi, type TemplateDefinition, type TemplatePreview } from "./api";
import { CompiledTemplateDocument } from "./CompiledTemplateDocument";

export function TemplatePreviewPanel({ api, definition, periods }: { api: TemplateApi; definition: TemplateDefinition; periods: KhtcPeriod[] }) {
  const [selected, setSelected] = useState("");
  const [scenario, setScenario] = useState<PreviewInput["scenario"]>("actual");
  const [result, setResult] = useState<{ value: TemplatePreview; fingerprint: string } | null>(null);
  const [requested, setRequested] = useState<string | null>(null);
  const requestNo = useRef(0);
  const invalidate = useCallback(() => { requestNo.current++; }, []);
  const act = useAction();
  const period = periods.find((p) => p.id === selected) ?? periods[0];
  const current = JSON.stringify({ definition, period: period?.id ?? null, scenario });
  useEffect(() => { invalidate(); return invalidate; }, [current, invalidate]);
  const preview = result?.fingerprint === current ? result.value : null;
  const render = async () => {
    const fingerprint = current, request = ++requestNo.current;
    setRequested(fingerprint);
    const value = await act.run(() => api.preview(definition.key, { definition, period_id: period?.id ?? "00000000-0000-0000-0000-000000000000", scenario }));
    if (value && requestNo.current === request) setResult({ value, fingerprint });
  };
  return <section className="card sec" aria-label="Xem trước mẫu báo cáo">
    <h2>Xem trước theo kỳ báo cáo</h2>
    <SelectField label="Kỳ xem trước" value={period?.id ?? ""} onChange={(v) => { requestNo.current++; setSelected(v); setResult(null); }} options={periods.map((p) => ({ value: p.id, label: p.name }))} placeholder="Kỳ mới nhất phù hợp" />
    <SelectField label="Tình huống" value={scenario ?? "actual"} onChange={(v) => { requestNo.current++; setScenario(v as PreviewInput["scenario"]); setResult(null); }} options={[
      { value: "actual", label: "Trạng thái dữ liệu thật" }, { value: "normal", label: "Mô phỏng bình thường" }, { value: "no_send", label: "Mô phỏng chưa gửi" }, { value: "unclosed", label: "Mô phỏng chưa chốt" }, { value: "no_activity", label: "Mô phỏng không hoạt động" }, { value: "provisional", label: "Mô phỏng Tạm tính" },
    ]} />
    <button type="button" className="btn" disabled={act.busy} onClick={() => void render()}>Xem trước toàn mẫu</button>
    {requested === current ? act.view : null}
    {result && !preview ? <p role="status">Nội dung đã thay đổi. Bấm Xem trước để cập nhật.</p> : null}
    {preview ? <article aria-label="Nội dung xem trước">
      <h3>{preview.context.period_name}</h3>
      {preview.scenario !== "actual" && preview.scenario !== "" ? <p className="alert alert-warn">Đang xem tình huống mô phỏng.</p> : null}
      <CompiledTemplateDocument document={preview.document} />
    </article> : null}
  </section>;
}
