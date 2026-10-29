// Client nhật ký nuôi CNTS (W11.48 — BRD CNTS v3.7 UR-XA-11 BM-08/09, UR-XA-12 BM-19, UR-TINH-12, FN-10, FN-13):
// /v1/extension/cnts/diary-cycles*, /diary-entries*, /diary-photos*. Chỉ số theo mẫu và quy tắc kiểm tra xem trước ở đây để
// kiểm thử đơn vị; máy chủ kiểm lại (quy tắc chặn khi gửi, vị trí, cảnh báo).
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey, uuidv7 } from "@/api/idempotency";
import { stableMultipart } from "@/api/multipart";

type S = components["schemas"];
export type Cycle = S["CNTSDiaryCycle"];
export type CycleDetail = S["CNTSDiaryCycleDetail"];
export type CycleList = S["CNTSDiaryCycleList"];
export type CycleSave = S["CNTSDiaryCycleSave"];
export type CycleClose = S["CNTSDiaryCycleClose"];
export type Entry = S["CNTSDiaryEntry"];
export type EntryDetail = S["CNTSDiaryEntryDetail"];
export type EntrySave = S["CNTSDiaryEntrySave"];
export type Tables = S["CNTSDiaryTables"];
export type Check = S["CNTSDiaryCheck"];
export type Photo = S["CNTSDiaryPhoto"];
export type Form = Cycle["form"];
export type EntryKind = Entry["entry_kind"];
export type EntryStatus = Entry["status"];

export const FORM_LABEL: Record<Form, string> = {
  "BM-08": "BM-08 · Gà lai thương phẩm",
  "BM-09": "BM-09 · Vịt xiêm",
  "BM-19": "BM-19 · Thủy sản",
};
export const KIND_LABEL: Record<EntryKind, string> = {
  daily: "Ghi hằng ngày",
  growth_env: "Cân đo + môi trường",
  env: "Môi trường",
  harvest: "Thu hoạch",
};
export const STATUS_LABEL: Record<EntryStatus, string> = {
  draft: "Nháp",
  submitted: "Đã gửi",
  returned: "Yêu cầu bổ sung",
  verified: "Đã xác thực",
};
export const STATUS_TONE: Record<EntryStatus, "neutral" | "info" | "warn" | "ok"> = { draft: "neutral", submitted: "info", returned: "warn", verified: "ok" };
export const PHOTO_KIND_LABEL: Record<Photo["kind"], string> = {
  weighing: "Ảnh cân mẫu",
  test_kit: "Ảnh bộ test",
  flock: "Ảnh đàn",
  other: "Ảnh khác",
};
export const DEATH_CAUSES = [
  ["benh", "Bệnh"],
  ["yeu", "Yếu"],
  ["de", "Đè"],
  ["thoi_tiet", "Thời tiết"],
  ["khac", "Khác"],
] as const;
export const HEALTH = [
  ["tot", "Tốt"],
  ["binh_thuong", "Bình thường"],
  ["kem", "Kém"],
  ["co_benh", "Có bệnh"],
] as const;

/** Chỉ số của mẫu nhật ký (cùng bảng với máy chủ — domain.MetricsOf). */
export interface MetricDef {
  code: string;
  label: string;
  unit?: string;
  type: "int" | "dec" | "text" | "enum";
  ref: string;
  kinds: EntryKind[];
  required: EntryKind[];
  values?: readonly (readonly [string, string])[];
  scale?: number;
}

const D: EntryKind[] = ["daily"];
const G: EntryKind[] = ["growth_env"];
const W: EntryKind[] = ["growth_env", "env", "harvest"];
const W2: EntryKind[] = ["growth_env", "env"];
const H: EntryKind[] = ["harvest"];

