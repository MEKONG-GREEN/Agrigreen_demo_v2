"use client";

import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { CoverageChip, SourceLabel } from "@/components/CoverageLegend";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { useAsync } from "@/lib/useAsync";
import { fromMechStatus } from "./coverage";
import type { CoopPin } from "./pins";

/**
 * Panel chi tiết HTX (BRD CGH FN-08 B5, AC-02): máy theo khâu kèm tình trạng + nhãn nguồn, chủ sở hữu
 * CHỈ nhãn đã che và SĐT che (mục 10.4), mức đáp ứng đã lưu của lần cân đối đang xem.
 * GET /v1/mechanization/cooperatives/{ref}/panel.
 */
export function CoopPanel({
  pin,
  runId,
  stageNames,
  onClose,
}: {
  pin: CoopPin;
  runId: string;
  stageNames: ReadonlyMap<string, string>;
  onClose: () => void;
}) {
  const { api } = useAuth();
  const panel = useAsync(
    () => unwrap(api.GET("/v1/mechanization/cooperatives/{ref}/panel", { params: { path: { ref: pin.ref }, query: { run_id: runId || undefined } } })),
    [api, pin.ref, runId],
  );
  const p = panel.data;
  const byStage = new Map<string, NonNullable<typeof p>["machines"]>();
  for (const m of p?.machines ?? []) byStage.set(m.farm_stage, [...(byStage.get(m.farm_stage) ?? []), m]);
  const cov = p?.coverage ?? null;

  return (
    <div data-testid="coop-panel">
      <div className="sec-h">
        <h2>{pin.name}</h2>
        <button type="button" className="btn btn-o btn-sm" onClick={onClose}>
          Đóng
        </button>
      </div>
      <div className="small" style={{ marginBottom: 8 }}>
        <code>{pin.code ?? "—"}</code> · {pin.communeName ?? pin.communeCode ?? "—"}
        {pin.provisional ? (
          <span className="chip c-warn" style={{ marginLeft: 6 }} title="Chưa có toạ độ — ghim đặt ở tâm xã">
            vị trí tạm
          </span>
        ) : null}
      </div>
      {panel.error ? <ProblemAlert problem={panel.error} onRetry={panel.reload} /> : null}
      {panel.loading && !p ? <Loading /> : null}
      {p ? (
        <>
          <div className="row" style={{ justifyContent: "space-between", marginBottom: 6 }}>
            <b>Mức đáp ứng</b>
            {cov ? <CoverageChip status={fromMechStatus(cov.overall.status)} /> : <span className="mute small">Chưa có kết quả cân đối</span>}
          </div>
          {cov ? (
            <ul className="list-plain small" style={{ marginBottom: 10 }} aria-label="Mức đáp ứng theo khâu">
              {Object.entries(cov.stages).map(([k, g]) => (
                <li key={k} data-stage={k}>
                  {stageNames.get(k) ?? k}: {g.coverage_pct === null ? g.status_label : `${formatDecimal(g.coverage_pct, 1)}% · ${g.status_label}`} · cần{" "}
                  {formatInt(g.required_whole)}, hoạt động {formatInt(g.capacity)}
                </li>
              ))}
            </ul>
          ) : null}
          {cov?.area_source_label ? (
            <p className="small" style={{ marginBottom: 8 }}>
              Diện tích: <SourceLabel source={cov.area_source} label={cov.area_source_label} />
            </p>
          ) : null}
          {p.machines.length === 0 ? <p className="mute small">{p.note ?? "Chưa có máy"}</p> : null}
          {Array.from(byStage.entries()).map(([stageKey, ms]) => (
            <section key={stageKey} style={{ marginBottom: 10 }} aria-label={`Máy khâu ${stageNames.get(stageKey) ?? stageKey}`}>
              <b className="small">
                {stageNames.get(stageKey) ?? stageKey} ({ms.length} máy)
              </b>
              <ul className="list-plain small">
                {ms.map((m) => (
                  <li key={m.ref} data-machine={m.code}>
                    <code>{m.code}</code> {m.machine_type} · {m.status_label} <span className="mute">[{m.source_label}]</span> · {m.owner_label} {m.owner_masked_phone}
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </>
      ) : null}
    </div>
  );
}
