"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useMemo, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { usePagedList } from "@/api/pagination";
import { ApiProblem, fieldErrorMap } from "@/api/problem";
import type { SlaStatus, SyncConflictDetail } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { MapView, type AreaCollection } from "@/components/MapView";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { useNow, SlaCountdown } from "@/components/SlaCountdown";
import { Tabs } from "@/components/Tabs";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDecimal } from "@/lib/decimal";
import { formatDateTime } from "@/lib/format";
import { toAreaGeometry } from "@/lib/geojson";
import { useAsync } from "@/lib/useAsync";

const SLA: Record<SlaStatus, { text: string; cls: string }> = {
  on_track: { text: "Trong hạn", cls: "c-ok" },
  due_soon: { text: "Sắp hết hạn (< 24 giờ)", cls: "c-warn" },
  overdue: { text: "Quá hạn SLA", cls: "c-bad" },
  resolved_on_time: { text: "Đã xử lý đúng hạn", cls: "c-mute" },
  resolved_late: { text: "Đã xử lý trễ hạn", cls: "c-mute" },
};

const ENTITY: Record<string, string> = {
  plot_boundary: "Ranh giới thửa",
  task_update: "Cập nhật nhiệm vụ",
  field_report: "Báo cáo hiện trường",
  consent: "Đồng ý",
};

const ROLE_COLOR: Record<string, string> = { record: "#f0871a", current: "#22b86c", overlapping: "#7a4fd6" };
const ROLE_TEXT: Record<string, string> = { record: "Bản gây xung đột", current: "Bản hiện hành", overlapping: "Thửa bị chồng lấn" };

/** Bản đồ xem trước các phiên bản ranh giới (mỗi phiên bản một màu). */
function VersionsMap({ versions }: { versions: { key: string; role: string; label: string; geometry: unknown }[] }) {
  const fc = useMemo<AreaCollection | null>(() => {
    const features = versions.flatMap((v) => {
      const g = v.geometry as { type?: unknown; coordinates?: unknown } | null | undefined;
      if (!g || (g.type !== "Polygon" && g.type !== "MultiPolygon") || !Array.isArray(g.coordinates)) return [];
      const geometry = toAreaGeometry({ type: g.type, coordinates: g.coordinates });
      return geometry ? [{ type: "Feature" as const, id: v.key, geometry, properties: { code: v.key, name: v.label, fill: ROLE_COLOR[v.role] ?? "#9fd8b4" } }] : [];
    });
    return features.length ? { type: "FeatureCollection", features } : null;
  }, [versions]);
  if (!fc) return null;
  return (
    <div>
      <MapView label="Xem trước các phiên bản ranh giới" areas={fc} height={300} showAreaLabels={false} />
      <ul className="legend row" style={{ flexDirection: "row", marginTop: 6 }}>
        {Array.from(new Set(versions.map((v) => v.role))).map((r) => (
          <li key={r}>
            <span className="sw" style={{ background: ROLE_COLOR[r] ?? "#9fd8b4" }} aria-hidden="true" />
            {ROLE_TEXT[r] ?? r}
          </li>
        ))}
      </ul>
    </div>
  );
}