export const POULTRY: MetricDef[] = [
  {
    code: "alive",
    label: "Số con sống",
    unit: "con",
    type: "int",
    ref: "NC-05",
    kinds: D,
    required: D,
  },
  {
    code: "dead",
    label: "Số con chết",
    unit: "con",
    type: "int",
    ref: "NC-06",
    kinds: D,
    required: [],
  },
  {
    code: "death_cause",
    label: "Nguyên nhân chết",
    type: "enum",
    ref: "NC-07",
    kinds: D,
    required: [],
    values: DEATH_CAUSES,
  },
  {
    code: "death_note",
    label: "Ghi chú nguyên nhân chết",
    type: "text",
    ref: "NC-07",
    kinds: D,
    required: [],
  },
  {
    code: "feed_kg",
    label: "Thức ăn sử dụng",
    unit: "kg",
    type: "dec",
    ref: "NC-08",
    kinds: D,
    required: D,
    scale: 2,
  },
  {
    code: "health",
    label: "Sức khỏe đàn",
    type: "enum",
    ref: "NC-08",
    kinds: D,
    required: [],
    values: HEALTH,
  },
  {
    code: "health_note",
    label: "Ghi chú sức khỏe",
    type: "text",
    ref: "NC-08",
    kinds: D,
    required: [],
  },
  {
    code: "age_week",
    label: "Tuần tuổi (khi cân)",
    unit: "tuần",
    type: "int",
    ref: "NC-12",
    kinds: D,
    required: [],
  },
  {
    code: "weight_avg_g",
    label: "Trọng lượng bình quân",
    unit: "g/con",
    type: "dec",
    ref: "NC-12",
    kinds: D,
    required: [],
    scale: 1,
  },
];

export const AQUATIC: MetricDef[] = [
  {
    code: "sample_count",
    label: "Số mẫu cân",
    unit: "con",
    type: "int",
    ref: "NT-04",
    kinds: G,
    required: G,
  },
  {
    code: "weight_avg_g",
    label: "Trọng lượng trung bình",
    unit: "g/con",
    type: "dec",
    ref: "NT-05",
    kinds: G,
    required: G,
    scale: 1,
  },
  {
    code: "length_avg_cm",
    label: "Chiều dài trung bình",
    unit: "cm",
    type: "dec",
    ref: "NT-06",
    kinds: G,
    required: G,
    scale: 1,
  },
  {
    code: "loss",
    label: "Hao hụt từ lần trước",
    unit: "con",
    type: "int",
    ref: "NT-07",
    kinds: W,
    required: W,
  },
  {
    code: "ph",
    label: "pH",
    type: "dec",
    ref: "NT-08",
    kinds: W,
    required: W2,
    scale: 1,
  },
  {
    code: "do_mgl",
    label: "Oxy hòa tan",
    unit: "mg/l",
    type: "dec",
    ref: "NT-09",
    kinds: W,
    required: W2,
    scale: 2,
  },
  {
    code: "temp_c",
    label: "Nhiệt độ",
    unit: "°C",
    type: "dec",
    ref: "NT-10",
    kinds: W,
    required: [],
    scale: 1,
  },
  {
    code: "salinity",
    label: "Độ mặn",
    unit: "‰",
    type: "dec",
    ref: "NT-10",
    kinds: W,
    required: [],
    scale: 1,
  },
  {
    code: "feed_kg",
    label: "Thức ăn sử dụng từ lần trước",
    unit: "kg",
    type: "dec",
    ref: "NT-11",
    kinds: W,
    required: W2,
    scale: 2,
  },
  {
    code: "action_note",
    label: "Biện pháp xử lý, ghi chú",
    type: "text",
    ref: "NT-12",
    kinds: W,
    required: [],
  },
  {
    code: "harvest_kg",
    label: "Sản lượng thu hoạch",
    unit: "kg",
    type: "dec",
    ref: "NT-02",
    kinds: H,
    required: H,
    scale: 2,
  },
  {
    code: "harvest_price",
    label: "Giá bán",
    unit: "đồng/kg",
    type: "dec",
    ref: "NT-02",
    kinds: H,
    required: [],
    scale: 0,
  },
];

export const metricsOf = (form: Form) => (form === "BM-19" ? AQUATIC : POULTRY);
export const kindsOf = (form: Form): EntryKind[] => (form === "BM-19" ? ["growth_env", "env", "harvest"] : ["daily"]);
/** Bảng phụ theo mẫu (sổ vịt xiêm 2026 không có phòng, trị bệnh và xuất bán — NC-16). */
export const tablesOf = (form: Form): (keyof Tables)[] =>
  form === "BM-08"
    ? ["feed_purchases", "feed_materials", "treatments", "vaccinations", "sales", "officer_notes"]
    : form === "BM-09"
      ? ["feed_purchases", "feed_materials", "vaccinations", "officer_notes"]
      : [];
export const TABLE_LABEL: Record<keyof Tables, string> = {
  feed_purchases: "Mua thức ăn chăn nuôi (NC-09)",
  feed_materials: "Mua nguyên liệu thức ăn chăn nuôi (NC-09)",
  treatments: "Phòng và trị bệnh (NC-10)",
  vaccinations: "Tiêm phòng (NC-11)",
  sales: "Xuất bán (NC-13)",
  officer_notes: "Ý kiến cán bộ theo dõi (NC-14)",
};

