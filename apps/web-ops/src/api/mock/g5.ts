// BACKEND GIẢ LẬP — G5: pf/apiclient (khách hàng Public API, khóa, đăng ký sản phẩm, gói hạn mức — ADR-0022)
// và pf/metering (sổ sử dụng, bảng kê 202 + job, điểm kiểm soát xuất DLP — ADR-0023/0024) trên Admin API.
// Hình dạng đúng contracts/openapi/admin.yaml. Quy tắc mô phỏng app/ của backend:
// - R-56: duyệt ≠ đề xuất → 403 apiclient.self_approval / metering.self_approval;
// - khóa: bí mật sinh MỘT lần ở phản hồi 201 (Cache-Control: no-store), mock CHỈ giữ tiền tố — không bí mật,
//   không băm; tối đa 2 khóa hiệu lực; xoay = khóa mới + khóa cũ chồng lấn 7 ngày;
// - tiền là chuỗi thập phân (numeric(18,4)/(18,2)), tính bằng BigInt (src/metering/money.ts).
// Trạng thái ghi được (khách hàng, khóa, đăng ký, gói, bảng kê, job, sổ xuất phát sinh) lưu ở sessionStorage của
// tab để tải lại trang vẫn còn — chỉ tiền tố khóa, không bao giờ có bí mật.
import type {
  ApiClientRecord,
  ApiKeyRecord,
  ApiQuotaPlan,
  ApiSubscription,
  EgressAnomaly,
  EgressChannel,
  EgressEvent,
  MeteringStatement,
  MeteringStatementJob,
  MeteringStatementLine,
  MeteringUsageDay,
  MeteringUsageEvent,
} from "../types";
import { uuidv7 } from "../idempotency";
import { lineAmount, sumMoney } from "@/metering/money";
import { addDays, vnToday } from "@/lib/vnDate";
import { hasRole, json, problem, reasonOf, validation, type Ctx, type Route } from "./http";
import { mockUuid } from "./metrics";
import { G5_ACTORS, TENANT, type MockPersona } from "./personas";

const STORE_KEY = "ag.mock.g5.v1";
const DAY = 86_400_000;

const ADMIN = "api_client_admin";
const APPROVER = "api_client_approver";
const VIEWER = "metering_viewer";
const OFFICER = "billing_officer";
const BAPPROVER = "billing_approver";

export const PRODUCTS = ["collection_volume", "hub_capacity", "mrv_package_status"] as const;
const PRODUCT_SCOPES: Record<string, readonly string[]> = {
  collection_volume: ["dp:collection_volume:read", "dp:collection_volume:export", "usage:read"],
  hub_capacity: ["dp:hub_capacity:read", "usage:read"],
  mrv_package_status: ["dp:mrv_package_status:read", "usage:read"],
};
export const CHANNELS: readonly EgressChannel[] = ["public_api", "mrv_package", "report_export", "valve", "partner_share"];
const DEFAULTS = { requests_per_minute: 60, daily_units: 10_000, monthly_units: 200_000, max_export_rows: 100_000 };
const MKG_TENANT = "01927b3e-0000-7000-8000-00000000b002";

// Khách hàng có sẵn (tổ chức GIẢ LẬP — không phải đơn vị thật).
export const CLIENT_IDS = {
  factory: "0192a5c0-0000-7000-8000-0000000000c1",
  verifier: "0192a5c0-0000-7000-8000-0000000000c2",
  buyer: "0192a5c0-0000-7000-8000-0000000000c3",
  logistics: "0192a5c0-0000-7000-8000-0000000000c4",
} as const;
const CLIENT_TENANTS: Record<string, string> = {
  [CLIENT_IDS.factory]: "0192a5b0-0000-7000-8000-0000000000b1",
  [CLIENT_IDS.verifier]: "0192a5b0-0000-7000-8000-0000000000b2",
  [CLIENT_IDS.buyer]: "0192a5b0-0000-7000-8000-0000000000b3",
  [CLIENT_IDS.logistics]: "0192a5b0-0000-7000-8000-0000000000b4",
};

interface KeyRow extends ApiKeyRecord {
  client_id: string;
}
interface SubRow extends ApiSubscription {
  client_id: string;
}
interface PlanRow extends ApiQuotaPlan {
  client_id: string;
}
interface JobRow extends MeteringStatementJob {
  polls: number;
}
interface G5State {
  clients: ApiClientRecord[];
  keys: KeyRow[];
  subs: SubRow[];
  plans: PlanRow[];
  statements: MeteringStatement[];
  jobs: JobRow[];
  /** Dòng sổ xuất phát sinh khi chạy mock (tải CSV bảng kê → kênh report_export). */
  egress: EgressEvent[];
}

// ---------------------------------------------------------------------------
// Tiện ích tất định
// ---------------------------------------------------------------------------
function hashInt(s: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619) >>> 0;
  return h;
}
/** Chuỗi hex 64 ký tự tất định (thay SHA-256 trong mock). */
export function hex64(s: string): string {
  let out = "";
  for (let i = 0; out.length < 64; i++) out += hashInt(`${i}|${s}`).toString(16).padStart(8, "0");
  return out.slice(0, 64);
}
const BASE32 = "abcdefghijklmnopqrstuvwxyz234567";
function prefixFrom(seed: string): string {
  let out = "";
  for (let i = 0; i < 12; i++) out += BASE32[hashInt(`${seed}|${i}`) % 32];
  return out;
}
function randomPrefix(): string {
  const b = new Uint8Array(12);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => BASE32[x % 32]).join("");
}
/** Bí mật mới (32 byte base64url, 43 ký tự) — trả về cho nơi gọi, KHÔNG lưu. */
function randomSecret(): string {
  const b = new Uint8Array(32);
  crypto.getRandomValues(b);
  return btoa(String.fromCharCode(...b))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}
const isoAt = (ms: number) => new Date(ms).toISOString();
const ago = (days: number) => isoAt(Date.now() - days * DAY);
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Kỳ "YYYY-MM" lùi n tháng so với tháng hiện tại (giờ VN). */
export function periodBack(n: number, today: string = vnToday()): string {
  const [y, m] = today.split("-").map((x) => Number.parseInt(x, 10)) as [number, number];
  const idx = y * 12 + (m - 1) - n;
  return `${Math.floor(idx / 12)}-${String((idx % 12) + 1).padStart(2, "0")}`;
}
function periodEndMs(period: string): number {
  const [y, m] = period.split("-").map((x) => Number.parseInt(x, 10)) as [number, number];
  return Date.UTC(m === 12 ? y + 1 : y, m === 12 ? 0 : m, 1) - 7 * 3_600_000;
}