function ResolveForm({ c, onDone }: { c: SyncConflictDetail; onDone: () => void }) {
  const { adminApi } = useAuth();
  const records = c.records.filter((r) => !!r.record_id);
  const [mode, setMode] = useState<"keep_version" | "correction">("keep_version");
  const [keep, setKeep] = useState(records[0]?.record_id ?? "");
  const [correctionId, setCorrectionId] = useState("");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const errs = problem ? fieldErrorMap(problem) : {};
  const labelOf = (rid: string | null) => c.versions.find((v) => v.record_id === rid)?.label;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    try {
      await unwrap(
        adminApi.POST("/v1/sync/conflicts/{id}/resolve", {
          params: { path: { id: c.conflict_id }, header: { "Idempotency-Key": newIdempotencyKey() } },
          body: {
            note: note.trim(),
            resolution: mode,
            resolved_record_id: mode === "keep_version" ? keep || undefined : correctionId.trim() || undefined,
          },
        }),
      );
      onDone();
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="panel stack" style={{ gap: 10 }} onSubmit={(e) => void submit(e)} noValidate data-testid="resolve-form">
      <b>Xử lý xung đột</b>
      <fieldset className="plain">
        <legend>Cách xử lý</legend>
        <label className="row small" style={{ gap: 6 }}>
          <input type="radio" name="res-mode" checked={mode === "keep_version"} onChange={() => setMode("keep_version")} />
          Giữ một phiên bản
        </label>
        <label className="row small" style={{ gap: 6 }}>
          <input type="radio" name="res-mode" checked={mode === "correction"} onChange={() => setMode("correction")} />
          Tạo bản ghi bù (đã sửa ở module sở hữu dữ liệu)
        </label>
      </fieldset>
      {mode === "keep_version" ? (
        <div className="field">
          <label htmlFor="res-keep">Phiên bản được giữ</label>
          <select id="res-keep" className="select" value={keep} onChange={(e) => setKeep(e.target.value)}>
            {records.map((r) => (
              <option key={r.record_id} value={r.record_id ?? ""}>
                {labelOf(r.record_id) ?? `Bản ghi từ máy ${r.device_id.slice(-4)}`} — {formatDateTime(r.event_time)}
              </option>
            ))}
          </select>
        </div>
      ) : (
        <div className="field">
          <label htmlFor="res-corr">Mã bản ghi bù (tuỳ chọn)</label>
          <input id="res-corr" className="input" value={correctionId} onChange={(e) => setCorrectionId(e.target.value)} placeholder="UUID bản ghi bù đã tạo" />
        </div>
      )}
      <div className="field">
        <label htmlFor="res-note">Ghi chú xử lý (tối thiểu 10 ký tự)</label>
        <textarea id="res-note" className="input textarea" value={note} onChange={(e) => setNote(e.target.value)} aria-invalid={!!errs.note} />
        {errs.note ? <span className="err">{errs.note}</span> : null}
      </div>
      {problem && problem.code !== "validation_failed" ? <ProblemAlert problem={problem} /> : null}
      <div>
        <button type="submit" className="btn btn-p btn-sm" disabled={busy || note.trim().length < 10}>
          {busy ? "Đang lưu…" : "Đóng xung đột"}
        </button>
      </div>
    </form>
  );
}

