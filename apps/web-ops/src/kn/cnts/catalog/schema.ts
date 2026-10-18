// Danh mục Chăn nuôi – Thủy sản (W11.41, BRD CNTS v3.7 FN-06 / UR-ADM-04). Định nghĩa tab + trường thuộc tính của
// từng danh mục cnts_* — khớp attrSpecs của pf/masterdata (internal/platform/masterdata/domain/cnts.go).

export type CntsCatalog =
  | "cnts_species"
  | "cnts_breed"
  | "cnts_vaccine"
  | "cnts_vaccination_schedule"
  | "cnts_disease"
  | "cnts_chemical"
  | "cnts_slaughter_facility"
  | "cnts_aquaculture_object"
  | "cnts_fishing_group"
  | "cnts_indicator"
  | "cnts_model_type"
  | "cnts_document_type";

export type Field =
  | { key: string; label: string; kind: "text"; required?: boolean; max?: number }
  | { key: string; label: string; kind: "select"; options: [string, string][]; required?: boolean }
  | { key: string; label: string; kind: "bool" }
  | { key: string; label: string; kind: "int" }
  | { key: string; label: string; kind: "ref"; catalog: CntsCatalog; multi: boolean; required?: boolean }
  | { key: string; label: string; kind: "codes"; hint: string }
  | { key: string; label: string; kind: "doses"; required: true };

const desc: Field = { key: "description", label: "Mô tả", kind: "text", max: 500 };
const order: Field = { key: "order", label: "Thứ tự cột", kind: "int" };

export const FACILITY_KIND: [string, string][] = [
  ["tap_trung", "Tập trung"],
  ["nho_le", "Nhỏ lẻ"],
];

export const REPORT_GROUP: [string, string][] = [
  ["heo", "Heo"],
  ["bo", "Bò"],
  ["cum_gia_cam", "Cúm gia cầm"],
  ["dai", "Dại"],
  ["khac", "Khác (không cộng báo cáo tuần)"],
];

export interface CatalogTab {
  id: CntsCatalog;
  label: string;
  fields: Field[];
  /** Tên cột thuộc tính tóm tắt trong bảng. */
  summary: string;
}