/** Lỗi hình thức của chỉ số (trước khi gửi). */
export function metricErrors(form: Form, kind: EntryKind, m: Record<string, string>): Record<string, string> {
  const e: Record<string, string> = {};
  for (const d of metricsOf(form)) {
    if (!d.kinds.includes(kind)) continue;
    const v = (m[d.code] ?? "").trim();
    if (!v) {
      if (d.required.includes(kind)) e[d.code] = `Bắt buộc (${d.ref})`;
      continue;
    }
    if (d.type === "int" && !/^\d+$/.test(v)) e[d.code] = "Số nguyên không âm";
    if (d.type === "dec") {
      const re = d.scale ? new RegExp(`^\\d+(\\.\\d{1,${d.scale}})?$`) : /^\d+$/;
      if (!re.test(v)) e[d.code] = d.scale ? `Số, tối đa ${d.scale} chữ số thập phân` : "Số nguyên";
    }
  }
  if (form === "BM-19" && m.ph && Number(m.ph) > 14) e.ph = "pH 0–14";
  if (!e.age_week && m.age_week?.trim() && Number(m.age_week) > MAX_GROWTH_WEEK) e.age_week = `Tuần tuổi 0–${MAX_GROWTH_WEEK}`;
  return e;
}

/** Số con sống xem trước (NC-05 = số sống lần trước − chết − xuất bán trong ngày). */
export function expectedAlive(prevAlive: number, dead: number, soldToday: number): number {
  return prevAlive - dead - soldToday;
}

/** Số con xuất bán trong ngày ghi (NC-13). */
export const soldOn = (t: Tables | undefined, day: string) => (t?.sales ?? []).filter((s) => s.date === day).reduce((n, s) => n + (s.heads || 0), 0);

/** Cảnh báo xem trước môi trường nước (pH 7,5–8,5; oxy ≥ 4 mg/l mặc định — tham số máy chủ có thể khác). */
export function waterWarnings(m: Record<string, string>): string[] {
  const out: string[] = [];
  const ph = Number(m.ph),
    dox = Number(m.do_mgl);
  if (m.ph && (ph < 7.5 || ph > 8.5)) out.push(`pH ${m.ph} ngoài ngưỡng 7,5–8,5`);
  if (m.do_mgl && dox < 4) out.push(`Oxy hòa tan ${m.do_mgl} mg/l dưới 4 mg/l`);
  return out;
}

/** Tổng chi, lợi nhuận (NC-15) — đồng, chuỗi số nguyên. */
export function accounting(c: Partial<CycleClose>): {
  total: number;
  profit: number | null;
} {
  const n = (v: string | null | undefined) => (v && /^\d+$/.test(v) ? Number(v) : 0);
  const total = n(c.cost_seed) + n(c.cost_feed) + n(c.cost_depreciation) + n(c.cost_transport) + n(c.cost_vet) + n(c.cost_probiotic);
  return {
    total,
    profit: c.revenue_amount && /^\d+$/.test(c.revenue_amount) ? n(c.revenue_amount) - total : null,
  };
}

/** Đồng → chuỗi có dấu chấm nhóm nghìn. */
export function money(v: string | number | null | undefined): string {
  if (v === null || v === undefined || v === "") return "—";
  const s = String(v);
  const neg = s.startsWith("-");
  return (neg ? "−" : "") + s.replace("-", "").replace(/\B(?=(\d{3})+(?!\d))/g, ".") + " đ";
}

export const dmy = (d: string | null | undefined) => (d ? d.slice(0, 10).split("-").reverse().join("/") : "—");

/** Nén ảnh về JPEG ≤ 500 KB (NT-13) trên trình duyệt; ảnh đã nhỏ giữ nguyên. */
export async function compressImage(file: Blob, maxBytes = 500 * 1024): Promise<Blob> {
  if (file.size <= maxBytes && /image\/(jpeg|png|webp)/.test(file.type)) return file;
  const bmp = await createImageBitmap(file);
  let scale = Math.min(1, 1600 / Math.max(bmp.width, bmp.height));
  for (let round = 0; round < 6; round++) {
    for (const q of [0.85, 0.7, 0.55, 0.4]) {
      const canvas = document.createElement("canvas");
      canvas.width = Math.max(1, Math.round(bmp.width * scale));
      canvas.height = Math.max(1, Math.round(bmp.height * scale));
      canvas.getContext("2d")?.drawImage(bmp, 0, 0, canvas.width, canvas.height);
      const out = await new Promise<Blob | null>((r) => canvas.toBlob(r, "image/jpeg", q));
      if (out && out.size <= maxBytes) return out;
    }
    scale *= 0.7;
  }
  throw new Error("Không nén được ảnh xuống 500 KB");
}

