// GIẢ LẬP pf/waterway (GIS US-WATER-01/02/03, mục 16) — hình dạng theo contracts/openapi (application.yaml, admin.yaml).
// Mạng sông/kênh TỔNG HỢP nối các tâm xã giả lập; định tuyến Dijkstra trên các đoạn ĐI ĐƯỢC của phiên bản đã phát hành.
// Phát hành phải KHÁC người dựng (R-56 → 403 self_approval_forbidden). Trạng thái giữ trong bộ nhớ tab.
import type { WaterwayBuild, WaterwayFinding, WaterwayLayerSegment, WaterwayNetworkVersion, WaterwayRoute, WaterwaySegment } from "../types";
import { uuidv7 } from "../idempotency";
import { haversineM, lineLengthM, parseBBox, touchesBBox, type LngLat } from "@/gis/geometry";
import { COMMUNES, PROVINCES } from "./data";
import { hasRole, iso, json, nowIso, pageSlice, problem, validation, type Ctx, type Route } from "./http";

const EDITORS = ["waterway_editor", "platform_admin"];
const PUBLISHERS = ["waterway_publisher", "platform_admin"];
const READERS = [...EDITORS, ...PUBLISHERS];
/** Hệ số Haversine khi không có đường thủy (tham số #10). */
const HAVERSINE_FACTOR = 1.3;
/** Bán kính tiếp cận (km) từ điểm tới đỉnh mạng gần nhất. */
const ACCESS_KM = 12;

const hex = (n: number, prefix: string) => `01927b3e-${prefix}-7000-8000-${n.toString(16).padStart(12, "0")}`;

interface SegmentState {
  ref: string;
  revisions: WaterwaySegment[];
}
const SEGMENTS: SegmentState[] = [];

const BUILD_PARAMS = {
  tolerances: { snap_m: "25", auto_join_max_m: "150", report_gap_max_m: "1000", on_segment_m: "20" },
  safety: { under_keel_m: "0.3", air_gap_m: "0.5", width_factor: "2" },
  vessels: [
    { code: "SL50", tons: "50", beam_m: "4.5", draft_m: "1.2", air_draft_m: "2.5" },
    { code: "SL100", tons: "100", beam_m: "5.5", draft_m: "1.6", air_draft_m: "3" },
    { code: "SL300", tons: "300", beam_m: "8", draft_m: "2.2", air_draft_m: "4" },
    { code: "SL500", tons: "500", beam_m: "10", draft_m: "2.8", air_draft_m: "5" },
  ],
};

function addSegment(name: string, cls: WaterwaySegment["waterway_class"], coords: LngLat[], width: string | null, depth: string | null, declared: string | null, status: WaterwaySegment["status"] = "confirmed") {
  const ref = hex(SEGMENTS.length + 1, "3a7e");
  const g = coords.map((c) => [+c[0].toFixed(6), +c[1].toFixed(6)] as LngLat);
  SEGMENTS.push({
    ref,
    revisions: [
      {
        segment_ref: ref,
        id: hex(SEGMENTS.length + 1, "3a7f"),
        revision: 1,
        seq: 1,
        name,
        waterway_class: cls,
        status,
        geometry: { type: "LineString", coordinates: g },
        length_m: lineLengthM(g).toFixed(1),
        width_m: width,
        depth_m: depth,
        max_load_tons: declared,
        data_source: "digitised",
        note: null,
        event_time: iso(300),
        recorded_at: iso(300),
        supersedes_id: null,
        correction_reason: null,
        actor_ref: "mock-admin-setter",
      },
    ],
  });
}

