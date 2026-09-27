// ============================================================================
// DỮ LIỆU GIẢ LẬP (SYNTHETIC) — chỉ dùng khi NEXT_PUBLIC_API_MOCK=1.
// - Tên tỉnh theo đơn vị hành chính sau sắp xếp 2025 vùng ĐBSCL; mã xã, tên xã,
//   HTX, mã HTX, ranh giới đều là DỮ LIỆU MINH HOẠ, không khớp thực tế.
// - Ranh giới là lục giác quanh tâm gần đúng — KHÔNG phải ranh giới hành chính.
// - Không có dữ liệu cá nhân thật: nhãn nông hộ là chuỗi ngẫu nhiên đã che.
// ============================================================================
import type { CghDashboard, CoverageItem, PriorityCooperative } from "../pending/cgh";
import type { LibraryDocument } from "../pending/content";
import type { SupportTask, TaskKind, TaskPriority, TaskStatus } from "../pending/extension";
import type { AdminBoundaryCollection, Plot, Position } from "../pending/geo";
import type { CatalogItem, CatalogSummary, Param, ParamProposal } from "../pending/masterdata";
import type { AdminUnit, Cooperative, HouseholdSummary, Org } from "../pending/party";

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

const rnd = mulberry32(20261001);
const pick = <T,>(arr: readonly T[], r: () => number = rnd): T => arr[Math.floor(r() * arr.length)] as T;
const int = (min: number, max: number, r: () => number = rnd) => Math.floor(min + r() * (max - min + 1));