/** Vị trí thiết bị lúc ghi (NT-15) — null khi trình duyệt không cho phép. */
export function currentFix(): Promise<EntrySave["location"] | null> {
  return new Promise((resolve) => {
    if (typeof navigator === "undefined" || !navigator.geolocation) return resolve(null);
    navigator.geolocation.getCurrentPosition(
      (p) =>
        resolve({
          lat: p.coords.latitude,
          lng: p.coords.longitude,
          accuracy_m: p.coords.accuracy.toFixed(1),
          mock_location: false,
        }),
      () => resolve(null),
      { enableHighAccuracy: true, timeout: 15000, maximumAge: 0 },
    );
  });
}

/** id ảnh do client sinh (R-11) + thời điểm chụp của MỘT lần tải ảnh — giữ nguyên khi bấm lại cùng ảnh (#172). */
export interface PhotoDraft {
  id: string;
  captured_at: string;
}

/**
 * Nhớ id/captured_at theo ảnh đang tải (hộ + loại + tên/cỡ/kiểu/thời điểm sửa của tệp gốc): chọn lại đúng ảnh đó sau khi
 * mất phản hồi → cùng id/captured_at → cùng thân multipart → cùng Idempotency-Key → máy chủ phát lại ảnh đã lưu thay vì
 * lưu bản thứ hai. Tải xong (done) thì quên; ảnh/loại/hộ khác → lần tải mới.
 */
export function createPhotoDrafts(now: () => Date = () => new Date()) {
  const pending = new Map<string, PhotoDraft>();
  const sig = (household_ref: string, kind: string, f: File) => [household_ref, kind, f.name, f.size, f.type, f.lastModified].join("\u0000");
  return {
    draft(household_ref: string, kind: Photo["kind"], file: File): PhotoDraft {
      const k = sig(household_ref, kind, file);
      let d = pending.get(k);
      if (!d) {
        d = { id: uuidv7(), captured_at: now().toISOString() };
        pending.set(k, d);
        if (pending.size > 32) pending.delete(pending.keys().next().value!);
      }
      return d;
    },
    done(d: PhotoDraft): void {
      for (const [k, v] of pending) if (v.id === d.id) pending.delete(k);
    },
  };
}

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });
const base = (ref: string, base_version: number) => ({
  params: { path: { ref }, ...idem() },
  body: { base_version },
});

