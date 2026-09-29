"use client";

import { useCallback, useMemo, useState } from "react";
import { listAllCooperatives } from "@/api/adapters/cooperatives";
import { listChildUnits } from "@/api/adapters/units";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { ApiProblem } from "@/api/problem";
import type { CoopImportResult, GeoRoadFeature } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import type { AnyFeatureCollection } from "@/components/LayerMap";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Loading, PageHead } from "@/components/ui";
import { formatDecimal } from "@/lib/decimal";
import { formatDateTime } from "@/lib/format";
import type { UploadFile } from "@/lib/files";
import { useAsync, toApiProblem } from "@/lib/useAsync";
import { boundsOf, DrawSection, EditorMap, ImportResult, rawBody, ReasonField, REGION_BBOX, SpatialFileInput } from "@/gis/editor";
import { draftGeometry, formatLength, validateDraft, type LngLat } from "@/gis/geometry";
import { ROAD_CLASS, ROAD_COLOR } from "@/gis/legend";

const TABS = [
  { id: "coop", label: "Ranh giới HTX" },
  { id: "road", label: "Đường bộ" },
  { id: "import", label: "Nhập ranh giới HTX từ tệp" },
];

/** Biên tập dữ liệu bản đồ dùng chung: ranh giới HTX (GIS US-BND-02/03/07), đường bộ (US-ROAD-02/03). */
export default function MapEditorPage() {
  const [tab, setTab] = useState("coop");
  return (
    <>
      <PageHead title="Biên tập bản đồ" subtitle="Vẽ/sửa ranh giới HTX và tuyến đường bộ trực tiếp trên bản đồ — mỗi lần lưu là một phiên bản mới kèm lý do, không xoá cứng" />
      <MfaNotice />
      <Tabs tabs={TABS} value={tab} onChange={setTab} label="Loại dữ liệu bản đồ">
        {tab === "coop" ? <CoopBoundaryEditor /> : tab === "road" ? <RoadEditor /> : <CoopImport />}
      </Tabs>
    </>
  );
}

const ACTION_LABEL: Record<string, string> = { recorded: "Ghi hình", retired: "Ngừng dùng", restored: "Khôi phục" };

