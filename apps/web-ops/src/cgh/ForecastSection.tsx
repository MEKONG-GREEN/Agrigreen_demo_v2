"use client";

import { useMemo, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { ApiProblem } from "@/api/problem";
import type { MechForecastResult } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { formatDate, formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

const SEASON_NAME: Record<string, string> = { DX: "Đông Xuân", HT: "Hè Thu", TD: "Thu Đông" };
const STAGE_NAME: Record<string, string> = { lam_dat: "Làm đất", gieo_sa: "Gieo sạ/cấy", cham_soc: "Chăm sóc", thu_hoach: "Thu hoạch" };

/** Chạy dự báo (Admin API, vai cgh_admin, MFA). Kết quả lưu bất biến, đọc lại qua Application API. */
function RunForecast({ onDone }: { onDone: (id: string) => void }) {
  const { adminApi, principal } = useAuth();
  const [season, setSeason] = useState("DX");
  const [year, setYear] = useState(String(new Date().getFullYear()));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [idemKey, setIdemKey] = useState(newIdempotencyKey);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    try {
      const res = await unwrap(
        adminApi.POST("/v1/mechanization/forecasts", {
          params: { header: { "Idempotency-Key": idemKey } },
          body: { season_code: season, season_year: Number(year) },
        }),
      );
      setIdemKey(newIdempotencyKey());
      const id = (res.run as { id?: unknown }).id;
      if (typeof id === "string") onDone(id);
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="panel stack" style={{ gap: 10 }} onSubmit={(e) => void submit(e)}>
      <b>Chạy dự báo mới (quản trị CGH)</b>
      {principal && !principal.mfa ? (
        <p className="small" style={{ color: "var(--amber)" }}>
          Cần đăng nhập có OTP — Admin API sẽ trả <code>mfa_required</code>.
        </p>
      ) : null}
      <div className="row">
        <div className="field">
          <label htmlFor="fc-season">Vụ</label>
          <select id="fc-season" className="select" value={season} onChange={(e) => {
              setSeason(e.target.value);
              setIdemKey(newIdempotencyKey());
            }}>
            {Object.entries(SEASON_NAME).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="fc-year">Năm bắt đầu vụ</label>
          <input id="fc-year" className="input" inputMode="numeric" value={year} onChange={(e) => {
              setYear(e.target.value);
              setIdemKey(newIdempotencyKey());
            }}
            style={{ width: 110 }}
          />
        </div>
        <button type="submit" className="btn btn-p btn-sm" style={{ alignSelf: "flex-end" }} disabled={busy}>
          {busy ? "Đang chạy…" : "Chạy và lưu dự báo"}
        </button>
      </div>
      {problem ? <ProblemAlert problem={problem} /> : null}
    </form>
  );
}

/**
 * Dự báo nhu cầu máy (CGH FN-09) — ĐỌC kết quả đã lưu, không tính lại ở client. Phương pháp cgh.forecast/v1 là TẠM,
 * cần Cục KTHT (DCRD) xác nhận (docs/dev/cgh-forecast.md).
 */
export function ForecastSection({ coopNames }: { coopNames: ReadonlyMap<string, { name: string; code: string | null }> }) {
  const { api, principal } = useAuth();
  const [picked, setPicked] = useState("");
  const [stage, setStage] = useState("");
  const [province, setProvince] = useState("");
  const runs = useAsync(() => unwrap(api.GET("/v1/mechanization/forecasts")), [api]);
  const runId = picked || runs.data?.items[0]?.id || "";
  const fc = useAsync(
    () =>
      runId
        ? unwrap(
            api.GET("/v1/mechanization/forecasts/{id}", {
              params: { path: { id: runId }, query: { farm_stage: stage || undefined, province_code: province || undefined } },
            }),
          )
        : Promise.resolve(null),
    [api, runId, stage, province],
  );
  const canRun = hasRole(principal, ROLES.cghAdmin);
  const rows = useMemo(() => {
    const list: MechForecastResult[] = [...(fc.data?.results ?? [])];
    // Thiếu nhiều nhất trước; chưa có dữ liệu xếp cuối.
    list.sort((a, b) => (a.gap ?? Number.MAX_SAFE_INTEGER) - (b.gap ?? Number.MAX_SAFE_INTEGER));
    return list.slice(0, 25);
  }, [fc.data]);
  const run = fc.data?.run;

  return (
    <section className="card sec" aria-labelledby="fc-title" style={{ marginTop: 16 }}>
      <div className="sec-h">
        <h2 id="fc-title">Dự báo nhu cầu máy (FN-09)</h2>
        {run ? (
          <span className="mute small">
            Vụ {SEASON_NAME[run.season_code] ?? run.season_code} {run.season_year} · mốc {formatDate(run.reference_date)} · lưu lúc {formatDateTime(run.computed_at)}
          </span>
        ) : null}
      </div>
      <div className="alert alert-warn" role="note" style={{ marginBottom: 12 }} data-testid="forecast-provisional">
        <Icon name="info" />
        <span>
          <b>Phương pháp dự báo tạm thời</b> ({run?.rule_version ?? "cgh.forecast/v1"}) — cần Cục Kinh tế hợp tác và PTNT xác nhận trước khi dùng
          chính thức. {run?.method.formula ? `Công thức: ${run.method.formula}.` : ""} {run?.method.note ?? ""}
        </span>
      </div>
      {canRun ? (
        <div style={{ marginBottom: 12 }}>
          <RunForecast
            onDone={(id) => {
              setPicked(id);
              runs.reload();
            }}
          />
        </div>
      ) : null}
      {runs.error ? <ProblemAlert problem={runs.error} onRetry={runs.reload} /> : null}
      {runs.data && runs.data.items.length === 0 ? (
        <p className="mute">Chưa có lần dự báo đã lưu. Quản trị CGH chạy dự báo trên Admin API.</p>
      ) : null}
      {runs.data && runs.data.items.length > 0 ? (
        <div className="row" style={{ marginBottom: 12 }}>
          <div className="field">
            <label htmlFor="fc-run">Lần dự báo</label>
            <select id="fc-run" className="select" value={runId} onChange={(e) => setPicked(e.target.value)}>
              {runs.data.items.map((r) => (
                <option key={r.id} value={r.id}>
                  {SEASON_NAME[r.season_code] ?? r.season_code} {r.season_year} — lần {r.seq} ({formatDate(r.computed_at)})
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="fc-stage">Khâu</label>
            <select id="fc-stage" className="select" value={stage} onChange={(e) => setStage(e.target.value)}>
              <option value="">Tất cả khâu</option>
              {(run?.pilot_stages ?? []).map((s) => (
                <option key={s} value={s}>
                  {STAGE_NAME[s] ?? s}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="fc-prov">Mã tỉnh</label>
            <input id="fc-prov" className="input" value={province} onChange={(e) => setProvince(e.target.value.trim())} placeholder="vd 82" style={{ width: 100 }} />
          </div>
        </div>
      ) : null}
      {fc.error ? <ProblemAlert problem={fc.error} onRetry={fc.reload} /> : null}
      {fc.loading && runId && !fc.data ? <Loading /> : null}
      {fc.data ? (
        <>
          <p className="small" style={{ marginBottom: 8 }}>
            Diện tích dự báo: <b style={{ display: "inline" }}>{formatDecimal(fc.data.summary.forecast_area_ha, 0)} ha</b>
            {Object.entries(fc.data.summary.required_by_stage).map(([k, v]) => (
              <span key={k}>
                {" "}
                · {STAGE_NAME[k] ?? k}: cần {formatInt(Number(v))} máy
              </span>
            ))}
          </p>
          <div className="tbl-wrap">
            <table className="tbl">
              <caption className="sr-only">Dự báo nhu cầu máy theo HTX và khâu</caption>
              <thead>
                <tr>
                  <th scope="col">HTX</th>
                  <th scope="col">Khâu / loại máy</th>
                  <th scope="col" className="num">Diện tích dự báo (ha)</th>
                  <th scope="col" className="num">Tăng trưởng</th>
                  <th scope="col" className="num">Cần (máy)</th>
                  <th scope="col" className="num">Hiện có</th>
                  <th scope="col" className="num">Chênh lệch</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => {
                  const coop = coopNames.get(r.cooperative_ref);
                  return (
                    <tr key={`${r.cooperative_ref}-${r.farm_stage}-${r.machine_type}`}>
                      <td>
                        <b>{coop?.name ?? "HTX"}</b>
                        <div className="mute small">{coop?.code ?? r.cooperative_ref.slice(-8)}</div>
                      </td>
                      <td>
                        {STAGE_NAME[r.farm_stage] ?? r.farm_stage} · {r.machine_type}
                      </td>
                      <td className="num">{r.forecast_area_ha === null ? "—" : formatDecimal(r.forecast_area_ha, 1)}</td>
                      <td className="num">
                        {r.growth_pct === null ? "—" : `${formatDecimal(r.growth_pct, 1)}%`}
                        {r.inputs.capped ? <div className="mute small">đã chặn ±20%</div> : null}
                      </td>
                      <td className="num">{r.required_whole === null ? "—" : formatInt(r.required_whole)}</td>
                      <td className="num">{formatInt(r.capacity_now)}</td>
                      <td className="num" style={{ color: (r.gap ?? 0) < 0 ? "var(--red)" : undefined, fontWeight: 700 }}>
                        {r.status !== "ok" ? (r.status === "no_norm" ? "Chưa có định mức" : "Chưa có dữ liệu") : (r.gap ?? 0) < 0 ? `Thiếu ${-(r.gap ?? 0)}` : `Đủ (+${r.gap ?? 0})`}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </>
      ) : null}
    </section>
  );
}