export function createCntsDiaryApi(api: ApiClient) {
  return {
    cycles: (
      q: {
        household_ref?: string;
        site_ref?: string;
        project_ref?: string;
        status?: "open" | "closed";
      } = {},
    ): Promise<CycleList> => unwrap(api.GET("/v1/extension/cnts/diary-cycles", { params: { query: q } })),
    cycle: (ref: string): Promise<CycleDetail> =>
      unwrap(
        api.GET("/v1/extension/cnts/diary-cycles/{ref}", {
          params: { path: { ref } },
        }),
      ),
    createCycle: (body: CycleSave): Promise<CycleDetail> => unwrap(api.POST("/v1/extension/cnts/diary-cycles", { params: idem(), body })),
    updateCycle: (ref: string, body: CycleSave): Promise<CycleDetail> =>
      unwrap(
        api.POST("/v1/extension/cnts/diary-cycles/{ref}", {
          params: { path: { ref }, ...idem() },
          body,
        }),
      ),
    closeCycle: (ref: string, body: CycleClose): Promise<CycleDetail> =>
      unwrap(
        api.POST("/v1/extension/cnts/diary-cycles/{ref}/close", {
          params: { path: { ref }, ...idem() },
          body,
        }),
      ),
    reopenCycle: (ref: string, base_version: number, reason: string): Promise<CycleDetail> =>
      unwrap(
        api.POST("/v1/extension/cnts/diary-cycles/{ref}/reopen", {
          params: { path: { ref }, ...idem() },
          body: { base_version, reason },
        }),
      ),
    entries: async (
      q: {
        cycle_ref?: string;
        household_ref?: string;
        project_ref?: string;
        status?: EntryStatus;
        review?: boolean;
        flagged?: boolean;
      } = {},
    ) => (await unwrap(api.GET("/v1/extension/cnts/diary-entries", { params: { query: q } }))).items,
    entry: (ref: string): Promise<EntryDetail> =>
      unwrap(
        api.GET("/v1/extension/cnts/diary-entries/{ref}", {
          params: { path: { ref } },
        }),
      ),
    /** Tạo lần ghi: mã UUIDv7 sinh ở máy (gửi lại an toàn), thời điểm thiết bị. */
    createEntry: (body: EntrySave): Promise<EntryDetail> =>
      unwrap(
        api.POST("/v1/extension/cnts/diary-entries", {
          params: idem(),
          body: {
            id: uuidv7(),
            captured_at: new Date().toISOString(),
            ...body,
          },
        }),
      ),
    updateEntry: (ref: string, body: EntrySave): Promise<EntryDetail> =>
      unwrap(
        api.POST("/v1/extension/cnts/diary-entries/{ref}", {
          params: { path: { ref }, ...idem() },
          body,
        }),
      ),
    submit: (ref: string, v: number): Promise<EntryDetail> => unwrap(api.POST("/v1/extension/cnts/diary-entries/{ref}/submit", base(ref, v))),
    verify: (ref: string, v: number): Promise<EntryDetail> => unwrap(api.POST("/v1/extension/cnts/diary-entries/{ref}/verify", base(ref, v))),
    giveBack: (ref: string, base_version: number, reason: string): Promise<EntryDetail> =>
      unwrap(
        api.POST("/v1/extension/cnts/diary-entries/{ref}/return", {
          params: { path: { ref }, ...idem() },
          body: { base_version, reason },
        }),
      ),
    correct: (ref: string, body: EntrySave): Promise<EntryDetail> =>
      unwrap(
        api.POST("/v1/extension/cnts/diary-entries/{ref}/correct", {
          params: { path: { ref }, ...idem() },
          body,
        }),
      ),
    // Thân multipart tất định (src/api/multipart.ts, #172) + id/captured_at của lần tải (PhotoDraft): bấm lại sau mất phản
    // hồi gửi đúng từng byte cũ → cùng Idempotency-Key, máy chủ phát lại ảnh đã lưu. api.POST phải khởi phát đồng bộ trong
    // phạm vi phiên thao tác (act.run, hoặc `idem` của act.run khi đứng sau await nén ảnh).
    uploadPhoto: (household_ref: string, kind: Photo["kind"], file: Blob, draft: PhotoDraft = { id: uuidv7(), captured_at: new Date().toISOString() }): Promise<Photo> => {
      const part = new File([file], "anh.jpg", { type: file.type, lastModified: 0 });
      const mp = stableMultipart([
        ["household_ref", household_ref],
        ["kind", kind],
        ["id", draft.id],
        ["captured_at", draft.captured_at],
        ["file", part],
      ]);
      return unwrap(
        api.POST("/v1/extension/cnts/diary-photos", {
          params: idem(),
          headers: { "Content-Type": mp.contentType },
          body: { household_ref, kind, file: file as unknown as string, id: draft.id, captured_at: draft.captured_at },
          bodySerializer: () => mp.body,
        }),
      );
    },
    /** Ảnh (bản rõ do máy chủ mở) → URL đối tượng để hiển thị; gọi URL.revokeObjectURL khi bỏ. */
    photoUrl: async (ref: string): Promise<string> => {
      const r = await api.GET("/v1/extension/cnts/diary-photos/{ref}/content", {
        params: { path: { ref } },
        parseAs: "blob",
      });
      if (r.error || !r.data) throw r.error ?? new Error("Không mở được ảnh");
      return URL.createObjectURL(r.data as Blob);
    },
  };
}

export type CntsDiaryApi = ReturnType<typeof createCntsDiaryApi>;

// ---------------------------------------------------------------------------
// Đủ trường sổ gốc BM-08 / BM-09 2026 (BF-CN-DIARY #109 — CN-08, CN-12)
// ---------------------------------------------------------------------------