(function seed() {
  const P = (code: string) => PROVINCES.find((p) => p.code === code)!.center;
  const C = (code: string) => COMMUNES.find((c) => c.code === code)!.center;
  // Trục sông chính (qua tâm tỉnh) — rộng, sâu: sà lan lớn đi được.
  addSegment("Sông Tiền (minh hoạ) đoạn An Giang – Đồng Tháp", "song", [P("91"), [105.55, 10.38], P("82")], "350", "6", null);
  addSegment("Sông Tiền (minh hoạ) đoạn Đồng Tháp – Vĩnh Long", "song", [P("82"), [106.15, 10.25], P("86")], "320", "5.5", null);
  addSegment("Sông Hậu (minh hoạ) đoạn An Giang – Cần Thơ", "song", [P("91"), [105.5, 10.05], P("92")], "400", "7", null);
  addSegment("Kênh Xà No (minh hoạ) Cần Thơ – Cà Mau", "kenh_chinh", [P("92"), [105.5, 9.5], P("96")], "40", "2.6", null);
  addSegment("Kênh nối Vĩnh Long – Cần Thơ (minh hoạ)", "kenh_chinh", [P("86"), P("92")], "30", "2", "150");
  // Kênh nội đồng / mương tới từng xã (tải trọng nhỏ dần; một số thiếu số liệu).
  for (const p of ["82", "91", "92", "86", "96"]) {
    COMMUNES.filter((c) => c.province_code === p).forEach((c, i) => {
      const cls = i % 3 === 0 ? "kenh_chinh" : i % 3 === 1 ? "noi_dong" : "muong";
      const width = cls === "kenh_chinh" ? "24" : cls === "noi_dong" ? "10" : "4";
      const depth = i === 3 ? null : cls === "kenh_chinh" ? "2.2" : cls === "noi_dong" ? "1.5" : "0.9";
      addSegment(`${cls === "muong" ? "Mương" : "Kênh"} ${c.name.replace(/^Xã /, "")} (minh hoạ)`, cls, [P(p), C(c.code)], width, depth, null);
    });
  }
  addSegment("Kênh mới đào (nháp)", "noi_dong", [C("82102"), C("82103")], "12", "1.8", null, "draft");
})();

/** Tải trọng lưu thông suy ra (tấn) + ràng buộc bất lợi nhất: rộng/sâu so với lớp phương tiện, có biên an toàn. */
function derive(s: WaterwaySegment): { max: string | null; limiting: string; certain: boolean } {
  if (s.max_load_tons && (!s.width_m || !s.depth_m)) return { max: s.max_load_tons, limiting: "declared", certain: true };
  if (!s.width_m) return { max: null, limiting: "unknown:width", certain: false };
  if (!s.depth_m) return { max: null, limiting: "unknown:depth", certain: false };
  const w = Number(s.width_m);
  const d = Number(s.depth_m);
  let byWidth = 0;
  let byDepth = 0;
  for (const v of BUILD_PARAMS.vessels) {
    if (w >= Number(v.beam_m) * 2) byWidth = Math.max(byWidth, Number(v.tons));
    if (d >= Number(v.draft_m) + 0.3) byDepth = Math.max(byDepth, Number(v.tons));
  }
  let max = Math.min(byWidth, byDepth);
  let limiting = byWidth <= byDepth ? "width" : "depth";
  if (s.max_load_tons && Number(s.max_load_tons) < max) {
    max = Number(s.max_load_tons);
    limiting = "declared";
  }
  if (max === Number(BUILD_PARAMS.vessels[BUILD_PARAMS.vessels.length - 1]!.tons) && limiting !== "declared") limiting = "none";
  return { max: String(max), limiting, certain: true };
}

const head = (s: SegmentState) => s.revisions[s.revisions.length - 1]!;

// ---------------------------------------------------------------------------
// Phiên bản mạng
// ---------------------------------------------------------------------------
interface VersionState {
  v: WaterwayNetworkVersion;
  segments: WaterwaySegment[];
  findings: WaterwayFinding[];
}
const VERSIONS: VersionState[] = [];
const BUILDS: (WaterwayBuild & { doneAt: number; actor: string })[] = [];

