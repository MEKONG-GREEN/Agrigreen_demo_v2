// ============================================================================
// DỮ LIỆU GIẢ LẬP (SYNTHETIC) — chỉ dùng khi NEXT_PUBLIC_API_MOCK=1.
// Hình dạng bản ghi = kiểu SINH từ contracts/openapi (src/api/types.ts), để giao diện chạy
// giả lập giống hệt khi nối backend thật. Bản đồ CGH ở mock/mechanization.ts.
// - Tên tỉnh theo đơn vị hành chính sau sắp xếp 2025 vùng ĐBSCL; mã xã, tên xã, HTX, mã HTX,
//   ranh giới đều là DỮ LIỆU MINH HOẠ, không khớp thực tế.
// - Ranh giới là lục giác quanh tâm gần đúng — KHÔNG phải ranh giới hành chính.
// - Không có dữ liệu cá nhân thật: nhãn nông hộ là chuỗi ngẫu nhiên đã che.
// ============================================================================
import type {
  AdminBoundaryFeature,
  AdminUnit,
  CatalogItem,
  CatalogName,
  ConsentState,
  ContentDocument,
  ContentKind,
  Cooperative,
  ExtTask,
  HouseholdListItem,
  Org,
  ParamVersion,
  PartyOrgEvent,
  PlotFeature,
} from "../types";