/** Vật tư mục 1 "Thông tin chung" của sổ (NC-03) — gợi ý; người ghi gõ thêm được. */
export const SUPPLY_ITEMS: Record<Form, string[]> = {
  "BM-08": ["Con giống", "Thức ăn gà con", "Thức ăn gà lớn", "Vaccine", "Chế phẩm Balasa N01"],
  "BM-09": ["Con giống", "Thức ăn vịt con", "Thức ăn vịt lớn", "Vaccine"],
  "BM-19": ["Con giống", "Thức ăn", "Chế phẩm", "Khác"],
};

/** Khoản chi thứ 6 của hạch toán (phần 2 mục 3): sổ gà ghi "Chế phẩm", sổ vịt xiêm ghi "Vaccine…". */
export const cost6Label = (form: Form) => (form === "BM-09" ? "Vaccine" : "Chế phẩm");

const num = (v: string | number | null | undefined) => {
  if (v === null || v === undefined || v === "") return null;
  const n = Number(v);
  return Number.isFinite(n) ? n : null;
};
const round = (n: number, d: number) => Math.round(n * 10 ** d) / 10 ** d;

/** Thành tiền (tự tính) của một dòng = số lượng × đơn giá, đồng nguyên; null khi thiếu số. */
export function lineAmount(qty: string | number | null | undefined, price: string | number | null | undefined): number | null {
  const q = num(qty),
    p = num(price);
  return q === null || p === null ? null : Math.round(q * p);
}

/** P (kg/con) của tổng thu = sản lượng / số con (phần 2 mục 3). */
export function kgPerHead(outputKg: string | null | undefined, heads: number | null | undefined): number | null {
  const o = num(outputKg);
  return o === null || !heads ? null : round(o / heads, 2);
}

/** Tuần tuổi sổ ghi ở phần 2 mục 2 (tuần 1, 2, 3, 4, 6, 8, 10, 12). */
export const GROWTH_WEEKS = [1, 2, 3, 4, 6, 8, 10, 12] as const;

/**
 * Tuần tuổi tối đa của bảng sinh trưởng — trùng `MaxAgeWeek` ở server (#109). Lần ghi quá ngưỡng (dữ liệu cũ nhập nhầm,
 * hoặc ngày tuổi lúc nhận quá lớn) không sinh dòng mà được báo "dữ liệu tuần tuổi bất thường"; bảng tối đa
 * 8 + (104 − 12) / 2 = 54 dòng.
 */
export const MAX_GROWTH_WEEK = 104;

/** Tuần tuổi của lần ghi: lấy "Tuần tuổi (khi cân)" nếu có, không thì tính từ ngày tuổi lúc nhận + số ngày từ ngày nhận. */
export function ageWeek(c: Pick<Cycle, "received_date" | "age_days">, e: Pick<Entry, "entry_date" | "metrics">): number {
  const w = e.metrics.age_week;
  if (w && /^\d+$/.test(w) && Number(w) > 0) return Number(w);
  const days = Math.round((Date.parse(e.entry_date) - Date.parse(c.received_date)) / 86_400_000) + (c.age_days ?? 0);
  return Math.max(1, Math.ceil(days / 7));
}

export interface GrowthRow {
  week: number;
  /** Số con sống ở lần ghi cuối của khoảng. */
  alive: number | null;
  /** Tỉ lệ sống so số con đầu kỳ (%). */
  survival: number | null;
  dead: number;
  /** Nguyên nhân chết trong khoảng. */
  causes: string;
  /** Thức ăn sử dụng trong khoảng (kg). */
  feedKg: number;
  /** Tiêu tốn thức ăn lũy kế (kg thức ăn / kg tăng trọng). */
  fcr: number | null;
  /** Trọng lượng trung bình (kg/con) ở lần cân cuối của khoảng. */
  weightKg: number | null;
}

export interface Growth {
  rows: GrowthRow[];
  total: { dead: number; feedKg: number; alive: number | null; survival: number | null; fcr: number | null };
  avg: { weightKg: number | null; feedPerHead: number | null };
  /** Lần ghi có tuần tuổi ngoài 1–MAX_GROWTH_WEEK: không đưa vào bảng (ngày ghi, tuần tuổi). */
  outOfRange: { entry_date: string; week: number }[];
}

/**
 * Chỉ tiêu sinh trưởng theo tuần tuổi (phần 2 mục 2 của sổ, NC-12) tính từ các lần ghi hằng ngày đã gửi (bỏ nháp). Dòng tuần
 * W gộp các tuần sau dòng trước tới W (dòng "Tuần 6" gồm tuần 5–6…); quá tuần 12 thêm dòng mỗi 2 tuần, tới tối đa
 * MAX_GROWTH_WEEK. Lần ghi có tuần tuổi ngoài ngưỡng không sinh dòng, trả ở `outOfRange` (#109). Số sống, tỉ lệ sống,
 * tiêu tốn tự tính; trọng lượng lấy theo lần cân (NC-12 nhập).
 */