function makeVersion(no: number, status: WaterwayNetworkVersion["status"], builtBy: string, builtAgo: number, publishedBy: string | null, segs: WaterwaySegment[]): VersionState {
  const id = hex(no, "3e70");
  const findings: WaterwayFinding[] = [
    { id: no * 10 + 1, kind: "t_join", segment_refs: segs.slice(0, 2).map((s) => s.segment_ref), location: { lat: 10.38, lng: 105.55 }, gap_m: null, auto_fixed: true, message: "Nối chữ T tự động tại điểm giao hai tuyến" },
    { id: no * 10 + 2, kind: "gap_joined", segment_refs: segs.slice(5, 7).map((s) => s.segment_ref), location: { lat: 10.5, lng: 105.9 }, gap_m: "86.4", auto_fixed: true, message: "Khe hở 86 m đã được nối tự động (≤ 150 m)" },
    { id: no * 10 + 3, kind: "gap_reported", segment_refs: segs.slice(8, 9).map((s) => s.segment_ref), location: { lat: 9.9, lng: 105.8 }, gap_m: "420", auto_fixed: false, message: "Khe hở 420 m — chỉ báo cáo, cần Admin kiểm tra thực địa" },
    { id: no * 10 + 4, kind: "dangling", segment_refs: segs.slice(12, 13).map((s) => s.segment_ref), location: { lat: 10.2, lng: 105.3 }, gap_m: null, auto_fixed: false, message: "Đầu tuyến treo (không nối tuyến nào)" },
  ];
  const doneMs = Date.now() - builtAgo * 86_400_000;
  return {
    segments: segs,
    findings,
    v: {
      id,
      version_no: no,
      status,
      segment_set_sha256: (no * 7919).toString(16).padStart(64, "a"),
      graph_sha256: (no * 104729).toString(16).padStart(64, "b"),
      rule_version: "ww-rules-1",
      as_of: new Date(doneMs - 60_000).toISOString(),
      segment_count: segs.length,
      structure_count: 0,
      vertex_count: segs.length + 6,
      edge_count: segs.length,
      component_count: 1,
      finding_count: findings.length,
      build_id: hex(no, "3eb0"),
      built_by: builtBy,
      built_at: new Date(doneMs).toISOString(),
      published_by: publishedBy,
      published_at: publishedBy ? new Date(doneMs + 3_600_000).toISOString() : null,
      effective_from: publishedBy ? new Date(doneMs + 3_600_000).toISOString() : null,
      superseded_at: status === "superseded" ? iso(builtAgo - 20) : null,
      params: BUILD_PARAMS,
    },
  };
}

(function seedVersions() {
  const confirmed = SEGMENTS.filter((s) => head(s).status === "confirmed").map(head);
  VERSIONS.push(makeVersion(1, "superseded", "mock-admin-setter", 90, "mock-admin-approver", confirmed.slice(0, confirmed.length - 4)));
  VERSIONS.push(makeVersion(2, "published", "mock-admin-setter", 40, "mock-admin-approver", confirmed));
  // v3 do "Quản trị viên A" dựng, chưa phát hành — người dựng tự phát hành → self_approval_forbidden.
  VERSIONS.push(makeVersion(3, "built", "mock-admin-setter", 1, null, confirmed));
})();

const published = () => VERSIONS.find((x) => x.v.status === "published");

function versionOf(ctx: Ctx): VersionState | Response | null {
  const q = ctx.url.searchParams.get("version");
  if (q === null) return published() ?? null;
  const v = VERSIONS.find((x) => x.v.version_no === Number(q) && x.v.status !== "built");
  return v ?? problem(404, "waterway.network_version_not_found", "Not found");
}

function minLoadOf(ctx: Ctx): string | Response {
  const m = ctx.url.searchParams.get("min_load_tons") ?? "0";
  if (!/^[0-9]+(\.[0-9]+)?$/.test(m)) return validation([{ field: "min_load_tons", reason: "số tấn ≥ 0" }]);
  return m;
}

function layerSegment(s: WaterwaySegment, minLoad: string): WaterwayLayerSegment {
  const d = derive(s);
  return {
    segment_ref: s.segment_ref,
    revision: s.revision,
    name: s.name,
    waterway_class: s.waterway_class,
    data_source: s.data_source,
    length_m: s.length_m,
    width_m: s.width_m,
    depth_m: s.depth_m,
    declared_max_load_tons: s.max_load_tons,
    max_load_tons: d.max,
    certain: d.certain,
    limiting: d.limiting,
    limiting_structure_ref: null,
    passable: d.max !== null && Number(d.max) >= Number(minLoad),
    geometry: s.geometry,
  };
}