/** Thứ tự tab theo FN-06. */
export const CNTS_TABS: CatalogTab[] = [
  {
    id: "cnts_species",
    label: "Vật nuôi",
    summary: "Nhóm · ĐVT",
    fields: [
      {
        key: "group",
        label: "Nhóm",
        kind: "select",
        required: true,
        options: [
          ["gia_suc", "Gia súc"],
          ["gia_cam", "Gia cầm"],
          ["thu_cung", "Thú cưng"],
          ["yen", "Yến"],
          ["thuy_san", "Thủy sản"],
          ["khac", "Khác"],
        ],
      },
      { key: "unit", label: "Đơn vị tính", kind: "text", required: true, max: 20 },
      { key: "herd_column", label: "Cột tổng đàn CN-01 (BM-04)", kind: "bool" },
      order,
      desc,
    ],
  },
  { id: "cnts_breed", label: "Giống", summary: "Loài", fields: [{ key: "species", label: "Loài", kind: "ref", catalog: "cnts_species", multi: false, required: true }, desc] },
  {
    id: "cnts_vaccine",
    label: "Vaccine",
    summary: "Loài so sánh · Nhóm báo cáo",
    fields: [
      { key: "species", label: "Loài so sánh (số liều trong tuần ≤ tổng đàn)", kind: "ref", catalog: "cnts_species", multi: true, required: true },
      { key: "diseases", label: "Phòng bệnh", kind: "ref", catalog: "cnts_disease", multi: true },
      { key: "report_group", label: "Nhóm cộng báo cáo tuần", kind: "select", options: REPORT_GROUP, required: true },
      order,
      desc,
    ],
  },
  {
    id: "cnts_vaccination_schedule",
    label: "Quy trình tiêm",
    summary: "Mô hình / giống · Số mũi",
    fields: [
      { key: "model_type", label: "Loại mô hình", kind: "ref", catalog: "cnts_model_type", multi: false },
      { key: "breed", label: "Giống", kind: "ref", catalog: "cnts_breed", multi: false },
      { key: "doses", label: "Danh sách mũi (vaccine, lần, ngày tuổi)", kind: "doses", required: true },
      desc,
    ],
  },
  {
    id: "cnts_disease",
    label: "Bệnh",
    summary: "Loài",
    fields: [{ key: "species", label: "Loài mắc", kind: "ref", catalog: "cnts_species", multi: true }, { key: "notifiable", label: "Phải công bố dịch", kind: "bool" }, desc],
  },
  {
    id: "cnts_chemical",
    label: "Hóa chất",
    summary: "ĐVT · Đối tượng",
    fields: [
      { key: "unit", label: "Đơn vị tính", kind: "text", required: true, max: 20 },
      {
        key: "target",
        label: "Dùng cho",
        kind: "select",
        options: [
          ["gia_suc", "Gia súc"],
          ["gia_cam", "Gia cầm"],
          ["chung", "Chung"],
        ],
      },
      desc,
    ],
  },
  {
    id: "cnts_slaughter_facility",
    label: "Cơ sở giết mổ",
    summary: "Xã · Loại · Mộc dấu GS / GC",
    fields: [
      { key: "commune_code", label: "Mã xã/phường/đặc khu", kind: "text", required: true, max: 40 },
      { key: "facility_kind", label: "Loại cơ sở", kind: "select", options: FACILITY_KIND, required: true },
      { key: "stamp_codes_gs", label: "Mã số khắc mộc dấu gia súc (GS)", kind: "codes", hint: "Nhiều mã cách nhau dấu phẩy, vd 91.589.05, 91.589.05.1" },
      { key: "stamp_codes_gc", label: "Mã số khắc mộc dấu gia cầm (GC)", kind: "codes", hint: "Trống nếu cơ sở không giết mổ gia cầm" },
      { key: "species", label: "Loài được giết mổ", kind: "ref", catalog: "cnts_species", multi: true },
      { key: "address", label: "Địa chỉ", kind: "text", max: 300 },
      desc,
    ],
  },
  {
    id: "cnts_aquaculture_object",
    label: "Đối tượng nuôi thủy sản",
    summary: "Đơn vị quy mô",
    fields: [
      {
        key: "scale_unit",
        label: "Đơn vị quy mô",
        kind: "select",
        required: true,
        options: [
          ["ha", "ha"],
          ["long", "Lồng"],
          ["veo", "Vèo"],
          ["m3", "m³"],
        ],
      },
      desc,
    ],
  },
  { id: "cnts_fishing_group", label: "Nhóm khai thác", summary: "ĐVT", fields: [{ key: "unit", label: "Đơn vị tính", kind: "text", max: 20 }, order, desc] },
  {
    id: "cnts_indicator",
    label: "Chỉ tiêu theo dõi",
    summary: "ĐVT · Kiểu",
    fields: [
      { key: "unit", label: "Đơn vị tính", kind: "text", max: 20 },
      {
        key: "value_type",
        label: "Kiểu giá trị",
        kind: "select",
        required: true,
        options: [
          ["decimal", "Số thập phân"],
          ["integer", "Số nguyên"],
          ["percent", "Phần trăm"],
        ],
      },
      desc,
    ],
  },
  {
    id: "cnts_model_type",
    label: "Loại mô hình",
    summary: "Lĩnh vực · Loài",
    fields: [
      {
        key: "sector",
        label: "Lĩnh vực",
        kind: "select",
        required: true,
        options: [
          ["chan_nuoi", "Chăn nuôi"],
          ["thuy_san", "Thủy sản"],
        ],
      },
      { key: "species", label: "Loài", kind: "ref", catalog: "cnts_species", multi: true },
      desc,
    ],
  },
  { id: "cnts_document_type", label: "Loại văn bản", summary: "Mô tả", fields: [desc] },
];

export function tabOf(id: CntsCatalog): CatalogTab {
  return CNTS_TABS.find((t) => t.id === id) ?? CNTS_TABS[0]!;
}