function CoopBoundaryEditor() {
  const { api } = useAuth();
  const coops = useAsync(() => listAllCooperatives(api), [api]);
  const [ref, setRef] = useState("");
  const [version, setVersion] = useState(0);
  const detail = useAsync(async () => {
    if (!ref) return null;
    try {
      return await unwrap(api.GET("/v1/geo/cooperative-boundaries/{ref}", { params: { path: { ref } } }));
    } catch (e) {
      if (e instanceof ApiProblem && e.code === "geo.cooperative_boundary_not_found") return null;
      throw e;
    }
  }, [api, ref, version]);
  const [drawing, setDrawing] = useState(false);
  const [coords, setCoords] = useState<LngLat[]>([]);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const cur = detail.data?.versions.at(-1);
  const status = detail.data?.cooperative.properties.status ?? null;
  const exists = !!detail.data;
  const currentRing = useMemo(() => ((cur?.geometry?.coordinates?.[0] as LngLat[] | undefined) ?? []).slice(0, -1), [cur]);
  const coop = coops.data?.find((c) => c.ref === ref);

  const context = useMemo<AnyFeatureCollection | null>(() => {
    if (!detail.data || drawing) return null;
    return { type: "FeatureCollection", features: [{ type: "Feature", geometry: detail.data.cooperative.geometry, properties: { fill: status === "retired" ? "#cfd8d3" : "#9fd8b4" } }] };
  }, [detail.data, drawing, status]);
  const focusCoords = drawing ? coords : currentRing;
  const loc = coop?.location ? ([Number(coop.location.lon), Number(coop.location.lat)] as LngLat) : null;
  const focus = useMemo(() => {
    const b = boundsOf(focusCoords);
    return b ? { key: `${ref}-${version}-${drawing}`, bounds: b } : loc ? { key: `${ref}-loc`, center: loc, zoom: 13 } : null;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- chỉ đổi khi chọn HTX / bắt đầu vẽ
  }, [ref, version, drawing, detail.data]);

  const start = () => {
    setDrawing(true);
    setCoords(exists ? currentRing : []);
    setProblem(null);
    setDone(null);
  };
  const run = async (fn: () => Promise<unknown>, msg: string) => {
    setBusy(true);
    setProblem(null);
    try {
      await fn();
      setDone(msg);
      setDrawing(false);
      setReason("");
      setVersion((v) => v + 1);
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };
  const invalid = validateDraft("polygon", coords);
  const save = () =>
    run(async () => {
      const geometry = draftGeometry("polygon", coords) as { type: "Polygon"; coordinates: unknown[] };
      if (!exists)
        await unwrap(api.POST("/v1/geo/cooperative-boundaries", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body: { cooperative_ref: ref || null, geometry } }));
      else
        await unwrap(
          api.POST("/v1/geo/cooperative-boundaries/{ref}/versions", {
            params: { path: { ref }, header: { "Idempotency-Key": newIdempotencyKey() } },
            body: { geometry, supersedes_id: cur!.boundary_id, correction_reason: reason.trim() },
          }),
        );
    }, exists ? "Đã lưu phiên bản ranh giới mới." : "Đã lưu ranh giới HTX.");
  const setStatus = (op: "retire" | "restore") =>
    run(
      () =>
        unwrap(
          api.POST(op === "retire" ? "/v1/geo/cooperative-boundaries/{ref}/retire" : "/v1/geo/cooperative-boundaries/{ref}/restore", {
            params: { path: { ref }, header: { "Idempotency-Key": newIdempotencyKey() } },
            body: { reason: reason.trim() },
          }),
        ),
      op === "retire" ? "Đã ngừng dùng ranh giới (ẩn khỏi bản đồ dùng chung, lịch sử vẫn giữ)." : "Đã khôi phục ranh giới.",
    );

  return (
    <div className="grid2" style={{ gridTemplateColumns: "minmax(0, 1.6fr) minmax(300px, 1fr)", alignItems: "start" }}>
      <div className="card" style={{ padding: 8 }}>
        <EditorMap label="Bản đồ biên tập ranh giới HTX" context={context} drawing={drawing} mode="polygon" coords={coords} onCoords={setCoords} focus={focus} />
      </div>
      <div className="card sec stack">
        <div className="field">
          <label htmlFor="bnd-coop">Hợp tác xã</label>
          <select
            id="bnd-coop"
            className="select"
            value={ref}
            onChange={(e) => {
              setRef(e.target.value);
              setDrawing(false);
              setDone(null);
              setProblem(null);
            }}
          >
            <option value="">— Chọn HTX —</option>
            {(coops.data ?? []).map((c) => (
              <option key={c.ref} value={c.ref}>
                {c.name} ({c.code})
              </option>
            ))}
          </select>
        </div>
        {ref && detail.loading ? <Loading /> : null}
        {detail.error ? <ProblemAlert problem={detail.error} onRetry={detail.reload} /> : null}
        {ref && !detail.loading ? (
          <p className="small" data-testid="bnd-state">
            {exists
              ? `Phiên bản ${cur?.version ?? "—"} · ${status === "retired" ? "đã ngừng dùng" : "đang dùng"} · ${formatDecimal(cur?.area_ha, 2)} ha`
              : "HTX chưa có ranh giới — đang hiện bằng marker vị trí."}
          </p>
        ) : null}
        {ref && !drawing ? (
          <div className="row">
            <button type="button" className="btn btn-p btn-sm" onClick={start} disabled={status === "retired"}>
              {exists ? "Sửa ranh giới" : "Vẽ ranh giới mới"}
            </button>
          </div>
        ) : null}
        {drawing ? (
          <>
            <DrawSection mode="polygon" coords={coords} onCoords={setCoords} />
            {exists ? <ReasonField value={reason} onChange={setReason} label="Lý do sửa (ít nhất 10 ký tự)" /> : null}
            <div className="row">
              <button type="button" className="btn btn-p btn-sm" disabled={busy || !!invalid || (exists && reason.trim().length < 10)} onClick={() => void save()}>
                {busy ? "Đang lưu…" : exists ? "Lưu phiên bản mới" : "Lưu ranh giới"}
              </button>
              <button type="button" className="btn btn-o btn-sm" onClick={() => setDrawing(false)}>
                Huỷ
              </button>
            </div>
          </>
        ) : null}
        {exists && !drawing ? (
          <div className="stack">
            <ReasonField value={reason} onChange={setReason} label="Lý do ngừng dùng / khôi phục (ít nhất 10 ký tự)" />
            <div className="row">
              {status === "active" ? (
                <button type="button" className="btn btn-d btn-sm" disabled={busy || reason.trim().length < 10} onClick={() => void setStatus("retire")}>
                  Ngừng dùng ranh giới
                </button>
              ) : (
                <button type="button" className="btn btn-o btn-sm" disabled={busy || reason.trim().length < 10} onClick={() => void setStatus("restore")}>
                  Khôi phục
                </button>
              )}
            </div>
          </div>
        ) : null}
        {problem ? <ProblemAlert problem={problem} /> : null}
        {done ? (
          <p className="chip c-ok" role="status">
            {done}
          </p>
        ) : null}
        {detail.data ? (
          <div className="tbl-wrap">
            <table className="tbl">
              <caption className="sr-only">Lịch sử ranh giới</caption>
              <thead>
                <tr>
                  <th scope="col">Phiên bản</th>
                  <th scope="col">Thao tác</th>
                  <th scope="col" className="num">Diện tích (ha)</th>
                  <th scope="col">Thời điểm</th>
                  <th scope="col">Lý do</th>
                </tr>
              </thead>
              <tbody>
                {[...detail.data.versions].reverse().map((v) => (
                  <tr key={v.boundary_id}>
                    <td>{v.version}</td>
                    <td>{ACTION_LABEL[v.action] ?? v.action}</td>
                    <td className="num">{formatDecimal(v.area_ha, 2)}</td>
                    <td className="small">{formatDateTime(v.recorded_at)}</td>
                    <td className="small">{v.correction_reason ?? "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </div>
    </div>
  );
}

function RoadEditor() {
  const { api, adminApi } = useAuth();
  const [version, setVersion] = useState(0);
  const roads = useAsync(() => unwrap(api.GET("/v1/geo/roads", { params: { query: { bbox: REGION_BBOX, limit: 1000 } } })), [api, version]);
  const provinces = useAsync(() => listChildUnits(api, null), [api]);
  const [sel, setSel] = useState<GeoRoadFeature | null>(null);
  const [drawing, setDrawing] = useState(false);
  const [coords, setCoords] = useState<LngLat[]>([]);
  const [form, setForm] = useState({ name: "", road_class: "", max_load_tons: "", province_code: "82" });
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [done, setDone] = useState<string | null>(null);

  const context = useMemo<AnyFeatureCollection | null>(() => {
    if (!roads.data) return null;
    return {
      type: "FeatureCollection",
      features: roads.data.features
        .filter((f) => !(drawing && sel && f.properties.ref === sel.properties.ref))
        .map((f) => ({ type: "Feature", geometry: f.geometry, properties: { ref: f.properties.ref, stroke: sel?.properties.ref === f.properties.ref ? "#1565c0" : ROAD_COLOR[f.properties.road_class], width: 3 } })),
    };
  }, [roads.data, sel, drawing]);

  const pick = useCallback((f: GeoRoadFeature | null) => {
    setSel(f);
    setDrawing(false);
    setProblem(null);
    setDone(null);
    if (f) setForm({ name: f.properties.name, road_class: f.properties.road_class, max_load_tons: f.properties.max_load_tons, province_code: f.properties.province_code });
  }, []);
  const startNew = () => {
    pick(null);
    setForm({ name: "", road_class: "", max_load_tons: "", province_code: "82" });
    setCoords([]);
    setDrawing(true);
  };
  const startEdit = () => {
    if (!sel) return;
    setCoords(sel.geometry.coordinates as LngLat[]);
    setDrawing(true);
  };
  const invalid = drawing ? validateDraft("line", coords) : null;
  const save = async () => {
    setBusy(true);
    setProblem(null);
    try {
      const geometry = draftGeometry("line", coords) as unknown as Record<string, never>;
      if (!sel)
        await unwrap(
          adminApi.POST("/v1/geo/roads", {
            params: { header: { "Idempotency-Key": newIdempotencyKey() } },
            body: { name: form.name, road_class: (form.road_class || undefined) as GeoRoadFeature["properties"]["road_class"], max_load_tons: form.max_load_tons, province_code: form.province_code, geometry },
          }),
        );
      else
        await unwrap(
          adminApi.POST("/v1/geo/roads/{ref}/versions", {
            params: { path: { ref: sel.properties.ref }, header: { "Idempotency-Key": newIdempotencyKey() } },
            body: {
              name: form.name,
              road_class: (form.road_class || undefined) as GeoRoadFeature["properties"]["road_class"],
              max_load_tons: form.max_load_tons,
              geometry,
              supersedes_id: sel.properties.segment_id,
              correction_reason: reason.trim(),
            },
          }),
        );
      setDone(sel ? "Đã lưu phiên bản tuyến mới — chiều dài do hệ thống tính lại." : "Đã thêm tuyến đường bộ.");
      setDrawing(false);
      setSel(null);
      setReason("");
      setVersion((v) => v + 1);
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="grid2" style={{ gridTemplateColumns: "minmax(0, 1.6fr) minmax(300px, 1fr)", alignItems: "start" }}>
      <div className="card" style={{ padding: 8 }}>
        <EditorMap
          label="Bản đồ biên tập đường bộ"
          context={context}
          drawing={drawing}
          mode="line"
          coords={coords}
          onCoords={setCoords}
          onPick={(p) => pick(roads.data?.features.find((f) => f.properties.ref === p.ref) ?? null)}
          focus={drawing ? null : sel ? { key: sel.properties.ref, bounds: boundsOf(sel.geometry.coordinates as LngLat[]) } : null}
        />
      </div>
      <div className="card sec stack">
        <div className="row">
          <button type="button" className="btn btn-p btn-sm" onClick={startNew}>
            Vẽ tuyến mới
          </button>
        </div>
        <div className="field">
          <label htmlFor="road-pick">Tuyến đang có</label>
          <select id="road-pick" className="select" value={sel?.properties.ref ?? ""} onChange={(e) => pick(roads.data?.features.find((f) => f.properties.ref === e.target.value) ?? null)}>
            <option value="">—</option>
            {(roads.data?.features ?? []).map((f) => (
              <option key={f.properties.ref} value={f.properties.ref}>
                {f.properties.name} · {formatLength(f.properties.length_m)}
              </option>
            ))}
          </select>
        </div>
        {(drawing || sel) ? (
          <>
            <div className="field">
              <label htmlFor="road-name">Tên tuyến</label>
              <input id="road-name" className="input" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </div>
            <div className="row">
              <div className="field" style={{ flex: 1 }}>
                <label htmlFor="road-class">Phân loại</label>
                <select id="road-class" className="select" value={form.road_class} onChange={(e) => setForm({ ...form, road_class: e.target.value })}>
                  <option value="">—</option>
                  {Object.entries(ROAD_CLASS).map(([k, v]) => (
                    <option key={k} value={k}>
                      {v}
                    </option>
                  ))}
                </select>
              </div>
              <div className="field" style={{ flex: 1 }}>
                <label htmlFor="road-load">Tải trọng tối đa (tấn)</label>
                <input id="road-load" className="input" inputMode="decimal" value={form.max_load_tons} onChange={(e) => setForm({ ...form, max_load_tons: e.target.value })} />
              </div>
            </div>
            {!sel ? (
              <div className="field">
                <label htmlFor="road-prov">Tỉnh</label>
                <select id="road-prov" className="select" value={form.province_code} onChange={(e) => setForm({ ...form, province_code: e.target.value })}>
                  {(provinces.data ?? []).map((u) => (
                    <option key={u.code} value={u.code}>
                      {u.name}
                    </option>
                  ))}
                </select>
              </div>
            ) : (
              <p className="small">
                Chiều dài hiện hành: <b>{formatLength(sel.properties.length_m)}</b> (hệ thống tính) · phiên bản {sel.properties.version}
              </p>
            )}
          </>
        ) : null}
        {sel && !drawing ? (
          <button type="button" className="btn btn-o btn-sm" onClick={startEdit}>
            Sửa hình tuyến / thuộc tính
          </button>
        ) : null}
        {drawing ? (
          <>
            <DrawSection mode="line" coords={coords} onCoords={setCoords} />
            {sel ? <ReasonField value={reason} onChange={setReason} label="Lý do sửa (ít nhất 10 ký tự)" /> : null}
            <div className="row">
              <button type="button" className="btn btn-p btn-sm" disabled={busy || !!invalid || (!!sel && reason.trim().length < 10)} onClick={() => void save()}>
                {busy ? "Đang lưu…" : sel ? "Lưu phiên bản mới" : "Lưu tuyến"}
              </button>
              <button type="button" className="btn btn-o btn-sm" onClick={() => setDrawing(false)}>
                Huỷ
              </button>
            </div>
          </>
        ) : null}
        {problem ? <ProblemAlert problem={problem} /> : null}
        {done ? (
          <p className="chip c-ok" role="status">
            {done}
          </p>
        ) : null}
        {roads.error ? <ProblemAlert problem={roads.error} onRetry={roads.reload} /> : null}
      </div>
    </div>
  );
}

function CoopImport() {
  const { adminApi } = useAuth();
  const [file, setFile] = useState<UploadFile | null>(null);
  const [format, setFormat] = useState<"geojson" | "kml" | "shapefile" | undefined>();
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [result, setResult] = useState<CoopImportResult | null>(null);
  const submit = async () => {
    if (!file || !format) return;
    setBusy(true);
    setProblem(null);
    setResult(null);
    try {
      setResult(
        await unwrap(
          adminApi.POST("/v1/geo/cooperative-boundaries/import", {
            params: { header: { "Idempotency-Key": newIdempotencyKey() }, query: { format, file_name: file.name, reason: reason.trim() || undefined } },
            ...rawBody(file),
          }),
        ),
      );
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
          Mỗi đối tượng gắn HTX theo <code>cooperative_ref</code> hoặc Mã HTX (<code>cooperative_code</code>/<code>ma_htx</code>). HTX đã có ranh giới → phiên bản mới thay phiên bản hiện
          hành. Chỉ WGS84 (EPSG:4326); đối tượng lỗi bị loại kèm lý do, đối tượng hợp lệ vẫn được nạp.
        </p>
        <SpatialFileInput
          id="coop-imp-file"
          onFile={(f, fmt) => {
            setFile(f);
            setFormat(fmt);
            setResult(null);
          }}
        />
        <ReasonField value={reason} onChange={setReason} label="Lý do thay ranh giới đang có (tuỳ chọn, ≥ 10 ký tự)" />
        <div>
          <button type="button" className="btn btn-p" disabled={!file || !format || busy} onClick={() => void submit()}>
            {busy ? "Đang nhập…" : "Nhập ranh giới HTX"}
          </button>
        </div>
        {problem ? <ProblemAlert problem={problem} /> : null}
      </div>
      {result ? <ImportResult title="Kết quả nhập ranh giới HTX" result={result} extra={(f) => { const x = result.features[f.index]; return x?.version ? `Phiên bản ${x.version}${x.replaced ? " (thay bản cũ)" : ""} · ${formatDecimal(x.area_ha, 2)} ha` : "—"; }} /> : null}
    </>
  );
}