// ---------------------------------------------------------------------------
// Dữ liệu gốc
// ---------------------------------------------------------------------------
function seed(): G5State {
  const client = (c: Partial<ApiClientRecord> & Pick<ApiClientRecord, "id" | "display_name" | "purpose" | "status">): ApiClientRecord => ({
    client_tenant_id: CLIENT_TENANTS[c.id] ?? uuidv7(),
    contact_ref: mockUuid(`contact|${c.id}`),
    auth_methods: ["api_key"],
    oauth_client_id: null,
    status_reason: null,
    proposed_by: G5_ACTORS.admin,
    proposed_at: ago(120),
    approved_by: c.status === "proposed" ? null : G5_ACTORS.approver,
    approved_at: c.status === "proposed" ? null : ago(118),
    ...c,
  });
  const clients: ApiClientRecord[] = [
    client({
      id: CLIENT_IDS.factory,
      display_name: "Nhà máy điện sinh khối Hậu Giang (giả lập)",
      purpose: "Lập kế hoạch thu mua rơm theo xã × vụ × tháng cho lò hơi sinh khối (proto SUB-DATA).",
      status: "active",
    }),
    client({
      id: CLIENT_IDS.verifier,
      display_name: "Đơn vị thẩm định độc lập A (giả lập)",
      purpose: "Theo dõi trạng thái gói bằng chứng MRV phục vụ thẩm định (KN US-MRV-05, chỉ xem).",
      status: "active",
      auth_methods: ["api_key", "oauth_client_credentials"],
      oauth_client_id: "vvb-a.mrv-reader",
      proposed_at: ago(90),
      approved_at: ago(88),
    }),
    client({
      id: CLIENT_IDS.buyer,
      display_name: "Quỹ mua tín chỉ carbon B (giả lập)",
      purpose: "Tra cứu trạng thái gói MRV trước khi đàm phán mua tín chỉ (chờ C-1).",
      status: "proposed",
      proposed_by: G5_ACTORS.other,
      proposed_at: ago(3),
    }),
    client({
      id: CLIENT_IDS.logistics,
      display_name: "Công ty vận tải sinh khối C (giả lập)",
      purpose: "Xem bậc sức chứa Hub để điều phối xe gom rơm theo tuần.",
      status: "suspended",
      status_reason: "Tạm ngưng chờ ký lại phụ lục hợp đồng dữ liệu (Q5-8).",
      proposed_at: ago(200),
      approved_at: ago(198),
    }),
  ];
  const key = (clientId: string, n: number, k: Partial<KeyRow>): KeyRow => ({
    id: mockUuid(`key|${clientId}|${n}`),
    client_id: clientId,
    key_prefix: prefixFrom(`${clientId}|${n}`),
    status: "active",
    issued_at: ago(100),
    expires_at: isoAt(Date.now() + 265 * DAY),
    grace_until: null,
    revoked_at: null,
    revoke_reason: null,
    ...k,
  });
  const keys: KeyRow[] = [
    key(CLIENT_IDS.factory, 1, {}),
    key(CLIENT_IDS.verifier, 1, { issued_at: ago(80), expires_at: isoAt(Date.now() + 285 * DAY) }),
    key(CLIENT_IDS.logistics, 1, { status: "revoked", revoked_at: ago(20), revoke_reason: "Nghi lộ khóa trên máy trạm của khách hàng.", issued_at: ago(190) }),
    key(CLIENT_IDS.logistics, 2, { issued_at: ago(20), expires_at: isoAt(Date.now() + 345 * DAY) }),
  ];
  const sub = (clientId: string, product: string, s: Partial<SubRow>): SubRow => ({
    id: mockUuid(`sub|${clientId}|${product}`),
    client_id: clientId,
    product,
    scopes: [`dp:${product}:read`, "usage:read"],
    status: "active",
    proposed_by: G5_ACTORS.admin,
    proposed_at: ago(115),
    approved_by: G5_ACTORS.approver,
    approved_at: ago(114),
    revoked_at: null,
    revoke_reason: null,
    ...s,
  });
  const subs: SubRow[] = [
    sub(CLIENT_IDS.factory, "collection_volume", { scopes: ["dp:collection_volume:read", "dp:collection_volume:export", "usage:read"] }),
    sub(CLIENT_IDS.factory, "hub_capacity", { status: "proposed", proposed_by: G5_ACTORS.other, proposed_at: ago(2), approved_by: null, approved_at: null }),
    sub(CLIENT_IDS.verifier, "mrv_package_status", { proposed_at: ago(87), approved_at: ago(86) }),
    sub(CLIENT_IDS.logistics, "hub_capacity", {}),
  ];
  const plan = (clientId: string, version: number, p: Partial<PlanRow>): PlanRow => ({
    client_id: clientId,
    version,
    ...DEFAULTS,
    effective_from: ago(114),
    status: "approved",
    proposed_by: G5_ACTORS.admin,
    proposed_at: ago(115),
    approved_by: G5_ACTORS.approver,
    approved_at: ago(114),
    ...p,
  });
  const plans: PlanRow[] = [
    plan(CLIENT_IDS.factory, 1, {}),
    plan(CLIENT_IDS.factory, 2, { requests_per_minute: 120, daily_units: 20_000, monthly_units: 400_000, effective_from: ago(40), proposed_at: ago(42), approved_at: ago(41) }),
    plan(CLIENT_IDS.verifier, 1, { requests_per_minute: 30, daily_units: 2_000, monthly_units: 40_000, max_export_rows: 10_000, effective_from: ago(86) }),
    plan(CLIENT_IDS.logistics, 1, {}),
  ];
  const st: G5State = { clients, keys, subs, plans, statements: [], jobs: [], egress: [] };
  // Bảng kê có sẵn: M-2 đã duyệt (nhà máy: có giá thử nghiệm; thẩm định: chỉ đơn vị), M-1 của nhà máy đang nháp do A yêu cầu.
  const m2 = periodBack(2);
  const m1 = periodBack(1);
  const priced = { collection_volume: "1500.5000" } as Record<string, string>;
  for (const [cid, period, by, approved, prices] of [
    [CLIENT_IDS.factory, m2, G5_ACTORS.admin, true, priced],
    [CLIENT_IDS.verifier, m2, G5_ACTORS.admin, true, null],
    [CLIENT_IDS.factory, m1, G5_ACTORS.admin, false, null],
  ] as const) {
    const requestedAt = approved ? periodEndMs(period) + 3 * DAY : Math.max(periodEndMs(period) + 3_600_000, Date.now() - 3_600_000);
    const s = buildStatement(st, cid, period, mockUuid(`job|${cid}|${period}`), by, requestedAt, prices);
    if (!s) continue;
    if (approved) {
      s.status = "approved";
      s.approved_by = G5_ACTORS.approver;
      s.approved_at = isoAt(Date.parse(s.requested_at) + 2 * DAY);
    }
    st.statements.push(s);
    st.jobs.push({
      id: s.job_id,
      period,
      client_id: cid,
      supersedes_id: null,
      correction_reason: null,
      status: "completed",
      statement_ids: [s.id],
      error: null,
      requested_by: by,
      requested_at: s.requested_at,
      completed_at: s.requested_at,
      polls: 9,
    });
  }
  return st;
}

let STATE: G5State | null = null;

function storage(): Storage | null {
  try {
    return typeof window !== "undefined" ? window.sessionStorage : null;
  } catch {
    return null;
  }
}

function state(): G5State {
  if (STATE) return STATE;
  const raw = storage()?.getItem(STORE_KEY);
  if (raw) {
    try {
      STATE = JSON.parse(raw) as G5State;
      return STATE;
    } catch {
      // hỏng → dựng lại
    }
  }
  STATE = seed();
  return STATE;
}

function save(): void {
  // Chỉ dữ liệu không bí mật: KeyRow không có trường nào chứa khóa (xem kiểm thử secret-once).
  try {
    storage()?.setItem(STORE_KEY, JSON.stringify(state()));
  } catch {
    // bỏ qua khi trình duyệt chặn lưu trữ
  }
}

/** Cho kiểm thử: dựng lại dữ liệu gốc và xóa bản lưu. */
export function resetG5Mock(): void {
  STATE = null;
  try {
    storage()?.removeItem(STORE_KEY);
  } catch {
    // bỏ qua
  }
}

/** Cho kiểm thử: bản chụp JSON của mọi thứ mock đang giữ (để quét chuỗi bí mật). */
export function g5MockSnapshot(): string {
  return JSON.stringify(state());
}

// ---------------------------------------------------------------------------
// Sổ sử dụng (tất định theo khách hàng × ngày × sản phẩm)
// ---------------------------------------------------------------------------
const USAGE_PRODUCTS: Record<string, { product: string; untilDaysAgo?: number }[]> = {
  [CLIENT_IDS.factory]: [{ product: "collection_volume" }],
  [CLIENT_IDS.verifier]: [{ product: "mrv_package_status" }],
  [CLIENT_IDS.logistics]: [{ product: "hub_capacity", untilDaysAgo: 21 }],
};

