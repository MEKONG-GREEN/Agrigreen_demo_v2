"use client";

import { useMemo, useState } from "react";
import { listChildUnits } from "@/api/adapters/units";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import type { FacilityImportResult, GeoCapacitySnapshot, GeoCapacityView, GeoFacilityEvent, GeoFacilityFeature } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import type { AnyFeatureCollection } from "@/components/LayerMap";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Loading, PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { formatDateTime } from "@/lib/format";
import type { UploadFile } from "@/lib/files";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { boundsOf, DrawSection, EditorMap, ImportResult, rawBody, ReasonField, REGION_BBOX, SpatialFileInput } from "@/gis/editor";
import { draftGeometry, validateDraft, type DrawMode, type LngLat } from "@/gis/geometry";
import { CAPACITY, CAPACITY_SOURCE, CAPACITY_UNIT, FACILITY_KIND, fillLabel } from "@/gis/legend";

const TABS = [
  { id: "list", label: "Danh sách & dung lượng" },
  { id: "edit", label: "Thêm / sửa trên bản đồ" },
  { id: "import", label: "Nhập vị trí từ tệp" },
];

type Facility = GeoFacilityFeature;

/** Kho/Hub: danh sách, chi tiết, nhập tay dung lượng, nhả số đồng bộ (GIS US-WH-01, BR-52); vẽ/sửa (US-BND-05/06); nhập tệp (US-BND-08). */
export default function FacilityAdminPage() {
  const { api } = useAuth();
  const [tab, setTab] = useState("list");
  const [version, setVersion] = useState(0);
  const [selRef, setSelRef] = useState<string | null>(null);
  const [editRef, setEditRef] = useState<string | null>(null);
  const list = useAsync(() => unwrap(api.GET("/v1/geo/facilities", { params: { query: { bbox: REGION_BBOX, include_retired: true, limit: 1000 } } })), [api, version]);
  const reload = () => setVersion((v) => v + 1);
  const items = list.data?.features ?? [];
  const sel = items.find((f) => f.properties.ref === selRef) ?? null;
  return (
    <>
      <PageHead title="Kho / Hub & dung lượng" subtitle="Kho của tenant và Hub MKG (nhận qua van công bố) — số Admin nhập tay không bị lần đồng bộ sau ghi đè cho tới khi nhả (BR-52)" />
      <MfaNotice />
      <Tabs tabs={TABS} value={tab} onChange={setTab} label="Chức năng kho/Hub">
        {tab === "list" ? (
          <div className="grid2" style={{ gridTemplateColumns: "minmax(0, 1.5fr) minmax(320px, 1fr)", alignItems: "start" }}>
            <section className="card sec" aria-label="Danh sách kho/Hub">
              {list.loading && !list.data ? <Loading /> : null}
              {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
              <div className="tbl-wrap">
                <table className="tbl">
                  <caption className="sr-only">Danh sách kho/Hub</caption>
                  <thead>
                    <tr>
                      <th scope="col">Tên</th>
                      <th scope="col">Loại</th>
                      <th scope="col" className="num">Lấp đầy</th>
                      <th scope="col">Nguồn số liệu</th>
                      <th scope="col">Trạng thái</th>
                    </tr>
                  </thead>
                  <tbody>
                    {items.map((f) => {
                      const c = f.properties.capacity;
                      return (
                        <tr key={f.properties.ref} data-ref={f.properties.ref} className={f.properties.ref === selRef ? "hl" : undefined}>
                          <td>
                            <button type="button" className="btn btn-o btn-sm" onClick={() => setSelRef(f.properties.ref)}>
                              {f.properties.name}
                            </button>
                            <div className="mute small">
                              {f.properties.code}
                              {f.properties.origin === "mkg" ? " · Hub MKG" : ""}
                            </div>
                          </td>
                          <td>{FACILITY_KIND[f.properties.kind]}</td>
                          <td className="num">
                            <span className="chip" style={{ background: "#fff", color: CAPACITY[c?.class ?? "unknown"].color, border: `1px solid ${CAPACITY[c?.class ?? "unknown"].color}` }}>
                              {fillLabel(c?.fill_pct)} · {CAPACITY[c?.class ?? "unknown"].label}
                            </span>
                          </td>
                          <td className="small">
                            {c?.source ? CAPACITY_SOURCE[c.source] : "—"}
                            {c?.sync_differs ? (
                              <div>
                                <span className="chip c-warn">Số đồng bộ khác số nhập tay</span>
                              </div>
                            ) : null}
                          </td>
                          <td>{f.properties.status === "retired" ? <span className="chip c-mute">Ngừng dùng</span> : <span className="chip c-ok">Đang dùng</span>}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </section>
            {sel ? (
              <FacilityDetail
                key={`${sel.properties.ref}-${version}`}
                f={sel}
                onChanged={reload}
                onEdit={() => {
                  setEditRef(sel.properties.ref);
                  setTab("edit");
                }}
              />
            ) : (
              <p className="card sec small mute">Chọn một kho/Hub để xem chi tiết, nhập tay dung lượng hoặc nhả số đồng bộ.</p>
            )}
          </div>
        ) : tab === "edit" ? (
          <FacilityEditor
            items={items}
            editRef={editRef}
            onDone={(ref) => {
              reload();
              setSelRef(ref);
              setEditRef(null);
              setTab("list");
            }}
          />
        ) : (
          <FacilityImport onDone={reload} />
        )}
      </Tabs>
    </>
  );
}

function CapacityBox({ c }: { c: GeoCapacityView }) {
  const cls = CAPACITY[c.class];
  return (
    <dl className="kv" style={{ gridTemplateColumns: "150px 1fr" }} data-testid="capacity-view">
      <dt>Mức lấp đầy</dt>
      <dd style={{ color: cls.color }}>
        {c.fill_pct ? `${formatDecimal(c.fill_pct, 1)}%` : "—"} · {cls.label}
      </dd>
      <dt>Sức chứa / đã dùng</dt>
      <dd>{c.capacity ? `${formatDecimal(c.capacity, 0)} / ${formatDecimal(c.used, 0)} ${CAPACITY_UNIT[c.unit ?? "tan"] ?? c.unit}` : "Chưa có số liệu"}</dd>
      <dt>Số đang áp dụng</dt>
      <dd>
        {c.source ? CAPACITY_SOURCE[c.source] : "—"} · {formatDateTime(c.as_of)}
      </dd>
      {c.pending_sync ? (
        <>
          <dt>Số đồng bộ đang chờ</dt>
          <dd>
            {formatDecimal(c.pending_sync.capacity, 0)} / {formatDecimal(c.pending_sync.used, 0)} · {formatDateTime(c.pending_sync.as_of)}
          </dd>
        </>
      ) : null}
    </dl>
  );
}

function FacilityDetail({ f, onChanged, onEdit }: { f: Facility; onChanged: () => void; onEdit: () => void }) {
  const { api, adminApi } = useAuth();
  const ref = f.properties.ref;
  const mkg = f.properties.origin === "mkg";
  const detail = useAsync(async (): Promise<{ capacity: GeoCapacityView; capacity_history: GeoCapacitySnapshot[]; events: GeoFacilityEvent[] }> => {
    if (mkg) {
      const d = await unwrap(api.GET("/v1/geo/hubs/{ref}/capacity", { params: { path: { ref } } }));
      return { capacity: d.capacity, capacity_history: d.capacity_history, events: [] };
    }
    const d = await unwrap(api.GET("/v1/geo/facilities/{ref}", { params: { path: { ref } } }));
    return { capacity: d.capacity, capacity_history: d.capacity_history, events: d.events };
  }, [api, ref, mkg]);
  const [cap, setCap] = useState("");
  const [used, setUsed] = useState("");
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const run = async (fn: () => Promise<unknown>, msg: string) => {
    setBusy(true);
    setProblem(null);
    setDone(null);
    try {
      await fn();
      setDone(msg);
      onChanged();
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };
  const key = () => ({ "Idempotency-Key": newIdempotencyKey() });
  const record = () =>
    run(
      () =>
        mkg
          ? unwrap(adminApi.POST("/v1/geo/hubs/{ref}/capacity", { params: { path: { ref }, header: key() }, body: { capacity: cap.trim(), used: used.trim() } }))
          : unwrap(adminApi.POST("/v1/geo/facilities/{ref}/capacity", { params: { path: { ref }, header: key() }, body: { capacity: cap.trim(), used: used.trim() } })),
      "Đã ghi số nhập tay — có hiệu lực ngay, lần đồng bộ sau không ghi đè.",
    );
  const release = () =>
    run(
      () =>
        mkg
          ? unwrap(adminApi.POST("/v1/geo/hubs/{ref}/capacity/release", { params: { path: { ref }, header: key() }, body: { reason: reason.trim() } }))
          : unwrap(adminApi.POST("/v1/geo/facilities/{ref}/capacity/release", { params: { path: { ref }, header: key() }, body: { reason: reason.trim() } })),
      "Đã nhả số nhập tay — áp dụng số đồng bộ.",
    );
  const setStatus = (op: "retire" | "restore") =>
    run(
      () =>
        unwrap(adminApi.POST(op === "retire" ? "/v1/geo/facilities/{ref}/retire" : "/v1/geo/facilities/{ref}/restore", { params: { path: { ref }, header: key() }, body: { reason: reason.trim() } })),
      op === "retire" ? "Đã ngừng dùng (ẩn khỏi bản đồ, Event Log giữ người xoá và lý do)." : "Đã khôi phục.",
    );
  const c = detail.data?.capacity ?? f.properties.capacity;
  const history = detail.data?.capacity_history ?? [];
  const events = detail.data?.events ?? [];
  const canRelease = c?.source === "manual" || c?.source === "import";
  return (
    <section className="card sec stack" aria-labelledby="fac-h" data-testid="facility-detail">
      <div className="sec-h">
        <h2 id="fac-h">{f.properties.name}</h2>
        {!mkg && f.properties.status === "active" ? (
          <button type="button" className="btn btn-o btn-sm" onClick={onEdit}>
            Sửa trên bản đồ
          </button>
        ) : null}
      </div>
      <p className="small mute">
        {FACILITY_KIND[f.properties.kind]} · {f.properties.code} · {mkg ? "Hub MKG — vị trí do van công bố, chỉ nhập tay dung lượng" : `phiên bản ${f.properties.version}`}
      </p>
      {detail.loading && !detail.data ? <Loading /> : null}
      {c ? <CapacityBox c={c} /> : null}
      {c?.sync_differs ? (
        <div className="alert alert-warn" role="status">
          <span>Van công bố có số mới hơn khác số Admin đang áp dụng. Xác nhận “Nhả số nhập tay” để dùng số đồng bộ.</span>
        </div>
      ) : null}
      <fieldset className="plain">
        <legend>Nhập tay dung lượng (US-WH-01)</legend>
        <div className="row">
          <div className="field" style={{ flex: 1 }}>
            <label htmlFor="cap-total">Sức chứa</label>
            <input id="cap-total" className="input" inputMode="decimal" value={cap} onChange={(e) => setCap(e.target.value)} />
          </div>
          <div className="field" style={{ flex: 1 }}>
            <label htmlFor="cap-used">Đã sử dụng</label>
            <input id="cap-used" className="input" inputMode="decimal" value={used} onChange={(e) => setUsed(e.target.value)} />
          </div>
        </div>
        <button type="button" className="btn btn-p btn-sm" style={{ marginTop: 8 }} disabled={busy || !cap || !used} onClick={() => void record()}>
          Ghi số nhập tay
        </button>
      </fieldset>
      <ReasonField value={reason} onChange={setReason} label="Lý do (nhả số đồng bộ, ngừng dùng/khôi phục — ít nhất 10 ký tự)" />
      <div className="row">
        <button type="button" className="btn btn-o btn-sm" disabled={busy || !canRelease || reason.trim().length < 10} onClick={() => void release()}>
          Nhả số nhập tay (áp dụng số đồng bộ)
        </button>
        {!mkg ? (
          f.properties.status === "active" ? (
            <button type="button" className="btn btn-d btn-sm" disabled={busy || reason.trim().length < 10} onClick={() => void setStatus("retire")}>
              Ngừng dùng
            </button>
          ) : (
            <button type="button" className="btn btn-o btn-sm" disabled={busy || reason.trim().length < 10} onClick={() => void setStatus("restore")}>
              Khôi phục
            </button>
          )
        ) : null}
      </div>
      {problem ? <ProblemAlert problem={problem} /> : null}
      {done ? (
        <p className="chip c-ok" role="status">
          {done}
        </p>
      ) : null}
      {history.length > 0 ? (
        <div className="tbl-wrap">
          <table className="tbl">
            <caption style={{ textAlign: "left", fontWeight: 700, padding: "6px 0" }}>Lịch sử dung lượng</caption>
            <thead>
              <tr>
                <th scope="col">Thời điểm</th>
                <th scope="col">Nguồn</th>
                <th scope="col" className="num">Sức chứa</th>
                <th scope="col" className="num">Đã dùng</th>
                <th scope="col">Ghi chú</th>
              </tr>
            </thead>
            <tbody>
              {[...history].reverse().map((h) => (
                <tr key={h.id}>
                  <td className="small">{formatDateTime(h.as_of)}</td>
                  <td className="small">
                    {CAPACITY_SOURCE[h.source]}
                    {h.action === "release" ? " — nhả" : ""}
                  </td>
                  <td className="num">{formatDecimal(h.capacity, 0)}</td>
                  <td className="num">{formatDecimal(h.used, 0)}</td>
                  <td className="small">{h.correction_reason ?? "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
      {events.length > 0 ? (
        <details>
          <summary className="small">Event Log ({formatInt(events.length)})</summary>
          <ul className="list-plain small">
            {[...events].reverse().map((e) => (
              <li key={e.id}>
                v{e.version} · {e.action} · {formatDateTime(e.recorded_at)} · {e.correction_reason ?? "—"}
              </li>
            ))}
          </ul>
        </details>
      ) : null}
    </section>
  );
}

function FacilityEditor({ items, editRef, onDone }: { items: Facility[]; editRef: string | null; onDone: (ref: string | null) => void }) {
  const { api, adminApi } = useAuth();
  const edit = items.find((f) => f.properties.ref === editRef) ?? null;
  const [mode, setMode] = useState<DrawMode>(edit?.properties.footprint ? "polygon" : "point");
  const [coords, setCoords] = useState<LngLat[]>(() =>
    edit ? (edit.properties.footprint ? ((edit.properties.footprint.coordinates[0] as LngLat[]) ?? []).slice(0, -1) : [edit.geometry.coordinates as LngLat]) : [],
  );
  const [form, setForm] = useState({
    name: edit?.properties.name ?? "",
    kind: edit?.properties.kind ?? "hub",
    province_code: edit?.properties.province_code ?? "",
    commune_code: edit?.properties.commune_code ?? "",
    capacity: edit?.properties.nominal_capacity ?? "",
    unit: edit?.properties.unit ?? "tan",
  });
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const provinces = useAsync(() => listChildUnits(api, null), [api]);
  const communes = useAsync(() => (form.province_code ? listChildUnits(api, form.province_code) : Promise.resolve([])), [api, form.province_code]);
  const context = useMemo<AnyFeatureCollection>(
    () => ({ type: "FeatureCollection", features: items.filter((f) => f.properties.ref !== editRef && f.properties.status === "active").map((f) => ({ type: "Feature", geometry: f.geometry, properties: { fill: "#7c8a83" } })) }),
    [items, editRef],
  );
  const invalid = coords.length === 0 ? "Gắn marker hoặc vẽ khuôn viên trên bản đồ." : validateDraft(mode, coords);
  const save = async () => {
    setBusy(true);
    setProblem(null);
    try {
      const geom = draftGeometry(mode, coords) as unknown as Record<string, never>;
      const shape = mode === "point" ? { point: geom } : { polygon: geom };
      const body = { name: form.name, kind: form.kind, province_code: form.province_code, commune_code: form.commune_code, capacity: form.capacity, unit: form.unit, ...shape };
      if (!edit) {
        const r = await unwrap(adminApi.POST("/v1/geo/facilities", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body }));
        onDone((r.facility as unknown as Facility).properties?.ref ?? null);
      } else {
        await unwrap(
          adminApi.POST("/v1/geo/facilities/{ref}/versions", {
            params: { path: { ref: edit.properties.ref }, header: { "Idempotency-Key": newIdempotencyKey() } },
            body: { ...body, supersedes_id: edit.properties.current_event_id ?? "", correction_reason: reason.trim(), remove_polygon: mode === "point" && !!edit.properties.footprint },
          }),
        );
        onDone(edit.properties.ref);
      }
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="grid2" style={{ gridTemplateColumns: "minmax(0, 1.6fr) minmax(300px, 1fr)", alignItems: "start" }}>
      <div className="card" style={{ padding: 8 }}>
        <EditorMap label="Bản đồ biên tập kho/Hub" context={context} drawing mode={mode} coords={coords} onCoords={setCoords} focus={edit ? { key: edit.properties.ref, bounds: boundsOf(coords) } : null} />
      </div>
      <div className="card sec stack">
        <h2 style={{ fontSize: 15 }}>{edit ? `Sửa ${edit.properties.name}` : "Thêm kho/Hub mới"}</h2>
        <fieldset className="plain">
          <legend>Hình trên bản đồ</legend>
          <label className="row small">
            <input type="radio" name="fac-mode" checked={mode === "point"} onChange={() => { setMode("point"); setCoords(coords.slice(0, 1)); }} /> Gắn marker (điểm)
          </label>
          <label className="row small">
            <input type="radio" name="fac-mode" checked={mode === "polygon"} onChange={() => setMode("polygon")} /> Vẽ khuôn viên (vùng)
          </label>
        </fieldset>
        <DrawSection mode={mode} coords={coords} onCoords={setCoords} />
        <div className="field">
          <label htmlFor="fac-name">Tên kho/Hub</label>
          <input id="fac-name" className="input" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
        </div>
        <div className="row">
          <div className="field" style={{ flex: 1 }}>
            <label htmlFor="fac-kind">Loại</label>
            <select id="fac-kind" className="select" value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as Facility["properties"]["kind"] })}>
              {Object.entries(FACILITY_KIND).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </div>
          <div className="field" style={{ flex: 1 }}>
            <label htmlFor="fac-cap">Sức chứa</label>
            <input id="fac-cap" className="input" inputMode="decimal" value={form.capacity} onChange={(e) => setForm({ ...form, capacity: e.target.value })} />
          </div>
          <div className="field" style={{ flex: 1 }}>
            <label htmlFor="fac-unit">Đơn vị tính</label>
            <select id="fac-unit" className="select" value={form.unit} onChange={(e) => setForm({ ...form, unit: e.target.value as Facility["properties"]["unit"] })}>
              {Object.entries(CAPACITY_UNIT).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </div>
        </div>
        <div className="row">
          <div className="field" style={{ flex: 1 }}>
            <label htmlFor="fac-prov">Tỉnh</label>
            <select id="fac-prov" className="select" value={form.province_code} onChange={(e) => setForm({ ...form, province_code: e.target.value, commune_code: "" })}>
              <option value="">—</option>
              {(provinces.data ?? []).map((u) => (
                <option key={u.code} value={u.code}>
                  {u.name}
                </option>
              ))}
            </select>
          </div>
          <div className="field" style={{ flex: 1 }}>
            <label htmlFor="fac-commune">Xã</label>
            <select id="fac-commune" className="select" value={form.commune_code} onChange={(e) => setForm({ ...form, commune_code: e.target.value })}>
              <option value="">—</option>
              {(communes.data ?? []).map((u) => (
                <option key={u.code} value={u.code}>
                  {u.name}
                </option>
              ))}
            </select>
          </div>
        </div>
        {edit ? <ReasonField value={reason} onChange={setReason} label="Lý do sửa (ít nhất 10 ký tự)" /> : null}
        {invalid ? <span className="small c-warn">{invalid}</span> : null}
        <div className="row">
          <button type="button" className="btn btn-p btn-sm" disabled={busy || !!invalid || (!!edit && reason.trim().length < 10)} onClick={() => void save()}>
            {busy ? "Đang lưu…" : edit ? "Lưu phiên bản mới" : "Lưu kho/Hub"}
          </button>
          <button type="button" className="btn btn-o btn-sm" onClick={() => onDone(editRef)}>
            Huỷ
          </button>
        </div>
        {problem ? <ProblemAlert problem={problem} /> : null}
      </div>
    </div>
  );
}

function FacilityImport({ onDone }: { onDone: () => void }) {
  const { adminApi } = useAuth();
  const [file, setFile] = useState<UploadFile | null>(null);
  const [format, setFormat] = useState<"geojson" | "kml" | "shapefile" | undefined>();
  const [kind, setKind] = useState<"kho" | "hub" | "nha_may">("hub");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [result, setResult] = useState<FacilityImportResult | null>(null);
  const submit = async () => {
    if (!file || !format) return;
    setBusy(true);
    setProblem(null);
    try {
      setResult(await unwrap(adminApi.POST("/v1/geo/facilities/import", { params: { header: { "Idempotency-Key": newIdempotencyKey() }, query: { format, file_name: file.name, kind } }, ...rawBody(file) })));
      onDone();
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <div className="card sec stack">
        <p className="small">
          Thuộc tính mỗi đối tượng: <code>name/ten</code>, <code>code/ma</code>, <code>kind/loai</code>, <code>capacity/suc_chua</code>, <code>unit/don_vi</code>, <code>province_code/ma_tinh</code>,{" "}
          <code>commune_code/ma_xa</code>. Điểm = marker, vùng = khuôn viên. Đối tượng lỗi bị loại kèm lý do.
        </p>
        <SpatialFileInput
          id="fac-imp-file"
          onFile={(f, fmt) => {
            setFile(f);
            setFormat(fmt);
            setResult(null);
          }}
        />
        <div className="field">
          <label htmlFor="fac-imp-kind">Loại mặc định khi thiếu thuộc tính loại</label>
          <select id="fac-imp-kind" className="select" value={kind} onChange={(e) => setKind(e.target.value as typeof kind)}>
            {Object.entries(FACILITY_KIND).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </div>
        <div>
          <button type="button" className="btn btn-p" disabled={!file || !format || busy} onClick={() => void submit()}>
            {busy ? "Đang nhập…" : "Nhập kho/Hub"}
          </button>
        </div>
        {problem ? <ProblemAlert problem={problem} /> : null}
      </div>
      {result ? <ImportResult title="Kết quả nhập kho/Hub" result={result} extra={(f) => result.features[f.index]?.facility_code ?? "—"} /> : null}
    </>
  );
}