// ---------------------------------------------------------------------------
// Định tuyến (Dijkstra trên đoạn đi được)
// ---------------------------------------------------------------------------
const key = (c: LngLat) => `${c[0].toFixed(5)},${c[1].toFixed(5)}`;

function route(segs: WaterwaySegment[], versionNo: number | null, from: LngLat, to: LngLat, minLoad: string): WaterwayRoute {
  const fallback = (reason: string): WaterwayRoute => ({
    version_no: versionNo,
    found: false,
    distance_km: ((haversineM(from, to) * HAVERSINE_FACTOR) / 1000).toFixed(2),
    access_km: "0",
    source: "haversine",
    source_label: `Ước tính đường chim bay × ${String(HAVERSINE_FACTOR).replace(".", ",")} (không có đường thủy)`,
    geometry: null,
    segment_refs: [],
    limiting: null,
    reason,
  });
  if (versionNo === null) return fallback("Chưa phát hành phiên bản mạng đường thủy nào.");
  const usable = segs.filter((s) => {
    const d = derive(s);
    return d.max !== null && Number(d.max) >= Number(minLoad);
  });
  const adj = new Map<string, { to: string; seg: WaterwaySegment; coords: LngLat[] }[]>();
  const pos = new Map<string, LngLat>();
  for (const s of usable) {
    const c = s.geometry.coordinates as LngLat[];
    const a = c[0]!;
    const b = c[c.length - 1]!;
    pos.set(key(a), a);
    pos.set(key(b), b);
    (adj.get(key(a)) ?? adj.set(key(a), []).get(key(a))!).push({ to: key(b), seg: s, coords: c });
    (adj.get(key(b)) ?? adj.set(key(b), []).get(key(b))!).push({ to: key(a), seg: s, coords: [...c].reverse() });
  }
  const nearest = (p: LngLat) => {
    let best: string | null = null;
    let bd = Infinity;
    for (const [k, c] of pos) {
      const d = haversineM(p, c);
      if (d < bd) [best, bd] = [k, d];
    }
    return { k: best, km: bd / 1000 };
  };
  const s = nearest(from);
  const t = nearest(to);
  if (!s.k || !t.k || s.km > ACCESS_KM || t.km > ACCESS_KM) return fallback(`Điểm nằm ngoài bán kính tiếp cận ${ACCESS_KM} km của mạng đi được với tải trọng ${minLoad} tấn.`);
  const dist = new Map<string, number>([[s.k, 0]]);
  const prev = new Map<string, { from: string; seg: WaterwaySegment; coords: LngLat[] }>();
  const todo = new Set<string>(pos.keys());
  while (todo.size) {
    let u: string | null = null;
    for (const k of todo) if (dist.has(k) && (u === null || dist.get(k)! < dist.get(u)!)) u = k;
    if (u === null || u === t.k) break;
    todo.delete(u);
    for (const e of adj.get(u) ?? []) {
      const nd = dist.get(u)! + Number(e.seg.length_m);
      if (nd < (dist.get(e.to) ?? Infinity)) {
        dist.set(e.to, nd);
        prev.set(e.to, { from: u, seg: e.seg, coords: e.coords });
      }
    }
  }
  if (!dist.has(t.k)) return fallback(`Không có đường thủy liên thông đủ tải trọng ${minLoad} tấn giữa hai điểm.`);
  const path: { seg: WaterwaySegment; coords: LngLat[] }[] = [];
  for (let k = t.k; k !== s.k; ) {
    const p = prev.get(k)!;
    path.unshift({ seg: p.seg, coords: p.coords });
    k = p.from;
  }
  const coords: LngLat[] = [from, ...path.flatMap((p, i) => (i === 0 ? p.coords : p.coords.slice(1))), to];
  if (path.length === 0) coords.splice(1, 0, pos.get(s.k)!);
  const lim = path.reduce<{ seg: WaterwaySegment; max: number } | null>((best, p) => {
    const m = Number(derive(p.seg).max);
    return best === null || m < best.max ? { seg: p.seg, max: m } : best;
  }, null);
  const accessKm = s.km + t.km;
  return {
    version_no: versionNo,
    found: true,
    distance_km: ((dist.get(t.k)! / 1000) + accessKm).toFixed(2),
    access_km: accessKm.toFixed(2),
    source: "routed",
    source_label: `Đường thủy theo mạng phiên bản ${versionNo} (Dijkstra)`,
    geometry: { type: "LineString", coordinates: coords },
    segment_refs: path.map((p) => p.seg.segment_ref),
    limiting: lim ? { segment_ref: lim.seg.segment_ref, max_load_tons: String(lim.max), certain: true, reason: `${lim.seg.name}: ${derive(lim.seg).limiting}`, structure_ref: null } : null,
    reason: null,
  };
}