/** Tổng theo ngày (giờ VN) của một khách hàng trong [from, to]. Chỉ ngày đã qua (không có tương lai). */
export function usageDays(clientId: string, from: string, to: string): MeteringUsageDay[] {
  const today = vnToday();
  const out: MeteringUsageDay[] = [];
  for (const { product, untilDaysAgo } of USAGE_PRODUCTS[clientId] ?? []) {
    const last = addDays(today, -(untilDaysAgo ?? 0));
    for (let d = from; d <= to && d <= last; d = addDays(d, 1)) {
      if (d < addDays(today, -200)) continue;
      const h = hashInt(`${clientId}|${product}|${d}`);
      const dow = new Date(`${d}T00:00:00Z`).getUTCDay();
      const base = dow === 0 || dow === 6 ? 4 : product === "mrv_package_status" ? 9 : 26;
      const calls = base + (h % 23);
      const errors = h % 4 === 0 ? 1 + (h % 3) : 0;
      // Một vài ngày có tải tệp xuất (đơn vị = ceil(dòng/1000)) — ADR-0023 metering.units.v1.
      const fileUnits = product === "collection_volume" && h % 7 === 0 ? 3 + (h % 5) : 0;
      out.push({ date: d, product, calls, units: calls - errors + fileUnits });
    }
  }
  return out.sort((a, b) => (a.date === b.date ? a.product.localeCompare(b.product) : a.date.localeCompare(b.date)));
}

const EVENT_T0 = Date.now();
const EVENTS = new Map<string, MeteringUsageEvent[]>();

/** 160 dòng sổ gần nhất (mới nhất trước) của một khách hàng — đúng quy tắc đơn vị metering.units.v1. */
export function usageEvents(clientId: string): MeteringUsageEvent[] {
  const cached = EVENTS.get(clientId);
  if (cached) return cached;
  const products = USAGE_PRODUCTS[clientId];
  if (!products) return [];
  const product = products[0]!.product;
  const offsetDays = products[0]!.untilDaysAgo ?? 0;
  const key = state().keys.find((k) => k.client_id === clientId);
  const out: MeteringUsageEvent[] = [];
  for (let i = 0; i < 160; i++) {
    const h = hashInt(`${clientId}|ev|${i}`);
    const at = EVENT_T0 - offsetDays * DAY - i * 23 * 60_000 - (h % 300) * 1000;
    const kinds = ["read", "read", "read", "read", "job_status", "export_request", "file_download", "meta"] as const;
    const call_kind = product === "collection_volume" ? kinds[h % kinds.length]! : (["read", "read", "meta"] as const)[h % 3]!;
    let status = 200;
    let problem_code: string | null = null;
    if (h % 17 === 0) {
      status = 429;
      problem_code = "apiclient.rate_limited";
    } else if (h % 29 === 0) {
      status = 403;
      problem_code = "apiclient.forbidden";
    } else if (call_kind === "export_request") status = 202;
    const replayed = call_kind === "export_request" && h % 5 === 0;
    const rows = call_kind === "file_download" ? 1500 + (h % 4000) : call_kind === "read" ? 20 + (h % 80) : 0;
    const units = status >= 400 || replayed ? 0 : call_kind === "read" ? 1 : call_kind === "file_download" ? Math.ceil(rows / 1000) : 0;
    const route = {
      read: `/v1/products/${product}`,
      job_status: "/v1/jobs/{id}",
      export_request: `/v1/products/${product}/exports`,
      file_download: "/v1/jobs/{id}/file",
      meta: "/v1/products",
    }[call_kind];
    out.push({
      id: mockUuid(`ue|${clientId}|${i}`),
      seq: 5000 - i,
      client_id: clientId,
      key_prefix: key?.key_prefix ?? prefixFrom(clientId),
      auth_method: clientId === CLIENT_IDS.verifier && h % 2 === 0 ? "oauth_client_credentials" : "api_key",
      call_kind,
      product,
      contract_version: 1,
      route_template: route,
      status,
      problem_code,
      data_tenant_id: status < 400 ? MKG_TENANT : null,
      units,
      rows_out: status < 400 ? rows : 0,
      bytes_out: status < 400 ? rows * 120 + 400 : 180,
      latency_ms: 18 + (h % 240),
      replayed,
      rule_version: "metering.units.v1",
      event_time: isoAt(at),
      recorded_at: isoAt(at + 3),
    });
  }
  EVENTS.set(clientId, out);
  return out;
}

// ---------------------------------------------------------------------------
// Bảng kê
// ---------------------------------------------------------------------------
function linesOf(clientId: string, period: string, prices: Record<string, string> | null): MeteringStatementLine[] {
  const from = `${period}-01`;
  const to = addDays(periodBack(-1, `${period}-15`) + "-01", -1);
  const agg = new Map<string, { calls: number; units: number }>();
  for (const d of usageDays(clientId, from, to)) {
    const a = agg.get(d.product) ?? { calls: 0, units: 0 };
    a.calls += d.calls;
    a.units += d.units;
    agg.set(d.product, a);
  }
  const out: MeteringStatementLine[] = [];
  for (const product of [...agg.keys()].sort()) {
    const a = agg.get(product)!;
    const ok = Math.min(a.units, a.calls);
    const price = prices?.[product] ?? null;
    out.push({
      product,
      status_class: "2xx",
      calls: ok,
      units: a.units,
      unit_price_vnd: price,
      amount_vnd: price ? lineAmount(price, a.units) : null,
      price_version: price ? `metering.price.default.${product}.per_unit_vnd@v1` : null,
    });
    if (a.calls > ok) {
      out.push({ product, status_class: "4xx", calls: a.calls - ok, units: 0, unit_price_vnd: price, amount_vnd: price ? "0.00" : null, price_version: price ? `metering.price.default.${product}.per_unit_vnd@v1` : null });
    }
  }
  return out;
}

function snapshotHash(clientId: string, period: string, lines: readonly MeteringStatementLine[], firstSeq: number, lastSeq: number): string {
  const body = lines.map((l) => `${l.product}|${l.status_class}|${l.calls}|${l.units}`).join(";");
  return hex64(`metering.statement.v1|${clientId}|${period}|${firstSeq}|${lastSeq}|${body}`);
}

function buildStatement(
  st: G5State,
  clientId: string,
  period: string,
  jobId: string,
  requestedBy: string,
  requestedAtMs: number,
  prices: Record<string, string> | null,
  supersedes: { id: string; reason: string } | null = null,
): MeteringStatement | null {
  const lines = linesOf(clientId, period, prices);
  if (lines.length === 0) return null;
  const c = st.clients.find((x) => x.id === clientId);
  const firstSeq = 1 + (hashInt(`${clientId}|${period}|f`) % 4000);
  const lastSeq = firstSeq + lines.reduce((a, l) => a + l.calls, 0) - 1;
  const total = prices ? sumMoney(lines.map((l) => l.amount_vnd ?? "0.00")) : null;
  return {
    id: uuidv7(requestedAtMs),
    client_id: clientId,
    client_tenant_id: c?.client_tenant_id ?? CLIENT_TENANTS[clientId] ?? clientId,
    period,
    status: "draft",
    lines,
    subscription_vnd: null,
    total_vnd: total,
    price_version: prices ? "metering.price.default@v1" : null,
    first_seq: firstSeq,
    last_seq: lastSeq,
    chain_head_hash: hex64(`chain|${clientId}|${period}|${lastSeq}`),
    snapshot_sha256: snapshotHash(clientId, period, lines, firstSeq, lastSeq),
    rule_version: "metering.statement.v1",
    job_id: jobId,
    requested_by: requestedBy,
    requested_at: isoAt(requestedAtMs),
    approved_by: null,
    approved_at: null,
    supersedes_id: supersedes?.id ?? null,
    correction_reason: supersedes?.reason ?? null,
    superseded_by: null,
  };
}

const CSV_HEADER = [
  "statement_id",
  "client_id",
  "client_tenant_id",
  "period",
  "status",
  "product",
  "status_class",
  "calls",
  "units",
  "unit_price_vnd",
  "amount_vnd",
  "price_version",
  "snapshot_sha256",
  "chain_head_hash",
  "first_seq",
  "last_seq",
  "rule_version",
  "supersedes_id",
];