/** Danh sách mã ngăn cách dấu phẩy (thuộc tính danh sách). */
export function codes(v: string | undefined): string[] {
  return (v ?? "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
}

export interface Dose {
  vaccine: string;
  shot: number;
  age_days: number;
}

/** Đọc danh sách mũi "<vaccine>/<lần>@<ngày tuổi>,…" (dạng chuẩn của máy chủ). Phần lỗi bị bỏ qua. */
export function parseDoses(v: string | undefined): Dose[] {
  const out: Dose[] = [];
  for (const part of codes(v)) {
    const m = /^([A-Za-z0-9][A-Za-z0-9_.-]*)\/(\d+)@(\d+)$/.exec(part);
    if (m) out.push({ vaccine: m[1]!, shot: Number(m[2]), age_days: Number(m[3]) });
  }
  return out;
}

/** Ghi danh sách mũi về chuỗi gửi máy chủ (máy chủ sắp lại theo ngày tuổi). */
export function formatDoses(ds: Dose[]): string {
  return ds
    .filter((d) => d.vaccine)
    .map((d) => `${d.vaccine}/${d.shot}@${d.age_days}`)
    .join(",");
}

/** Ngày dự kiến của mũi = ngày nhận giống + ngày tuổi − tuổi lúc nhận (UR-TINH-10). yyyy-mm-dd. */
export function expectedDate(received: string, ageAtReceipt: number, d: Dose): string {
  const t = new Date(`${received}T00:00:00Z`);
  t.setUTCDate(t.getUTCDate() + d.age_days - ageAtReceipt);
  return t.toISOString().slice(0, 10);
}

/** Kiểm phía máy khách trước khi gửi (máy chủ vẫn kiểm lại): trường bắt buộc + ràng buộc liên trường. */
export function validateAttrs(tab: CatalogTab, attrs: Record<string, string>): Record<string, string> {
  const errs: Record<string, string> = {};
  for (const f of tab.fields) {
    const v = (attrs[f.key] ?? "").trim();
    if ("required" in f && f.required && !v) errs[f.key] = "Bắt buộc";
    if (f.kind === "int" && v && !/^\d{1,3}$/.test(v)) errs[f.key] = "Số nguyên 0–999";
  }
  if (tab.id === "cnts_vaccination_schedule" && !attrs.model_type && !attrs.breed) errs.model_type = "Gắn loại mô hình hoặc giống";
  if (tab.id === "cnts_vaccination_schedule" && parseDoses(attrs.doses).length === 0) errs.doses = "Cần ít nhất một mũi";
  if (tab.id === "cnts_slaughter_facility") {
    const gc = codes(attrs.stamp_codes_gc);
    const dup = codes(attrs.stamp_codes_gs).find((c) => gc.includes(c));
    if (dup) errs.stamp_codes_gc = `Mã ${dup} trùng giữa GS và GC`;
  }
  return errs;
}

/** Bỏ thuộc tính rỗng (máy chủ coi thuộc tính vắng = không đặt) và facility_ref (máy chủ cấp, giữ cố định). */
export function cleanAttrs(attrs: Record<string, string>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(attrs)) {
    if (k === "facility_ref") continue;
    if (v.trim() !== "") out[k] = v.trim();
  }
  return out;
}

/** Ngưỡng mặc định CNTS — tham số có phiên bản pf/masterdata (BRD Mục III.5, UR-TINH-13). */
export const CNTS_THRESHOLDS: { key: string; label: string; unit: string }[] = [
  { key: "cnts.indicator_min:PH", label: "pH tối thiểu", unit: "" },
  { key: "cnts.indicator_max:PH", label: "pH tối đa", unit: "" },
  { key: "cnts.indicator_min:DO", label: "Oxy hòa tan tối thiểu", unit: "mg/l" },
  { key: "cnts.herd_change_pct", label: "Biến động tổng đàn BM-04", unit: "%" },
  { key: "cnts.fishing_change_pct", label: "Biến động khai thác BM-17", unit: "%" },
  { key: "cnts.slaughter_spike_factor", label: "KSGM bất thường (lần trung bình 4 tuần)", unit: "lần" },
  { key: "cnts.slaughter_spike_min_days", label: "KSGM bất thường: số ngày có số liệu tối thiểu", unit: "ngày" },
  { key: "cnts.lookback_periods", label: "Kỳ gần nhất: tìm lùi tối đa", unit: "kỳ" },
  { key: "cnts.location_distance_m", label: "Khoảng cách lần ghi tới điểm", unit: "m" },
  { key: "cnts.gps_accuracy_m", label: "Sai số GPS tối đa", unit: "m" },
  { key: "cnts.log_overdue_grace_days", label: "Quá hạn ghi nhật ký thêm", unit: "ngày" },
];

/** Tệp mẫu nhập Excel/CSV của danh mục (cột catalog, code, name, valid_from + thuộc tính). */
export function templateRows(tab: CatalogTab): string[][] {
  const head = ["catalog", "code", "name", "valid_from", ...tab.fields.map((f) => f.key)];
  const sample: Record<string, string> = {
    cnts_slaughter_facility: "CS_MAU|Cơ sở giết mổ mẫu|2026-10-01|30292|tap_trung|91.589.05,91.589.05.1||HEO,TRAU|Ấp 1|",
  };
  const row = (sample[tab.id] ?? `MA_MAU|Tên mục mẫu|`).split("|");
  return [head, [tab.id, ...row, ...Array(Math.max(0, head.length - 1 - row.length)).fill("")].slice(0, head.length)];
}