function parseLatLng(s: string | null): LngLat | null {
  const m = s ? /^\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*$/.exec(s) : null;
  return m ? [Number(m[2]), Number(m[1])] : null;
}

// ---------------------------------------------------------------------------
// Ghi đoạn (US-WATER-02/03)
// ---------------------------------------------------------------------------
const measure = (v: unknown): string | null | "bad" => {
  if (v === undefined || v === null || v === "") return null;
  const s = String(v).trim();
  return /^\d+(\.\d+)?$/.test(s) && Number(s) > 0 ? s : "bad";
};

function segmentInput(body: unknown): Response | Omit<WaterwaySegment, "segment_ref" | "id" | "revision" | "seq" | "event_time" | "recorded_at" | "supersedes_id" | "correction_reason" | "actor_ref"> {
  const b = (body ?? {}) as Record<string, unknown>;
  const name = typeof b.name === "string" ? b.name.trim() : "";
  const cls = b.waterway_class as WaterwaySegment["waterway_class"] | undefined;
  if (!name || !cls || !["kenh_chinh", "noi_dong", "muong", "song"].includes(cls)) return problem(422, "validation_failed", "Validation failed", { detail: "Vui lòng nhập đầy đủ Tên tuyến và Phân loại." });
  const width = measure(b.width_m);
  const depth = measure(b.depth_m);
  const load = measure(b.max_load_tons);
  if (width === "bad" || depth === "bad" || load === "bad") return problem(422, "validation_failed", "Validation failed", { detail: "Chiều rộng, độ sâu và tải trọng tối đa phải là số lớn hơn 0." });
  const g = b.geometry as { type?: string; coordinates?: LngLat[] } | undefined;
  if (!g || g.type !== "LineString" || !Array.isArray(g.coordinates) || new Set(g.coordinates.map((c) => `${c[0]},${c[1]}`)).size < 2) return validation([{ field: "geometry", reason: "LineString ≥ 2 đỉnh WGS84" }]);
  const coords = g.coordinates.map((c) => [Number(c[0]), Number(c[1])] as LngLat);
  return {
    name,
    waterway_class: cls,
    status: (b.status as WaterwaySegment["status"] | undefined) ?? "draft",
    geometry: { type: "LineString", coordinates: coords },
    length_m: lineLengthM(coords).toFixed(1),
    width_m: width,
    depth_m: depth,
    max_load_tons: load,
    data_source: typeof b.data_source === "string" ? b.data_source : "digitised",
    note: typeof b.note === "string" ? b.note : null,
  };
}

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------
function buildOut(b: (typeof BUILDS)[number]): WaterwayBuild {
  // Job dựng hoàn tất sau ~2 giây (worker giả lập); tạo phiên bản mới với các đoạn đã xác nhận tại requested_at.
  if (b.status === "queued" && Date.now() >= b.doneAt) {
    const segs = SEGMENTS.filter((s) => head(s).status === "confirmed").map(head);
    const latest = VERSIONS.filter((x) => x.v.status !== "superseded").at(-1);
    const same = latest && latest.segments.length === segs.length && latest.segments.every((s, i) => s.id === segs[i]?.id);
    if (same) {
      b.reused = true;
      b.network_version_no = latest.v.version_no;
    } else {
      const no = Math.max(...VERSIONS.map((x) => x.v.version_no)) + 1;
      const vs = makeVersion(no, "built", b.actor, 0, null, segs);
      vs.v.build_id = b.id;
      VERSIONS.push(vs);
      b.reused = false;
      b.network_version_no = no;
    }
    b.status = "completed";
    b.completed_at = nowIso();
  }
  const { doneAt: _d, actor: _a, ...out } = b;
  void _d;
  void _a;
  return out;
}

