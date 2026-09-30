"use client";

import { useState } from "react";
import { idem, unwrap, unwrapAs } from "@/api/client";
import type { DispatchResult, DispatchSuggestion } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ActionStatus, FacilitySelect, Ref, Section, TextField, useAction, useFacilities } from "@/components/kit";
import { PageHead } from "@/components/ui";
import { formatKg, isKg } from "@/lib/kg";
import { PRODUCT_LABEL } from "@/lib/rules";

export default function DispatchPage() {
  const { api, principal } = useAuth();
  const facilities = useFacilities();
  const [fid, setFid] = useState("");
  const facility = fid || facilities.data?.[0]?.id || "";
  const [mass, setMass] = useState("");
  const [dest, setDest] = useState("");
  const [sugg, setSugg] = useState<DispatchSuggestion | null>(null);
  const [manual, setManual] = useState<Record<string, string>>({});
  const [done, setDone] = useState<DispatchResult | null>(null);
  const look = useAction();
  const act = useAction();
  const canWrite = hasRole(principal, ROLES.hubOperator);

  const suggest = async () => {
    setDone(null);
    const r = await look.run(() => unwrap(api.GET("/v1/hub/facilities/{id}/dispatch-suggestion", { params: { path: { id: facility }, query: { mass_kg: mass.trim() } } })));
    if (r) {
      setSugg(r);
      setManual({});
    }
  };
  const manualAllocs = Object.entries(manual)
    .filter(([, v]) => v.trim())
    .map(([lot_id, v]) => ({ lot_id, mass_kg: v.trim() }));
  const dispatch = async (useManual: boolean) => {
    const r = await act.run(
      () =>
        unwrapAs<DispatchResult>(
          api.POST("/v1/hub/facilities/{id}/dispatches", {
            params: { path: { id: facility }, header: idem() },
            body: { mass_kg: mass.trim(), ...(dest.trim() ? { destination_ref: dest.trim() } : {}), ...(useManual ? { allocations: manualAllocs } : {}) },
          }),
        ),
      (d) => `Đã xuất ${formatKg(d.dispatch.mass_kg)} theo ${d.dispatch.strategy === "manual" ? "phân bổ chỉ định" : d.dispatch.risk_rule}`,
    );
    if (r) setDone(r);
  };
  const codeOf = (id: string | undefined) => sugg?.lots?.find((l) => l.lot_id === id)?.code ?? "";

  return (
    <div className="stack">
      <PageHead title="Xuất theo rủi ro" subtitle="Đề xuất thứ tự xuất theo điểm rủi ro xuống cấp (không FEFO). Xuất phân bổ ĐỦ theo lô — không đủ tồn theo lô thì không xuất." />
      <Section title="Lượng cần xuất">
        <div className="stack">
          <div className="grid3">
            <FacilitySelect value={facility} onChange={setFid} facilities={facilities.data ?? []} />
            <TextField label="Khối lượng xuất (kg)" value={mass} onChange={setMass} inputMode="decimal" />
            <TextField label="Chuyến / đơn hàng (tuỳ chọn)" value={dest} onChange={setDest} hint="UUID của mkg/logistics, mkg/orders (G4)" />
          </div>
          <div className="row">
            <button type="button" className="btn btn-o btn-sm" disabled={!facility || !isKg(mass) || look.busy} onClick={() => void suggest()}>
              Xem đề xuất theo rủi ro
            </button>
          </div>
          <ActionStatus a={look} />
          <ActionStatus a={act} />
        </div>
      </Section>
      {sugg ? (
        <Section title={`Đề xuất (${sugg.risk_rule})`}>
          <div className="stack">
            {sugg.shortfall_kg ? (
              <div className="alert alert-warn" role="status">
                Thiếu {formatKg(sugg.shortfall_kg, 3)} so với tồn theo lô — không thể xuất đủ lượng này.
              </div>
            ) : null}
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Phân bổ đề xuất">
                <thead>
                  <tr>
                    <th>Lô</th>
                    <th>Dạng hàng</th>
                    <th className="num">Điểm rủi ro</th>
                    <th className="num">Còn lại</th>
                    <th className="num">Đề xuất lấy</th>
                    <th>Chỉ định tay (kg)</th>
                  </tr>
                </thead>
                <tbody>
                  {sugg.lots?.map((l) => {
                    const a = sugg.allocations?.find((x) => x.lot_id === l.lot_id);
                    return (
                      <tr key={l.lot_id}>
                        <td className="mono">{l.code}</td>
                        <td>{PRODUCT_LABEL[l.product ?? ""] ?? l.product}</td>
                        <td className="num">{l.risk_score}</td>
                        <td className="num">{formatKg(l.remaining_kg)}</td>
                        <td className="num">{a ? formatKg(a.mass_kg) : "—"}</td>
                        <td>
                          <input
                            className="input"
                            aria-label={`Chỉ định tay lô ${l.code}`}
                            inputMode="decimal"
                            value={manual[l.lot_id!] ?? ""}
                            onChange={(e) => setManual((m) => ({ ...m, [l.lot_id!]: e.target.value }))}
                            style={{ width: 120 }}
                          />
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            {canWrite ? (
              <div className="row">
                <button type="button" className="btn btn-p btn-sm" disabled={act.busy || !!sugg.shortfall_kg} onClick={() => void dispatch(false)}>
                  Xuất theo đề xuất rủi ro
                </button>
                <button type="button" className="btn btn-o btn-sm" disabled={act.busy || !manualAllocs.length} onClick={() => void dispatch(true)}>
                  Xuất theo chỉ định tay
                </button>
              </div>
            ) : null}
          </div>
        </Section>
      ) : null}
      {done ? (
        <Section title="Phiếu xuất">
          <dl className="kv">
            <dt>Mã phiếu</dt>
            <dd>
              <Ref id={done.dispatch.id} />
            </dd>
            <dt>Cách phân bổ</dt>
            <dd>{done.dispatch.strategy === "manual" ? "Chỉ định tay" : `Theo rủi ro (${done.dispatch.risk_rule})`}</dd>
            {done.dispatch.allocations.map((a) => (
              <FragmentRow key={a.lot_id} label={codeOf(a.lot_id) || (a.lot_id ?? "")} value={formatKg(a.mass_kg, 3)} />
            ))}
          </dl>
        </Section>
      ) : null}
    </div>
  );
}

function FragmentRow({ label, value }: { label: string; value: string }) {
  return (
    <>
      <dt className="mono">{label}</dt>
      <dd>{value}</dd>
    </>
  );
}
