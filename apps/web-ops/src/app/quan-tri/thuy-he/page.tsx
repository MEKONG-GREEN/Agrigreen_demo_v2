"use client";

import { useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import type { WaterwayBuild, WaterwayFinding, WaterwayLayerSegment, WaterwayNetworkVersion, WaterwayRoute, WaterwaySegment } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import type { AnyFeatureCollection } from "@/components/LayerMap";
import { MfaNotice } from "@/components/MfaNotice";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Loading, PageHead } from "@/components/ui";
import { formatDecimal, formatInt } from "@/lib/decimal";
import { formatDateTime } from "@/lib/format";
import { JOB_STATUS, pollUntil } from "@/lib/poll";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { boundsOf, DrawSection, EditorMap, ReasonField } from "@/gis/editor";
import { draftGeometry, formatLength, parseLngLat, validateDraft, type LngLat } from "@/gis/geometry";
import { limitingLabel, WATERWAY_CLASS, WATERWAY_STATUS } from "@/gis/legend";

const TABS = [
  { id: "versions", label: "Phiên bản mạng" },
  { id: "segments", label: "Tuyến sông/kênh" },
  { id: "route", label: "Thử định tuyến" },
];

const VERSION_STATUS: Record<string, { text: string; cls: string }> = {
  built: { text: "Đã dựng — chờ phát hành", cls: "c-warn" },
  published: { text: "Đang có hiệu lực", cls: "c-ok" },
  superseded: { text: "Đã bị thay", cls: "c-mute" },
};

const FINDING_KIND: Record<string, string> = {
  crossing: "Giao cắt",
  t_join: "Nối chữ T",
  gap_joined: "Khe hở đã nối",
  gap_reported: "Khe hở chỉ báo cáo",
  dangling: "Đầu treo",
  isolated: "Tuyến cô lập",
};

/** Mạng đường thủy có phiên bản (mục 16; GIS US-WATER-01/02/03): dựng → kiểm phát hiện → phát hành (khác người dựng) → định tuyến. */
export default function WaterwayAdminPage() {
  const [tab, setTab] = useState("versions");
  return (
    <>
      <PageHead title="Mạng đường thủy" subtitle="Vẽ/sửa tuyến sông/kênh, dựng phiên bản mạng, kiểm tra phát hiện, phát hành và thử định tuyến theo tải trọng sà lan" />
      <MfaNotice />
      <Tabs tabs={TABS} value={tab} onChange={setTab} label="Chức năng mạng đường thủy">
        {tab === "versions" ? <Versions /> : tab === "segments" ? <Segments /> : <RouteTester />}
      </Tabs>
    </>
  );
}