function SyncConflictPanel({ id, onChanged }: { id: string; onChanged: () => void }) {
  const { adminApi } = useAuth();
  const now = useNow();
  const [version, setVersion] = useState(0);
  const d = useAsync(() => unwrap(adminApi.GET("/v1/sync/conflicts/{id}", { params: { path: { id } } })), [adminApi, id, version]);
  const c = d.data;
  const geoVersions = (c?.versions ?? []).map((v) => ({ key: v.record_id, role: v.role, label: v.label, geometry: (v.data as Record<string, unknown>).geometry }));
  return (
    <div className="stack" data-testid="conflict-panel">
      {d.error ? <ProblemAlert problem={d.error} onRetry={d.reload} /> : null}
      {d.loading && !c ? <Loading /> : null}
      {c ? (
        <>
          <div className="row" style={{ justifyContent: "space-between" }}>
            <h2 style={{ fontSize: 17 }}>{ENTITY[c.entity] ?? c.entity}</h2>
            {c.sla_status ? <span className={`chip ${SLA[c.sla_status].cls}`}>{SLA[c.sla_status].text}</span> : null}
          </div>
          <dl className="kv small">
            <dt>Mở lúc</dt>
            <dd>{formatDateTime(c.opened_at)}</dd>
            <dt>Hạn SLA</dt>
            <dd>
              <SlaCountdown dueAt={c.due_at} resolvedAt={c.resolved_at} now={now} />
            </dd>
            <dt>Trạng thái</dt>
            <dd>{c.status === "open" ? "Đang mở" : `Đã đóng${c.resolution ? ` (${c.resolution === "keep_version" ? "giữ phiên bản" : "bản ghi bù"})` : ""}`}</dd>
            {c.resolution_note ? (
              <>
                <dt>Ghi chú</dt>
                <dd>{c.resolution_note}</dd>
              </>
            ) : null}
          </dl>
          <div>
            <h3 style={{ fontSize: 14, marginBottom: 6 }}>Hai phiên bản</h3>
            {!c.versions_available ? <p className="mute small">Module sở hữu dữ liệu chưa cung cấp phiên bản để so sánh.</p> : null}
            <div className="tbl-wrap">
              <table className="tbl">
                <thead>
                  <tr>
                    <th scope="col">Phiên bản</th>
                    <th scope="col">Thời điểm</th>
                    <th scope="col">Dữ liệu</th>
                  </tr>
                </thead>
                <tbody>
                  {c.versions.map((v) => {
                    const data = v.data as Record<string, unknown>;
                    return (
                      <tr key={v.record_id}>
                        <td>
                          <span className="sw" style={{ display: "inline-block", width: 10, height: 10, borderRadius: 3, background: ROLE_COLOR[v.role], marginRight: 6 }} aria-hidden="true" />
                          <b>{v.label}</b>
                          {v.current ? <div className="mute small">đang hiện hành</div> : null}
                        </td>
                        <td className="small">{formatDateTime(v.event_time)}</td>
                        <td className="small">
                          {Object.entries(data)
                            .filter(([k]) => k !== "geometry")
                            .map(([k, val]) => (
                              <div key={k}>
                                <span className="mute">{k}:</span> {k === "area_ha" && typeof val === "string" ? `${formatDecimal(val, 2)} ha` : String(val)}
                              </div>
                            ))}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </div>
          {c.entity === "plot_boundary" ? <VersionsMap versions={geoVersions} /> : null}
          <details>
            <summary className="small">Bản ghi máy gửi ({c.records.length}) — chỉ mã băm, không có nội dung</summary>
            <ul className="list-plain small">
              {c.records.map((r) => (
                <li key={r.record_idem_key}>
                  {r.op} · {r.status} · máy <code>{r.device_id.slice(-8)}</code> · {formatDateTime(r.event_time)}
                </li>
              ))}
            </ul>
          </details>
          {c.status === "open" ? (
            <ResolveForm
              c={c}
              onDone={() => {
                setVersion((v) => v + 1);
                onChanged();
              }}
            />
          ) : null}
        </>
      ) : null}
    </div>
  );
}

function SyncTab({ focus, onFocus }: { focus: string | null; onFocus: (id: string | null) => void }) {
  const { adminApi } = useAuth();
  const [sla, setSla] = useState<"" | "on_track" | "due_soon" | "overdue">("");
  const [entity, setEntity] = useState("");
  const [status, setStatus] = useState<"" | "open" | "resolved">("open");
  const [version, setVersion] = useState(0);
  const list = usePagedList(
    (cursor) =>
      unwrap(
        adminApi.GET("/v1/sync/conflicts", {
          params: { query: { sla: sla || undefined, entity: entity || undefined, status: status || undefined, cursor, limit: 50 } },
        }),
      ),
    [adminApi, sla, entity, status, version],
  );
  return (
    <div className="grid2" style={{ gridTemplateColumns: "minmax(0, 1.2fr) minmax(320px, 1fr)", alignItems: "start" }}>
      <div>
        <div className="row" style={{ marginBottom: 12 }}>
          <div className="field">
            <label htmlFor="cf-sla">SLA</label>
            <select id="cf-sla" className="select" value={sla} onChange={(e) => setSla(e.target.value as typeof sla)}>
              <option value="">Tất cả</option>
              <option value="overdue">Quá hạn</option>
              <option value="due_soon">Sắp hết hạn (&lt; 24 giờ)</option>
              <option value="on_track">Trong hạn</option>
            </select>
          </div>
          <div className="field">
            <label htmlFor="cf-entity">Loại dữ liệu</label>
            <select id="cf-entity" className="select" value={entity} onChange={(e) => setEntity(e.target.value)}>
              <option value="">Tất cả</option>
              {Object.entries(ENTITY).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="cf-status">Trạng thái</label>
            <select id="cf-status" className="select" value={status} onChange={(e) => setStatus(e.target.value as typeof status)}>
              <option value="open">Đang mở</option>
              <option value="resolved">Đã đóng</option>
              <option value="">Tất cả</option>
            </select>
          </div>
        </div>
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        <ul className="list-plain">
          {list.items.map((c) => (
            <li key={c.conflict_id}>
              <button type="button" onClick={() => onFocus(c.conflict_id)} style={{ textAlign: "left", width: "100%" }} aria-current={focus === c.conflict_id ? "true" : undefined}>
                <div className="row" style={{ justifyContent: "space-between" }}>
                  <b>{ENTITY[c.entity] ?? c.entity}</b>
                  {c.sla_status ? <span className={`chip ${SLA[c.sla_status].cls}`}>{SLA[c.sla_status].text}</span> : null}
                </div>
                <div className="mute small">
                  Mở {formatDateTime(c.opened_at)} · hạn {formatDateTime(c.due_at)}
                </div>
              </button>
            </li>
          ))}
        </ul>
        {list.loading ? <Loading /> : null}
        {!list.loading && !list.error && list.items.length === 0 ? <Empty>Không có xung đột phù hợp.</Empty> : null}
        <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
      </div>
      <aside className="card sec" aria-label="Chi tiết xung đột">
        {focus ? <SyncConflictPanel key={focus} id={focus} onChanged={() => setVersion((v) => v + 1)} /> : <p className="mute">Chọn một xung đột để xem hai phiên bản và xử lý.</p>}
      </aside>
    </div>
  );
}

function GeoConflictPanel({ id, onChanged }: { id: string; onChanged: () => void }) {
  const { api } = useAuth();
  const [version, setVersion] = useState(0);
  const d = useAsync(() => unwrap(api.GET("/v1/geo/conflicts/{id}", { params: { path: { id } } })), [api, id, version]);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const c = d.data;
  const resolve = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    try {
      await unwrap(api.POST("/v1/geo/conflicts/{id}/resolve", { params: { path: { id }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: { note: note.trim() } }));
      setVersion((v) => v + 1);
      onChanged();
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="stack">
      {d.error ? <ProblemAlert problem={d.error} onRetry={d.reload} /> : null}
      {d.loading && !c ? <Loading /> : null}
      {c ? (
        <>
          <h2 style={{ fontSize: 17 }}>{c.kind === "concurrent_edit" ? "Sửa đồng thời" : c.kind === "cross_coop_overlap" ? "Chồng lấn HTX khác" : "Chồng lấn cùng HTX"}</h2>
          <ul className="list-plain small">
            {c.versions.map((v) => (
              <li key={v.boundary_id}>
                <b style={{ display: "inline" }}>{ROLE_TEXT[v.role] ?? v.role}</b> · thửa {v.plot_code} · phiên bản {v.version} · {formatDecimal(v.area_ha, 2)} ha
              </li>
            ))}
          </ul>
          <VersionsMap versions={c.versions.map((v) => ({ key: v.boundary_id, role: v.role, label: `${ROLE_TEXT[v.role] ?? v.role} ${v.plot_code}`, geometry: v.geometry }))} />
          {c.status === "open" ? (
            <form className="panel stack" style={{ gap: 8 }} onSubmit={(e) => void resolve(e)}>
              <label htmlFor="geo-note" style={{ fontWeight: 600 }}>
                Ghi chú xử lý (≥ 10 ký tự; sửa hình là một phiên bản ranh giới mới)
              </label>
              <textarea id="geo-note" className="input textarea" value={note} onChange={(e) => setNote(e.target.value)} />
              {problem ? <ProblemAlert problem={problem} /> : null}
              <div>
                <button type="submit" className="btn btn-p btn-sm" disabled={busy || note.trim().length < 10}>
                  {busy ? "Đang lưu…" : "Đóng xung đột"}
                </button>
              </div>
            </form>
          ) : (
            <p className="small">Đã xử lý {formatDateTime(c.resolved_at)} — {c.resolution_note}</p>
          )}
        </>
      ) : null}
    </div>
  );
}

function GeoTab() {
  const { api } = useAuth();
  const [focus, setFocus] = useState<string | null>(null);
  const [version, setVersion] = useState(0);
  const list = usePagedList((cursor) => unwrap(api.GET("/v1/geo/conflicts", { params: { query: { status: "open", cursor, limit: 50 } } })), [api, version]);
  return (
    <div className="grid2" style={{ gridTemplateColumns: "minmax(0, 1.2fr) minmax(320px, 1fr)", alignItems: "start" }}>
      <div>
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        <ul className="list-plain">
          {list.items.map((c) => (
            <li key={c.id}>
              <button type="button" onClick={() => setFocus(c.id)} style={{ textAlign: "left", width: "100%" }}>
                <b>{c.kind === "concurrent_edit" ? "Sửa đồng thời" : "Chồng lấn ranh giới"}</b>
                <div className="mute small">
                  Xã {c.commune_code} · {c.max_overlap_pct ? `chồng lấn ${formatDecimal(c.max_overlap_pct, 1)}% · ` : ""}hạn {formatDateTime(c.due_at)}
                </div>
              </button>
            </li>
          ))}
        </ul>
        {list.loading ? <Loading /> : null}
        {!list.loading && !list.error && list.items.length === 0 ? <Empty>Không có xung đột ranh giới đang mở.</Empty> : null}
        <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
      </div>
      <aside className="card sec" aria-label="Chi tiết xung đột ranh giới">
        {focus ? <GeoConflictPanel key={focus} id={focus} onChanged={() => setVersion((v) => v + 1)} /> : <p className="mute">Chọn một xung đột ranh giới.</p>}
      </aside>
    </div>
  );
}

/** Đối soát xung đột đồng bộ (US-SYNC-03) — Admin API (vai sync_conflict_resolver, MFA) + xung đột ranh giới pf/geo. */
function ConflictsInner() {
  const params = useSearchParams();
  const router = useRouter();
  const focus = params.get("id");
  const [tab, setTab] = useState("sync");
  return (
    <>
      <PageHead title="Xung đột đồng bộ" subtitle="Bản ghi từ máy ngoại tuyến mâu thuẫn với dữ liệu máy chủ — SLA xử lý 72 giờ (C-8, tạm)" />
      <MfaNotice />
      <div className="alert alert-info" style={{ marginBottom: 16 }}>
        <Icon name="info" />
        <span>
          Dữ liệu không bị ghi đè hay xoá: quyết định “giữ phiên bản” hoặc “bản ghi bù” được module sở hữu dữ liệu áp dụng bằng bản ghi mới (chỉ ghi
          thêm, RB-1).
        </span>
      </div>
      <div className="card sec">
        <Tabs
          label="Loại xung đột"
          value={tab}
          onChange={setTab}
          tabs={[
            { id: "sync", label: "Sổ xung đột đồng bộ" },
            { id: "geo", label: "Chồng lấn ranh giới" },
          ]}
        >
          {tab === "sync" ? <SyncTab focus={focus} onFocus={(id) => router.replace(id ? `/quan-tri/xung-dot?id=${id}` : "/quan-tri/xung-dot")} /> : <GeoTab />}
        </Tabs>
      </div>
    </>
  );
}

export default function ConflictsPage() {
  return (
    <Suspense fallback={<Loading />}>
      <ConflictsInner />
    </Suspense>
  );
}
