"use client";

import { useState } from "react";
import { idem, unwrap, unwrapAs } from "@/api/client";
import type { LotTickets, Product, ReceiptResult } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { ActionStatus, Chip, FacilitySelect, Ref, RefPicker, Section, SelectField, TextField, useAction, useFacilities } from "@/components/kit";
import { PageHead } from "@/components/ui";
import { formatKg, isKg, isPct } from "@/lib/kg";
import { PRODUCT_LABEL, validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

type SourceKind = "plot" | "cooperative" | "roll_count" | "supplier" | "lot" | "stocktake";
interface SourceRow {
  kind: SourceKind;
  ref: string;
  mass_kg: string;
}

const SOURCE_LABEL: Record<SourceKind, string> = { plot: "Thửa", cooperative: "HTX", roll_count: "Lô gom (số cuộn)", supplier: "Nhà cung cấp", lot: "Lô khác", stocktake: "Kiểm kê" };

export default function ReceiptPage() {
  const { api } = useAuth();
  const facilities = useFacilities();
  const [fid, setFid] = useState("");
  const facility = fid || facilities.data?.[0]?.id || "";
  const [mode, setMode] = useState<"ticket" | "manual">("ticket");
  const [lotRef, setLotRef] = useState("");
  const [product, setProduct] = useState<Product>("straw_bale");
  const [mass, setMass] = useState("");
  const [moisture, setMoisture] = useState("");
  const [manualReason, setManualReason] = useState("");
  const [sources, setSources] = useState<SourceRow[]>([{ kind: "cooperative", ref: "", mass_kg: "" }]);
  const [result, setResult] = useState<ReceiptResult | null>(null);
  const act = useAction();

  const lotTickets = useAsync(
    async () => (mode === "ticket" && /^[0-9a-f-]{36}$/i.test(lotRef) ? unwrapAs<LotTickets>(api.GET("/v1/measurement/lots/{id}/tickets", { params: { path: { id: lotRef } } })) : null),
    [api, lotRef, mode],
  );
  const hubIn = lotTickets.data?.tickets.find((t) => t.point === "HUB_IN" && t.effective);

  const srcOk = sources.every((s) => /^[0-9a-f-]{36}$/i.test(s.ref.trim()) && (sources.length === 1 || isKg(s.mass_kg)));
  const manualOk = isKg(mass) && (!moisture || isPct(moisture));
  const canSubmit = !!facility && srcOk && (mode === "ticket" ? !!hubIn : manualOk);

  const submit = async () => {
    const body = {
      product,
      sources: sources.map((s) => ({
        kind: s.kind,
        ref: s.ref.trim(),
        ...(sources.length > 1 ? { mass_kg: s.mass_kg.trim() } : { ratio: "1.000000" }),
      })),
      ...(mode === "ticket" ? { weigh_ticket_ref: hubIn!.id } : { mass_kg: mass.trim(), ...(moisture.trim() ? { moisture_pct: moisture.trim() } : {}), ...(manualReason.trim() ? { manual_reason: manualReason.trim() } : {}) }),
    };
    const r = await act.run(
      () => unwrap(api.POST("/v1/hub/facilities/{id}/receipts", { params: { path: { id: facility }, header: idem() }, body })),
      (x) => `Đã nhận lô ${x.lot?.code} — ${formatKg(x.lot?.initial_kg, 3)}${x.over_capacity ? " (VƯỢT sức chứa — vẫn ghi nhận)" : ""}`,
    );
    if (r) setResult(r);
  };

  const setSource = (i: number, p: Partial<SourceRow>) => setSources((rows) => rows.map((r, j) => (j === i ? { ...r, ...p } : r)));

  return (
    <div className="stack">
      <PageHead title="Nhận hàng" subtitle="Nhận một lô có N nguồn (tỷ lệ Σ = 1). Khối lượng lô chuẩn = phiếu cân HUB_IN; nhập tay chỉ khi cân hỏng, có lý do (Q-MKG-04)." />
      <Section title="Lô nhận">
        <div className="stack">
          <div className="grid3">
            <FacilitySelect value={facility} onChange={setFid} facilities={facilities.data ?? []} />
            <SelectField label="Cách xác định khối lượng" value={mode} onChange={(v) => setMode(v as "ticket" | "manual")} options={[{ value: "ticket", label: "Theo phiếu cân HUB_IN" }, { value: "manual", label: "Nhập tay (cân hỏng)" }]} />
            <SelectField label="Dạng hàng" value={product} onChange={(v) => setProduct(v as Product)} options={Object.entries(PRODUCT_LABEL).map(([value, label]) => ({ value, label }))} />
          </div>
          {mode === "ticket" ? (
            <>
              <RefPicker label="Mã lô trên phiếu cân" kind="lot" value={lotRef} onChange={setLotRef} hint="Lô gom đã qua cân HUB_IN tại hub" />
              {hubIn ? (
                <div className="panel" role="status" aria-label="Phiếu cân HUB_IN">
                  Phiếu cân HUB_IN <Ref id={hubIn.id} /> · <b>{formatKg(hubIn.mass_kg, 3)}</b> · ẩm {hubIn.moisture_pct ?? "—"} % · nguồn {hubIn.source} · <span className="mono">{hubIn.rule_version}</span>
                </div>
              ) : lotRef && !lotTickets.loading ? (
                <p className="notice">Lô chưa có phiếu cân HUB_IN hiệu lực.</p>
              ) : null}
            </>
          ) : (
            <div className="grid3">
              <TextField label="Khối lượng nhập tay (kg)" value={mass} onChange={setMass} inputMode="decimal" />
              <TextField label="Độ ẩm (%)" value={moisture} onChange={setMoisture} inputMode="decimal" />
              <TextField label="Lý do nhập tay" value={manualReason} onChange={setManualReason} hint={validReason(manualReason) || !manualReason ? "Bắt buộc khi hub.weigh.v1 ticket_required" : "Ít nhất 10 ký tự"} />
            </div>
          )}
          <fieldset className="panel stack">
            <legend>Nguồn của lô</legend>
            {sources.map((s, i) => (
              <div className="grid3" key={i}>
                <SelectField label={`Loại nguồn ${i + 1}`} value={s.kind} onChange={(v) => setSource(i, { kind: v as SourceKind })} options={Object.entries(SOURCE_LABEL).map(([value, label]) => ({ value, label }))} />
                <TextField label={`Mã nguồn ${i + 1}`} value={s.ref} onChange={(v) => setSource(i, { ref: v })} placeholder="UUID" />
                <TextField label={`Khối lượng nguồn ${i + 1} (kg)`} value={s.mass_kg} onChange={(v) => setSource(i, { mass_kg: v })} hint="Bắt buộc khi nhiều nguồn (chia tỷ lệ dư lớn nhất)" inputMode="decimal" />
              </div>
            ))}
            <div className="row">
              <button type="button" className="btn btn-o btn-sm" onClick={() => setSources((r) => [...r, { kind: "cooperative", ref: "", mass_kg: "" }])}>
                Thêm nguồn
              </button>
              {sources.length > 1 ? (
                <button type="button" className="btn btn-o btn-sm" onClick={() => setSources((r) => r.slice(0, -1))}>
                  Bớt nguồn
                </button>
              ) : null}
            </div>
          </fieldset>
          <div className="row">
            <button type="button" className="btn btn-p btn-sm" disabled={!canSubmit || act.busy} onClick={() => void submit()}>
              Ghi nhận lô
            </button>
          </div>
          <ActionStatus a={act} />
        </div>
      </Section>
      {result ? (
        <Section title="Lô vừa nhận">
          <dl className="kv">
            <dt>Mã lô</dt>
            <dd>
              {result.lot?.code} <Ref id={result.lot?.id} />
            </dd>
            <dt>Khối lượng</dt>
            <dd>{formatKg(result.lot?.initial_kg, 3)}</dd>
            <dt>Nguồn (tỷ lệ)</dt>
            <dd>{result.lot?.sources?.map((s) => `${s.kind} ${s.ratio}`).join(" · ")}</dd>
            <dt>Sức chứa</dt>
            <dd>{result.over_capacity ? <Chip status="critical" label="Vượt sức chứa" /> : <Chip status="within" label="Trong sức chứa" />}</dd>
          </dl>
        </Section>
      ) : null}
    </div>
  );
}