/** CSV tất định như `metering/domain.StatementCSV` (UTF-8, dấu phẩy, LF; dòng sắp theo sản phẩm, lớp trạng thái). */
export function statementCsv(s: MeteringStatement): string {
  const lines = [...s.lines].sort((a, b) => (a.product === b.product ? a.status_class.localeCompare(b.status_class) : a.product < b.product ? -1 : 1));
  const row = (product: string, cls: string, calls: string, units: string, price: string, amount: string, pv: string) =>
    [s.id, s.client_id, s.client_tenant_id, s.period, s.status, product, cls, calls, units, price, amount, pv, s.snapshot_sha256, s.chain_head_hash, String(s.first_seq), String(s.last_seq), s.rule_version, s.supersedes_id ?? ""].join(",");
  const rows = [CSV_HEADER.join(",")];
  let calls = 0;
  let units = 0;
  for (const l of lines) {
    rows.push(row(l.product, l.status_class, String(l.calls), String(l.units), l.unit_price_vnd ?? "", l.amount_vnd ?? "", l.price_version ?? ""));
    calls += l.calls;
    units += l.units;
  }
  if (s.subscription_vnd) rows.push(row("_subscription", "", "", "", "", s.subscription_vnd, ""));
  rows.push(row("_total", "", String(calls), String(units), "", s.total_vnd ?? "", ""));
  return `${rows.join("\n")}\n`;
}

// ---------------------------------------------------------------------------
// Sổ xuất (DLP) — tenant chủ dữ liệu = tenant của persona
// ---------------------------------------------------------------------------
let EGRESS: { events: EgressEvent[]; anomalies: EgressAnomaly[] } | null = null;

function egressSeed(): { events: EgressEvent[]; anomalies: EgressAnomaly[] } {
  if (EGRESS) return EGRESS;
  const events: EgressEvent[] = [];
  const today = vnToday();
  const cps: Record<EgressChannel, { kind: EgressEvent["counterparty_kind"]; ref: string; product: string; records: () => number }[]> = {
    public_api: [
      { kind: "api_client", ref: CLIENT_IDS.factory, product: "collection_volume", records: () => 40 },
      { kind: "api_client", ref: CLIENT_IDS.verifier, product: "mrv_package_status", records: () => 6 },
    ],
    mrv_package: [{ kind: "actor", ref: G5_ACTORS.other, product: "mrv_evidence_package", records: () => 1200 }],
    report_export: [
      { kind: "actor", ref: G5_ACTORS.admin, product: "kn_monthly_summary", records: () => 1 },
      { kind: "actor", ref: G5_ACTORS.other, product: "htx_collection_statement", records: () => 1 },
    ],
    valve: [{ kind: "tenant", ref: MKG_TENANT, product: "collection_statement", records: () => 1 }],
    partner_share: [{ kind: "tenant", ref: MKG_TENANT, product: "dp_collection_volume", records: () => 25 }],
  };
  let seq = 9000;
  for (let day = 13; day >= 0; day--) {
    const date = addDays(today, -day);
    for (const ch of CHANNELS) {
      for (const cp of cps[ch]) {
        const h = hashInt(`${ch}|${cp.ref}|${date}`);
        const n = ch === "valve" ? 3 + (h % 4) : ch === "report_export" ? h % 3 : 1 + (h % 2);
        let daily = 0;
        for (let i = 0; i < n; i++) {
          const spike = ch === "public_api" && cp.ref === CLIENT_IDS.factory && day === 2 && i === 0;
          const records = spike ? 61_250 : cp.records() + (hashInt(`${h}|${i}`) % (cp.records() + 3));
          daily += records;
          const quota = ch === "mrv_package" ? 50_000 : 0;
          const thresholds: Record<EgressChannel, number> = { public_api: 50_000, mrv_package: 10_000, report_export: 20_000, valve: 20_000, partner_share: 50_000 };
          const at = Date.parse(`${date}T01:00:00Z`) + (i * 3 + (h % 3)) * 3_600_000;
          events.push({
            id: mockUuid(`eg|${ch}|${cp.ref}|${date}|${i}`),
            seq: seq++,
            channel: ch,
            source_id: mockUuid(`src|${ch}|${date}|${i}|${cp.ref}`),
            counterparty_kind: cp.kind,
            counterparty_ref: cp.ref,
            product: cp.product,
            contract_version: 1,
            records,
            bytes: records * 140 + 512,
            business_date: date,
            decision: "allowed",
            reason: "",
            mode: "observe",
            daily_records: daily,
            daily_quota: quota,
            anomaly: daily > thresholds[ch],
            actor_ref: cp.kind === "actor" ? cp.ref : G5_ACTORS.other,
            event_time: isoAt(at),
            recorded_at: isoAt(at + 2),
          });
        }
      }
    }
  }
  // Một lần bị từ chối ở kênh gói MRV (enforce, vượt hạn mức) để thấy cột "Quyết định".
  const denyDate = addDays(today, -5);
  events.push({
    id: mockUuid(`eg|deny|${denyDate}`),
    seq: seq++,
    channel: "mrv_package",
    source_id: mockUuid(`src|deny|${denyDate}`),
    counterparty_kind: "actor",
    counterparty_ref: G5_ACTORS.other,
    product: "mrv_evidence_package",
    contract_version: 1,
    records: 52_000,
    bytes: 52_000 * 140,
    business_date: denyDate,
    decision: "denied",
    reason: "quota_exceeded",
    mode: "enforce",
    daily_records: 53_400,
    daily_quota: 50_000,
    anomaly: true,
    actor_ref: G5_ACTORS.other,
    event_time: isoAt(Date.parse(`${denyDate}T08:30:00Z`)),
    recorded_at: isoAt(Date.parse(`${denyDate}T08:30:01Z`)),
  });
  events.sort((a, b) => b.event_time.localeCompare(a.event_time) || b.seq - a.seq);
  const firstAnomaly = (pred: (e: EgressEvent) => boolean) => [...events].reverse().find(pred);
  const anomalies: EgressAnomaly[] = [];
  const spike = firstAnomaly((e) => e.channel === "public_api" && e.anomaly);
  if (spike)
    anomalies.push({
      id: mockUuid(`an|${spike.id}`),
      business_date: spike.business_date,
      channel: "public_api",
      counterparty_kind: "api_client",
      counterparty_ref: spike.counterparty_ref,
      product: spike.product,
      egress_event_id: spike.id,
      daily_records: spike.daily_records,
      threshold: 1_980,
      basis: "median",
      mode: "observe",
      detected_at: spike.event_time,
    });
  const deny = events.find((e) => e.decision === "denied");
  if (deny)
    anomalies.push({
      id: mockUuid(`an|${deny.id}`),
      business_date: deny.business_date,
      channel: "mrv_package",
      counterparty_kind: "actor",
      counterparty_ref: deny.counterparty_ref,
      product: deny.product,
      egress_event_id: deny.id,
      daily_records: deny.daily_records,
      threshold: 10_000,
      basis: "absolute",
      mode: "enforce",
      detected_at: deny.event_time,
    });
  for (const ch of ["valve", "partner_share", "report_export"] as const) {
    const e = firstAnomaly((x) => x.channel === ch && x.anomaly);
    if (e)
      anomalies.push({
        id: mockUuid(`an|${e.id}`),
        business_date: e.business_date,
        channel: ch,
        counterparty_kind: e.counterparty_kind,
        counterparty_ref: e.counterparty_ref,
        product: e.product,
        egress_event_id: e.id,
        daily_records: e.daily_records,
        threshold: 20_000,
        basis: "absolute",
        mode: "observe",
        detected_at: e.event_time,
      });
  }
  anomalies.sort((a, b) => b.detected_at.localeCompare(a.detected_at));
  EGRESS = { events, anomalies };
  return EGRESS;
}

// ---------------------------------------------------------------------------
// Tiện ích route
// ---------------------------------------------------------------------------
const forbidden = () => problem(403, "forbidden", "Forbidden");
const clientNotFound = () => problem(404, "apiclient.client_not_found", "API client not found");
const stateConflict = (detail: string) => problem(409, "apiclient.state", "State conflict", { detail });

function need(p: MockPersona, ...roles: string[]): Response | null {
  return hasRole(p, ...roles) ? null : forbidden();
}

