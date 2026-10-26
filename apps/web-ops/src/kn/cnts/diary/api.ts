// Client nhật ký nuôi CNTS (W11.48 — BRD CNTS v3.7 UR-XA-11 BM-08/09, UR-XA-12 BM-19, UR-TINH-12, FN-10, FN-13):
// /v1/extension/cnts/diary-cycles*, /diary-entries*, /diary-photos*. Chỉ số theo mẫu và quy tắc kiểm tra xem trước ở đây để
// kiểm thử đơn vị; máy chủ kiểm lại (quy tắc chặn khi gửi, vị trí, cảnh báo).
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey, uuidv7 } from "@/api/idempotency";

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
    ? ["feed_purchases", "treatments", "vaccinations", "sales", "officer_notes"]
    : form === "BM-09"
      ? ["feed_purchases", "vaccinations", "officer_notes"]
      : [];
export const TABLE_LABEL: Record<keyof Tables, string> = {
  feed_purchases: "Mua thức ăn (NC-09)",
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
    uploadPhoto: (household_ref: string, kind: Photo["kind"], file: Blob): Promise<Photo> =>
      unwrap(
        api.POST("/v1/extension/cnts/diary-photos", {
          params: idem(),
          body: {
            household_ref,
            kind,
            file: file as unknown as string,
            id: uuidv7(),
            captured_at: new Date().toISOString(),
          },
          bodySerializer: (b: { household_ref: string; kind: string; file: string; id?: string; captured_at?: string }) => {
            const fd = new FormData();
            fd.append("household_ref", b.household_ref);
            fd.append("kind", b.kind);
            if (b.id) fd.append("id", b.id);
            if (b.captured_at) fd.append("captured_at", b.captured_at);
            fd.append("file", b.file as unknown as Blob, "anh.jpg");
            return fd;
          },
        }),
      ),
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