export function growthTable(
  c: Pick<Cycle, "received_date" | "age_days" | "initial_count" | "initial_weight_g">,
  entries: Pick<Entry, "entry_date" | "metrics" | "status">[],
): Growth {
  const outOfRange: Growth["outOfRange"] = [];
  const es: Pick<Entry, "entry_date" | "metrics" | "status">[] = [];
  const weekOf: number[] = [];
  for (const e of entries.filter((x) => x.status !== "draft").sort((a, b) => a.entry_date.localeCompare(b.entry_date))) {
    const w = ageWeek(c, e);
    if (Number.isFinite(w) && w >= 1 && w <= MAX_GROWTH_WEEK) {
      es.push(e);
      weekOf.push(w);
    } else outOfRange.push({ entry_date: e.entry_date, week: w });
  }
  const weeks: number[] = [...GROWTH_WEEKS];
  const maxWeek = weekOf.reduce((m, w) => Math.max(m, w), 0);
  while (weeks.at(-1)! < maxWeek) weeks.push(weeks.at(-1)! + 2);
  const startKg = (num(c.initial_weight_g) ?? 0) / 1000;
  const causeLabel = (v: string) => DEATH_CAUSES.find((x) => x[0] === v)?.[1] ?? v;
  let prev = 0,
    cumFeed = 0;
  const rows: GrowthRow[] = weeks.map((week) => {
    const inRow = es.filter((_, i) => weekOf[i]! > prev && weekOf[i]! <= week);
    prev = week;
    let alive: number | null = null,
      weightKg: number | null = null,
      dead = 0,
      feedKg = 0;
    const causes = new Set<string>();
    for (const e of inRow) {
      const a = num(e.metrics.alive);
      if (a !== null) alive = a;
      const w = num(e.metrics.weight_avg_g);
      if (w !== null) weightKg = round(w / 1000, 3);
      const d = num(e.metrics.dead) ?? 0;
      dead += d;
      if (d > 0 && e.metrics.death_cause) causes.add(causeLabel(e.metrics.death_cause));
      feedKg += num(e.metrics.feed_kg) ?? 0;
    }
    cumFeed += feedKg;
    const gain = alive !== null && weightKg !== null ? alive * weightKg - c.initial_count * startKg : 0;
    return {
      week,
      alive,
      survival: alive === null || !c.initial_count ? null : round((alive / c.initial_count) * 100, 1),
      dead,
      causes: [...causes].join(", "),
      feedKg: round(feedKg, 2),
      fcr: gain > 0 ? round(cumFeed / gain, 2) : null,
      weightKg,
    };
  });
  const lastAlive = [...rows].reverse().find((r) => r.alive !== null) ?? null;
  const lastFcr = [...rows].reverse().find((r) => r.fcr !== null)?.fcr ?? null;
  const weighed = rows.filter((r) => r.weightKg !== null).map((r) => r.weightKg!);
  return {
    rows,
    total: {
      dead: rows.reduce((n, r) => n + r.dead, 0),
      feedKg: round(cumFeed, 2),
      alive: lastAlive?.alive ?? null,
      survival: lastAlive?.survival ?? null,
      fcr: lastFcr,
    },
    avg: {
      weightKg: weighed.length ? round(weighed.reduce((a, b) => a + b, 0) / weighed.length, 3) : null,
      feedPerHead: c.initial_count ? round(cumFeed / c.initial_count, 2) : null,
    },
    outOfRange,
  };
}

export interface TechTarget {
  label: string;
  unit: string;
  target: string;
  /** Giá trị thực tế từ bảng sinh trưởng (null khi chưa có số liệu). */
  actual: (g: Growth) => number | null;
  ok: (v: number) => boolean;
}

const rowAt = (g: Growth, week: number) => g.rows.find((r) => r.week === week);