/** Trang theo cursor = id của phần tử cuối trang trước (hợp đồng: cursor uuid). */
function idPage<T extends { id: string }>(items: T[], url: URL, def = 50, max = 200): { items: T[]; next_cursor: string | null } {
  const limit = Math.min(Math.max(Number(url.searchParams.get("limit") ?? def) || def, 1), max);
  const cursor = url.searchParams.get("cursor");
  const start = cursor ? items.findIndex((x) => x.id === cursor) + 1 : 0;
  const slice = items.slice(start, start + limit);
  const next = start + limit < items.length ? (slice[slice.length - 1]?.id ?? null) : null;
  return { items: slice, next_cursor: next };
}

function findClient(id: string | undefined): ApiClientRecord | undefined {
  return state().clients.find((c) => c.id === id);
}

function keyView(k: KeyRow): ApiKeyRecord {
  const { client_id: _c, ...rest } = k;
  void _c;
  const expired = k.status === "active" && Date.parse(k.expires_at) <= Date.now();
  return expired ? { ...rest, status: "expired" } : rest;
}
function subView(s: SubRow): ApiSubscription {
  const { client_id: _c, ...rest } = s;
  void _c;
  return rest;
}
function planView(p: PlanRow): ApiQuotaPlan {
  const { client_id: _c, ...rest } = p;
  void _c;
  return rest;
}

function effectiveKeys(clientId: string): KeyRow[] {
  const now = Date.now();
  return state().keys.filter(
    (k) => k.client_id === clientId && Date.parse(k.expires_at) > now && (k.status === "active" || (k.status === "rotated" && !!k.grace_until && Date.parse(k.grace_until) > now)),
  );
}

/** Phát hành khóa mới: lưu tiền tố + hạn; bí mật chỉ đi vào phản hồi. */
function newKey(clientId: string): { row: KeyRow; secret: string } {
  const prefix = randomPrefix();
  const now = Date.now();
  const row: KeyRow = {
    id: uuidv7(now),
    client_id: clientId,
    key_prefix: prefix,
    status: "active",
    issued_at: isoAt(now),
    expires_at: isoAt(now + 365 * DAY),
    grace_until: null,
    revoked_at: null,
    revoke_reason: null,
  };
  return { row, secret: `agk_${prefix}_${randomSecret()}` };
}

const NO_STORE = { "Cache-Control": "no-store" };

function reasonOr422(body: unknown): string | Response {
  const r = reasonOf(body);
  if (r.length < 10 || r.length > 500) return validation([{ field: "reason", reason: "bắt buộc, 10–500 ký tự" }]);
  return r;
}

// ---------------------------------------------------------------------------
// Route pf/apiclient (Admin API)
// ---------------------------------------------------------------------------
const AC = "\\/v1\\/api-clients";
const ID = "([0-9a-f-]{36})";
const rx = (s: string) => new RegExp(`^${s}$`);