export const waterwayAppRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/waterway\/segments$/,
    handler: (ctx) => {
      const v = versionOf(ctx);
      if (v instanceof Response) return v;
      const minLoad = minLoadOf(ctx);
      if (minLoad instanceof Response) return minLoad;
      const hasMin = ctx.url.searchParams.get("min_load_tons") !== null;
      if (!v) return json({ items: [], next_cursor: null, total: 0, passable_count: 0, min_load_tons: minLoad, version_no: null, message: null });
      const bb = parseBBox(ctx.url.searchParams.get("bbox"));
      const all = v.segments.filter((s) => !bb || touchesBBox(s.geometry.coordinates as LngLat[], bb)).map((s) => layerSegment(s, minLoad));
      const passable = all.filter((s) => s.passable).length;
      const { slice, next } = pageSlice(all, ctx.url, 50, 200);
      return json({
        items: slice,
        next_cursor: next,
        total: all.length,
        passable_count: passable,
        min_load_tons: minLoad,
        version_no: v.v.version_no,
        message: hasMin && passable === 0 ? `Không tìm thấy tuyến sông/kênh phù hợp tải trọng sà lan ${minLoad} tấn.` : null,
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/waterway\/routes$/,
    handler: (ctx) => {
      const from = parseLatLng(ctx.url.searchParams.get("from"));
      const to = parseLatLng(ctx.url.searchParams.get("to"));
      if (!from || !to) return validation([{ field: !from ? "from" : "to", reason: "dạng lat,lng" }]);
      const v = versionOf(ctx);
      if (v instanceof Response) return v;
      const minLoad = minLoadOf(ctx);
      if (minLoad instanceof Response) return minLoad;
      return json(route(v?.segments ?? [], v?.v.version_no ?? null, from, to, minLoad));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/waterway\/networks\/current$/,
    handler: () => {
      const v = published();
      if (!v) return problem(404, "waterway.network_version_not_found", "Not found");
      const { id, version_no, segment_set_sha256, graph_sha256, rule_version, effective_from, as_of, segment_count, vertex_count, edge_count } = v.v;
      return json({ id, version_no, status: "published", segment_set_sha256, graph_sha256, rule_version, effective_from, as_of, segment_count, vertex_count, edge_count });
    },
  },
];

export const waterwayAdminRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/waterway\/segments$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...READERS)) return problem(403, "forbidden", "Forbidden");
      const st = ctx.url.searchParams.get("status");
      const items = SEGMENTS.map(head).filter((s) => !st || s.status === st);
      const { slice, next } = pageSlice(items, ctx.url, 50, 200);
      return json({ items: slice, next_cursor: next });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/waterway\/segments$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...EDITORS)) return problem(403, "forbidden", "Forbidden");
      const input = segmentInput(ctx.body);
      if (input instanceof Response) return input;
      const ref = uuidv7();
      const seg: WaterwaySegment = { ...input, segment_ref: ref, id: uuidv7(), revision: 1, seq: 1, event_time: nowIso(), recorded_at: nowIso(), supersedes_id: null, correction_reason: null, actor_ref: ctx.persona.me.actor_ref };
      SEGMENTS.push({ ref, revisions: [seg] });
      return json(seg, 201, { Location: `/v1/waterway/segments/${ref}` });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/waterway\/segments\/([\w-]+)$/,
    handler: (ctx) => {
      const st = SEGMENTS.find((s) => s.ref === ctx.params[0]);
      return st ? json({ head: head(st), history: [...st.revisions].reverse() }) : problem(404, "waterway.segment_not_found", "Not found");
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/waterway\/segments\/([\w-]+)\/revisions$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...EDITORS)) return problem(403, "forbidden", "Forbidden");
      const st = SEGMENTS.find((s) => s.ref === ctx.params[0]);
      if (!st) return problem(404, "waterway.segment_not_found", "Not found");
      const b = (ctx.body ?? {}) as { expected_revision?: number; reason?: string };
      const reason = typeof b.reason === "string" ? b.reason.trim() : "";
      if (reason.length < 5 || reason.length > 500) return validation([{ field: "reason", reason: "Lý do 5–500 ký tự" }]);
      const cur = head(st);
      if (b.expected_revision !== cur.revision) return problem(409, "waterway.revision_conflict", "Revision conflict", { current_revision: cur.revision });
      const input = segmentInput(ctx.body);
      if (input instanceof Response) return input;
      const seg: WaterwaySegment = { ...input, segment_ref: st.ref, id: uuidv7(), revision: cur.revision + 1, seq: cur.seq + 1, event_time: nowIso(), recorded_at: nowIso(), supersedes_id: cur.id, correction_reason: reason, actor_ref: ctx.persona.me.actor_ref };
      st.revisions.push(seg);
      return json(seg, 201);
    },
  },
  { method: "GET", pattern: /^\/v1\/waterway\/structures$/, handler: () => json({ items: [], next_cursor: null }) },
  {
    method: "POST",
    pattern: /^\/v1\/waterway\/networks\/builds$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...EDITORS)) return problem(403, "forbidden", "Forbidden");
      const b = { id: uuidv7(), status: "queued" as const, requested_by: ctx.persona.me.actor_ref, requested_at: nowIso(), network_version_no: null, reused: null, error_code: null, completed_at: null, params: BUILD_PARAMS, doneAt: Date.now() + 2000, actor: ctx.persona.me.actor_ref };
      BUILDS.push(b as (typeof BUILDS)[number]);
      return json(buildOut(b as (typeof BUILDS)[number]), 202, { Location: `/v1/waterway/networks/builds/${b.id}` });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/waterway\/networks\/builds\/([\w-]+)$/,
    handler: (ctx) => {
      const b = BUILDS.find((x) => x.id === ctx.params[0]);
      return b ? json(buildOut(b)) : problem(404, "waterway.build_not_found", "Not found");
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/waterway\/networks$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...READERS)) return problem(403, "forbidden", "Forbidden");
      const items = [...VERSIONS].reverse().map((x) => x.v);
      const { slice, next } = pageSlice(items, ctx.url, 50, 200);
      return json({ items: slice, next_cursor: next });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/waterway\/networks\/(\d+)$/,
    handler: (ctx) => {
      const v = VERSIONS.find((x) => x.v.version_no === Number(ctx.params[0]));
      return v ? json(v.v) : problem(404, "waterway.network_version_not_found", "Not found");
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/waterway\/networks\/(\d+)\/publish$/,
    handler: (ctx) => {
      if (!hasRole(ctx.persona, ...PUBLISHERS)) return problem(403, "forbidden", "Forbidden");
      const v = VERSIONS.find((x) => x.v.version_no === Number(ctx.params[0]));
      if (!v) return problem(404, "waterway.network_version_not_found", "Not found");
      if (v.v.status !== "built") return problem(409, "waterway.version_not_publishable", "Not publishable");
      if (v.v.built_by === ctx.persona.me.actor_ref) return problem(403, "self_approval_forbidden", "Publisher must differ from builder");
      const cur = published();
      if (cur) {
        cur.v.status = "superseded";
        cur.v.superseded_at = nowIso();
      }
      Object.assign(v.v, { status: "published", published_by: ctx.persona.me.actor_ref, published_at: nowIso(), effective_from: nowIso() });
      return json(v.v);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/waterway\/networks\/(\d+)\/findings$/,
    handler: (ctx) => {
      const v = VERSIONS.find((x) => x.v.version_no === Number(ctx.params[0]));
      if (!v) return problem(404, "waterway.network_version_not_found", "Not found");
      const kind = ctx.url.searchParams.get("kind");
      const items = v.findings.filter((f) => !kind || f.kind === kind);
      const { slice, next } = pageSlice(items, ctx.url, 50, 200);
      return json({ items: slice, next_cursor: next });
    },
  },
];