function hexId(n: number, prefix: string): string {
  const h = n.toString(16).padStart(12, "0");
  return `01927b3e-${prefix}-7000-8000-${h}`;
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

export const ADMIN_UNITS: AdminUnit[] = [
  ...PROVINCES.map((p) => ({
    code: p.code,
    name: p.name,
    level: "province" as const,
    parent_code: null,
    valid_from: "2025-07-01",
    valid_to: null,
  })),
  ...COMMUNES.map((c) => ({
    code: c.code,
    name: c.name,
    level: "commune" as const,
    parent_code: c.province_code,
    valid_from: "2025-07-01",
    valid_to: null,
  })),
];

export function boundaries(level: "province" | "commune"): AdminBoundaryCollection {
  if (level === "province") {
    return {
      type: "FeatureCollection",
      features: PROVINCES.map((p) => ({
        type: "Feature",
        id: p.code,
        geometry: { type: "Polygon", coordinates: hexagon(p.center, 0.26) },
        properties: { code: p.code, name: p.name, level: "province", parent_code: null },
      })),
    };
  }
  return {
    type: "FeatureCollection",
    features: COMMUNES.map((c) => ({
      type: "Feature",
      id: c.code,
      geometry: { type: "Polygon", coordinates: hexagon(c.center, 0.065) },
      properties: { code: c.code, name: c.name, level: "commune", parent_code: c.province_code },
    })),
  };
}

// --------------------------------------------------------------------------
// HTX
// --------------------------------------------------------------------------
const COOP_PATTERNS = [
  "HTX Nông nghiệp {x}",
  "HTX Lúa chất lượng cao {x}",
  "HTX Dịch vụ nông nghiệp {x}",
  "HTX Sản xuất lúa giống {x}",
  "Tổ hợp tác lúa {x}",
];
const OFFICERS = ["KNV Tổ 1 (giả lập)", "KNV Tổ 2 (giả lập)", "KNV Tổ 3 (giả lập)", null];

export interface MockCooperative extends Cooperative {
  location: LngLat;
}

export const COOPERATIVES: MockCooperative[] = COMMUNES.flatMap((c, ci) => {
  const count = 1 + (ci % 3);
  const province = PROVINCES.find((p) => p.code === c.province_code);
  return Array.from({ length: count }, (_, k) => {
    const n = ci * 3 + k + 1;
    const pattern = COOP_PATTERNS[(ci + k) % COOP_PATTERNS.length] ?? "HTX {x}";
    const place = c.name.replace(/^Xã /, "");
    const area = (int(80, 950) + rnd()).toFixed(4);
    const plots = int(20, 260);
    return {
      id: hexId(n, "c00b"),
      code: `MG-HTX-${c.province_code}-${String(n).padStart(3, "0")}`,
      name: pattern.replace("{x}", k === 0 ? place : `${place} ${k + 1}`),
      kind: pattern.startsWith("Tổ hợp tác") ? "tht" : "htx",
      province_code: c.province_code,
      province_name: province?.name ?? "",
      commune_code: c.code,
      commune_name: c.name,
      member_households: int(25, 340),
      area_ha: area,
      digitized_plots: plots,
      status: n % 11 === 0 ? "inactive" : "active",
      founded_on: `20${int(10, 23)}-0${int(1, 9)}-1${int(0, 9)}`,
      extension_officer: pick(OFFICERS),
      updated_at: new Date(Date.now() - int(1, 30) * 86_400_000).toISOString(),
      location: [c.center[0] + (rnd() - 0.5) * 0.08, c.center[1] + (rnd() - 0.5) * 0.08] as LngLat,
    } satisfies MockCooperative;
  });
});

const INITIALS = "ABCDĐGHKLMNPQSTVX";

/** Nhãn nông hộ ĐÃ CHE — sinh ngẫu nhiên, không tương ứng người thật. */
function maskedLabel(r: () => number): string {
  const tag = Math.floor(r() * 0xffff).toString(16).toUpperCase().padStart(4, "0");
  const parts = Array.from({ length: 3 }, (_, i) => {
    const ch = INITIALS[Math.floor(r() * INITIALS.length)] ?? "N";
    return i === 2 ? ch : `${ch}${"*".repeat(2 + Math.floor(r() * 2))}`;
  });
  return `Hộ #${tag} · ${parts.join(" ")}`;
}

export function householdsOf(coop: Cooperative): HouseholdSummary[] {
  const r = mulberry32(parseInt(coop.id.slice(-6), 16));
  const n = Math.min(coop.member_households, 57);
  return Array.from({ length: n }, (_, i) => ({
    ref: hexId(parseInt(coop.id.slice(-4), 16) * 1000 + i, "a0e0"),
    masked_label: maskedLabel(r),
    commune_name: coop.commune_name,
    plots: 1 + Math.floor(r() * 4),
    area_ha: (0.3 + r() * 4.5).toFixed(4),
    consent: r() < 0.82 ? "granted" : r() < 0.5 ? "withdrawn" : "none",
    joined_on: `202${Math.floor(r() * 6)}-0${1 + Math.floor(r() * 9)}-15`,
  }));
}

export function plotsOf(coop: Cooperative): Plot[] {
  const r = mulberry32(parseInt(coop.id.slice(-5), 16) + 7);
  const n = Math.min(coop.digitized_plots + 8, 64);
  return Array.from({ length: n }, (_, i) => ({
    id: hexId(parseInt(coop.id.slice(-4), 16) * 1000 + i, "b10f"),
    label: `SPOT-${coop.province_code}-${String(parseInt(coop.id.slice(-3), 16) * 100 + i).padStart(6, "0")}`,
    cooperative_id: coop.id,
    commune_code: coop.commune_code,
    area_ha: (0.4 + r() * 5).toFixed(4),
    digitized: i < coop.digitized_plots,
    crop: pick(["Lúa OM5451", "Lúa OM18", "Lúa Đài Thơm 8", "Lúa ST25"], r),
    updated_at: new Date(Date.now() - Math.floor(r() * 40) * 86_400_000).toISOString(),
  }));
}

// --------------------------------------------------------------------------
// Tổ chức (cây TW → Tỉnh → Xã)
// --------------------------------------------------------------------------
const NATIONAL_REF = hexId(1, "0e90");
export const ORGS: Org[] = [
  { ref: NATIONAL_REF, kind: "ttkn_national", code: "TTKNQG", name: "Trung tâm Khuyến nông Quốc gia", admin_unit_code: null, parent_ref: null, active: true },
  { ref: hexId(2, "0e90"), kind: "dcrd", code: "CUC-KTHT", name: "Cục Kinh tế hợp tác và PTNT", admin_unit_code: null, parent_ref: null, active: true },
  { ref: hexId(3, "0e90"), kind: "mkg", code: "MKG", name: "Mekong Green", admin_unit_code: null, parent_ref: null, active: true },
  ...PROVINCES.flatMap((p, pi): Org[] => {
    const provRef = hexId(100 + pi, "0e90");
    return [
      { ref: provRef, kind: "ttkn_province", code: `TTKN-${p.code}`, name: `TTKN tỉnh ${p.name}`, admin_unit_code: p.code, parent_ref: NATIONAL_REF, active: true },
      ...COMMUNES.filter((c) => c.province_code === p.code).map((c, ci): Org => ({
        ref: hexId(1000 + pi * 10 + ci, "0e90"),
        kind: "extension_team",
        code: `TKN-${c.code}`,
        name: `Tổ khuyến nông cộng đồng ${c.name}`,
        admin_unit_code: c.code,
        parent_ref: provRef,
        active: !(pi === 5 && ci === 3),
      })),
    ];
  }),
];

// --------------------------------------------------------------------------
// Nhiệm vụ hỗ trợ
// --------------------------------------------------------------------------
const TASK_TITLES: { kind: TaskKind; title: string }[] = [
  { kind: "hoi_dap", title: "Hỏi về liều lượng phân đạm giai đoạn đẻ nhánh" },
  { kind: "phan_anh_khan", title: "Phản ánh rầy nâu mật độ cao trên ruộng lúa 35 ngày" },
  { kind: "ho_tro_ky_thuat", title: "Hỗ trợ vẽ lại ranh giới thửa sau sáp nhập ấp" },
  { kind: "hoi_dap", title: "Hỏi lịch xuống giống vụ Thu Đông theo khuyến cáo" },
  { kind: "hau_kiem", title: "Hậu kiểm nhật ký canh tác tưới ướt khô xen kẽ" },
  { kind: "phan_anh_khan", title: "Phản ánh xâm nhập mặn tại cống đầu kênh" },
  { kind: "ho_tro_ky_thuat", title: "Hướng dẫn cài app hiện trường cho tổ hợp tác" },
  { kind: "hoi_dap", title: "Hỏi cách xử lý rơm rạ không đốt đồng" },
  { kind: "hau_kiem", title: "Đối chiếu diện tích sạ báo cáo với ranh giới thửa" },
  { kind: "hoi_dap", title: "Hỏi dấu hiệu bệnh đạo ôn cổ bông" },
];
const TASK_SLA_HOURS: Record<TaskKind, number> = { phan_anh_khan: 24, hoi_dap: 48, ho_tro_ky_thuat: 72, hau_kiem: 120 };

export const TASKS: SupportTask[] = Array.from({ length: 36 }, (_, i) => {
  const t = TASK_TITLES[i % TASK_TITLES.length] ?? TASK_TITLES[0]!;
  const coop = COOPERATIVES[(i * 7) % COOPERATIVES.length]!;
  const ageH = int(1, 150);
  const created = Date.now() - ageH * 3_600_000;
  const due = created + TASK_SLA_HOURS[t.kind] * 3_600_000;
  const status: TaskStatus = i % 6 === 5 ? "resolved" : i % 4 === 1 ? "in_progress" : "open";
  const priority: TaskPriority = t.kind === "phan_anh_khan" ? "urgent" : pick(["low", "normal", "normal", "high"]);
  return {
    id: hexId(5000 + i, "7a5c"),
    code: `NV-${coop.province_code}-${String(2600 + i)}`,
    title: t.title,
    kind: t.kind,
    status,
    priority,
    province_name: coop.province_name,
    commune_code: coop.commune_code,
    commune_name: coop.commune_name,
    cooperative_id: coop.id,
    cooperative_name: coop.name,
    assignee: status === "open" && i % 3 === 0 ? null : pick(OFFICERS.filter(Boolean) as string[]),
    created_at: new Date(created).toISOString(),
    due_at: new Date(due).toISOString(),
    resolved_at: status === "resolved" ? new Date(created + int(2, TASK_SLA_HOURS[t.kind] + 12) * 3_600_000).toISOString() : null,
  };
});

// --------------------------------------------------------------------------
// Thư viện số
// --------------------------------------------------------------------------
const DOCS: Omit<LibraryDocument, "id" | "published_at" | "read_count">[] = [
  { title: "Quy trình canh tác lúa chất lượng cao, phát thải thấp vùng ĐBSCL", kind: "quy_trinh", topic: "Canh tác", summary: "Các bước làm đất, gieo sạ, bón phân, tưới và thu hoạch theo hướng giảm phát thải.", version: 3, issued_by_level: "tw", issuer_name: "TTKN Quốc gia", has_local_supplement: true },
  { title: "Sổ tay kỹ thuật tưới ướt khô xen kẽ (AWD)", kind: "so_tay", topic: "Tưới tiêu", summary: "Cách đặt ống quan trắc mực nước, thời điểm rút nước và cho nước vào lại.", version: 2, issued_by_level: "tw", issuer_name: "TTKN Quốc gia", has_local_supplement: false },
  { title: "Hướng dẫn quản lý rơm rạ sau thu hoạch", kind: "so_tay", topic: "Sau thu hoạch", summary: "Thu gom, cuộn rơm, ủ phân hữu cơ; không đốt đồng.", version: 1, issued_by_level: "tw", issuer_name: "TTKN Quốc gia", has_local_supplement: true },
  { title: "Video: Kỹ thuật sạ cụm kết hợp vùi phân", kind: "video", topic: "Gieo sạ", summary: "Minh hoạ vận hành máy sạ cụm và điều chỉnh lượng giống.", version: 1, issued_by_level: "tw", issuer_name: "TTKN Quốc gia", has_local_supplement: false },
  { title: "Quy trình “1 Phải 5 Giảm” trong sản xuất lúa", kind: "quy_trinh", topic: "Canh tác", summary: "Phải dùng giống xác nhận; giảm giống, phân đạm, thuốc BVTV, nước tưới, thất thoát sau thu hoạch.", version: 4, issued_by_level: "tw", issuer_name: "TTKN Quốc gia", has_local_supplement: true },
  { title: "Hướng dẫn nhận biết và phòng trừ rầy nâu", kind: "infographic", topic: "Bảo vệ thực vật", summary: "Ngưỡng mật độ, thời điểm phun, luân phiên hoạt chất.", version: 2, issued_by_level: "tw", issuer_name: "TTKN Quốc gia", has_local_supplement: false },
  { title: "Hướng dẫn phòng bệnh đạo ôn lá và cổ bông", kind: "so_tay", topic: "Bảo vệ thực vật", summary: "Điều kiện phát sinh, triệu chứng, biện pháp canh tác và hoá học.", version: 1, issued_by_level: "tw", issuer_name: "TTKN Quốc gia", has_local_supplement: false },
  { title: "Bổ sung đặc thù: lịch xuống giống vùng ngập sâu Đồng Tháp", kind: "van_ban", topic: "Lịch thời vụ", summary: "Khung lịch né lũ cho các xã vùng trũng.", version: 1, issued_by_level: "tinh", issuer_name: "TTKN tỉnh Đồng Tháp", has_local_supplement: false },
  { title: "Bổ sung đặc thù: ứng phó xâm nhập mặn vụ Đông Xuân tại Cà Mau", kind: "van_ban", topic: "Tưới tiêu", summary: "Theo dõi độ mặn, lịch đóng mở cống, giống chịu mặn.", version: 2, issued_by_level: "tinh", issuer_name: "TTKN tỉnh Cà Mau", has_local_supplement: false },
  { title: "Video: Vận hành thiết bị bay phun thuốc an toàn", kind: "video", topic: "Cơ giới hoá", summary: "Kiểm tra trước bay, pha thuốc, vùng đệm an toàn.", version: 1, issued_by_level: "tw", issuer_name: "TTKN Quốc gia", has_local_supplement: false },
  { title: "Sổ tay ghi nhật ký canh tác điện tử cho HTX", kind: "so_tay", topic: "Chuyển đổi số", summary: "Ghi nhật ký trên app hiện trường, chụp ảnh minh chứng, đồng bộ khi có mạng.", version: 2, issued_by_level: "tw", issuer_name: "TTKN Quốc gia", has_local_supplement: false },
  { title: "Quy trình sản xuất lúa giống cấp xác nhận", kind: "quy_trinh", topic: "Giống", summary: "Chọn ruộng, khử lẫn, thu hoạch và bảo quản giống.", version: 1, issued_by_level: "tw", issuer_name: "TTKN Quốc gia", has_local_supplement: false },
];

export const DOCUMENTS: LibraryDocument[] = DOCS.map((d, i) => ({
  ...d,
  id: hexId(7000 + i, "d0c0"),
  published_at: new Date(Date.now() - (i * 9 + 3) * 86_400_000).toISOString(),
  read_count: int(40, 4800),
}));

// --------------------------------------------------------------------------
// Danh mục & tham số
// --------------------------------------------------------------------------
export const CATALOGS: Record<string, { name: string; items: CatalogItem[] }> = {
  rice_variety: {
    name: "Giống lúa",
    items: [
      ["OM5451", "OM5451", { nhom: "Thơm nhẹ", tgst: "95–100 ngày" }, true],
      ["OM18", "OM18", { nhom: "Chất lượng cao", tgst: "95–100 ngày" }, true],
      ["DT8", "Đài Thơm 8", { nhom: "Thơm", tgst: "95–100 ngày" }, true],
      ["ST25", "ST25", { nhom: "Đặc sản", tgst: "100–105 ngày" }, true],
      ["OM4900", "OM4900", { nhom: "Chất lượng cao", tgst: "95–100 ngày" }, true],
      ["IR50404", "IR50404", { nhom: "Thường", tgst: "90–95 ngày", ghi_chu: "Ngừng khuyến cáo" }, false],
    ].map(([code, name, attrs, active]) => ({ catalog: "rice_variety", code: code as string, name: name as string, attrs: attrs as Record<string, string>, active: active as boolean })),
  },
  farm_stage: {
    name: "Khâu sản xuất",
    items: [
      ["lam_dat", "Làm đất"],
      ["gieo_sa", "Gieo sạ / cấy"],
      ["cham_soc", "Chăm sóc, phun thuốc"],
      ["thu_hoach", "Thu hoạch"],
      ["sau_thu_hoach", "Sấy, xử lý rơm rạ"],
    ].map(([code, name]) => ({ catalog: "farm_stage", code: code!, name: name!, attrs: {}, active: true })),
  },
  machine_type: {
    name: "Chủng loại máy",
    items: [
      ["may_cay", "Máy cày / xới", "lam_dat"],
      ["may_sa", "Máy sạ cụm / máy cấy", "gieo_sa"],
      ["drone", "Thiết bị bay phun thuốc", "cham_soc"],
      ["may_gat", "Máy gặt đập liên hợp", "thu_hoach"],
      ["may_say", "Máy sấy", "sau_thu_hoach"],
      ["may_cuon_rom", "Máy cuộn rơm", "sau_thu_hoach"],
    ].map(([code, name, stage]) => ({ catalog: "machine_type", code: code!, name: name!, attrs: { khau: stage! }, active: true })),
  },
  season: {
    name: "Mùa vụ",
    items: [
      ["DX2026", "Đông Xuân 2025–2026", false],
      ["HT2026", "Hè Thu 2026", true],
      ["TD2026", "Thu Đông 2026", true],
    ].map(([code, name, active]) => ({ catalog: "season", code: code as string, name: name as string, attrs: {}, active: active as boolean })),
  },
  pest: {
    name: "Sâu bệnh hại",
    items: [
      ["ray_nau", "Rầy nâu"],
      ["dao_on", "Bệnh đạo ôn"],
      ["sau_cuon_la", "Sâu cuốn lá"],
      ["oc_buou_vang", "Ốc bươu vàng"],
      ["chay_bia_la", "Bệnh cháy bìa lá"],
    ].map(([code, name]) => ({ catalog: "pest", code: code!, name: name!, attrs: {}, active: true })),
  },
};

export function catalogSummaries(): CatalogSummary[] {
  return Object.entries(CATALOGS).map(([catalog, c]) => ({ catalog, name: c.name, item_count: c.items.length }));
}

const iso = (daysAgo: number) => new Date(Date.now() - daysAgo * 86_400_000).toISOString();

export const PARAMS: Param[] = [
  {
    key: "cgh.coverage.sufficient_pct",
    name: "Ngưỡng Đủ máy (mức đáp ứng)",
    unit: "%",
    kind: "decimal",
    current: { version: 1, value: "85", text: null, unit: "%", effective_from: "2026-01-01", source_doc: "Văn bản DCRD (giả lập) số 01/2026", approved_by: "Quản trị viên B (giả lập)", approved_at: iso(260) },
    pending: [],
  },
  {
    key: "cgh.coverage.attention_pct",
    name: "Ngưỡng Cần chú ý",
    unit: "%",
    kind: "decimal",
    current: { version: 1, value: "60", text: null, unit: "%", effective_from: "2026-01-01", source_doc: "Văn bản DCRD (giả lập) số 01/2026", approved_by: "Quản trị viên B (giả lập)", approved_at: iso(260) },
    pending: [],
  },
  {
    key: "cgh.coverage.surplus_pct",
    name: "Ngưỡng Thừa máy",
    unit: "%",
    kind: "decimal",
    current: { version: 1, value: "120", text: null, unit: "%", effective_from: "2026-01-01", source_doc: "Văn bản DCRD (giả lập) số 01/2026", approved_by: "Quản trị viên B (giả lập)", approved_at: iso(260) },
    pending: [],
  },
  {
    key: "cgh.norm_ha_per_machine:may_gat:thu_hoach",
    name: "Định mức máy gặt đập liên hợp — khâu thu hoạch",
    unit: "ha/máy/vụ",
    kind: "decimal",
    current: { version: 2, value: "50", text: null, unit: "ha/máy/vụ", effective_from: "2026-04-01", source_doc: "Văn bản DCRD (giả lập) số 07/2026", approved_by: "Quản trị viên B (giả lập)", approved_at: iso(180) },
    pending: [],
  },
  {
    key: "cgh.norm_ha_per_machine:drone:cham_soc",
    name: "Định mức thiết bị bay — khâu chăm sóc",
    unit: "ha/máy/vụ",
    kind: "decimal",
    current: { version: 1, value: "300", text: null, unit: "ha/máy/vụ", effective_from: "2026-01-01", source_doc: "Văn bản DCRD (giả lập) số 01/2026", approved_by: "Quản trị viên B (giả lập)", approved_at: iso(260) },
    pending: [],
  },
  {
    key: "kn.task_sla_hours:phan_anh_khan",
    name: "SLA xử lý phản ánh khẩn",
    unit: "giờ",
    kind: "decimal",
    current: { version: 1, value: "24", text: null, unit: "giờ", effective_from: "2026-07-01", source_doc: "Quyết định TTKNQG (giả lập) 12/QĐ", approved_by: "Quản trị viên B (giả lập)", approved_at: iso(90) },
    pending: [],
  },
  {
    key: "kn.task_sla_hours:hoi_dap",
    name: "SLA trả lời hỏi đáp kỹ thuật",
    unit: "giờ",
    kind: "decimal",
    current: { version: 1, value: "48", text: null, unit: "giờ", effective_from: "2026-07-01", source_doc: "Quyết định TTKNQG (giả lập) 12/QĐ", approved_by: "Quản trị viên B (giả lập)", approved_at: iso(90) },
    pending: [],
  },
];

function proposal(p: Partial<ParamProposal> & Pick<ParamProposal, "key" | "value" | "proposed_by" | "proposed_by_name" | "reason">): ParamProposal {
  return {
    id: hexId(9000 + Math.floor(rnd() * 900), "9a7a"),
    text: null,
    unit: PARAMS.find((x) => x.key === p.key)?.unit ?? "",
    effective_from: "2026-11-01",
    source_doc: "Văn bản (giả lập) số 21/2026",
    status: "pending",
    proposed_at: iso(1),
    decided_by: null,
    decided_by_name: null,
    decided_at: null,
    decision_note: null,
    ...p,
  };
}

// Một đề xuất do "người đặt" (persona setter) tạo — người này KHÔNG được tự duyệt.
PARAMS[3]!.pending.push(
  proposal({ key: PARAMS[3]!.key, value: "45", proposed_by: "mock-admin-setter", proposed_by_name: "Quản trị viên A (giả lập)", reason: "Cập nhật theo năng suất máy thực tế vụ Hè Thu" }),
);
// Một đề xuất do người khác tạo — persona setter/approver có thể thử duyệt.
PARAMS[6]!.pending.push(
  proposal({ key: PARAMS[6]!.key, value: "36", proposed_by: "mock-admin-other", proposed_by_name: "Quản trị viên C (giả lập)", reason: "Rút ngắn thời gian phản hồi hỏi đáp theo chỉ đạo" }),
);

// --------------------------------------------------------------------------
// Bản đồ CGH (Cục KTHT)
// --------------------------------------------------------------------------
const COVERAGE_BY_PROVINCE: Record<string, { demand: number; capacity: number } | null> = {
  "82": { demand: 412, capacity: 358 }, // ~86.9% Đủ
  "91": { demand: 530, capacity: 318 }, // 60% đúng ngưỡng → Cần chú ý
  "92": { demand: 296, capacity: 154 }, // ~52% Thiếu
  "86": { demand: 188, capacity: 236 }, // ~125.5% Thừa
  "96": { demand: 244, capacity: 190 }, // ~77.9% Cần chú ý
  "80": null, // Chưa có dữ liệu
};

function pct(capacity: number, demand: number): string {
  // Tính bằng số nguyên để giữ 2 chữ số thập phân chính xác (không float cho hiển thị ngưỡng).
  const scaled = Math.round((capacity * 10000) / demand);
  return `${Math.floor(scaled / 100)}.${String(scaled % 100).padStart(2, "0")}`;
}

export function coverageItems(stageFactor = 1): CoverageItem[] {
  return PROVINCES.map((p) => {
    const c = COVERAGE_BY_PROVINCE[p.code];
    if (!c) {
      return { admin_unit_code: p.code, admin_unit_name: p.name, demand_machines: null, capacity_machines: null, coverage_pct: null, source: "none", updated_at: null };
    }
    const demand = Math.max(1, Math.round(c.demand * stageFactor));
    const capacity = Math.round(c.capacity * stageFactor);
    return {
      admin_unit_code: p.code,
      admin_unit_name: p.name,
      demand_machines: demand,
      capacity_machines: capacity,
      coverage_pct: pct(capacity, demand),
      source: p.code === "96" ? "manual" : "app_htx",
      updated_at: iso(p.code === "96" ? 6 : 1),
    };
  });
}

export function priorityCooperatives(stage: string): PriorityCooperative[] {
  const r = mulberry32(stage.length * 97 + 11);
  return COOPERATIVES.filter((c) => c.province_code !== "80" && c.status === "active")
    .map((c) => {
      const demand = 2 + Math.floor(r() * 9);
      const capacity = Math.max(0, demand + Math.floor(r() * 7) - 4);
      return {
        cooperative_id: c.id,
        code: c.code,
        name: c.name,
        province_name: c.province_name,
        commune_name: c.commune_name,
        stage,
        machine_type: stage === "cham_soc" ? "drone" : stage === "lam_dat" ? "may_cay" : "may_gat",
        demand_machines: demand,
        capacity_machines: capacity,
        coverage_pct: pct(capacity, demand),
        gap: capacity - demand,
        source: r() < 0.8 ? "app_htx" : "manual",
      } satisfies PriorityCooperative;
    })
    .sort((a, b) => a.gap - b.gap || a.capacity_machines - b.capacity_machines);
}

export function cghDashboard(season: string): CghDashboard {
  const pri = priorityCooperatives("thu_hoach");
  return {
    season,
    as_of: iso(0.1),
    total_machines: 1256,
    total_cooperatives: COOPERATIVES.filter((c) => c.province_code !== "80").length,
    cooperatives_sufficient: pri.filter((p) => p.gap >= 0).length,
    cooperatives_short: pri.filter((p) => p.gap < 0).length,
    machines_by_type: [
      { machine_type: "may_gat", name: "Máy gặt đập liên hợp", count: 468 },
      { machine_type: "may_cay", name: "Máy cày / xới", count: 352 },
      { machine_type: "drone", name: "Thiết bị bay phun thuốc", count: 214 },
      { machine_type: "may_sa", name: "Máy sạ cụm / máy cấy", count: 131 },
      { machine_type: "may_say", name: "Máy sấy", count: 91 },
    ],
  };
}

// --------------------------------------------------------------------------
// Tổng quan
// --------------------------------------------------------------------------
/** Đơn vị chưa gửi số liệu kỳ này → dashboard phải hiện "Số liệu tạm tính". */
export const UNREPORTED_UNITS = new Set<string>(["80", `82${"104"}`]);
export const TARGET_AREA_BY_PROVINCE: Record<string, number> = { "82": 62000, "91": 88000, "92": 54000, "86": 41000, "96": 36000, "80": 28000 };