export const apiClientAdminRoutes: Route[] = [
  {
    method: "GET",
    pattern: rx(AC),
    handler: ({ persona, url }) => {
      const deny = need(persona, ADMIN, APPROVER);
      if (deny) return deny;
      const status = url.searchParams.get("status");
      if (status && !["proposed", "active", "suspended", "revoked"].includes(status)) return validation([{ field: "status", reason: "trạng thái không hợp lệ" }]);
      const list = state()
        .clients.filter((c) => !status || c.status === status)
        .sort((a, b) => b.proposed_at.localeCompare(a.proposed_at));
      return json(idPage(list, url, 100, 500));
    },
  },
  {
    method: "POST",
    pattern: rx(AC),
    handler: ({ persona, body }) => {
      const deny = need(persona, ADMIN);
      if (deny) return deny;
      const b = (body ?? {}) as Record<string, unknown>;
      const allowed = new Set(["client_tenant_id", "display_name", "purpose", "contact_ref", "auth_methods", "oauth_client_id"]);
      const extra = Object.keys(b).filter((k) => !allowed.has(k));
      if (extra.length) return problem(400, "bad_request", "Unknown fields", { detail: `Trường không có trong hợp đồng: ${extra.join(", ")}` });
      const errs: { field: string; reason: string }[] = [];
      const str = (k: string) => (typeof b[k] === "string" ? (b[k] as string).trim() : "");
      if (!UUID.test(str("client_tenant_id"))) errs.push({ field: "client_tenant_id", reason: "phải là UUID" });
      if (str("display_name").length < 2 || str("display_name").length > 200) errs.push({ field: "display_name", reason: "2–200 ký tự" });
      if (str("purpose").length < 10 || str("purpose").length > 500) errs.push({ field: "purpose", reason: "10–500 ký tự" });
      if (!UUID.test(str("contact_ref"))) errs.push({ field: "contact_ref", reason: "phải là UUID" });
      const methods = Array.isArray(b.auth_methods) ? (b.auth_methods as unknown[]).map(String) : ["api_key"];
      if (methods.some((m) => m !== "api_key" && m !== "oauth_client_credentials")) errs.push({ field: "auth_methods", reason: "chỉ api_key, oauth_client_credentials" });
      const oauth = str("oauth_client_id");
      if (methods.includes("oauth_client_credentials") && !/^[A-Za-z0-9._-]{3,100}$/.test(oauth)) errs.push({ field: "oauth_client_id", reason: "bắt buộc khi dùng OAuth" });
      if (errs.length) return validation(errs);
      if (state().clients.some((c) => c.client_tenant_id === str("client_tenant_id") && c.status !== "revoked"))
        return stateConflict("tenant bên thứ ba này đã có khách hàng API chưa thu hồi");
      const now = Date.now();
      const c: ApiClientRecord = {
        id: uuidv7(now),
        client_tenant_id: str("client_tenant_id"),
        display_name: str("display_name"),
        purpose: str("purpose"),
        contact_ref: str("contact_ref"),
        auth_methods: methods,
        oauth_client_id: oauth || null,
        status: "proposed",
        status_reason: null,
        proposed_by: persona.me.actor_ref,
        proposed_at: isoAt(now),
        approved_by: null,
        approved_at: null,
      };
      state().clients.push(c);
      save();
      return json(c, 201, { Location: `/v1/api-clients/${c.id}` });
    },
  },
  {
    method: "GET",
    pattern: rx(`${AC}\\/${ID}`),
    handler: ({ persona, params }) => need(persona, ADMIN, APPROVER) ?? (findClient(params[0]) ? json(findClient(params[0])) : clientNotFound()),
  },
  {
    method: "POST",
    pattern: rx(`${AC}\\/${ID}\\/(approve|suspend|reactivate|revoke)`),
    handler: ({ persona, params, body }) => {
      const op = params[1]!;
      const deny = need(persona, op === "approve" || op === "reactivate" ? APPROVER : ADMIN);
      if (deny) return deny;
      const c = findClient(params[0]);
      if (!c) return clientNotFound();
      const now = isoAt(Date.now());
      if (op === "approve") {
        if (c.status !== "proposed") return stateConflict("khách hàng không ở trạng thái chờ duyệt");
        if (c.proposed_by === persona.me.actor_ref) return problem(403, "apiclient.self_approval", "Self approval");
        Object.assign(c, { status: "active", approved_by: persona.me.actor_ref, approved_at: now });
      } else if (op === "reactivate") {
        if (c.status !== "suspended") return stateConflict("khách hàng không ở trạng thái tạm ngưng");
        Object.assign(c, { status: "active", status_reason: null });
      } else {
        const reason = reasonOr422(body);
        if (reason instanceof Response) return reason;
        if (c.status === "revoked") return stateConflict("khách hàng đã bị thu hồi");
        if (op === "suspend" && c.status !== "active") return stateConflict("chỉ tạm ngưng khách hàng đang hoạt động");
        Object.assign(c, { status: op === "suspend" ? "suspended" : "revoked", status_reason: reason });
        if (op === "revoke") {
          for (const k of state().keys.filter((x) => x.client_id === c.id && (x.status === "active" || x.status === "rotated")))
            Object.assign(k, { status: "revoked", revoked_at: now, revoke_reason: "Khách hàng bị thu hồi" });
          for (const s of state().subs.filter((x) => x.client_id === c.id && x.status !== "revoked"))
            Object.assign(s, { status: "revoked", revoked_at: now, revoke_reason: "Khách hàng bị thu hồi" });
        }
      }
      save();
      return json(c);
    },
  },
  // --- khóa ---
  {
    method: "GET",
    pattern: rx(`${AC}\\/${ID}\\/keys`),
    handler: ({ persona, params }) => {
      const deny = need(persona, ADMIN, APPROVER);
      if (deny) return deny;
      if (!findClient(params[0])) return clientNotFound();
      const items = state()
        .keys.filter((k) => k.client_id === params[0])
        .sort((a, b) => b.issued_at.localeCompare(a.issued_at))
        .map(keyView);
      return json({ items });
    },
  },
  {
    method: "POST",
    pattern: rx(`${AC}\\/${ID}\\/keys`),
    handler: ({ persona, params }) => {
      const deny = need(persona, ADMIN);
      if (deny) return deny;
      const c = findClient(params[0]);
      if (!c) return clientNotFound();
      if (c.status !== "active") return stateConflict("chỉ phát hành khóa cho khách hàng đang hoạt động");
      if (effectiveKeys(c.id).length >= 2) return problem(409, "apiclient.too_many_keys", "Too many keys");
      const { row, secret } = newKey(c.id);
      state().keys.push(row);
      save();
      return json({ key_id: row.id, client_id: c.id, key_prefix: row.key_prefix, api_key: secret, expires_at: row.expires_at }, 201, NO_STORE);
    },
  },
  {
    method: "POST",
    pattern: rx(`${AC}\\/${ID}\\/keys\\/${ID}\\/rotate`),
    handler: ({ persona, params }) => {
      const deny = need(persona, ADMIN);
      if (deny) return deny;
      const c = findClient(params[0]);
      if (!c) return clientNotFound();
      const old = state().keys.find((k) => k.client_id === c.id && k.id === params[1]);
      if (!old) return problem(404, "apiclient.key_not_found", "Key not found");
      if (c.status !== "active") return stateConflict("khách hàng không hoạt động");
      if (old.status !== "active" || Date.parse(old.expires_at) <= Date.now()) return stateConflict("chỉ xoay được khóa đang hiệu lực");
      const { row, secret } = newKey(c.id);
      const grace = isoAt(Date.now() + 7 * DAY);
      Object.assign(old, { status: "rotated", grace_until: grace });
      state().keys.push(row);
      save();
      return json({ key_id: row.id, client_id: c.id, key_prefix: row.key_prefix, api_key: secret, expires_at: row.expires_at, previous_key_grace_until: grace }, 201, NO_STORE);
    },
  },
  {
    method: "POST",
    pattern: rx(`${AC}\\/${ID}\\/keys\\/${ID}\\/revoke`),
    handler: ({ persona, params, body }) => {
      const deny = need(persona, ADMIN);
      if (deny) return deny;
      if (!findClient(params[0])) return clientNotFound();
      const k = state().keys.find((x) => x.client_id === params[0] && x.id === params[1]);
      if (!k) return problem(404, "apiclient.key_not_found", "Key not found");
      const reason = reasonOr422(body);
      if (reason instanceof Response) return reason;
      if (k.status !== "revoked") Object.assign(k, { status: "revoked", revoked_at: isoAt(Date.now()), revoke_reason: reason, grace_until: null });
      save();
      return json(keyView(k));
    },
  },
  // --- đăng ký sản phẩm ---
  {
    method: "GET",
    pattern: rx(`${AC}\\/${ID}\\/subscriptions`),
    handler: ({ persona, params }) => {
      const deny = need(persona, ADMIN, APPROVER);
      if (deny) return deny;
      if (!findClient(params[0])) return clientNotFound();
      return json({ items: state().subs.filter((s) => s.client_id === params[0]).sort((a, b) => b.proposed_at.localeCompare(a.proposed_at)).map(subView) });
    },
  },
  {
    method: "POST",
    pattern: rx(`${AC}\\/${ID}\\/subscriptions`),
    handler: ({ persona, params, body }) => {
      const deny = need(persona, ADMIN);
      if (deny) return deny;
      const c = findClient(params[0]);
      if (!c) return clientNotFound();
      const b = (body ?? {}) as { product?: unknown; scopes?: unknown };
      const product = typeof b.product === "string" ? b.product : "";
      const scopes = Array.isArray(b.scopes) ? b.scopes.map(String) : [];
      const allowed = PRODUCT_SCOPES[product];
      const errs: { field: string; reason: string }[] = [];
      if (!allowed) errs.push({ field: "product", reason: "sản phẩm không có trong contracts/dataproduct" });
      if (scopes.length < 1 || scopes.length > 3) errs.push({ field: "scopes", reason: "1–3 phạm vi" });
      if (allowed) for (const s of scopes) if (!allowed.includes(s)) errs.push({ field: "scopes", reason: `${s} không có trong hợp đồng ${product}` });
      if (errs.length) return validation(errs);
      if (c.status === "revoked") return stateConflict("khách hàng đã bị thu hồi");
      if (state().subs.some((s) => s.client_id === c.id && s.product === product && s.status !== "revoked")) return stateConflict("đã có đăng ký sản phẩm này chưa thu hồi");
      const now = Date.now();
      const s: SubRow = {
        id: uuidv7(now),
        client_id: c.id,
        product,
        scopes,
        status: "proposed",
        proposed_by: persona.me.actor_ref,
        proposed_at: isoAt(now),
        approved_by: null,
        approved_at: null,
        revoked_at: null,
        revoke_reason: null,
      };
      state().subs.push(s);
      save();
      return json(subView(s), 201);
    },
  },
  {
    method: "POST",
    pattern: rx(`${AC}\\/${ID}\\/subscriptions\\/${ID}\\/(approve|revoke)`),
    handler: ({ persona, params, body }) => {
      const op = params[2]!;
      const deny = need(persona, op === "approve" ? APPROVER : ADMIN);
      if (deny) return deny;
      if (!findClient(params[0])) return clientNotFound();
      const s = state().subs.find((x) => x.client_id === params[0] && x.id === params[1]);
      if (!s) return problem(404, "apiclient.subscription_not_found", "Subscription not found");
      if (op === "approve") {
        if (s.status !== "proposed") return stateConflict("đăng ký không ở trạng thái chờ duyệt");
        if (s.proposed_by === persona.me.actor_ref) return problem(403, "apiclient.self_approval", "Self approval");
        Object.assign(s, { status: "active", approved_by: persona.me.actor_ref, approved_at: isoAt(Date.now()) });
      } else {
        const reason = reasonOr422(body);
        if (reason instanceof Response) return reason;
        if (s.status !== "revoked") Object.assign(s, { status: "revoked", revoked_at: isoAt(Date.now()), revoke_reason: reason });
      }
      save();
      return json(subView(s));
    },
  },
  // --- gói hạn mức ---
  {
    method: "GET",
    pattern: rx(`${AC}\\/${ID}\\/plans`),
    handler: ({ persona, params }) => {
      const deny = need(persona, ADMIN, APPROVER);
      if (deny) return deny;
      if (!findClient(params[0])) return clientNotFound();
      return json({ items: state().plans.filter((p) => p.client_id === params[0]).sort((a, b) => b.version - a.version).map(planView), defaults: DEFAULTS });
    },
  },
  {
    method: "POST",
    pattern: rx(`${AC}\\/${ID}\\/plans`),
    handler: ({ persona, params, body }) => {
      const deny = need(persona, ADMIN);
      if (deny) return deny;
      const c = findClient(params[0]);
      if (!c) return clientNotFound();
      const b = (body ?? {}) as Record<string, unknown>;
      const errs: { field: string; reason: string }[] = [];
      const intOf = (k: string, min: number, max: number) => {
        const v = b[k];
        if (typeof v !== "number" || !Number.isInteger(v) || v < min || v > max) errs.push({ field: k, reason: `số nguyên ${min}–${max}` });
        return typeof v === "number" ? v : 0;
      };
      const rpm = intOf("requests_per_minute", 1, 100_000);
      const daily = intOf("daily_units", 1, Number.MAX_SAFE_INTEGER);
      const monthly = intOf("monthly_units", 1, Number.MAX_SAFE_INTEGER);
      const rows = intOf("max_export_rows", 1, 100_000_000);
      const eff = typeof b.effective_from === "string" ? b.effective_from : isoAt(Date.now());
      if (!Number.isFinite(Date.parse(eff))) errs.push({ field: "effective_from", reason: "ngày giờ RFC 3339" });
      if (errs.length) return validation(errs);
      if (c.status === "revoked") return stateConflict("khách hàng đã bị thu hồi");
      const version = Math.max(0, ...state().plans.filter((p) => p.client_id === c.id).map((p) => p.version)) + 1;
      const p: PlanRow = {
        client_id: c.id,
        version,
        requests_per_minute: rpm,
        daily_units: daily,
        monthly_units: monthly,
        max_export_rows: rows,
        effective_from: new Date(eff).toISOString(),
        status: "proposed",
        proposed_by: persona.me.actor_ref,
        proposed_at: isoAt(Date.now()),
        approved_by: null,
        approved_at: null,
      };
      state().plans.push(p);
      save();
      return json(planView(p), 201);
    },
  },
  {
    method: "POST",
    pattern: rx(`${AC}\\/${ID}\\/plans\\/(\\d+)\\/approve`),
    handler: ({ persona, params }) => {
      const deny = need(persona, APPROVER);
      if (deny) return deny;
      if (!findClient(params[0])) return clientNotFound();
      const p = state().plans.find((x) => x.client_id === params[0] && x.version === Number(params[1]));
      if (!p) return problem(404, "apiclient.plan_not_found", "Plan not found");
      if (p.status !== "proposed") return stateConflict("phiên bản gói đã được duyệt");
      if (p.proposed_by === persona.me.actor_ref) return problem(403, "apiclient.self_approval", "Self approval");
      Object.assign(p, { status: "approved", approved_by: persona.me.actor_ref, approved_at: isoAt(Date.now()) });
      save();
      return json(planView(p));
    },
  },
];

