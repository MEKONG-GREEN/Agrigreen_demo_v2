"use client";

import { useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { usePagedList } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import type { CropActivity, CropFarmLogState, CropLogBatchResult, CropSOPStep } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { LocationChip, ReasonDialog, ResultList, StatusChip, useCatalog } from "@/htx/common";
import { ACTIVITIES, ACTIVITY_LABEL, LOG_STATUS, activityLabel, parseDecimalInput } from "@/htx/rules";
import { CoopPicker, ReadOnlyNotice, useCoopFields, useHtxScope, type HtxScope } from "@/htx/scope";
import { useSeasonIndex } from "@/htx/seasons";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";

interface LogDraft {
  activity: CropActivity;
  step: string;
  machines: string[];
  input: { name: string; qty: string; unit: string };
  note: string;
}

const EMPTY: LogDraft = { activity: "bon_phan", step: "", machines: [], input: { name: "", qty: "", unit: "kg" }, note: "" };

/** Trường nội dung nhật ký dùng chung cho ghi hộ, ghi hàng loạt và sửa. */
function LogFields({ draft, set, steps, idp }: { draft: LogDraft; set: (d: LogDraft) => void; steps: CropSOPStep[]; idp: string }) {
  const machines = useCatalog("machine_type");
  const qtyBad = draft.input.qty !== "" && parseDecimalInput(draft.input.qty) === null;
  return (
    <>
      <div className="row" style={{ alignItems: "flex-start" }}>
        <div className="field">
          <label htmlFor={`${idp}-act`}>Hoạt động</label>
          <select id={`${idp}-act`} className="select" value={draft.activity} onChange={(e) => set({ ...draft, activity: e.target.value as CropActivity })}>
            {ACTIVITIES.map((a) => (
              <option key={a} value={a}>
                {ACTIVITY_LABEL[a]}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor={`${idp}-step`}>Bước SOP</label>
          <select id={`${idp}-step`} className="select" value={draft.step} onChange={(e) => set({ ...draft, step: e.target.value })}>
            <option value="">Không gắn bước</option>
            {steps.map((s) => (
              <option key={s.seq} value={String(s.seq)}>
                {s.seq}. {s.name}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor={`${idp}-in`}>Vật tư</label>
          <input id={`${idp}-in`} className="input" placeholder="vd NPK 20-20-15" value={draft.input.name} onChange={(e) => set({ ...draft, input: { ...draft.input, name: e.target.value } })} />
        </div>
        <div className="field">
          <label htmlFor={`${idp}-qty`}>Lượng</label>
          <input id={`${idp}-qty`} className="input grid-input" inputMode="decimal" aria-invalid={qtyBad} value={draft.input.qty} onChange={(e) => set({ ...draft, input: { ...draft.input, qty: e.target.value } })} />
          {qtyBad ? <span className="err">Lượng phải là số.</span> : null}
        </div>
        <div className="field">
          <label htmlFor={`${idp}-unit`}>Đơn vị</label>
          <input id={`${idp}-unit`} className="input grid-input" value={draft.input.unit} onChange={(e) => set({ ...draft, input: { ...draft.input, unit: e.target.value } })} />
        </div>
      </div>
      <fieldset style={{ border: 0, padding: 0, margin: 0 }}>
        <legend className="small">Máy móc cơ giới đã dùng</legend>
        <div className="row">
          {machines.map((m) => (
            <label key={m.code} className="row small" style={{ gap: 4 }}>
              <input
                type="checkbox"
                checked={draft.machines.includes(m.code)}
                onChange={(e) => set({ ...draft, machines: e.target.checked ? [...draft.machines, m.code] : draft.machines.filter((x) => x !== m.code) })}
              />
              {m.name}
            </label>
          ))}
        </div>
      </fieldset>
      <div className="field">
        <label htmlFor={`${idp}-note`}>Ghi chú (không ghi SĐT/CCCD)</label>
        <textarea id={`${idp}-note`} className="textarea" maxLength={2000} value={draft.note} onChange={(e) => set({ ...draft, note: e.target.value })} />
      </div>
    </>
  );
}

function contentOf(d: LogDraft) {
  const qty = parseDecimalInput(d.input.qty);
  return {
    activity_code: d.activity,
    sop_step_seq: d.step ? Number(d.step) : null,
    machine_codes: d.machines,
    inputs: d.input.name.trim() && qty ? [{ name: d.input.name.trim(), qty, unit: d.input.unit.trim() || "kg" }] : [],
    note: d.note.trim() || undefined,
  };
}

/** Bước SOP của vụ đang mở trên thửa (để gắn bước khi ghi). */
function useSeasonSteps(seasonRef: string | null): CropSOPStep[] {
  const { api } = useAuth();
  const d = useAsync(() => (seasonRef ? unwrap(api.GET("/v1/cropcycle/seasons/{ref}", { params: { path: { ref: seasonRef } } })).then((x) => x.steps) : Promise.resolve([] as CropSOPStep[])), [api, seasonRef]);
  return d.data ?? [];
}

/** Ghi nhật ký hộ nông dân (HTX US-LOG-02) — chặn lô chưa mở vụ. GPS: web không có định vị → "Thiếu GPS". */
function OnBehalfForm({ scope, onDone }: { scope: HtxScope; onDone: () => void }) {
  const { api } = useAuth();
  const fields = useCoopFields(scope.coopRef);
  const index = useSeasonIndex(scope.coopRef);
  const [plot, setPlot] = useState("");
  const [draft, setDraft] = useState<LogDraft>(EMPTY);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [ok, setOk] = useState<string | null>(null);
  const openSeason = index.seasons.find((s) => s.plot_ref === plot && s.status === "open") ?? null;
  const steps = useSeasonSteps(openSeason?.season_ref ?? null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!plot) return;
    setBusy(true);
    setProblem(null);
    setOk(null);
    try {
      await unwrap(
        api.POST("/v1/cropcycle/farm-logs", {
          params: { header: { "Idempotency-Key": newIdempotencyKey() } },
          body: { plot_ref: plot, ...contentOf(draft), gps: { source: "no_device" } },
        }),
      );
      setOk("Đã ghi nhật ký hộ — trạng thái Chờ duyệt.");
      setDraft(EMPTY);
      onDone();
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form className="panel stack" style={{ gap: 10, marginBottom: 16 }} onSubmit={(e) => void submit(e)} aria-label="Ghi nhật ký hộ nông dân">
      <b>Ghi nhật ký hộ nông dân</b>
      <div className="field" style={{ maxWidth: 420 }}>
        <label htmlFor="ob-plot">Lô ruộng</label>
        <select id="ob-plot" className="select" value={plot} onChange={(e) => setPlot(e.target.value)}>
          <option value="">— Chọn lô —</option>
          {fields.plots.map((p) => {
            const open = index.seasons.some((s) => s.plot_ref === p.properties.ref && s.status === "open");
            return (
              <option key={p.properties.ref} value={p.properties.ref}>
                {p.properties.code} · {fields.householdLabel(p.properties.household_ref)}
                {open ? "" : " (chưa mở vụ)"}
              </option>
            );
          })}
        </select>
      </div>
      <LogFields draft={draft} set={setDraft} steps={steps} idp="ob" />
      <div>
        <button type="submit" className="btn btn-p btn-sm" disabled={busy || !plot}>
          {busy ? "Đang ghi…" : "Ghi nhật ký hộ"}
        </button>
      </div>
      {problem ? <ProblemAlert problem={problem} /> : null}
      {ok ? <p role="status">{ok}</p> : null}
    </form>
  );
}

/** Ghi nhật ký hàng loạt nhiều lô cùng hoạt động (HTX US-LOG-03) — lô lỗi bị loại kèm lý do. */
function BatchForm({ scope, onDone }: { scope: HtxScope; onDone: () => void }) {
  const { api } = useAuth();
  const fields = useCoopFields(scope.coopRef);
  const index = useSeasonIndex(scope.coopRef);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [draft, setDraft] = useState<LogDraft>(EMPTY);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [result, setResult] = useState<CropLogBatchResult | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (picked.size === 0) return;
    setBusy(true);
    setProblem(null);
    setResult(null);
    try {
      const res = await unwrap(
        api.POST("/v1/cropcycle/farm-logs/batch", {
          params: { header: { "Idempotency-Key": newIdempotencyKey() } },
          body: { plot_refs: [...picked], ...contentOf(draft), gps: { source: "no_device" } },
        }),
      );
      setResult(res);
      setPicked(new Set());
      onDone();
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <form className="panel stack" style={{ gap: 10, marginBottom: 16 }} onSubmit={(e) => void submit(e)} aria-label="Ghi nhật ký hàng loạt">
      <b>Ghi nhật ký hàng loạt</b>
      <span className="small mute">Tích các lô cùng hoạt động trong ngày — hệ thống lưu một bản ghi chung, lô chưa mở vụ hoặc ngoài HTX bị loại kèm lý do.</span>
      <fieldset style={{ border: 0, padding: 0, margin: 0 }}>
        <legend className="small">Lô ruộng ({picked.size} đã chọn)</legend>
        <div style={{ maxHeight: 200, overflow: "auto", display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(220px, 1fr))", gap: 4 }}>
          {fields.plots.map((p) => {
            const open = index.seasons.some((s) => s.plot_ref === p.properties.ref && s.status === "open");
            return (
              <label key={p.properties.ref} className="row small" style={{ gap: 6 }}>
                <input
                  type="checkbox"
                  checked={picked.has(p.properties.ref)}
                  onChange={(e) =>
                    setPicked((cur) => {
                      const next = new Set(cur);
                      if (e.target.checked) next.add(p.properties.ref);
                      else next.delete(p.properties.ref);
                      return next;
                    })
                  }
                />
                <span>
                  <b style={{ display: "inline" }}>{p.properties.code}</b>
                  {open ? "" : <span className="mute"> · chưa mở vụ</span>}
                </span>
              </label>
            );
          })}
        </div>
      </fieldset>
      <LogFields draft={draft} set={setDraft} steps={[]} idp="bt" />
      <div>
        <button type="submit" className="btn btn-p btn-sm" disabled={busy || picked.size === 0}>
          {busy ? "Đang ghi…" : `Ghi cho ${picked.size} lô`}
        </button>
      </div>
      {problem ? <ProblemAlert problem={problem} /> : null}
      {result ? (
        <>
          <p role="status">
            <b>
              Đã ghi {result.accepted}/{result.requested} lô trong một lượt
            </b>
            {result.skipped.length ? ` · ${result.skipped.length} lô bị loại` : ""}
          </p>
          <ResultList
            caption="Lô bị loại khỏi lượt ghi hàng loạt"
            rows={result.skipped.map((s) => ({ key: s.plot_ref ?? s.code ?? "", label: s.code ?? s.plot_ref ?? "—", ok: false, status: "Bị loại", reason: s.reason }))}
          />
        </>
      ) : null}
    </form>
  );
}

/** Nhật ký canh tác (HTX US-LOG-01/02/03): danh sách kèm nhãn GPS, ghi hộ, ghi hàng loạt, sửa bằng bản ghi bù. */
export default function FarmLogsPage() {
  const { api } = useAuth();
  const scope = useHtxScope();
  const [status, setStatus] = useState<"" | "pending" | "approved" | "needs_supplement">("");
  const [loc, setLoc] = useState("");
  const [panel, setPanel] = useState<"" | "onbehalf" | "batch">("");
  const [version, setVersion] = useState(0);
  const [editing, setEditing] = useState<CropFarmLogState | null>(null);
  const index = useSeasonIndex(scope.coopRef, [version]);
  const fields = useCoopFields(scope.coopRef);
  const list = usePagedList(
    (cursor) => unwrap(api.GET("/v1/cropcycle/farm-logs", { params: { query: { cooperative_ref: scope.coopRef || undefined, status: status || undefined, cursor, limit: 50 } } })),
    [api, scope.coopRef, status, version],
  );
  const items = list.items.filter((l) => !loc || l.location_status === loc);
  const reload = () => setVersion((v) => v + 1);
  const writable = scope.canWrite && !!scope.coopRef;

  return (
    <>
      <PageHead
        title="Nhật ký canh tác"
        subtitle="Nhật ký theo lô ruộng; nhãn vị trí “Vị trí không khớp”, “Thiếu GPS” không chặn lưu nhưng hiện rõ khi duyệt"
        actions={
          writable ? (
            <>
              <button type="button" className="btn btn-o btn-sm" aria-expanded={panel === "onbehalf"} onClick={() => setPanel((p) => (p === "onbehalf" ? "" : "onbehalf"))}>
                <Icon name="journal" size={15} />
                Ghi hộ nông dân
              </button>
              <button type="button" className="btn btn-o btn-sm" aria-expanded={panel === "batch"} onClick={() => setPanel((p) => (p === "batch" ? "" : "batch"))}>
                Ghi hàng loạt
              </button>
            </>
          ) : null
        }
      />
      <ReadOnlyNotice scope={scope} />
      {panel === "onbehalf" && writable ? <OnBehalfForm scope={scope} onDone={reload} /> : null}
      {panel === "batch" && writable ? <BatchForm scope={scope} onDone={reload} /> : null}
      <div className="card sec">
        <form className="row" role="search" style={{ marginBottom: 14, alignItems: "flex-end" }} onSubmit={(e) => e.preventDefault()}>
          <CoopPicker scope={scope} />
          <div className="field">
            <label htmlFor="lg-status">Trạng thái duyệt</label>
            <select id="lg-status" className="select" value={status} onChange={(e) => setStatus(e.target.value as typeof status)}>
              <option value="">Tất cả</option>
              <option value="pending">Chờ duyệt</option>
              <option value="approved">Đã duyệt</option>
              <option value="needs_supplement">Cần bổ sung</option>
            </select>
          </div>
          <div className="field">
            <label htmlFor="lg-loc">Vị trí</label>
            <select id="lg-loc" className="select" value={loc} onChange={(e) => setLoc(e.target.value)}>
              <option value="">Tất cả</option>
              <option value="match">Vị trí khớp</option>
              <option value="mismatch">Vị trí không khớp</option>
              <option value="missing">Thiếu GPS</option>
            </select>
          </div>
        </form>
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">Nhật ký canh tác</caption>
            <thead>
              <tr>
                <th scope="col">Thời điểm</th>
                <th scope="col">Lô ruộng</th>
                <th scope="col">Nông hộ</th>
                <th scope="col">Hoạt động</th>
                <th scope="col">Vị trí</th>
                <th scope="col">Người ghi</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">Thao tác</th>
              </tr>
            </thead>
            <tbody>
              {items.map((l) => {
                const s = index.byRef.get(l.season_ref);
                return (
                  <tr key={l.id}>
                    <td>{formatDateTime(l.event_time)}</td>
                    <td>
                      <b>{s?.plot_code ?? "—"}</b>
                      {l.version > 1 ? <div className="mute small">Phiên bản {l.version}</div> : null}
                    </td>
                    <td>{fields.householdLabel(l.household_ref)}</td>
                    <td>
                      {activityLabel(l.activity_code)}
                      {l.sop_step_seq ? <div className="mute small">Bước SOP {l.sop_step_seq}</div> : null}
                    </td>
                    <td>
                      <LocationChip status={l.location_status} distance={l.distance_m} />
                    </td>
                    <td>{l.on_behalf ? <span className="chip c-violet">Ghi hộ</span> : <span className="chip c-mute">Nông dân</span>}</td>
                    <td>
                      <StatusChip map={LOG_STATUS} value={l.status} />
                      {l.review_note ? <div className="mute small">{l.review_note}</div> : null}
                    </td>
                    <td>
                      {scope.canWrite ? (
                        <button type="button" className="btn btn-o btn-sm" onClick={() => setEditing(l)} aria-label={`Sửa nhật ký lô ${s?.plot_code ?? ""} lúc ${formatDateTime(l.event_time)}`}>
                          Sửa
                        </button>
                      ) : null}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        {list.loading ? <Loading /> : null}
        {!list.loading && !list.error && items.length === 0 ? <Empty>Không có nhật ký phù hợp bộ lọc.</Empty> : null}
        <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
      </div>
      {editing ? <CorrectLog log={editing} onClose={() => setEditing(null)} onDone={() => { setEditing(null); reload(); }} /> : null}
    </>
  );
}

/** Sửa nhật ký bằng phiên bản mới (bản ghi bù) — thời điểm và GPS giữ nguyên (HTX US-LOG-01 AC-3). */
function CorrectLog({ log, onClose, onDone }: { log: CropFarmLogState; onClose: () => void; onDone: () => void }) {
  const { api } = useAuth();
  const detail = useAsync(() => unwrap(api.GET("/v1/cropcycle/farm-logs/{id}", { params: { path: { id: log.id } } })), [api, log.id]);
  const steps = useSeasonSteps(log.season_ref);
  const cur = detail.data?.log;
  const [draft, setDraft] = useState<LogDraft | null>(null);
  const effective: LogDraft =
    draft ??
    (cur
      ? {
          activity: cur.activity_code as CropActivity,
          step: cur.sop_step_seq ? String(cur.sop_step_seq) : "",
          machines: cur.machine_codes ?? [],
          input: { name: cur.inputs?.[0]?.name ?? "", qty: cur.inputs?.[0]?.qty ?? "", unit: cur.inputs?.[0]?.unit ?? "kg" },
          note: cur.note ?? "",
        }
      : EMPTY);
  return (
    <ReasonDialog
      title="Sửa nhật ký (bản ghi bù)"
      label="Lý do sửa (ít nhất 10 ký tự)"
      confirmText="Lưu phiên bản mới"
      onClose={onClose}
      run={async (reason, key) => {
        await unwrap(
          api.POST("/v1/cropcycle/farm-logs/{id}/corrections", {
            params: { path: { id: log.id }, header: { "Idempotency-Key": key } },
            body: { ...contentOf(effective), correction_reason: reason },
          }),
        );
        onDone();
      }}
    >
      <p className="small mute">
        Thời điểm {formatDateTime(log.event_time)} và vị trí GPS không sửa được sau khi lưu. Phiên bản mới quay về trạng thái Chờ duyệt.
      </p>
      {detail.error ? <ProblemAlert problem={detail.error} /> : null}
      {cur ? <LogFields draft={effective} set={setDraft} steps={steps} idp="cor" /> : <Loading />}
    </ReasonDialog>
  );
}