/** PRNG có hạt giống để dữ liệu ổn định giữa các lần tải. */
export function mulberry32(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

type LngLat = [number, number];
type Position = number[];

const rnd = mulberry32(20261001);
const pick = <T,>(arr: readonly T[], r: () => number = rnd): T => arr[Math.floor(r() * arr.length)] as T;
const int = (min: number, max: number, r: () => number = rnd) => Math.floor(min + r() * (max - min + 1));
const iso = (daysAgo: number) => new Date(Date.now() - daysAgo * 86_400_000).toISOString();

export function hexId(n: number, prefix: string): string {
  const h = n.toString(16).padStart(12, "0");
  return `01927b3e-${prefix}-7000-8000-${h}`;
}

/** Cộng hai chuỗi thập phân 4 chữ số lẻ bằng số nguyên (không float, R-10). */
export function addDecimal4(a: string, b: string): string {
  const scaled = (s: string) => {
    const [i = "0", f = ""] = s.split(".");
    return BigInt(i) * 10000n + BigInt((f + "0000").slice(0, 4));
  };
  const v = scaled(a) + scaled(b);
  return `${v / 10000n}.${String(v % 10000n).padStart(4, "0")}`;
}

export interface MockProvince {
  code: string;
  name: string;
  center: LngLat;
}

export const PROVINCES: readonly MockProvince[] = [
  { code: "82", name: "Đồng Tháp", center: [105.9, 10.45] },
  { code: "91", name: "An Giang", center: [105.2, 10.25] },
  { code: "92", name: "Cần Thơ", center: [105.85, 9.85] },
  { code: "86", name: "Vĩnh Long", center: [106.38, 10.05] },
  { code: "96", name: "Cà Mau", center: [105.15, 9.2] },
  { code: "80", name: "Tây Ninh", center: [106.25, 10.97] },
];

const COMMUNE_NAMES = [
  "Mỹ An", "Tân Hòa", "Phú Thành", "Long Hưng", "Bình Thạnh", "An Phước", "Hòa Bình", "Thạnh Lợi",
  "Tân Phú", "Vĩnh Thạnh", "Mỹ Hiệp", "Phước Long", "Thới Lai", "Định Hòa", "Lợi An", "Tân Long",
  "Hưng Điền", "Bình Minh", "Phong Hòa", "Trường Xuân", "Nhơn Mỹ", "Khánh An", "Tân Thạnh", "Mỹ Phước",
];

export interface MockCommune {
  code: string;
  name: string;
  province_code: string;
  center: LngLat;
}

export const COMMUNES: readonly MockCommune[] = PROVINCES.flatMap((p, pi) => {
  const n = 4;
  return Array.from({ length: n }, (_, i) => {
    const angle = (i / n) * Math.PI * 2 + pi;
    return {
      code: `${p.code}${String(101 + i).padStart(3, "0")}`,
      name: `Xã ${COMMUNE_NAMES[(pi * n + i) % COMMUNE_NAMES.length]}`,
      province_code: p.code,
      center: [p.center[0] + Math.cos(angle) * 0.13, p.center[1] + Math.sin(angle) * 0.12] as LngLat,
    };
  });
});

export function hexagon(center: LngLat, r: number): Position[][] {
  const ring: Position[] = [];
  for (let i = 0; i < 6; i++) {
    const a = (Math.PI / 3) * i + Math.PI / 6;
    ring.push([+(center[0] + Math.cos(a) * r).toFixed(5), +(center[1] + Math.sin(a) * r * 0.92).toFixed(5)]);
  }
  ring.push(ring[0] as Position);
  return [ring];
}

// --------------------------------------------------------------------------
// pf/masterdata — đơn vị hành chính (GET /v1/masterdata/admin-units)
// --------------------------------------------------------------------------
export const ADMIN_UNITS: AdminUnit[] = [
  ...PROVINCES.map((p): AdminUnit => ({
    code: p.code,
    name: p.name,
    level: "province",
    parent_code: null,
    valid_from: "2025-07-01",
    valid_to: null,
    successor_codes: [],
  })),
  ...COMMUNES.map((c): AdminUnit => ({
    code: c.code,
    name: c.name,
    level: "commune",
    parent_code: c.province_code,
    valid_from: "2025-07-01",
    valid_to: null,
    successor_codes: [],
  })),
];

/** GET /v1/geo/admin-boundaries/{code} — null khi "chưa nhập ranh giới" (geo.admin_boundary_not_found). */
/** Ranh giới kèm `parent_code` (dạng phần tử của GET /v1/geo/admin-boundaries?level=). */
export type MockBoundary = Omit<AdminBoundaryFeature, "properties"> & { properties: AdminBoundaryFeature["properties"] & { parent_code: string | null } };

export function boundaryOf(code: string): MockBoundary | null {
  const p = PROVINCES.find((x) => x.code === code);
  const c = p ? undefined : COMMUNES.find((x) => x.code === code);
  if (!p && !c) return null;
  const level = p ? "province" : "commune";
  const center = (p ?? c)!.center;
  return {
    type: "Feature",
    id: code,
    geometry: { type: "Polygon", coordinates: hexagon(center, p ? 0.26 : 0.065) },
    properties: {
      code,
      level,
      name: (p ?? c)!.name,
      parent_code: c?.province_code ?? null,
      source: "Ranh giới minh hoạ (giả lập)",
      area_ha: p ? "312480.5000" : "4120.2500",
      imported_at: iso(30),
    },
  };
}

// --------------------------------------------------------------------------
// pf/party — tổ chức, HTX, nông hộ
// --------------------------------------------------------------------------
export const NATIONAL_REF = hexId(1, "0e90");
export const DCRD_REF = hexId(2, "0e90");
export const MKG_REF = hexId(3, "0e90");
export const provinceOrgRef = (pi: number) => hexId(100 + pi, "0e90");
export const communeOrgRef = (pi: number, ci: number) => hexId(1000 + pi * 10 + ci, "0e90");

const COOP_PATTERNS = [
  "HTX Nông nghiệp {x}",
  "HTX Lúa chất lượng cao {x}",
  "HTX Dịch vụ nông nghiệp {x}",
  "HTX Sản xuất lúa giống {x}",
  "Tổ hợp tác lúa {x}",
];

/** Thông tin phụ chỉ mock cần. `api` gồm cả trường đọc bổ sung G-03 backend trả. */
export interface MockCooperative {
  api: Cooperative;
  province_code: string;
  commune_name: string;
  province_name: string;
  member_households: number;
  plots: number;
  lnglat: LngLat;
}

export const COOPERATIVES: MockCooperative[] = COMMUNES.flatMap((c, ci) => {
  const count = 1 + (ci % 3);
  const pi = PROVINCES.findIndex((p) => p.code === c.province_code);
  const province = PROVINCES[pi];
  return Array.from({ length: count }, (_, k): MockCooperative => {
    const n = ci * 3 + k + 1;
    const pattern = COOP_PATTERNS[(ci + k) % COOP_PATTERNS.length] ?? "HTX {x}";
    const place = c.name.replace(/^Xã /, "");
    const lnglat: LngLat = [c.center[0] + (rnd() - 0.5) * 0.08, c.center[1] + (rnd() - 0.5) * 0.08];
    const active = n % 11 !== 0;
    const provisional = n % 9 === 0;
    const members = int(25, 340);
    const plots = int(8, 60);
    return {
      province_code: c.province_code,
      commune_name: c.name,
      province_name: province?.name ?? "",
      member_households: members,
      plots,
      lnglat,
      api: {
        province_code: c.province_code,
        commune_name: c.name,
        province_name: province?.name ?? null,
        member_households: members,
        digitized_plots: c.province_code === "80" ? null : plots - Math.floor(plots / 13) - Math.floor(plots / 7),
        ref: hexId(n, "c00b"),
        kind: "cooperative",
        code: `HTX${c.province_code}${String(n).padStart(5, "0")}`,
        name: pattern.replace("{x}", k === 0 ? place : `${place} ${k + 1}`),
        admin_unit_code: c.code,
        parent_ref: communeOrgRef(pi, ci % 4),
        representative_ref: null,
        active,
        status_reason: active ? null : "Tạm ngừng hoạt động theo đề nghị của HTX (giả lập)",
        // D2: một phần HTX là HTX tạm do Khuyến nông lập — "Chưa xác nhận" tới khi HTX thật nhận quyền.
        confirmation_status: n % 4 === 0 ? "unconfirmed" : "confirmed",
        confirmation_label: n % 4 === 0 ? "Chưa xác nhận" : "Đã xác nhận",
        tax_code: `${1800000000 + n * 7919}`.slice(0, 10),
        area_ha: `${int(80, 950)}.${String(int(0, 9999)).padStart(4, "0")}`,
        established_on: `20${int(10, 23)}-0${int(1, 9)}-1${int(0, 9)}`,
        location: provisional ? null : { lat: lnglat[1].toFixed(6), lon: lnglat[0].toFixed(6) },
        location_provisional: provisional,
      },
    };
  });
});

export function findCoop(ref: string | undefined): MockCooperative | undefined {
  return COOPERATIVES.find((c) => c.api.ref === ref);
}

const SURNAMES = ["Nguyễn", "Trần", "Lê", "Phạm", "Huỳnh", "Võ", "Đặng", "Bùi"];
const INITIALS = "ABCDĐGHKLMNPQSTVX";

/** Số nông hộ giả lập của HTX (householdsOf) và mã nông hộ thứ i (vòng). */
export function householdCount(coop: MockCooperative): number {
  return Math.min(coop.member_households, 57);
}
export function householdRefOf(coop: MockCooperative, i: number): string {
  return hexId(parseInt(coop.api.ref.slice(-4), 16) * 1000 + (i % householdCount(coop)), "a0e0");
}

/** SĐT chủ hộ TỔNG HỢP (seed-synthetic) dạng 09990xxxxx — không phải số thật; chỉ mock pf/accounts dùng để khớp. */
export function syntheticPhone(coop: MockCooperative, index: number): string {
  const base = (parseInt(coop.api.ref.slice(-3), 16) % 900) * 100;
  return `09990${String(base + index).padStart(5, "0")}`;
}

/**
 * HTX của persona "Quản lý HTX" (coop_manager): HTX đang hoạt động đầu tiên ở xã của KNV xã giả lập
 * (xã đầu tiên của Đồng Tháp) — để KNV xã thấy cùng dữ liệu mùa vụ ở chế độ chỉ đọc.
 */
export function htxCoop(): MockCooperative {
  const commune = COMMUNES.find((c) => c.province_code === "82");
  return COOPERATIVES.find((c) => c.api.active && c.api.admin_unit_code === commune?.code) ?? COOPERATIVES.find((c) => c.api.active)!;
}

/** GET /v1/cooperatives/{ref}/households — nhãn ĐÃ CHE sinh ngẫu nhiên, không tương ứng người thật. */
export function householdsOf(coop: MockCooperative): HouseholdListItem[] {
  const r = mulberry32(parseInt(coop.api.ref.slice(-6), 16));
  const n = householdCount(coop);
  return Array.from({ length: n }, (_, i): HouseholdListItem => {
    const unavailable = r() < 0.04;
    const a = INITIALS[Math.floor(r() * INITIALS.length)] ?? "V";
    const b = INITIALS[Math.floor(r() * INITIALS.length)] ?? "A";
    // G-04: trạng thái đồng ý theo mục đích — 1/3 đồng ý MRV, một ít đã rút, còn lại chưa có bản ghi.
    const consents: ConsentState[] =
      i % 3 === 0
        ? [{ purpose: "mrv.evidence", status: "granted", event_time: iso(40 - (i % 20)) }]
        : i % 7 === 1
          ? [{ purpose: "mrv.evidence", status: "withdrawn", event_time: iso(5) }]
          : [];
    if (i % 4 === 0) consents.push({ purpose: "extension.field_support", status: "granted", event_time: iso(60) });
    return {
      consents,
      household_ref: householdRefOf(coop, i),
      cooperative_ref: coop.api.ref,
      commune_code: coop.api.admin_unit_code ?? "",
      cultivated_area_ha: r() < 0.05 ? null : `${(0.3 + r() * 4.5).toFixed(4)}`,
      member_since: `202${Math.floor(r() * 6)}-0${1 + Math.floor(r() * 9)}-15`,
      active: r() > 0.06,
      label: unavailable ? "" : `${pick(SURNAMES, r)} ${a}. ${b}.`,
      masked_phone: `09******${syntheticPhone(coop, i).slice(-2)}`,
      label_unavailable: unavailable,
    };
  });
}

/** GET /v1/cooperatives/{ref}/plots — PlotFeature quanh vị trí HTX. */
export function plotsOf(coop: MockCooperative): PlotFeature[] {
  const r = mulberry32(parseInt(coop.api.ref.slice(-5), 16) + 7);
  return Array.from({ length: coop.plots }, (_, i): PlotFeature => {
    const ref = hexId(parseInt(coop.api.ref.slice(-4), 16) * 1000 + i, "b10f");
    const status = i % 13 === 12 ? "retired" : i % 7 === 6 ? "draft" : "active";
    const center: LngLat = [coop.lnglat[0] + (r() - 0.5) * 0.02, coop.lnglat[1] + (r() - 0.5) * 0.02];
    return {
      type: "Feature",
      id: ref,
      geometry: { type: "Polygon", coordinates: hexagon(center, 0.0012) },
      properties: {
        ref,
        code: `TH${String(parseInt(coop.api.ref.slice(-3), 16) * 100 + i).padStart(7, "0")}`,
        name: i % 3 === 0 ? `Thửa ${i + 1} ấp ${1 + (i % 5)}` : null,
        commune_code: coop.api.admin_unit_code ?? "",
        cooperative_ref: status === "draft" ? null : coop.api.ref,
        // Thửa đang dùng thuộc một nông hộ của HTX (vòng theo householdsOf) — App HTX mở vụ theo nông hộ.
        household_ref: status === "active" ? householdRefOf(coop, i) : null,
        status,
        status_reason: status === "retired" ? "Chuyển mục đích sử dụng đất (giả lập)" : null,
        version: 1 + (i % 3),
        boundary_id: hexId(parseInt(coop.api.ref.slice(-4), 16) * 1000 + i, "b0dd"),
        area_ha: (0.4 + r() * 5).toFixed(4),
        source: pick(["kn_web", "kn_field_app", "htx_app", "import_file"] as const, r),
        needs_review: i % 17 === 5,
        revision: 1 + (i % 4),
        created_at: iso(60 + i),
        updated_at: iso(Math.floor(r() * 40)),
      },
    };
  });
}

/** GET /v1/orgs — danh sách phẳng, client dựng cây theo parent_ref. */
export const ORGS: Org[] = [
  { ref: NATIONAL_REF, kind: "ttkn_national", code: "TTKNQG", name: "Trung tâm Khuyến nông Quốc gia", admin_unit_code: null, parent_ref: null, representative_ref: null, active: true },
  { ref: hexId(2, "0e90"), kind: "dcrd", code: "CUC-KTHT", name: "Cục Kinh tế hợp tác và PTNT", admin_unit_code: null, parent_ref: null, representative_ref: null, active: true },
  { ref: hexId(3, "0e90"), kind: "mkg", code: "MKG", name: "Mekong Green", admin_unit_code: null, parent_ref: null, representative_ref: null, active: true },
  ...PROVINCES.flatMap((p, pi): Org[] => [
    { ref: provinceOrgRef(pi), kind: "ttkn_province", code: `TTKN-${p.code}`, name: `TTKN tỉnh ${p.name}`, admin_unit_code: p.code, parent_ref: NATIONAL_REF, representative_ref: null, active: true },
    ...COMMUNES.filter((c) => c.province_code === p.code).map((c, ci): Org => ({
      ref: communeOrgRef(pi, ci),
      kind: "extension_team",
      code: `TKN-${c.code}`,
      name: `Tổ khuyến nông cộng đồng ${c.name}`,
      admin_unit_code: c.code,
      parent_ref: provinceOrgRef(pi),
      representative_ref: null,
      active: !(pi === 5 && ci === 3),
    })),
  ]),
  ...COOPERATIVES.map((c): Org => {
    const { ref, kind, code, name, admin_unit_code, parent_ref, representative_ref, active } = c.api;
    return { ref, kind, code, name, admin_unit_code, parent_ref, representative_ref, active };
  }),
];

const ADMIN_ACTOR = "01927b3e-ad00-7000-8000-000000000001";

/** Admin GET /v1/orgs/{ref}/events — sổ biến động (mock ghi thêm khi đổi tên). */
export const ORG_EVENTS = new Map<string, PartyOrgEvent[]>();

export function orgEvents(org: Org): PartyOrgEvent[] {
  let list = ORG_EVENTS.get(org.ref);
  if (!list) {
    list = [
      {
        seq: 1,
        action: "created",
        event_time: iso(400),
        recorded_at: iso(400),
        actor_ref: ADMIN_ACTOR,
        reason: null,
        name: org.name,
        admin_unit_code: org.admin_unit_code,
        parent_ref: org.parent_ref,
        prev_parent_ref: null,
        prev_admin_unit_code: null,
        active: true,
      },
    ];
    if (!org.active) {
      list.push({ ...list[0]!, seq: 2, action: "deactivated", event_time: iso(20), recorded_at: iso(20), reason: "Ngừng hoạt động (giả lập)", active: false });
    }
    ORG_EVENTS.set(org.ref, list);
  }
  return list;
}

// --------------------------------------------------------------------------
// pf/extension — nhiệm vụ hỗ trợ
// --------------------------------------------------------------------------
const TASK_TEXT: { category: ExtTask["category"]; text: string }[] = [
  { category: "ky_thuat", text: "Hỏi về liều lượng phân đạm giai đoạn đẻ nhánh cho giống OM18" },
  { category: "sau_benh", text: "Phản ánh rầy nâu mật độ cao trên ruộng lúa 35 ngày tuổi" },
  { category: "ky_thuat", text: "Hỗ trợ vẽ lại ranh giới thửa sau sáp nhập ấp" },
  { category: "khac", text: "Hỏi lịch xuống giống vụ Thu Đông theo khuyến cáo của tỉnh" },
  { category: "co_gioi", text: "Cần hỗ trợ điều phối máy gặt đập liên hợp cho 40 ha" },
  { category: "sau_benh", text: "Phản ánh ốc bươu vàng gây hại sau sạ tại cánh đồng lớn" },
  { category: "ky_thuat", text: "Hướng dẫn cài app hiện trường cho tổ hợp tác mới" },
  { category: "khac", text: "Hỏi cách xử lý rơm rạ không đốt đồng sau thu hoạch" },
  { category: "co_gioi", text: "Đề nghị kiểm tra thiết bị bay phun thuốc trước vụ" },
  { category: "sau_benh", text: "Hỏi dấu hiệu bệnh đạo ôn cổ bông và thời điểm phun" },
];
const SLA_HOURS = "24";

export const OFFICER_REFS = ["01927b3e-0ff1-7000-8000-000000000001", "01927b3e-0ff1-7000-8000-000000000002", "01927b3e-0ff1-7000-8000-000000000003"];
/** Tên cán bộ trong danh bạ trực (giả lập, không phải người thật). */
export const OFFICER_NAMES: Record<string, string> = {
  [OFFICER_REFS[0]!]: "KNV Trần Thị C (giả lập)",
  [OFFICER_REFS[1]!]: "KNV Lê Văn D (giả lập)",
  [OFFICER_REFS[2]!]: "KNV Phạm Thị E (giả lập)",
};

export const TASKS: ExtTask[] = Array.from({ length: 36 }, (_, i): ExtTask => {
  const t = TASK_TEXT[i % TASK_TEXT.length] ?? TASK_TEXT[0]!;
  const coop = COOPERATIVES[(i * 7) % COOPERATIVES.length]!;
  const ageH = int(1, 60);
  const created = Date.now() - ageH * 3_600_000;
  const due = created + Number(SLA_HOURS) * 3_600_000;
  const status: ExtTask["status"] = i % 6 === 5 ? "resolved" : i % 5 === 1 ? "in_progress" : i % 4 === 2 ? "accepted" : i % 3 === 0 ? "new" : "assigned";
  const priority = t.category === "sau_benh" ? "urgent" : pick(["low", "normal", "normal", "high"]);
  const resolvedAt = status === "resolved" ? created + int(2, 30) * 3_600_000 : null;
  const overdue = resolvedAt ? resolvedAt > due : Date.now() > due;
  const assignee = status === "new" ? null : pick(OFFICER_REFS);
  return {
    assignee_name: assignee ? (OFFICER_NAMES[assignee] ?? null) : null,
    cooperative_name: coop.api.name,
    commune_name: coop.commune_name,
    task_id: hexId(5000 + i, "7a5c"),
    commune_code: coop.api.admin_unit_code ?? "",
    province_code: coop.province_code,
    cooperative_ref: coop.api.ref,
    household_ref: null,
    plot_ref: null,
    category: t.category,
    description: t.text,
    priority,
    source: pick(["officer", "coop_manager", "htx_app"] as const),
    status,
    assignee_ref: assignee,
    created_by: ADMIN_ACTOR,
    created_at: new Date(created).toISOString(),
    sla_hours: SLA_HOURS,
    due_at: new Date(due).toISOString(),
    overdue,
    escalated_at: null,
    escalated_unit: null,
    escalation_org_ref: null,
    resolved_at: resolvedAt ? new Date(resolvedAt).toISOString() : null,
    last_note: null,
    version: 1 + (i % 4),
  };
});

// --------------------------------------------------------------------------
// pf/content — thư viện số (chỉ tài liệu đã xuất bản)
// --------------------------------------------------------------------------
const DOCS: { title: string; kind: ContentKind; category: string | null; summary: string; tags: string[]; province?: string }[] = [
  { title: "Quy trình canh tác lúa chất lượng cao, phát thải thấp vùng ĐBSCL", kind: "procedure", category: "technique", summary: "Các bước làm đất, gieo sạ, bón phân, tưới và thu hoạch theo hướng giảm phát thải.", tags: ["farm_stage:gieo_sa"] },
  { title: "Hướng dẫn kỹ thuật tưới ướt khô xen kẽ (AWD)", kind: "guide", category: "technique", summary: "Cách đặt ống quan trắc mực nước, thời điểm rút nước và cho nước vào lại.", tags: ["farm_stage:cham_soc"] },
  { title: "Hướng dẫn quản lý rơm rạ sau thu hoạch", kind: "guide", category: "technique", summary: "Thu gom, cuộn rơm, ủ phân hữu cơ; không đốt đồng.", tags: ["farm_stage:sau_thu_hoach"] },
  { title: "Cảnh báo rầy nâu di trú cuối vụ Hè Thu", kind: "alert", category: "pest_warning", summary: "Mật độ rầy tăng tại các xã ven sông; kiểm tra đồng ruộng 3 ngày/lần.", tags: ["pest:ray_nau"] },
  { title: "Quy trình “1 Phải 5 Giảm” trong sản xuất lúa", kind: "procedure", category: "technique", summary: "Phải dùng giống xác nhận; giảm giống, phân đạm, thuốc BVTV, nước tưới, thất thoát sau thu hoạch.", tags: [] },
  { title: "Hướng dẫn nhận biết và phòng trừ bệnh đạo ôn", kind: "guide", category: "pest_warning", summary: "Điều kiện phát sinh, triệu chứng, biện pháp canh tác và hoá học.", tags: ["pest:dao_on"] },
  { title: "Bổ sung đặc thù: lịch xuống giống vùng ngập sâu Đồng Tháp", kind: "supplement", category: "technique", summary: "Khung lịch né lũ cho các xã vùng trũng.", tags: ["season:TD"], province: "82" },
  { title: "Bổ sung đặc thù: ứng phó xâm nhập mặn vụ Đông Xuân tại Cà Mau", kind: "supplement", category: "technique", summary: "Theo dõi độ mặn, lịch đóng mở cống, giống chịu mặn.", tags: ["season:DX"], province: "96" },
  { title: "Chính sách hỗ trợ cơ giới hoá khâu thu hoạch năm 2026", kind: "news", category: "policy", summary: "Tóm tắt điều kiện, mức hỗ trợ và hồ sơ đề nghị.", tags: [] },
  { title: "Giá lúa tươi tại ruộng tuần 38", kind: "news", category: "market", summary: "Bảng giá tham khảo theo giống tại một số tỉnh.", tags: ["rice_variety:OM18"] },
  { title: "Hướng dẫn ghi nhật ký canh tác điện tử cho HTX", kind: "guide", category: "technique", summary: "Ghi nhật ký trên app hiện trường, chụp ảnh minh chứng, đồng bộ khi có mạng.", tags: [] },
  { title: "Quy trình sản xuất lúa giống cấp xác nhận", kind: "procedure", category: "technique", summary: "Chọn ruộng, khử lẫn, thu hoạch và bảo quản giống.", tags: [] },
];

export const DOCUMENTS: ContentDocument[] = DOCS.map((d, i): ContentDocument => ({
  ref: hexId(7000 + i, "d0c0"),
  kind: d.kind,
  audience: d.province ? { level: "province", units: [d.province], province_code: d.province } : { level: "national", units: [], province_code: null },
  parent_ref: d.kind === "supplement" ? hexId(7000, "d0c0") : null,
  status: "published",
  title: d.title,
  summary: d.summary,
  category: d.category,
  tags: d.tags,
  published_version_no: 1 + (i % 4),
  published_at: iso(i * 9 + 3),
  publisher_ref: ADMIN_ACTOR,
  retired_at: null,
  retire_reason: null,
}));

// --------------------------------------------------------------------------
// pf/masterdata — danh mục & tham số (theo seeddata của backend)
// --------------------------------------------------------------------------
const cat = (catalog: CatalogName, rows: [string, string, Record<string, string>, boolean?][]): CatalogItem[] =>
  rows.map(([code, name, attrs, inactive]) => ({
    catalog,
    code,
    name,
    attrs,
    active: !inactive,
    deactivation_reason: inactive ? "Ngừng khuyến cáo theo văn bản của Cục Trồng trọt (giả lập)" : null,
    revision: inactive ? 2 : 1,
    updated_at: iso(inactive ? 30 : 200),
  }));

export const CATALOGS: Record<CatalogName, CatalogItem[]> = {
  rice_variety: cat("rice_variety", [
    ["OM5451", "OM5451", { growth_days: "95", expected_yield_t_per_ha: "6.75" }],
    ["OM18", "OM18", { growth_days: "97", expected_yield_t_per_ha: "6.75" }],
    ["DT8", "Đài Thơm 8", { growth_days: "100", expected_yield_t_per_ha: "6.5" }],
    ["ST25", "ST25 (lúa đặc sản)", { growth_days: "105", expected_yield_t_per_ha: "5.5" }],
    ["IR50404", "IR50404", { growth_days: "90", expected_yield_t_per_ha: "7.25" }, true],
  ]),
  farm_stage: cat("farm_stage", [
    ["lam_dat", "Làm đất", { pilot: "true" }],
    ["gieo_sa", "Gieo sạ / Cấy", { pilot: "true" }],
    ["cham_soc", "Chăm sóc", { pilot: "true" }],
    ["thu_hoach", "Thu hoạch", { pilot: "true" }],
    ["sau_thu_hoach", "Sau thu hoạch", { pilot: "false" }],
  ]),
  machine_type: cat("machine_type", [
    ["MLD", "Máy làm đất (máy cày/xới)", { farm_stage: "lam_dat" }],
    ["MGS", "Máy gieo sạ / cấy", { farm_stage: "gieo_sa" }],
    ["MPT", "Máy phun thuốc / drone", { farm_stage: "cham_soc" }],
    ["MGD", "Máy gặt đập liên hợp", { farm_stage: "thu_hoach" }],
    ["MCR", "Máy cuộn rơm", { farm_stage: "sau_thu_hoach" }],
    ["MSL", "Máy sấy lúa", { farm_stage: "sau_thu_hoach" }],
  ]),
  season: cat("season", [
    ["DX", "Đông Xuân", { start_month: "11", end_month: "3" }],
    ["HT", "Hè Thu", { start_month: "4", end_month: "8" }],
    ["TD", "Thu Đông", { start_month: "8", end_month: "12" }],
  ]),
  pest: cat("pest", [
    ["ray_nau", "Rầy nâu", { kind: "sau_hai" }],
    ["sau_cuon_la", "Sâu cuốn lá nhỏ", { kind: "sau_hai" }],
    ["dao_on", "Bệnh đạo ôn", { kind: "benh_hai" }],
    ["oc_buou_vang", "Ốc bươu vàng", { kind: "dong_vat_hai" }],
  ]),
  vessel_class: cat("vessel_class", []),
  // KN v7.7 FN-AD-04.05 (gói P2) — dữ liệu nền tạm (Q-01).
  crop: cat("crop", [
    ["RAU_MAU", "Rau màu", { is_group: "true" }],
    ["RAU_MUONG", "Rau muống", { parent_code: "RAU_MAU", is_group: "false", unit: "tấn" }],
  ]),
  // Mã khớp sổ vật tư giả lập (kn-collect.ts MATERIALS đọc từ đây — BF-TT-MAT #105).
  material: cat("material", [
    ["phan_ure", "Phân urê", { unit: "tấn", group: "Phân bón" }],
    ["phan_npk", "Phân NPK", { unit: "tấn", group: "Phân bón" }],
    ["thuoc_bvtv", "Thuốc BVTV", { unit: "lít", group: "Thuốc BVTV" }],
    ["bay_den", "Bẫy đèn", { unit: "cái", group: "Thiết bị" }],
    ["giong_lua_cu", "Giống lúa (ngừng dùng)", { unit: "kg", group: "Giống" }, true],
  ]),
  production_model: cat("production_model", [["1P5G", "1 phải 5 giảm", {}]]),
  report_type: cat("report_type", [
    ["tuan", "Báo cáo tuần", { period: "week", level: "commune" }],
    ["dot_xuat", "Báo cáo đột xuất", { period: "ad_hoc", level: "both" }],
  ]),
  // W11.41 BRD CNTS v3.7 FN-06 — theo seeddata cnts_* của backend (rút gọn).
  cnts_species: cat("cnts_species", [
    ["HEO", "Heo", { group: "gia_suc", unit: "con", herd_column: "true", order: "1" }],
    ["TRAU", "Trâu", { group: "gia_suc", unit: "con", herd_column: "true", order: "3" }],
    ["BO", "Bò", { group: "gia_suc", unit: "con", herd_column: "true", order: "4" }],
    ["VIT", "Vịt", { group: "gia_cam", unit: "con", herd_column: "true", order: "5" }],
    ["GA", "Gà", { group: "gia_cam", unit: "con", herd_column: "true", order: "6" }],
    ["CHO_MEO", "Chó, mèo", { group: "thu_cung", unit: "con", herd_column: "true", order: "7" }],
  ]),
  cnts_breed: cat("cnts_breed", []),
  cnts_disease: cat("cnts_disease", [
    ["ASF", "Dịch tả heo Châu Phi", { species: "HEO", notifiable: "true" }],
    ["VDNC", "Viêm da nổi cục", { species: "BO,TRAU", notifiable: "true" }],
    ["CUM_GIA_CAM", "Cúm gia cầm", { species: "GA,VIT", notifiable: "true" }],
    ["NEWCASTLE", "Newcastle", { species: "GA" }],
  ]),
  cnts_vaccine: cat("cnts_vaccine", [
    ["VX_ASF", "Tả heo Châu Phi", { diseases: "ASF", species: "HEO", report_group: "heo", order: "1" }],
    ["VX_VDNC_BO", "Viêm da nổi cục bò", { diseases: "VDNC", species: "BO,TRAU", report_group: "bo", order: "5" }],
    ["VX_CUM_GIA_CAM", "Cúm gia cầm (H5N1)", { diseases: "CUM_GIA_CAM", species: "GA,VIT", report_group: "cum_gia_cam", order: "8" }],
    ["VX_ND_F", "Newcastle chủng F", { diseases: "NEWCASTLE", species: "GA", report_group: "khac", order: "20" }],
  ]),
  cnts_vaccination_schedule: cat("cnts_vaccination_schedule", [
    ["QT_GA_THUONG_PHAM", "Quy trình tiêm gà thương phẩm", { model_type: "GA_THUONG_PHAM", doses: "VX_ND_F/1@3,VX_CUM_GIA_CAM/1@17,VX_CUM_GIA_CAM/2@40" }],
  ]),
  cnts_chemical: cat("cnts_chemical", [["HC_GIA_SUC", "Hóa chất khử trùng – gia súc", { unit: "lít", target: "gia_suc" }]]),
  cnts_slaughter_facility: cat("cnts_slaughter_facility", [
    [
      "CS_BINH_DUC",
      "Cơ sở giết mổ Bình Đức (giả lập)",
      { facility_ref: "0190f1c4-7d3e-7000-8000-00000000c501", commune_code: "30292", facility_kind: "tap_trung", stamp_codes_gs: "91.589.05,91.589.05.1", species: "HEO,TRAU" },
    ],
  ]),
  cnts_aquaculture_object: cat("cnts_aquaculture_object", [["CA_TRA", "Cá tra", { scale_unit: "ha" }]]),
  cnts_fishing_group: cat("cnts_fishing_group", [
    ["CA", "Cá", { unit: "tấn", order: "1" }],
    ["TOM", "Tôm", { unit: "tấn", order: "2" }],
    ["MUC", "Mực", { unit: "tấn", order: "3" }],
    ["KHAC", "Khác", { unit: "tấn", order: "4" }],
  ]),
  cnts_indicator: cat("cnts_indicator", [
    ["PH", "pH", { value_type: "decimal" }],
    ["DO", "Oxy hòa tan", { unit: "mg/l", value_type: "decimal" }],
  ]),
  cnts_model_type: cat("cnts_model_type", [["GA_THUONG_PHAM", "Gà thương phẩm", { sector: "chan_nuoi", species: "GA" }]]),
  cnts_document_type: cat("cnts_document_type", [["QUY_TRINH", "Quy trình kỹ thuật", {}]]),
};

/** Tên cán bộ quản trị theo actor_ref (danh bạ pf/extension, G-07). Không có → null (web hiện mã). */
export const ACTOR_NAMES: Record<string, string> = {
  "mock-admin-setter": "Quản trị viên A (giả lập)",
  "mock-admin-approver": "Quản trị viên B (giả lập)",
  "mock-admin-other": "Chuyên viên TTKNQG (giả lập)",
};

function version(p: Partial<ParamVersion> & Pick<ParamVersion, "key" | "value" | "unit" | "version">): ParamVersion {
  const setter = p.setter_ref ?? "mock-admin-other";
  const approver = p.approver_ref === undefined ? "mock-admin-approver" : p.approver_ref;
  return {
    setter_name: ACTOR_NAMES[setter] ?? null,
    approver_name: approver ? (ACTOR_NAMES[approver] ?? null) : null,
    id: hexId(9000 + Math.floor(rnd() * 9000), "9a7a"),
    text: null,
    effective_from: "2026-01-01T00:00:00+07:00",
    source_doc: "QĐ số 128/QĐ-KTHT ngày 15/01/2026 (minh hoạ)",
    correction_reason: null,
    status: "approved",
    setter_ref: "mock-admin-other",
    submitted_at: iso(262),
    approver_ref: "mock-admin-approver",
    decided_at: iso(260),
    decision_note: null,
    created_at: iso(263),
    ...p,
  };
}

/** Admin GET /v1/masterdata/params/versions — mọi phiên bản, mọi trạng thái. */
export const PARAM_VERSIONS: ParamVersion[] = [
  version({ key: "cgh.coverage.sufficient_min_pct", value: "85", unit: "%", version: 1 }),
  version({ key: "cgh.coverage.attention_min_pct", value: "60", unit: "%", version: 1 }),
  version({ key: "cgh.coverage.surplus_min_pct", value: "120", unit: "%", version: 1 }),
  version({ key: "cgh.norm_ha_per_machine:MGD:thu_hoach", value: "90", unit: "ha/máy/vụ", version: 1 }),
  version({ key: "cgh.norm_ha_per_machine:MPT:cham_soc", value: "260", unit: "ha/máy/vụ", version: 1 }),
  version({ key: "extension.task_sla_hours", value: "24", unit: "giờ", version: 1, effective_from: "2026-07-01T00:00:00+07:00", source_doc: "Quyết định TTKNQG (giả lập) 12/QĐ" }),
  // Ngưỡng lấp đầy kho/Hub đã duyệt (GIS US-HUB-01) — khác mặc định nhúng 80/95 để minh hoạ nguồn "Đã duyệt".
  version({ key: "geo.hub_capacity_warn_pct", value: "60", unit: "%", version: 1, source_doc: "QĐ quy chuẩn hiển thị GIS (giả lập) 07/QĐ" }),
  version({ key: "geo.hub_capacity_full_pct", value: "85", unit: "%", version: 1, source_doc: "QĐ quy chuẩn hiển thị GIS (giả lập) 07/QĐ" }),
  // W11.41 ngưỡng mặc định CNTS (seed pf/masterdata).
  version({ key: "cnts.indicator_min:PH", value: "7.5", unit: "", version: 1, source_doc: "BRD CNTS v3.7 Mục III.5 — ngưỡng mặc định (W11.41)" }),
  version({ key: "cnts.indicator_max:PH", value: "8.5", unit: "", version: 1, source_doc: "BRD CNTS v3.7 Mục III.5 — ngưỡng mặc định (W11.41)" }),
  version({ key: "cnts.indicator_min:DO", value: "4", unit: "mg/l", version: 1, source_doc: "BRD CNTS v3.7 Mục III.5 — ngưỡng mặc định (W11.41)" }),
  version({ key: "cnts.herd_change_pct", value: "15", unit: "%", version: 1, source_doc: "BRD CNTS v3.7 Mục III.5 — ngưỡng mặc định (W11.41)" }),
  version({ key: "cnts.fishing_change_pct", value: "50", unit: "%", version: 1, source_doc: "BRD CNTS v3.7 Mục III.5 — ngưỡng mặc định (W11.41)" }),
  version({ key: "cnts.slaughter_spike_factor", value: "3", unit: "lần", version: 1, source_doc: "BRD CNTS v3.7 Mục III.5 — ngưỡng mặc định (W11.41)" }),
  version({ key: "cnts.slaughter_spike_min_days", value: "7", unit: "ngày", version: 1, source_doc: "BRD CNTS v3.7 Mục III.5 — ngưỡng mặc định (W11.41)" }),
  version({ key: "cnts.lookback_periods", value: "4", unit: "kỳ", version: 1, source_doc: "BRD CNTS v3.7 Mục III.5 — ngưỡng mặc định (W11.41)" }),
  version({ key: "cnts.location_distance_m", value: "200", unit: "m", version: 1, source_doc: "BRD CNTS v3.7 Mục III.5 — ngưỡng mặc định (W11.41)" }),
  version({ key: "cnts.gps_accuracy_m", value: "50", unit: "m", version: 1, source_doc: "BRD CNTS v3.7 Mục III.5 — ngưỡng mặc định (W11.41)" }),
  version({ key: "cnts.log_overdue_grace_days", value: "1", unit: "ngày", version: 1, source_doc: "BRD CNTS v3.7 Mục III.5 — ngưỡng mặc định (W11.41)" }),
  // Chờ duyệt do "người đặt" (persona setter) gửi — người này KHÔNG được tự duyệt (R-56).
  version({
    key: "cgh.norm_ha_per_machine:MGD:thu_hoach",
    value: "85",
    unit: "ha/máy/vụ",
    version: 2,
    effective_from: "2026-11-01T00:00:00+07:00",
    source_doc: "Văn bản (giả lập) số 21/2026",
    status: "submitted",
    setter_ref: "mock-admin-setter",
    submitted_at: iso(1),
    approver_ref: null,
    decided_at: null,
    created_at: iso(1),
  }),
  // Chờ duyệt do người khác gửi — persona setter/approver thử duyệt được.
  version({
    key: "extension.task_sla_hours",
    value: "36",
    unit: "giờ",
    version: 2,
    effective_from: "2026-11-01T00:00:00+07:00",
    source_doc: "Công văn TTKNQG (giả lập) 45/CV",
    status: "submitted",
    setter_ref: "mock-admin-other",
    submitted_at: iso(2),
    approver_ref: null,
    decided_at: null,
    created_at: iso(2),
  }),
];

// --------------------------------------------------------------------------
// Tổng quan (GET /v1/extension/dashboard)
// --------------------------------------------------------------------------
/** Xã chưa gửi báo cáo kỳ này → KPI "Số liệu tạm tính" (provisional). */
export const UNREPORTED_COMMUNES = new Set<string>(["82104", "80101", "80102", "80103", "80104"]);

// --------------------------------------------------------------------------
// G-06 / G-07 — danh sách danh mục và sổ đăng ký tham số (Admin API)
// --------------------------------------------------------------------------
export const CATALOG_META: Record<CatalogName, { name: string; attrs_schema: string[] }> = {
  rice_variety: { name: "Giống lúa", attrs_schema: ["growth_days", "expected_yield_t_per_ha"] },
  farm_stage: { name: "Khâu sản xuất", attrs_schema: ["pilot", "description"] },
  machine_type: { name: "Chủng loại máy", attrs_schema: ["farm_stage"] },
  season: { name: "Mùa vụ", attrs_schema: ["start_month", "end_month"] },
  pest: { name: "Sâu bệnh hại", attrs_schema: ["kind", "affected_crops", "severity_thresholds"] },
  vessel_class: { name: "Lớp phương tiện thủy", attrs_schema: ["capacity_t", "beam_m", "draft_m"] },
  crop: { name: "Cây trồng", attrs_schema: ["parent_code", "is_group", "unit", "description"] },
  material: { name: "Vật tư", attrs_schema: ["unit", "group", "description"] },
  production_model: { name: "Mô hình sản xuất", attrs_schema: ["description"] },
  report_type: { name: "Loại báo cáo", attrs_schema: ["period", "level", "description"] },
  cnts_species: { name: "Vật nuôi", attrs_schema: ["description", "group", "herd_column", "order", "unit"] },
  cnts_breed: { name: "Giống vật nuôi", attrs_schema: ["description", "species"] },
  cnts_vaccine: { name: "Vaccine", attrs_schema: ["description", "diseases", "order", "report_group", "species"] },
  cnts_vaccination_schedule: { name: "Quy trình tiêm", attrs_schema: ["breed", "description", "doses", "model_type"] },
  cnts_disease: { name: "Bệnh", attrs_schema: ["description", "notifiable", "species"] },
  cnts_chemical: { name: "Hóa chất", attrs_schema: ["description", "target", "unit"] },
  cnts_slaughter_facility: {
    name: "Cơ sở giết mổ",
    attrs_schema: ["address", "commune_code", "description", "facility_kind", "facility_ref", "species", "stamp_codes_gc", "stamp_codes_gs"],
  },
  cnts_aquaculture_object: { name: "Đối tượng nuôi thủy sản", attrs_schema: ["description", "scale_unit"] },
  cnts_fishing_group: { name: "Nhóm khai thác", attrs_schema: ["description", "order", "unit"] },
  cnts_indicator: { name: "Chỉ tiêu theo dõi", attrs_schema: ["description", "unit", "value_type"] },
  cnts_model_type: { name: "Loại mô hình", attrs_schema: ["description", "sector", "species"] },
  cnts_document_type: { name: "Loại văn bản", attrs_schema: ["description"] },
};

export interface MockParamDef {
  key: string;
  name: string;
  unit: string;
  kind: "decimal" | "text" | "color";
  min: string | null;
  max: string | null;
}

export const PARAM_REGISTRY: MockParamDef[] = [
  { key: "cgh.coverage.attention_min_pct", name: "Ngưỡng Cần chú ý (mức đáp ứng)", unit: "%", kind: "decimal", min: "0", max: "500" },
  { key: "cgh.coverage.sufficient_min_pct", name: "Ngưỡng Đủ máy (mức đáp ứng)", unit: "%", kind: "decimal", min: "0", max: "500" },
  { key: "cgh.coverage.surplus_min_pct", name: "Ngưỡng Thừa máy", unit: "%", kind: "decimal", min: "0", max: "500" },
  { key: "cgh.norm_ha_per_machine:MGD:thu_hoach", name: "Định mức máy gặt đập liên hợp — khâu thu hoạch", unit: "ha/máy/vụ", kind: "decimal", min: "1", max: "5000" },
  { key: "cgh.norm_ha_per_machine:MPT:cham_soc", name: "Định mức máy phun thuốc/drone — khâu chăm sóc", unit: "ha/máy/vụ", kind: "decimal", min: "1", max: "5000" },
  { key: "extension.task_sla_hours", name: "SLA xử lý nhiệm vụ hỗ trợ", unit: "giờ", kind: "decimal", min: "1", max: "720" },
  { key: "geo.overlap_tolerance_pct", name: "Ngưỡng chồng lấn ranh giới thửa", unit: "%", kind: "decimal", min: "0", max: "100" },
  { key: "reports.daily_export_quota", name: "Hạn mức xuất báo cáo mỗi ngày", unit: "lần/người/ngày", kind: "decimal", min: "1", max: "1000" },
  { key: "sync.conflict_sla_hours", name: "SLA xử lý xung đột đồng bộ", unit: "giờ", kind: "decimal", min: "1", max: "720" },
  // Quy chuẩn hiển thị bản đồ GIS (GIS US-CFG-01/02) — màu HEX là tham số kiểu "color" (giá trị ở `text`).
  { key: "gis.stage_color:chuan_bi", name: "Màu giai đoạn — Chuẩn bị đất", unit: "", kind: "color", min: null, max: null },
  { key: "gis.stage_color:sinh_truong", name: "Màu giai đoạn — Sinh trưởng", unit: "", kind: "color", min: null, max: null },
  { key: "gis.stage_color:sap_thu", name: "Màu giai đoạn — Sắp thu hoạch", unit: "", kind: "color", min: null, max: null },
  { key: "gis.stage_color:chin_ro", name: "Màu giai đoạn — Chín rộ / đang thu hoạch", unit: "", kind: "color", min: null, max: null },
  { key: "gis.stage_color:da_thu", name: "Màu giai đoạn — Đã thu hoạch", unit: "", kind: "color", min: null, max: null },
  { key: "gis.stage_color:none", name: "Màu giai đoạn — Chưa có vụ", unit: "", kind: "color", min: null, max: null },
  { key: "gis.capacity_color:available", name: "Màu lấp đầy kho/Hub — Còn chỗ", unit: "", kind: "color", min: null, max: null },
  { key: "gis.capacity_color:near_full", name: "Màu lấp đầy kho/Hub — Sắp đầy", unit: "", kind: "color", min: null, max: null },
  { key: "gis.capacity_color:full", name: "Màu lấp đầy kho/Hub — Đã đầy", unit: "", kind: "color", min: null, max: null },
  { key: "gis.capacity_color:unknown", name: "Màu lấp đầy kho/Hub — Chưa có số liệu", unit: "", kind: "color", min: null, max: null },
  { key: "gis.ripening_outline_color", name: "Màu viền vùng chín rộ 1–3 ngày tới", unit: "", kind: "color", min: null, max: null },
  { key: "geo.hub_capacity_warn_pct", name: "Ngưỡng Sắp đầy kho/Hub", unit: "%", kind: "decimal", min: "0", max: "100" },
  { key: "geo.hub_capacity_full_pct", name: "Ngưỡng Đã đầy kho/Hub", unit: "%", kind: "decimal", min: "0", max: "100" },
  { key: "geo.plot_layer_min_zoom", name: "Mức zoom tối thiểu lớp thửa", unit: "", kind: "decimal", min: "1", max: "22" },
];