// ---------------------------------------------------------------------------
// Route pf/metering (Admin API)
// ---------------------------------------------------------------------------
const READERS = [VIEWER, OFFICER, BAPPROVER];
const EGRESS_READERS = [VIEWER, "platform_admin"];
const ISO_DATE = /^\d{4}-\d{2}-\d{2}$/;
const meteringNotFound = () => problem(404, "metering.not_found", "Not found");

function currentStatements(): MeteringStatement[] {
  return [...state().statements].sort((a, b) => b.period.localeCompare(a.period) || b.requested_at.localeCompare(a.requested_at));
}

/** Hoàn tất job khi được hỏi lần thứ 2 (mô phỏng worker pf.metering.statement). */
function advanceJob(j: JobRow): void {
  if (j.status !== "queued") return;
  j.polls += 1;
  if (j.polls < 2) return;
  const st = state();
  const targets = j.client_id ? [j.client_id] : st.clients.filter((c) => c.status !== "proposed").map((c) => c.id);
  const ids: string[] = [];
  for (const cid of targets) {
    if (!j.supersedes_id) {
      const active = st.statements.find((s) => s.client_id === cid && s.period === j.period && !s.superseded_by);
      if (active) {
        ids.push(active.id);
        continue;
      }
      const s = buildStatement(st, cid, j.period, j.id, j.requested_by, Date.now(), null);
      if (s) {
        st.statements.push(s);
        ids.push(s.id);
      }
    } else {
      const old = st.statements.find((s) => s.id === j.supersedes_id);
      if (!old || old.superseded_by) continue;
      const s = buildStatement(st, cid, j.period, j.id, j.requested_by, Date.now(), null, { id: old.id, reason: j.correction_reason ?? "" });
      if (s) {
        st.statements.push(s);
        old.superseded_by = s.id;
        ids.push(s.id);
      }
    }
  }
  Object.assign(j, { status: "completed", statement_ids: ids, completed_at: isoAt(Date.now()) });
  save();
}

function jobView(j: JobRow): MeteringStatementJob {
  const { polls: _p, ...rest } = j;
  void _p;
  return rest;
}

function paramsList(): { key: string; value: string | null; source: string }[] {
  const records: Record<EgressChannel, string> = { public_api: "50000", mrv_package: "10000", report_export: "20000", valve: "20000", partner_share: "50000" };
  const quota: Record<EgressChannel, string> = { public_api: "0", mrv_package: "50000", report_export: "0", valve: "0", partner_share: "0" };
  const out: { key: string; value: string | null; source: string }[] = [];
  for (const ch of CHANNELS) {
    out.push({ key: `metering.egress.${ch}.daily_records_quota`, value: quota[ch], source: "default" });
    out.push({ key: `metering.egress.${ch}.anomaly_factor`, value: "3", source: "default" });
    out.push({ key: `metering.egress.${ch}.anomaly_records`, value: records[ch], source: ch === "valve" ? "v2" : "default" });
  }
  out.push({ key: "metering.price.default.subscription_vnd_per_month", value: null, source: "unset" });
  for (const p of [...PRODUCTS].sort()) out.push({ key: `metering.price.default.${p}.per_unit_vnd`, value: null, source: "unset" });
  return out;
}

function egressFilter(ctx: Ctx): { channel: string | null; from: string | null; to: string | null } | Response {
  const channel = ctx.url.searchParams.get("channel");
  const from = ctx.url.searchParams.get("from");
  const to = ctx.url.searchParams.get("to");
  const errs: { field: string; reason: string }[] = [];
  if (channel && !CHANNELS.includes(channel as EgressChannel)) errs.push({ field: "channel", reason: "kênh không hợp lệ" });
  if (from && !ISO_DATE.test(from)) errs.push({ field: "from", reason: "YYYY-MM-DD" });
  if (to && !ISO_DATE.test(to)) errs.push({ field: "to", reason: "YYYY-MM-DD" });
  if (from && to && from > to) errs.push({ field: "to", reason: "không trước ngày bắt đầu" });
  return errs.length ? validation(errs) : { channel, from, to };
}

