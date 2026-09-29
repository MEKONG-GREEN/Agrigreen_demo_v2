"use client";

import { useMemo, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import type { SimCandidateHub, SimCompare, SimResultEnvelope, SimScenario, SimSimulation } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Loading, PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { formatDateTime } from "@/lib/format";
import { JOB_STATUS, pollUntil } from "@/lib/poll";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { ReasonField } from "@/gis/editor";
import { formatVnd, PAYBACK_STATUS, savingVsBaseline, summarize } from "@/sim/result";

const TABS = [
  { id: "scenarios", label: "Kịch bản & kết quả" },
  { id: "hubs", label: "Hub ứng viên" },
  { id: "compare", label: "So sánh kịch bản" },
];

const MODE: Record<string, string> = { auto: "Tự chọn", road: "Đường bộ", waterway: "Đường thủy" };

/** Mô phỏng chọn Hub (CGH FN-02/FN-11/FN-14; mục 12): Hub ứng viên, kịch bản, chạy (202 → việc), kết quả, so sánh, Chính thức. */
export default function SimulationPage() {
  const { api } = useAuth();
  const [tab, setTab] = useState("scenarios");
  const [version, setVersion] = useState(0);
  const hubs = useAsync(() => collectPages<SimCandidateHub>((cursor) => unwrap(api.GET("/v1/candidate-hubs", { params: { query: { cursor, limit: 200 } } })), 1000), [api, version]);
  const scenarios = useAsync(() => collectPages<SimScenario>((cursor) => unwrap(api.GET("/v1/scenarios", { params: { query: { cursor, limit: 200 } } })), 1000), [api, version]);
  const reload = () => setVersion((v) => v + 1);
  return (
    <>
      <PageHead title="Mô phỏng chọn Hub" subtitle="Chi phí logistics/tấn, TCO, ROI, thời gian hoàn vốn, dòng vật liệu và so sánh với phương án không Hub (No-Hub Baseline)" />
      {hubs.error ? <ProblemAlert problem={hubs.error} onRetry={hubs.reload} /> : null}
      {scenarios.error ? <ProblemAlert problem={scenarios.error} onRetry={scenarios.reload} /> : null}
      <Tabs tabs={TABS} value={tab} onChange={setTab} label="Chức năng mô phỏng">
        {tab === "scenarios" ? (
          <Scenarios scenarios={scenarios.data ?? []} hubs={hubs.data ?? []} loading={scenarios.loading && !scenarios.data} onChanged={reload} />
        ) : tab === "hubs" ? (
          <Hubs hubs={hubs.data ?? []} onChanged={reload} />
        ) : (
          <Compare scenarios={scenarios.data ?? []} />
        )}
      </Tabs>
    </>
  );
}

function useAction() {
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const run = async (fn: () => Promise<unknown>, msg: string | null) => {
    setBusy(true);
    setProblem(null);
    setDone(null);
    try {
      await fn();
      setDone(msg);
      return true;
    } catch (e) {
      setProblem(toApiProblem(e));
      return false;
    } finally {
      setBusy(false);
    }
  };
  const view = (
    <>
      {problem ? <ProblemAlert problem={problem} /> : null}
      {done ? (
        <p className="chip c-ok" role="status">
          {done}
        </p>
      ) : null}
    </>
  );
  return { busy, run, view, setDone };
}

function Hubs({ hubs, onChanged }: { hubs: SimCandidateHub[]; onChanged: () => void }) {
  const { api } = useAuth();
  const [form, setForm] = useState({ name: "", lat: "", lng: "", unit: "" });
  const [retireId, setRetireId] = useState<string | null>(null);
  const [reason, setReason] = useState("");
  const act = useAction();
  const create = async (e: FormEvent) => {
    e.preventDefault();
    const ok = await act.run(
      () =>
        unwrap(
          api.POST("/v1/candidate-hubs", {
            params: { header: { "Idempotency-Key": newIdempotencyKey() } },
            body: { name: form.name.trim(), location: { lat: Number(form.lat), lng: Number(form.lng) }, unit_code: form.unit.trim() || undefined },
          }),
        ),
      "Đã tạo Hub ứng viên.",
    );
    if (ok) {
      setForm({ name: "", lat: "", lng: "", unit: "" });
      onChanged();
    }
  };
  const retire = async (id: string) => {
    const ok = await act.run(() => unwrap(api.POST("/v1/candidate-hubs/{id}/retire", { params: { path: { id }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { reason: reason.trim() } })), "Đã ngừng dùng Hub (không xoá).");
    if (ok) {
      setRetireId(null);
      setReason("");
      onChanged();
    }
  };
  return (
    <div className="grid2" style={{ gridTemplateColumns: "minmax(0, 1.5fr) minmax(300px, 1fr)", alignItems: "start" }}>
      <section className="card sec" aria-label="Hub ứng viên">
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">Hub ứng viên</caption>
            <thead>
              <tr>
                <th scope="col">Mã</th>
                <th scope="col">Tên</th>
                <th scope="col">Toạ độ</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">Thao tác</th>
              </tr>
            </thead>
            <tbody>
              {hubs.map((h) => (
                <tr key={h.id}>
                  <td>{h.code}</td>
                  <td>{h.name}</td>
                  <td className="small">
                    {h.location.lat.toFixed(4)}, {h.location.lng.toFixed(4)}
                  </td>
                  <td>
                    {h.status === "active" ? <span className="chip c-ok">Đang dùng</span> : <span className="chip c-mute" title={h.retired_reason ?? ""}>Ngừng dùng</span>}
                  </td>
                  <td>
                    {h.status === "active" ? (
                      <button type="button" className="btn btn-o btn-sm" onClick={() => setRetireId(h.id)}>
                        Ngừng dùng
                      </button>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {retireId ? (
          <div className="panel stack" style={{ marginTop: 12 }}>
            <ReasonField value={reason} onChange={setReason} label="Lý do ngừng dùng Hub (ít nhất 10 ký tự)" />
            <div className="row">
              <button type="button" className="btn btn-d btn-sm" disabled={act.busy || reason.trim().length < 10} onClick={() => void retire(retireId)}>
                Xác nhận ngừng dùng
              </button>
              <button type="button" className="btn btn-o btn-sm" onClick={() => setRetireId(null)}>
                Huỷ
              </button>
            </div>
          </div>
        ) : null}
      </section>
      <form className="card sec stack" onSubmit={(e) => void create(e)} aria-label="Tạo Hub ứng viên">
        <h2 style={{ fontSize: 15 }}>Tạo Hub ứng viên</h2>
        <div className="field">
          <label htmlFor="hub-name">Tên Hub</label>
          <input id="hub-name" className="input" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
        </div>
        <div className="row">
          <div className="field" style={{ flex: 1 }}>
            <label htmlFor="hub-lat">Vĩ độ</label>
            <input id="hub-lat" className="input" inputMode="decimal" value={form.lat} onChange={(e) => setForm({ ...form, lat: e.target.value })} />
          </div>
          <div className="field" style={{ flex: 1 }}>
            <label htmlFor="hub-lng">Kinh độ</label>
            <input id="hub-lng" className="input" inputMode="decimal" value={form.lng} onChange={(e) => setForm({ ...form, lng: e.target.value })} />
          </div>
        </div>
        <div className="field">
          <label htmlFor="hub-unit">Mã đơn vị hành chính (tuỳ chọn)</label>
          <input id="hub-unit" className="input" value={form.unit} onChange={(e) => setForm({ ...form, unit: e.target.value })} />
        </div>
        <button type="submit" className="btn btn-p btn-sm" disabled={act.busy || form.name.trim().length < 2 || !form.lat || !form.lng}>
          Tạo Hub
        </button>
        {act.view}
      </form>
    </div>
  );
}

function Scenarios({ scenarios, hubs, loading, onChanged }: { scenarios: SimScenario[]; hubs: SimCandidateHub[]; loading: boolean; onChanged: () => void }) {
  const { api } = useAuth();
  const [selId, setSelId] = useState<string | null>(null);
  const [form, setForm] = useState({ name: "", lifecycle: "10", discounted: false, lat: "10.03", lng: "105.78", demand: "" });
  const act = useAction();
  const sel = scenarios.find((s) => s.id === selId) ?? null;
  const create = async (e: FormEvent) => {
    e.preventDefault();
    let id: string | null = null;
    const ok = await act.run(async () => {
      const s = await unwrap(
        api.POST("/v1/scenarios", {
          params: { header: { "Idempotency-Key": newIdempotencyKey() } },
          body: {
            name: form.name.trim(),
            lifecycle_years: Number(form.lifecycle) || 0,
            discounted: form.discounted,
            reference_year: 2026,
            baseline_mode: "no_hub",
            plant: { location: { lat: Number(form.lat), lng: Number(form.lng) }, annual_demand_tons: form.demand.trim() || null },
          },
        }),
      );
      id = s.id;
    }, "Đã tạo kịch bản.");
    if (ok) {
      setForm({ ...form, name: "" });
      onChanged();
      setSelId(id);
    }
  };
  return (
    <div className="grid2" style={{ gridTemplateColumns: "minmax(280px, 0.8fr) minmax(0, 2fr)", alignItems: "start" }}>
      <div className="stack">
        <section className="card sec" aria-label="Danh sách kịch bản">
          {loading ? <Loading /> : null}
          <ul className="list-plain">
            {scenarios.map((s) => (
              <li key={s.id}>
                <button type="button" className={`btn btn-o btn-sm ${s.id === selId ? "hl" : ""}`} style={{ width: "100%", justifyContent: "flex-start" }} onClick={() => setSelId(s.id)} aria-pressed={s.id === selId}>
                  {s.code} · {s.name}
                </button>
                <div className="small mute">
                  {formatInt(s.hubs.length)} Hub · vòng đời {s.lifecycle_years} năm{s.discounted ? " · có chiết khấu" : ""}
                </div>
              </li>
            ))}
          </ul>
        </section>
        <form className="card sec stack" onSubmit={(e) => void create(e)} aria-label="Tạo kịch bản">
          <h2 style={{ fontSize: 15 }}>Tạo kịch bản</h2>
          <div className="field">
            <label htmlFor="sc-name">Tên kịch bản</label>
            <input id="sc-name" className="input" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </div>
          <div className="row">
            <div className="field" style={{ flex: 1 }}>
              <label htmlFor="sc-life">Vòng đời (năm)</label>
              <input id="sc-life" className="input" inputMode="numeric" value={form.lifecycle} onChange={(e) => setForm({ ...form, lifecycle: e.target.value })} />
            </div>
            <label className="row small" style={{ flex: 1, alignSelf: "flex-end" }}>
              <input type="checkbox" checked={form.discounted} onChange={(e) => setForm({ ...form, discounted: e.target.checked })} /> Chiết khấu dòng tiền
            </label>
          </div>
          <div className="row">
            <div className="field" style={{ flex: 1 }}>
              <label htmlFor="sc-lat">Nhà máy — vĩ độ</label>
              <input id="sc-lat" className="input" value={form.lat} onChange={(e) => setForm({ ...form, lat: e.target.value })} />
            </div>
            <div className="field" style={{ flex: 1 }}>
              <label htmlFor="sc-lng">Nhà máy — kinh độ</label>
              <input id="sc-lng" className="input" value={form.lng} onChange={(e) => setForm({ ...form, lng: e.target.value })} />
            </div>
          </div>
          <div className="field">
            <label htmlFor="sc-demand">Nhu cầu nhà máy (tấn/năm, bỏ trống = tham số #12)</label>
            <input id="sc-demand" className="input" inputMode="decimal" value={form.demand} onChange={(e) => setForm({ ...form, demand: e.target.value })} />
          </div>
          <button type="submit" className="btn btn-p btn-sm" disabled={act.busy || form.name.trim().length < 2}>
            Tạo kịch bản
          </button>
          {act.view}
        </form>
      </div>
      {sel ? <ScenarioDetail key={sel.id} s={sel} hubs={hubs} onChanged={onChanged} /> : <p className="card sec small mute">Chọn một kịch bản để gán Hub, chạy mô phỏng và xem kết quả.</p>}
    </div>
  );
}

function ScenarioDetail({ s, hubs, onChanged }: { s: SimScenario; hubs: SimCandidateHub[]; onChanged: () => void }) {
  const { api } = useAuth();
  const [version, setVersion] = useState(0);
  const history = useAsync(() => unwrap(api.GET("/v1/scenarios/{id}/history", { params: { path: { id: s.id } } })), [api, s.id, version]);
  const simIds = useMemo(() => Array.from(new Set((history.data?.items ?? []).filter((c) => c.kind === "simulation_requested" && c.simulation_id).map((c) => c.simulation_id!))).reverse(), [history.data]);
  const sims = useAsync(() => Promise.all(simIds.map((id) => unwrap(api.GET("/v1/simulations/{id}", { params: { path: { id } } })))), [api, simIds.join(","), version]);
  const [selSim, setSelSim] = useState<string | null>(null);
  const [attach, setAttach] = useState({ hub_id: "", radius: "25", capacity: "30000", barge: "", mode_field_hub: "auto", mode_hub_plant: "auto", land_mode: "thue" });
  const [detachId, setDetachId] = useState<string | null>(null);
  const [reason, setReason] = useState("");
  const [sensitivity, setSensitivity] = useState(false);
  const [job, setJob] = useState<{ id: string; status: string; error: string | null } | null>(null);
  const act = useAction();
  const hubName = (id: string) => hubs.find((h) => h.id === id)?.name ?? id;
  const refresh = () => {
    setVersion((v) => v + 1);
    onChanged();
  };

  const doAttach = async (e: FormEvent) => {
    e.preventDefault();
    if (
      await act.run(
        () =>
          unwrap(
            api.POST("/v1/scenarios/{id}/hubs", {
              params: { path: { id: s.id }, header: { "Idempotency-Key": newIdempotencyKey() } },
              body: {
                hub_id: attach.hub_id,
                service_radius_km: attach.radius.trim() || undefined,
                design_capacity_tons: attach.capacity.trim() || undefined,
                barge_payload_tons: attach.barge.trim() || undefined,
                mode_field_hub: attach.mode_field_hub as "auto",
                mode_hub_plant: attach.mode_hub_plant as "auto",
                land_mode: attach.land_mode as "thue",
              },
            }),
          ),
        "Đã gán Hub vào kịch bản.",
      )
    )
      refresh();
  };
  const doDetach = async (hubId: string) => {
    if (await act.run(() => unwrap(api.POST("/v1/scenarios/{id}/hubs/{hub_id}/detach", { params: { path: { id: s.id, hub_id: hubId }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { reason: reason.trim() } })), "Đã gỡ Hub (bản ghi bù kèm lý do).")) {
      setDetachId(null);
      setReason("");
      refresh();
    }
  };
  const runSim = async () => {
    await act.run(async () => {
      const sim = await unwrap(api.POST("/v1/simulations", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: { scenario_id: s.id, sensitivity, mode: "evaluate" } }));
      setJob({ id: sim.id, status: sim.status, error: null });
      setVersion((v) => v + 1);
      const last = await pollUntil(
        () => unwrap(api.GET("/v1/jobs/{id}", { params: { path: { id: sim.id } } })),
        (j) => j.status === "completed" || j.status === "failed",
        { intervalMs: 1000, onUpdate: (j) => setJob({ id: j.id, status: j.status, error: j.error_code ?? null }) },
      );
      setVersion((v) => v + 1);
      if (last.status === "completed") setSelSim(sim.id);
    }, null);
  };
  const activeHubs = hubs.filter((h) => h.status === "active");

  return (
    <div className="stack">
      <section className="card sec stack" aria-labelledby="scd-h">
        <div className="sec-h">
          <h2 id="scd-h">
            {s.code} · {s.name}
          </h2>
          <span className="small mute">phiên bản {s.version}</span>
        </div>
        <p className="small">
          Nhà máy {s.plant.location.lat.toFixed(4)}, {s.plant.location.lng.toFixed(4)} · nhu cầu {s.plant.annual_demand_tons ? `${formatDecimal(s.plant.annual_demand_tons, 0)} tấn/năm` : "theo tham số #12"} · vòng đời{" "}
          {s.lifecycle_years} năm{s.discounted ? " (chiết khấu)" : ""} · baseline: {s.baseline_mode === "no_hub" ? "No-Hub" : s.baseline_mode}
        </p>
        <div className="tbl-wrap">
          <table className="tbl">
            <caption style={{ textAlign: "left", fontWeight: 700, padding: "6px 0" }}>Hub đang gán</caption>
            <thead>
              <tr>
                <th scope="col">Hub</th>
                <th scope="col" className="num">Bán kính (km)</th>
                <th scope="col" className="num">Tồn kho tối đa (tấn)</th>
                <th scope="col">Ruộng → Hub</th>
                <th scope="col">Hub → nhà máy</th>
                <th scope="col">Thao tác</th>
              </tr>
            </thead>
            <tbody>
              {s.hubs.length === 0 ? (
                <tr>
                  <td colSpan={6} className="mute small">
                    Chưa gán Hub nào — gán ít nhất một Hub trước khi chạy mô phỏng.
                  </td>
                </tr>
              ) : null}
              {s.hubs.map((h) => (
                <tr key={h.scenario_hub_id}>
                  <td>
                    {h.hub_code} · {hubName(h.hub_id)}
                  </td>
                  <td className="num">{formatDecimal(h.service_radius_km, 1)}</td>
                  <td className="num">{formatDecimal(h.design_capacity_tons, 0)}</td>
                  <td>{MODE[h.mode_field_hub]}</td>
                  <td>{MODE[h.mode_hub_plant]}</td>
                  <td>
                    <button type="button" className="btn btn-o btn-sm" onClick={() => setDetachId(h.hub_id)}>
                      Gỡ Hub
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {detachId ? (
          <div className="panel stack">
            <ReasonField value={reason} onChange={setReason} label={`Lý do gỡ ${hubName(detachId)} (ít nhất 10 ký tự)`} />
            <div className="row">
              <button type="button" className="btn btn-d btn-sm" disabled={act.busy || reason.trim().length < 10} onClick={() => void doDetach(detachId)}>
                Xác nhận gỡ Hub
              </button>
              <button type="button" className="btn btn-o btn-sm" onClick={() => setDetachId(null)}>
                Huỷ
              </button>
            </div>
          </div>
        ) : null}
        <form className="panel stack" onSubmit={(e) => void doAttach(e)} aria-label="Gán Hub vào kịch bản">
          <div className="row" style={{ flexWrap: "wrap" }}>
            <div className="field" style={{ flex: 2, minWidth: 180 }}>
              <label htmlFor="att-hub">Hub ứng viên</label>
              <select id="att-hub" className="select" value={attach.hub_id} onChange={(e) => setAttach({ ...attach, hub_id: e.target.value })}>
                <option value="">—</option>
                {activeHubs.map((h) => (
                  <option key={h.id} value={h.id}>
                    {h.code} · {h.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="field" style={{ flex: 1, minWidth: 110 }}>
              <label htmlFor="att-radius">Bán kính phục vụ (km)</label>
              <input id="att-radius" className="input" value={attach.radius} onChange={(e) => setAttach({ ...attach, radius: e.target.value })} />
            </div>
            <div className="field" style={{ flex: 1, minWidth: 110 }}>
              <label htmlFor="att-cap">Tồn kho tối đa (tấn)</label>
              <input id="att-cap" className="input" value={attach.capacity} onChange={(e) => setAttach({ ...attach, capacity: e.target.value })} />
            </div>
            <div className="field" style={{ flex: 1, minWidth: 110 }}>
              <label htmlFor="att-plant">Hub → nhà máy</label>
              <select id="att-plant" className="select" value={attach.mode_hub_plant} onChange={(e) => setAttach({ ...attach, mode_hub_plant: e.target.value })}>
                {Object.entries(MODE).map(([k, v]) => (
                  <option key={k} value={k}>
                    {v}
                  </option>
                ))}
              </select>
            </div>
          </div>
          <div>
            <button type="submit" className="btn btn-o btn-sm" disabled={act.busy || !attach.hub_id}>
              Gán / cập nhật Hub
            </button>
          </div>
        </form>
        <div className="row">
          <label className="row small">
            <input type="checkbox" checked={sensitivity} onChange={(e) => setSensitivity(e.target.checked)} /> Kèm phân tích độ nhạy (FN-18)
          </label>
          <button type="button" className="btn btn-p" disabled={act.busy || s.hubs.length === 0} onClick={() => void runSim()}>
            Chạy mô phỏng
          </button>
          {job ? (
            <span className="small" role="status" data-testid="job-status">
              Việc mô phỏng: <b>{JOB_STATUS[job.status] ?? job.status}</b>
              {job.error ? ` · ${job.error}` : ""}
            </span>
          ) : null}
        </div>
        {act.view}
      </section>
      <section className="card sec" aria-label="Các lượt mô phỏng">
        <h2 style={{ fontSize: 15 }}>Các lượt mô phỏng</h2>
        {sims.loading && !sims.data ? <Loading /> : null}
        {sims.data && sims.data.length === 0 ? <p className="small mute">Chưa chạy lượt nào.</p> : null}
        <ul className="list-plain">
          {(sims.data ?? []).map((m) => (
            <li key={m.id} className="row" style={{ justifyContent: "space-between" }}>
              <span className="small">
                {formatDateTime(m.requested_at)} · <b>{JOB_STATUS[m.status]}</b> · {m.official ? <span className="chip c-ok">Chính thức</span> : <span className="chip c-mute">Tham khảo</span>}
              </span>
              {m.has_result ? (
                <button type="button" className="btn btn-o btn-sm" onClick={() => setSelSim(m.id)} aria-pressed={selSim === m.id}>
                  Xem kết quả
                </button>
              ) : null}
            </li>
          ))}
        </ul>
      </section>
      {selSim ? <ResultView key={selSim} id={selSim} sim={sims.data?.find((x) => x.id === selSim) ?? null} onChanged={() => setVersion((v) => v + 1)} /> : null}
    </div>
  );
}

function Kpi({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="panel" role="group" aria-label={label}>
      <div className="small mute">{label}</div>
      <div style={{ fontSize: 20, fontWeight: 800 }}>{value}</div>
      {hint ? <div className="small">{hint}</div> : null}
    </div>
  );
}

function ResultView({ id, sim, onChanged }: { id: string; sim: SimSimulation | null; onChanged: () => void }) {
  const { api } = useAuth();
  const res = useAsync<SimResultEnvelope>(() => unwrap(api.GET("/v1/simulations/{id}/result", { params: { path: { id } } })), [api, id]);
  const act = useAction();
  const sum = useMemo(() => (res.data ? summarize(res.data.result) : null), [res.data]);
  const eligible = res.data?.official_eligible ?? sim?.official_eligible ?? false;
  const official = res.data?.official ?? sim?.official ?? false;
  const markOfficial = async () => {
    if (await act.run(() => unwrap(api.POST("/v1/simulations/{id}/mark-official", { params: { path: { id }, header: { "Idempotency-Key": newIdempotencyKey() } } })), "Đã đánh dấu Chính thức.")) {
      res.reload();
      onChanged();
    }
  };
  const diff = sum ? savingVsBaseline(sum.costPerTon, sum.baselineCostPerTon) : null;
  return (
    <section className="card sec stack" aria-labelledby="res-h" data-testid="sim-result">
      <div className="sec-h">
        <h2 id="res-h">Kết quả mô phỏng</h2>
        <span>{official ? <span className="chip c-ok">Chính thức</span> : <span className="chip c-mute">Tham khảo</span>}</span>
      </div>
      {res.loading && !res.data ? <Loading /> : null}
      {res.error ? <ProblemAlert problem={res.error} onRetry={res.reload} /> : null}
      {sum ? (
        <>
          <div className="grid4" style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(170px, 1fr))", gap: 10 }}>
            <Kpi label="Chi phí logistics / tấn" value={formatVnd(sum.costPerTon)} hint={diff ? `${diff.saving ? "Tiết kiệm" : "Cao hơn"} ${formatDecimal(Math.abs(diff.pct), 1)}% so với No-Hub` : undefined} />
            <Kpi label="TCO (vòng đời)" value={formatVnd(sum.tco)} hint={sum.lifecycleYears ? `${sum.lifecycleYears} năm` : undefined} />
            <Kpi label="TCO / tấn" value={formatVnd(sum.tcoPerTon)} />
            <Kpi label="ROI" value={sum.roiPct === null ? "—" : `${formatDecimal(sum.roiPct, 1)}%`} />
            <Kpi label="Thời gian hoàn vốn" value={sum.paybackYears === null ? "—" : `${sum.paybackYears} năm`} hint={sum.paybackStatus ? PAYBACK_STATUS[sum.paybackStatus] ?? sum.paybackStatus : undefined} />
            <Kpi label="Sản lượng giao nhà máy" value={sum.deliveredTons === null ? "—" : `${formatInt(sum.deliveredTons)} tấn`} hint={sum.coveragePct === null ? undefined : `đáp ứng ${formatDecimal(sum.coveragePct, 1)}% nhu cầu`} />
            <Kpi label="CAPEX" value={formatVnd(sum.capex)} />
          </div>
          {sum.plantWarning ? <p className="notice">{sum.plantWarning}</p> : null}
          <div className="grid2" style={{ gridTemplateColumns: "1fr 1fr", gap: 12 }}>
            <div className="panel">
              <h3 className="small">Dòng vật liệu (tấn/năm)</h3>
              <dl className="dl-inline" aria-label="Dòng vật liệu">
                <dt>Ruộng → Hub</dt>
                <dd>{formatInt(sum.flows.fieldHub)}</dd>
                <dt>Hub → nhà máy</dt>
                <dd>{formatInt(sum.flows.hubPlant)}</dd>
                <dt>Ruộng → nhà máy trực tiếp</dt>
                <dd>{formatInt(sum.flows.direct)}</dd>
                <dt>Không thu gom</dt>
                <dd>{formatInt(sum.flows.uncollected)}</dd>
              </dl>
            </div>
            <div className="panel" data-testid="baseline">
              <h3 className="small">No-Hub Baseline</h3>
              <dl className="dl-inline">
                <dt>Chi phí / tấn</dt>
                <dd>{formatVnd(sum.baselineCostPerTon)}</dd>
                <dt>Sản lượng giao</dt>
                <dd>{sum.baselineDeliveredTons === null ? "—" : `${formatInt(sum.baselineDeliveredTons)} tấn`}</dd>
                <dt>Nguồn</dt>
                <dd>{sum.baselineLabel ?? (sum.baselineAvailable ? "Ruộng → nhà máy trực tiếp" : "Không có baseline")}</dd>
              </dl>
            </div>
          </div>
          <div className="tbl-wrap">
            <table className="tbl">
              <caption style={{ textAlign: "left", fontWeight: 700, padding: "6px 0" }}>Theo Hub</caption>
              <thead>
                <tr>
                  <th scope="col">Hub</th>
                  <th scope="col" className="num">Sản lượng (tấn)</th>
                  <th scope="col" className="num">Tới nhà máy (km)</th>
                  <th scope="col">Nguồn khoảng cách</th>
                  <th scope="col" className="num">Tồn đỉnh (tấn)</th>
                  <th scope="col" className="num">Sử dụng kho</th>
                </tr>
              </thead>
              <tbody>
                {sum.hubs.map((h) => (
                  <tr key={h.code}>
                    <td>{h.code}</td>
                    <td className="num">{formatInt(h.deliveredTons)}</td>
                    <td className="num">{formatDecimal(h.distanceKm, 1)}</td>
                    <td className="small">{h.distanceSource ?? "—"}</td>
                    <td className="num">{formatInt(h.peakInventory)}</td>
                    <td className="num">{h.utilizationPct === null ? "—" : `${formatDecimal(h.utilizationPct, 1)}%`}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {sum.warnings.length > 0 ? (
            <ul className="small" style={{ marginLeft: 18 }}>
              {sum.warnings.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
          ) : null}
          <div className="stack">
            <div className="row">
              <button
                type="button"
                className="btn btn-p btn-sm"
                disabled={!eligible || official || act.busy}
                aria-describedby={!eligible ? "official-why" : undefined}
                onClick={() => void markOfficial()}
              >
                Chính thức
              </button>
              <span className="small mute">Bộ tham số: {sum.parameterSet ?? res.data?.snapshot?.param_set_version ?? "—"}</span>
            </div>
            {!eligible && !official ? (
              <div id="official-why" className="alert alert-warn" role="note">
                <div>
                  <b>Chưa đánh dấu Chính thức được</b>
                  <span className="small">
                    Ảnh chụp đầu vào còn tham số chưa có phiên bản được phê duyệt (CGH FN-01 BR-04, mã <code>simulation.params_unapproved</code>). Kết quả chỉ dùng tham khảo — phê duyệt tham số rồi
                    chạy lại.
                  </span>
                  {sum.unapproved.length > 0 ? (
                    <ul className="small" style={{ margin: "4px 0 0 18px" }} aria-label="Tham số chưa phê duyệt">
                      {sum.unapproved.map((p) => (
                        <li key={p.code}>
                          {p.number !== null ? `#${p.number} ` : ""}
                          {p.name} (<code>{p.code}</code>)
                        </li>
                      ))}
                    </ul>
                  ) : null}
                </div>
              </div>
            ) : null}
            {act.view}
          </div>
        </>
      ) : null}
    </section>
  );
}

function Compare({ scenarios }: { scenarios: SimScenario[] }) {
  const { api } = useAuth();
  const all = useAsync(async () => {
    const out: { sim: SimSimulation; scenario: SimScenario }[] = [];
    for (const s of scenarios) {
      const h = await unwrap(api.GET("/v1/scenarios/{id}/history", { params: { path: { id: s.id } } }));
      const ids = Array.from(new Set(h.items.filter((c) => c.kind === "simulation_requested" && c.simulation_id).map((c) => c.simulation_id!)));
      for (const id of ids) {
        const sim = await unwrap(api.GET("/v1/simulations/{id}", { params: { path: { id } } }));
        if (sim.has_result) out.push({ sim, scenario: s });
      }
    }
    return out;
  }, [api, scenarios]);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [result, setResult] = useState<SimCompare | null>(null);
  const act = useAction();
  const run = async () => {
    await act.run(async () => {
      setResult(await unwrap(api.POST("/v1/scenarios:compare", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: { simulation_ids: Array.from(picked), include_baseline: true } })));
    }, null);
  };
  return (
    <div className="stack">
      <section className="card sec stack" aria-label="Chọn lượt mô phỏng để so sánh">
        {all.loading && !all.data ? <Loading /> : null}
        {all.error ? <ProblemAlert problem={all.error} onRetry={all.reload} /> : null}
        <fieldset className="plain">
          <legend>Lượt mô phỏng đã có kết quả</legend>
          {(all.data ?? []).map(({ sim, scenario }) => (
            <label key={sim.id} className="row small">
              <input
                type="checkbox"
                checked={picked.has(sim.id)}
                onChange={(e) =>
                  setPicked((p) => {
                    const n = new Set(p);
                    if (e.target.checked) n.add(sim.id);
                    else n.delete(sim.id);
                    return n;
                  })
                }
              />
              {scenario.code} · {scenario.name} — {formatDateTime(sim.requested_at)} {sim.official ? "(Chính thức)" : "(Tham khảo)"}
            </label>
          ))}
        </fieldset>
        <div>
          <button type="button" className="btn btn-p btn-sm" disabled={picked.size === 0 || act.busy} onClick={() => void run()}>
            So sánh
          </button>
        </div>
        {act.view}
      </section>
      {result ? (
        <section className="card sec" aria-label="Bảng so sánh kịch bản" data-testid="compare-table">
          {result.blocked ? (
            <p className="notice" role="status">
              {result.blocked}
            </p>
          ) : null}
          {result.warnings.map((w) => (
            <p key={w} className="small mute">
              {w}
            </p>
          ))}
          <div className="tbl-wrap">
            <table className="tbl">
              <caption className="sr-only">So sánh kịch bản (xếp theo TCO/tấn)</caption>
              <thead>
                <tr>
                  <th scope="col">Kịch bản</th>
                  <th scope="col" className="num">Số Hub</th>
                  <th scope="col" className="num">Sản lượng (tấn)</th>
                  <th scope="col" className="num">Chi phí/tấn</th>
                  <th scope="col" className="num">TCO/tấn</th>
                  <th scope="col" className="num">CAPEX</th>
                  <th scope="col" className="num">ROI</th>
                  <th scope="col">Hoàn vốn</th>
                  <th scope="col" className="num">Đáp ứng nhu cầu</th>
                </tr>
              </thead>
              <tbody>
                {result.rows.map((r) => (
                  <tr key={r.simulation_id ?? r.code} data-baseline={r.is_baseline_column ? "true" : undefined}>
                    <td>
                      <b>{r.code}</b> · {r.name}
                      {r.is_baseline_column ? <span className="chip c-info" style={{ marginLeft: 6 }}>Baseline</span> : r.official ? <span className="chip c-ok" style={{ marginLeft: 6 }}>Chính thức</span> : null}
                    </td>
                    <td className="num">{r.hub_count}</td>
                    <td className="num">{formatDecimal(r.delivered_tons, 0)}</td>
                    <td className="num">{formatVnd(r.cost_per_ton)}</td>
                    <td className="num">{formatVnd(r.tco_per_ton)}</td>
                    <td className="num">{formatVnd(r.capex)}</td>
                    <td className="num">{r.roi_pct === null || r.roi_pct === undefined ? "—" : `${formatDecimal(r.roi_pct, 1)}%`}</td>
                    <td className="small">{r.payback_years ? `${r.payback_years} năm` : PAYBACK_STATUS[r.payback_status] ?? r.payback_status}</td>
                    <td className="num">{formatDecimal(r.plant_demand_coverage_pct, 1)}%</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      ) : null}
    </div>
  );
}