/** Các yêu cầu kỹ thuật cần đạt (phần 2 mục 1 của sổ) — so với số liệu thực tế. */
export const TECH_TARGETS: Partial<Record<Form, TechTarget[]>> = {
  "BM-08": [
    { label: "Tỷ lệ sống của gà 4 tuần tuổi", unit: "%", target: "95", actual: (g) => rowAt(g, 4)?.survival ?? null, ok: (v) => v >= 95 },
    { label: "Tỷ lệ nuôi sống đến 12 tuần tuổi", unit: "%", target: "90", actual: (g) => rowAt(g, 12)?.survival ?? null, ok: (v) => v >= 90 },
    { label: "Tiêu tốn thức ăn", unit: "kg", target: "2,8", actual: (g) => g.total.fcr, ok: (v) => v <= 2.8 },
    {
      label: "Trọng lượng gà đến 12 tuần tuổi",
      unit: "kg",
      target: "1,6–1,8",
      actual: (g) => rowAt(g, 12)?.weightKg ?? null,
      ok: (v) => v >= 1.6 && v <= 1.8,
    },
  ],
  "BM-09": [
    // 70 ngày tuổi = tuần 10.
    { label: "Tỉ lệ nuôi sống đến 70 ngày", unit: "%", target: "≥ 90", actual: (g) => rowAt(g, 10)?.survival ?? null, ok: (v) => v >= 90 },
    { label: "Trọng lượng TB lúc 70 ngày", unit: "kg", target: "≥ 3", actual: (g) => rowAt(g, 10)?.weightKg ?? null, ok: (v) => v >= 3 },
    { label: "Tiêu tốn thức ăn (thức ăn hỗn hợp hoàn chỉnh)", unit: "kg", target: "≤ 3", actual: (g) => g.total.fcr, ok: (v) => v <= 3 },
  ],
};

/** Số kiểu Việt (dấu phẩy thập phân). */
export const vn = (n: number | null | undefined) => (n === null || n === undefined ? "—" : String(n).replace(".", ","));

/** Cột bảng phụ theo sổ gốc (mục 3–7, phần 3). */
export type TableField = { key: string; label: string; type?: "date" | "number" | "text" };
const PURCHASE = (name: string): TableField[] => [
  { key: "date", label: "Ngày", type: "date" },
  { key: "name", label: name },
  { key: "quantity_kg", label: "Số lượng (kg)" },
  { key: "unit_price", label: "Đơn giá (đ/kg)" },
  { key: "note", label: "Ghi chú" },
];
export const TABLE_FIELDS: Record<keyof Tables, TableField[]> = {
  feed_purchases: PURCHASE("Tên thức ăn"),
  feed_materials: PURCHASE("Tên nguyên liệu thức ăn"),
  treatments: [
    { key: "date", label: "Ngày", type: "date" },
    { key: "condition", label: "Thể trạng" },
    { key: "symptoms", label: "Triệu chứng" },
    { key: "drug", label: "Tên thuốc sử dụng" },
    { key: "dosage", label: "Liều lượng, cách dùng" },
    { key: "treater", label: "Người điều trị" },
    { key: "withdrawal_days", label: "Thời gian ngưng thuốc (ngày)", type: "number" },
    { key: "result", label: "Kết quả điều trị" },
  ],
  vaccinations: [
    { key: "date", label: "Ngày tiêm phòng", type: "date" },
    { key: "vaccine", label: "Tên vaccine" },
    { key: "dosage", label: "Liều lượng" },
    { key: "method", label: "Cách dùng" },
    { key: "mfg_date", label: "Ngày sản xuất vaccine", type: "date" },
    { key: "shot", label: "Lần (nếu có)", type: "number" },
    { key: "note", label: "Ghi chú" },
  ],
  sales: [
    { key: "date", label: "Ngày", type: "date" },
    { key: "kind", label: "Loại" },
    { key: "heads", label: "Số lượng bán (con)", type: "number" },
    { key: "weight_kg", label: "Khối lượng (kg)" },
    { key: "unit_price", label: "Đơn giá (đ/kg)" },
  ],
  officer_notes: [
    { key: "date", label: "Ngày", type: "date" },
    { key: "officer", label: "Họ và tên cán bộ" },
    { key: "content", label: "Nội dung yêu cầu" },
    { key: "farmer_opinion", label: "Ý kiến nông dân" },
  ],
};
/** Cột (số lượng, đơn giá) để tính thành tiền tự động. */
export const AMOUNT_OF: Partial<Record<keyof Tables, [string, string]>> = {
  feed_purchases: ["quantity_kg", "unit_price"],
  feed_materials: ["quantity_kg", "unit_price"],
  sales: ["weight_kg", "unit_price"],
};