export const meteringAdminRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/metering\/usage$/,
    handler: ({ persona, url }) => {
      const deny = need(persona, ...READERS);
      if (deny) return deny;
      const client = url.searchParams.get("client_id") ?? "";
      const from = url.searchParams.get("from") ?? "";
      const to = url.searchParams.get("to") ?? "";
      const errs: { field: string; reason: string }[] = [];
      if (!UUID.test(client)) errs.push({ field: "client_id", reason: "bắt buộc, UUID" });
      if (!ISO_DATE.test(from)) errs.push({ field: "from", reason: "YYYY-MM-DD" });
      if (!ISO_DATE.test(to)) errs.push({ field: "to", reason: "YYYY-MM-DD" });
      if (!errs.length && from > to) errs.push({ field: "to", reason: "không trước ngày bắt đầu" });
      if (!errs.length && Date.parse(to) - Date.parse(from) > 366 * DAY) errs.push({ field: "to", reason: "tối đa 366 ngày" });
      if (errs.length) return validation(errs);
      return json({ items: usageDays(client, from, to) });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/metering\/usage-events$/,
    handler: ({ persona, url }) => {
      const deny = need(persona, ...READERS);
      if (deny) return deny;
      const client = url.searchParams.get("client_id") ?? "";
      if (!UUID.test(client)) return validation([{ field: "client_id", reason: "bắt buộc, UUID" }]);
      return json(idPage(usageEvents(client), url, 50, 200));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/metering\/statements$/,
    handler: ({ persona, url }) => {
      const deny = need(persona, ...READERS);
      if (deny) return deny;
      const period = url.searchParams.get("period");
      const client = url.searchParams.get("client_id");
      const list = currentStatements().filter((s) => (!period || s.period === period) && (!client || s.client_id === client));
      return json(idPage(list, url, 50, 200));
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/metering\/statements$/,
    handler: ({ persona, body }) => {
      const deny = need(persona, OFFICER);
      if (deny) return deny;
      const b = (body ?? {}) as Record<string, unknown>;
      const period = typeof b.period === "string" ? b.period : "";
      if (!/^\d{4}-(0[1-9]|1[0-2])$/.test(period)) return validation([{ field: "period", reason: "dạng YYYY-MM" }]);
      if (Date.now() < periodEndMs(period)) return problem(409, "metering.period_not_ended", "Period not ended", { detail: `kỳ ${period} chưa kết thúc (giờ Việt Nam)` });
      const client = typeof b.client_id === "string" ? b.client_id : null;
      const supersedes = typeof b.supersedes_id === "string" ? b.supersedes_id : null;
      const reason = typeof b.correction_reason === "string" ? b.correction_reason.trim() : null;
      if ((supersedes === null) !== (reason === null)) return validation([{ field: "correction_reason", reason: "bảng kê bù cần cả supersedes_id và correction_reason" }]);
      if (supersedes && !client) return validation([{ field: "client_id", reason: "bảng kê bù phải ghi khách hàng" }]);
      if (reason !== null && (reason.length < 10 || reason.length > 500)) return validation([{ field: "correction_reason", reason: "10–500 ký tự" }]);
      if (client && !findClient(client)) return meteringNotFound();
      if (supersedes) {
        const old = state().statements.find((s) => s.id === supersedes);
        if (!old || old.client_id !== client || old.period !== period) return problem(404, "metering.not_found", "Not found", { detail: "không có bảng kê cần thay của khách hàng và kỳ này" });
        if (old.superseded_by) return problem(409, "metering.statement_frozen", "Superseded", { detail: `bảng kê đã được thay bởi ${old.superseded_by}` });
      }
      const now = Date.now();
      const j: JobRow = {
        id: uuidv7(now),
        period,
        client_id: client,
        supersedes_id: supersedes,
        correction_reason: reason,
        status: "queued",
        statement_ids: [],
        error: null,
        requested_by: persona.me.actor_ref,
        requested_at: isoAt(now),
        completed_at: null,
        polls: 0,
      };
      state().jobs.push(j);
      save();
      return json(jobView(j), 202, { Location: `/v1/metering/statement-jobs/${j.id}` });
    },
  },
  {
    method: "GET",
    pattern: rx(`\\/v1\\/metering\\/statement-jobs\\/${ID}`),
    handler: ({ persona, params }) => {
      const deny = need(persona, ...READERS);
      if (deny) return deny;
      const j = state().jobs.find((x) => x.id === params[0]);
      if (!j) return meteringNotFound();
      advanceJob(j);
      return json(jobView(j));
    },
  },
  {
    method: "GET",
    pattern: rx(`\\/v1\\/metering\\/statements\\/${ID}`),
    handler: ({ persona, params }) => {
      const deny = need(persona, ...READERS);
      if (deny) return deny;
      const s = state().statements.find((x) => x.id === params[0]);
      return s ? json(s) : meteringNotFound();
    },
  },
  {
    method: "POST",
    pattern: rx(`\\/v1\\/metering\\/statements\\/${ID}\\/approve`),
    handler: ({ persona, params }) => {
      const deny = need(persona, BAPPROVER);
      if (deny) return deny;
      const s = state().statements.find((x) => x.id === params[0]);
      if (!s) return meteringNotFound();
      if (s.superseded_by) return problem(409, "metering.statement_frozen", "Superseded");
      if (s.status === "approved") return s.approved_by === persona.me.actor_ref ? json(s, 200) : problem(409, "metering.statement_frozen", "Frozen");
      if (s.requested_by === persona.me.actor_ref) return problem(403, "metering.self_approval", "Self approval");
      Object.assign(s, { status: "approved", approved_by: persona.me.actor_ref, approved_at: isoAt(Date.now()) });
      save();
      return json(s, 201);
    },
  },
  {
    method: "GET",
    pattern: rx(`\\/v1\\/metering\\/statements\\/${ID}\\/verify`),
    handler: ({ persona, params }) => {
      const deny = need(persona, ...READERS);
      if (deny) return deny;
      const s = state().statements.find((x) => x.id === params[0]);
      if (!s) return meteringNotFound();
      const rebuilt = snapshotHash(s.client_id, s.period, s.lines, s.first_seq, s.last_seq);
      return json({ statement_id: s.id, stored_sha256: s.snapshot_sha256, rebuilt_sha256: rebuilt, match: rebuilt === s.snapshot_sha256 });
    },
  },
  {
    method: "GET",
    pattern: rx(`\\/v1\\/metering\\/statements\\/${ID}\\/export\\.csv`),
    handler: ({ persona, params }) => {
      const deny = need(persona, OFFICER, BAPPROVER);
      if (deny) return deny;
      const s = state().statements.find((x) => x.id === params[0]);
      if (!s) return meteringNotFound();
      const csv = statementCsv(s);
      // Điểm kiểm soát xuất kênh report_export (observe): một dòng sổ xuất cho mỗi lần tải.
      const now = Date.now();
      const today = vnToday();
      const sameDay = state().egress.filter((e) => e.business_date === today && e.counterparty_ref === persona.me.actor_ref);
      state().egress.unshift({
        id: uuidv7(now),
        seq: 20_000 + state().egress.length,
        channel: "report_export",
        source_id: uuidv7(now),
        counterparty_kind: "actor",
        counterparty_ref: persona.me.actor_ref,
        product: "metering_statement",
        contract_version: 1,
        records: s.lines.length,
        bytes: csv.length,
        business_date: today,
        decision: "allowed",
        reason: "",
        mode: "observe",
        daily_records: sameDay.reduce((a, e) => a + e.records, 0) + s.lines.length,
        daily_quota: 0,
        anomaly: false,
        actor_ref: persona.me.actor_ref,
        event_time: isoAt(now),
        recorded_at: isoAt(now + 2),
      });
      save();
      return new Response(csv, {
        status: 200,
        headers: {
          "Content-Type": "text/csv; charset=utf-8",
          "AG-Snapshot-SHA256": s.snapshot_sha256,
          "Content-Disposition": `attachment; filename="statement-${s.period}-${s.id}.csv"`,
          "X-Trace-Id": uuidv7().replace(/-/g, ""),
        },
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/metering\/params$/,
    handler: ({ persona, url }) => {
      const deny = need(persona, ...READERS, "platform_admin");
      if (deny) return deny;
      const at = url.searchParams.get("at");
      if (at && !Number.isFinite(Date.parse(at))) return validation([{ field: "at", reason: "RFC 3339" }]);
      return json({ items: paramsList() });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/metering\/egress$/,
    handler: (ctx) => {
      const deny = need(ctx.persona, ...EGRESS_READERS);
      if (deny) return deny;
      const f = egressFilter(ctx);
      if (f instanceof Response) return f;
      // RLS: chỉ tenant của người gọi (mock: mọi persona cùng tenant TENANT).
      if (ctx.persona.me.tenant_id !== TENANT) return json({ items: [], next_cursor: null });
      const all = [...state().egress, ...egressSeed().events].filter(
        (e) => (!f.channel || e.channel === f.channel) && (!f.from || e.business_date >= f.from) && (!f.to || e.business_date <= f.to),
      );
      return json(idPage(all, ctx.url, 50, 200));
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/metering\/egress\/anomalies$/,
    handler: (ctx) => {
      const deny = need(ctx.persona, ...EGRESS_READERS);
      if (deny) return deny;
      const channel = ctx.url.searchParams.get("channel");
      if (channel && !CHANNELS.includes(channel as EgressChannel)) return validation([{ field: "channel", reason: "kênh không hợp lệ" }]);
      return json(idPage(egressSeed().anomalies.filter((a) => !channel || a.channel === channel), ctx.url, 50, 200));
    },
  },
];

/** Tên hiển thị của mã tham chiếu trong mock (khách hàng, persona G5) — chỉ chế độ giả lập. */
export const G5_REF_NAMES: Readonly<Record<string, string>> = {
  [G5_ACTORS.admin]: "Quản trị khách hàng API A (giả lập)",
  [G5_ACTORS.approver]: "Người duyệt API B (giả lập)",
  [G5_ACTORS.viewer]: "Người xem đo lường (giả lập)",
  [G5_ACTORS.other]: "Quản trị khách hàng API C (giả lập)",
  [MKG_TENANT]: "Tenant MKG (giả lập)",
};