function Versions() {
  const { adminApi, principal } = useAuth();
  const [version, setVersion] = useState(0);
  const list = useAsync(() => collectPages<WaterwayNetworkVersion>((cursor) => unwrap(adminApi.GET("/v1/waterway/networks", { params: { query: { cursor, limit: 50 } } })), 200), [adminApi, version]);
  const [selNo, setSelNo] = useState<number | null>(null);
  const [build, setBuild] = useState<WaterwayBuild | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const sel = list.data?.find((v) => v.version_no === selNo) ?? null;

  const requestBuild = async () => {
    setBusy(true);
    setProblem(null);
    setDone(null);
    try {
      const b = await unwrap(adminApi.POST("/v1/waterway/networks/builds", { params: { header: { "Idempotency-Key": newIdempotencyKey() } } }));
      setBuild(b);
      const last = await pollUntil(
        () => unwrap(adminApi.GET("/v1/waterway/networks/builds/{id}", { params: { path: { id: b.id } } })),
        (x) => x.status !== "queued",
        { intervalMs: 800, onUpdate: setBuild },
      );
      if (last.status === "completed") {
        setDone(last.reused ? `Tập tuyến không đổi — dùng lại phiên bản ${last.network_version_no}.` : `Đã dựng phiên bản ${last.network_version_no}. Kiểm tra phát hiện rồi nhờ người khác phát hành.`);
        setSelNo(last.network_version_no);
      }
      setVersion((v) => v + 1);
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };

  const publish = async (no: number) => {
    setBusy(true);
    setProblem(null);
    setDone(null);
    try {
      await unwrap(adminApi.POST("/v1/waterway/networks/{version}/publish", { params: { path: { version: no }, header: { "Idempotency-Key": newIdempotencyKey() } } }));
      setDone(`Đã phát hành phiên bản ${no} — định tuyến và lớp bản đồ dùng phiên bản này.`);
      setVersion((v) => v + 1);
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="grid2" style={{ gridTemplateColumns: "minmax(0, 1.3fr) minmax(320px, 1fr)", alignItems: "start" }}>
      <section className="card sec stack" aria-label="Phiên bản mạng">
        <div className="row">
          <button type="button" className="btn btn-p btn-sm" disabled={busy} onClick={() => void requestBuild()}>
            Yêu cầu dựng phiên bản mới
          </button>
          {build ? (
            <span className="small" role="status" data-testid="build-status">
              Lượt dựng: <b>{JOB_STATUS[build.status] ?? build.status}</b>
              {build.error_code ? ` · ${build.error_code}` : ""}
            </span>
          ) : null}
        </div>
        {problem ? <ProblemAlert problem={problem} /> : null}
        {done ? (
          <p className="chip c-ok" role="status">
            {done}
          </p>
        ) : null}
        {list.loading && !list.data ? <Loading /> : null}
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">Phiên bản mạng đường thủy</caption>
            <thead>
              <tr>
                <th scope="col">Phiên bản</th>
                <th scope="col">Trạng thái</th>
                <th scope="col" className="num">Tuyến</th>
                <th scope="col" className="num">Phát hiện</th>
                <th scope="col">Dựng lúc</th>
                <th scope="col">Thao tác</th>
              </tr>
            </thead>
            <tbody>
              {(list.data ?? []).map((v) => (
                <tr key={v.id} className={v.version_no === selNo ? "hl" : undefined} data-version={v.version_no}>
                  <td>
                    <button type="button" className="btn btn-o btn-sm" onClick={() => setSelNo(v.version_no)}>
                      Phiên bản {v.version_no}
                    </button>
                  </td>
                  <td>
                    <span className={`chip ${VERSION_STATUS[v.status]?.cls ?? ""}`}>{VERSION_STATUS[v.status]?.text ?? v.status}</span>
                  </td>
                  <td className="num">{formatInt(v.segment_count)}</td>
                  <td className="num">{formatInt(v.finding_count)}</td>
                  <td className="small">
                    {formatDateTime(v.built_at)}
                    {v.built_by === principal?.subject ? <div className="mute">do bạn dựng</div> : null}
                  </td>
                  <td>
                    {v.status === "built" ? (
                      <button type="button" className="btn btn-p btn-sm" disabled={busy} onClick={() => void publish(v.version_no)}>
                        Phát hành phiên bản {v.version_no}
                      </button>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="small mute">Người phát hành phải khác người đã yêu cầu dựng phiên bản (R-56). Phiên bản đang có hiệu lực chuyển sang “Đã bị thay” nhưng vẫn định tuyến được khi ghim.</p>
      </section>
      {sel ? <VersionDetail key={sel.version_no} v={sel} /> : <p className="card sec small mute">Chọn một phiên bản để xem tham số dựng và phát hiện.</p>}
    </div>
  );
}

function VersionDetail({ v }: { v: WaterwayNetworkVersion }) {
  const { adminApi } = useAuth();
  const [kind, setKind] = useState("");
  const findings = useAsync(
    () => collectPages<WaterwayFinding>((cursor) => unwrap(adminApi.GET("/v1/waterway/networks/{version}/findings", { params: { path: { version: v.version_no }, query: { cursor, limit: 200, kind: (kind || undefined) as WaterwayFinding["kind"] | undefined } } })), 1000),
    [adminApi, v.version_no, kind],
  );
  return (
    <section className="card sec stack" aria-labelledby="ver-h" data-testid="version-detail">
      <h2 id="ver-h" style={{ fontSize: 15 }}>
        Phiên bản {v.version_no}
      </h2>
      <dl className="kv" style={{ gridTemplateColumns: "150px 1fr" }}>
        <dt>Trạng thái</dt>
        <dd>{VERSION_STATUS[v.status]?.text ?? v.status}</dd>
        <dt>Đồ thị</dt>
        <dd>
          {formatInt(v.vertex_count)} đỉnh · {formatInt(v.edge_count)} cạnh · {formatInt(v.component_count)} thành phần liên thông
        </dd>
        <dt>Mốc dữ liệu</dt>
        <dd>{formatDateTime(v.as_of)}</dd>
        <dt>Phát hành</dt>
        <dd>{v.published_at ? formatDateTime(v.published_at) : "Chưa phát hành"}</dd>
        <dt>Dung sai dựng</dt>
        <dd className="small">
          bắt điểm {v.params.tolerances?.snap_m ?? "—"} m · nối tự động ≤ {v.params.tolerances?.auto_join_max_m ?? "—"} m · báo cáo ≤ {v.params.tolerances?.report_gap_max_m ?? "—"} m
        </dd>
      </dl>
      <div className="field">
        <label htmlFor="find-kind">Lọc phát hiện</label>
        <select id="find-kind" className="select" value={kind} onChange={(e) => setKind(e.target.value)}>
          <option value="">Tất cả</option>
          {Object.entries(FINDING_KIND).map(([k, t]) => (
            <option key={k} value={k}>
              {t}
            </option>
          ))}
        </select>
      </div>
      {findings.error ? <ProblemAlert problem={findings.error} /> : null}
      <ul className="list-plain" aria-label="Phát hiện khi dựng">
        {(findings.data ?? []).map((f) => (
          <li key={f.id} className="small">
            <span className={`chip ${f.auto_fixed ? "c-ok" : "c-warn"}`}>{FINDING_KIND[f.kind] ?? f.kind}</span> {f.message}
            <div className="mute">
              {f.location.lat.toFixed(4)}, {f.location.lng.toFixed(4)}
              {f.gap_m ? ` · khe ${formatDecimal(f.gap_m, 0)} m` : ""} · {f.auto_fixed ? "đã tự sửa" : "cần kiểm tra"}
            </div>
          </li>
        ))}
      </ul>
      {findings.data && findings.data.length === 0 ? <p className="small mute">Không có phát hiện.</p> : null}
    </section>
  );
}

function Segments() {
  const { adminApi } = useAuth();
  const [version, setVersion] = useState(0);
  const list = useAsync(() => collectPages<WaterwaySegment>((cursor) => unwrap(adminApi.GET("/v1/waterway/segments", { params: { query: { cursor, limit: 200 } } })), 1000), [adminApi, version]);
  const [sel, setSel] = useState<WaterwaySegment | null>(null);
  const [drawing, setDrawing] = useState(false);
  const [coords, setCoords] = useState<LngLat[]>([]);
  const [form, setForm] = useState({ name: "", waterway_class: "", width_m: "", depth_m: "", max_load_tons: "", status: "draft" });
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const context = useMemo<AnyFeatureCollection | null>(
    () =>
      list.data
        ? {
            type: "FeatureCollection",
            features: list.data
              .filter((s) => !(drawing && sel?.segment_ref === s.segment_ref))
              .map((s) => ({ type: "Feature", geometry: s.geometry, properties: { segment_ref: s.segment_ref, stroke: sel?.segment_ref === s.segment_ref ? "#e67e22" : s.status === "draft" ? "#9aa5a0" : "#3f8fd2", width: 3 } })),
          }
        : null,
    [list.data, sel, drawing],
  );
  const pick = (s: WaterwaySegment | null) => {
    setSel(s);
    setDrawing(false);
    setProblem(null);
    setDone(null);
    if (s) setForm({ name: s.name, waterway_class: s.waterway_class, width_m: s.width_m ?? "", depth_m: s.depth_m ?? "", max_load_tons: s.max_load_tons ?? "", status: s.status });
  };
  const invalid = drawing ? validateDraft("line", coords) : null;
  const save = async () => {
    setBusy(true);
    setProblem(null);
    try {
      const body = {
        name: form.name,
        waterway_class: form.waterway_class as WaterwaySegment["waterway_class"],
        geometry: draftGeometry("line", drawing ? coords : (sel?.geometry.coordinates as LngLat[])) as WaterwaySegment["geometry"],
        width_m: form.width_m.trim() || null,
        depth_m: form.depth_m.trim() || null,
        max_load_tons: form.max_load_tons.trim() || null,
        status: form.status as WaterwaySegment["status"],
        data_source: (sel?.data_source ?? "digitised") as "digitised" | "official" | "survey" | "osm",
      };
      if (!sel) await unwrap(adminApi.POST("/v1/waterway/segments", { params: { header: { "Idempotency-Key": newIdempotencyKey() } }, body }));
      else
        await unwrap(
          adminApi.POST("/v1/waterway/segments/{ref}/revisions", {
            params: { path: { ref: sel.segment_ref }, header: { "Idempotency-Key": newIdempotencyKey() } },
            body: { ...body, expected_revision: sel.revision, reason: reason.trim() },
          }),
        );
      setDone(sel ? "Đã lưu bản sửa — thay đổi vào lớp dùng chung sau khi dựng và phát hành phiên bản mạng mới." : "Đã thêm tuyến (nháp — xác nhận để đưa vào lần dựng sau).");
      setSel(null);
      setDrawing(false);
      setReason("");
      setVersion((v) => v + 1);
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };
  const editing = drawing || !!sel;
  return (
    <div className="grid2" style={{ gridTemplateColumns: "minmax(0, 1.6fr) minmax(300px, 1fr)", alignItems: "start" }}>
      <div className="card" style={{ padding: 8 }}>
        <EditorMap
          label="Bản đồ biên tập đường thủy"
          context={context}
          drawing={drawing}
          mode="line"
          coords={coords}
          onCoords={setCoords}
          onPick={(p) => pick(list.data?.find((s) => s.segment_ref === p.segment_ref) ?? null)}
          focus={!drawing && sel ? { key: sel.segment_ref, bounds: boundsOf(sel.geometry.coordinates as LngLat[]) } : null}
        />
      </div>
      <div className="card sec stack">
        <div className="row">
          <button
            type="button"
            className="btn btn-p btn-sm"
            onClick={() => {
              pick(null);
              setForm({ name: "", waterway_class: "", width_m: "", depth_m: "", max_load_tons: "", status: "draft" });
              setCoords([]);
              setDrawing(true);
            }}
          >
            Vẽ tuyến sông/kênh mới
          </button>
        </div>
        <div className="field">
          <label htmlFor="seg-pick">Tuyến đang có</label>
          <select id="seg-pick" className="select" value={sel?.segment_ref ?? ""} onChange={(e) => pick(list.data?.find((s) => s.segment_ref === e.target.value) ?? null)}>
            <option value="">—</option>
            {(list.data ?? []).map((s) => (
              <option key={s.segment_ref} value={s.segment_ref}>
                {s.name} · {WATERWAY_STATUS[s.status]} · {formatLength(s.length_m)}
              </option>
            ))}
          </select>
        </div>
        {editing ? (
          <>
            <div className="field">
              <label htmlFor="seg-name">Tên tuyến</label>
              <input id="seg-name" className="input" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
            </div>
            <div className="row">
              <div className="field" style={{ flex: 1 }}>
                <label htmlFor="seg-class">Phân loại</label>
                <select id="seg-class" className="select" value={form.waterway_class} onChange={(e) => setForm({ ...form, waterway_class: e.target.value })}>
                  <option value="">—</option>
                  {Object.entries(WATERWAY_CLASS).map(([k, v]) => (
                    <option key={k} value={k}>
                      {v}
                    </option>
                  ))}
                </select>
              </div>
              <div className="field" style={{ flex: 1 }}>
                <label htmlFor="seg-status">Trạng thái</label>
                <select id="seg-status" className="select" value={form.status} onChange={(e) => setForm({ ...form, status: e.target.value })}>
                  {Object.entries(WATERWAY_STATUS).map(([k, v]) => (
                    <option key={k} value={k}>
                      {v}
                    </option>
                  ))}
                </select>
              </div>
            </div>
            <div className="row">
              <div className="field" style={{ flex: 1 }}>
                <label htmlFor="seg-width">Chiều rộng lòng kênh (m)</label>
                <input id="seg-width" className="input" inputMode="decimal" value={form.width_m} onChange={(e) => setForm({ ...form, width_m: e.target.value })} />
              </div>
              <div className="field" style={{ flex: 1 }}>
                <label htmlFor="seg-depth">Độ sâu (m)</label>
                <input id="seg-depth" className="input" inputMode="decimal" value={form.depth_m} onChange={(e) => setForm({ ...form, depth_m: e.target.value })} />
              </div>
              <div className="field" style={{ flex: 1 }}>
                <label htmlFor="seg-load">Tải trọng tối đa (tấn)</label>
                <input id="seg-load" className="input" inputMode="decimal" value={form.max_load_tons} onChange={(e) => setForm({ ...form, max_load_tons: e.target.value })} />
              </div>
            </div>
            {sel && !drawing ? (
              <>
                <p className="small">
                  Chiều dài: <b>{formatLength(sel.length_m)}</b> (hệ thống tính) · bản sửa {sel.revision}
                </p>
                <button
                  type="button"
                  className="btn btn-o btn-sm"
                  onClick={() => {
                    setCoords(sel.geometry.coordinates as LngLat[]);
                    setDrawing(true);
                  }}
                >
                  Sửa hình tuyến (kéo/thêm/xoá điểm)
                </button>
              </>
            ) : null}
            {drawing ? <DrawSection mode="line" coords={coords} onCoords={setCoords} /> : null}
            {sel ? <ReasonField value={reason} onChange={setReason} label="Lý do sửa (5–500 ký tự)" min={5} /> : null}
            <div className="row">
              <button type="button" className="btn btn-p btn-sm" disabled={busy || !!invalid || (!!sel && reason.trim().length < 5)} onClick={() => void save()}>
                {busy ? "Đang lưu…" : sel ? "Lưu bản sửa" : "Lưu tuyến"}
              </button>
              <button type="button" className="btn btn-o btn-sm" onClick={() => pick(null)}>
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
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
      </div>
    </div>
  );
}

function RouteTester() {
  const { api, adminApi } = useAuth();
  const versions = useAsync(() => collectPages<WaterwayNetworkVersion>((cursor) => unwrap(adminApi.GET("/v1/waterway/networks", { params: { query: { cursor, limit: 50 } } })), 200), [adminApi]);
  const [ver, setVer] = useState("");
  const segs = useAsync(
    () => collectPages<WaterwayLayerSegment>((cursor) => unwrap(api.GET("/v1/waterway/segments", { params: { query: { cursor, limit: 200, version: ver ? Number(ver) : undefined } } })), 1000),
    [api, ver],
  );
  const [from, setFrom] = useState<LngLat | null>(null);
  const [to, setTo] = useState<LngLat | null>(null);
  const [fromText, setFromText] = useState("");
  const [toText, setToText] = useState("");
  const [load, setLoad] = useState("100");
  const [route, setRoute] = useState<WaterwayRoute | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const context = useMemo<AnyFeatureCollection>(() => {
    const features: AnyFeatureCollection["features"] = (segs.data ?? []).map((s) => ({ type: "Feature", geometry: s.geometry, properties: { stroke: "#9cc3e6", width: 2 } }));
    if (route?.geometry) features.push({ type: "Feature", geometry: route.geometry, properties: { stroke: "#b3261e", width: 5 } });
    if (from) features.push({ type: "Feature", geometry: { type: "Point", coordinates: from }, properties: { fill: "#1b8a54" } });
    if (to) features.push({ type: "Feature", geometry: { type: "Point", coordinates: to }, properties: { fill: "#b3261e" } });
    return { type: "FeatureCollection", features };
  }, [segs.data, route, from, to]);

  const setPoint = (which: "from" | "to", text: string) => {
    const p = parseLngLat(text);
    if (which === "from") setFrom(p);
    else setTo(p);
  };
  const run = async () => {
    if (!from || !to) return;
    setBusy(true);
    setProblem(null);
    try {
      setRoute(
        await unwrap(
          api.GET("/v1/waterway/routes", {
            params: { query: { from: `${from[1]},${from[0]}`, to: `${to[1]},${to[0]}`, min_load_tons: load.trim() || undefined, version: ver ? Number(ver) : undefined } },
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
    <div className="grid2" style={{ gridTemplateColumns: "minmax(0, 1.6fr) minmax(300px, 1fr)", alignItems: "start" }}>
      <div className="card" style={{ padding: 8 }}>
        <p className="small mute" style={{ padding: 4 }}>
          Bấm bản đồ: lần 1 đặt điểm đi, lần 2 đặt điểm đến (bấm tiếp để đặt lại).
        </p>
        <EditorMap
          label="Bản đồ thử định tuyến đường thủy"
          context={context}
          drawing={false}
          mode="point"
          coords={[]}
          onCoords={() => undefined}
          onAnyClick={(p) => {
            if (!from || (from && to)) {
              setFrom(p);
              setTo(null);
              setFromText(`${p[0].toFixed(5)}, ${p[1].toFixed(5)}`);
              setToText("");
            } else {
              setTo(p);
              setToText(`${p[0].toFixed(5)}, ${p[1].toFixed(5)}`);
            }
          }}
          focus={route?.geometry ? { key: `r-${route.distance_km}`, bounds: boundsOf(route.geometry.coordinates as LngLat[]) } : null}
        />
      </div>
      <div className="card sec stack">
        <div className="field">
          <label htmlFor="rt-from">Điểm đi (kinh độ, vĩ độ)</label>
          <input id="rt-from" className="input" value={fromText} onChange={(e) => { setFromText(e.target.value); setPoint("from", e.target.value); }} placeholder="105.2, 10.25" />
        </div>
        <div className="field">
          <label htmlFor="rt-to">Điểm đến (kinh độ, vĩ độ)</label>
          <input id="rt-to" className="input" value={toText} onChange={(e) => { setToText(e.target.value); setPoint("to", e.target.value); }} placeholder="105.85, 9.85" />
        </div>
        <div className="row">
          <div className="field" style={{ flex: 1 }}>
            <label htmlFor="rt-load">Tải trọng sà lan (tấn)</label>
            <input id="rt-load" className="input" inputMode="decimal" value={load} onChange={(e) => setLoad(e.target.value)} />
          </div>
          <div className="field" style={{ flex: 1 }}>
            <label htmlFor="rt-ver">Phiên bản mạng</label>
            <select id="rt-ver" className="select" value={ver} onChange={(e) => setVer(e.target.value)}>
              <option value="">Đang có hiệu lực</option>
              {(versions.data ?? [])
                .filter((v) => v.status !== "built")
                .map((v) => (
                  <option key={v.version_no} value={v.version_no}>
                    Phiên bản {v.version_no} ({VERSION_STATUS[v.status]?.text})
                  </option>
                ))}
            </select>
          </div>
        </div>
        <button type="button" className="btn btn-p btn-sm" disabled={!from || !to || busy} onClick={() => void run()}>
          {busy ? "Đang tính…" : "Tìm đường thủy"}
        </button>
        {problem ? <ProblemAlert problem={problem} /> : null}
        {route ? (
          <section aria-label="Kết quả định tuyến" data-testid="route-result">
            <p>
              <span className={`chip ${route.found ? "c-ok" : "c-warn"}`}>{route.found ? "Có đường thủy" : "Không có đường thủy"}</span>
            </p>
            <dl className="kv" style={{ gridTemplateColumns: "150px 1fr", marginTop: 8 }}>
              <dt>Quãng đường</dt>
              <dd>{formatDecimal(route.distance_km, 2)} km</dd>
              <dt>Chặng tiếp cận</dt>
              <dd>{formatDecimal(route.access_km, 2)} km</dd>
              <dt>Nguồn tính</dt>
              <dd>{route.source_label}</dd>
              <dt>Phiên bản mạng</dt>
              <dd>{route.version_no ?? "—"}</dd>
              <dt>Ràng buộc bất lợi nhất</dt>
              <dd>
                {route.limiting
                  ? `${route.limiting.max_load_tons ? `${formatDecimal(route.limiting.max_load_tons, 0)} tấn` : "chưa kết luận"} — ${route.limiting.reason.includes(":") ? `${route.limiting.reason.split(":")[0]}: ${limitingLabel(route.limiting.reason.split(":").slice(1).join(":").trim())}` : route.limiting.reason}`
                  : "—"}
              </dd>
              {route.reason ? (
                <>
                  <dt>Lý do</dt>
                  <dd>{route.reason}</dd>
                </>
              ) : null}
            </dl>
          </section>
        ) : null}
      </div>
    </div>
  );
}
