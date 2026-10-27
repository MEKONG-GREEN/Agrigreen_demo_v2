// Client Báo cáo dịch bệnh BM-10 và báo nghi dịch (W11.49 — BRD CNTS v3.7 FN-11, UR-XA-10): ổ dịch có phiên bản diễn
// biến (/v1/extension/cnts/outbreaks*), lớp điểm cho bản đồ (/outbreak-points), tin báo nghi dịch (/suspects*).
// openapi-fetch sinh từ contracts/openapi/application.yaml; Bearer, X-AG-Context, Idempotency-Key do createApiClient gắn.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseNumberInput, type NumberRule } from "@/components/form";
import { compareDecimal } from "@/lib/decimal";

type S = components["schemas"];
export type Outbreak = S["CNTSOutbreak"];
export type OutbreakInput = S["CNTSOutbreakInput"];
export type OutbreakList = S["CNTSOutbreakList"];
export type OutbreakDetail = S["CNTSOutbreakDetail"];
export type Suspect = S["CNTSSuspect"];
export type SuspectInput = S["CNTSSuspectInput"];
export type OutbreakStatus = Outbreak["status"];

// BF-CN-OUTBREAK (UP-07): ổ dịch vừa gửi chờ Cấp Tỉnh xác nhận → "Chờ xác nhận".
export const STATUS_LABEL: Record<OutbreakStatus, string> = { reported: "Chờ xác nhận", monitoring: "Đang theo dõi", closed: "Đã kết thúc" };
export const STATUS_TONE: Record<OutbreakStatus, "neutral" | "ok" | "warn" | "bad" | "info"> = { reported: "bad", monitoring: "warn", closed: "ok" };
export const SUSPECT_LABEL: Record<Suspect["status"], string> = { reported: "Chờ xác nhận", confirmed: "Đã xác nhận", dismissed: "Bác bỏ" };
export const SOURCE_LABEL: Record<Outbreak["source"], string> = { ubnd_xa: "UBND xã", nong_ho: "Nông hộ báo", can_bo: "Cán bộ phát hiện" };
export const EVENT_LABEL: Record<Outbreak["event"], string> = {
  reported: "Gửi báo cáo",
  updated: "Cập nhật diễn biến",
  closed: "Cấp Tỉnh kết thúc",
  reopened: "Cấp Tỉnh mở lại",
};

/** Trường của biểu nhập (chuỗi như ô nhập). */
export interface OutbreakForm {
  commune_code: string;
  disease_code: string;
  species_code: string;
  received_at: string; // datetime-local (giờ máy)
  source: Outbreak["source"] | "";
  locality: string;
  lat: string;
  lon: string;
  accuracy_m: string;
  location_source: "gps" | "map";
  herd_size: string;
  herd_age_days: string;
  dead: string;
  symptoms: string;
  sample_date: string;
  lab_name: string;
  lab_result: string;
  culled_head: string;
  culled_kg: string;
  chemical_liters: string;
  buffer_note: string;
  prevention_note: string;
  as_of: string;
  progress_note: string;
}

export function emptyForm(commune = ""): OutbreakForm {
  return {
    commune_code: commune, disease_code: "", species_code: "", received_at: "", source: "", locality: "", lat: "", lon: "", accuracy_m: "",
    // UP-08: ô số mặc định 0 (không âm) — quy mô đàn vẫn phải > 0 khi gửi; ngày tuổi, sai số GPS để trống (không có nghĩa 0).
    location_source: "gps", herd_size: "0", herd_age_days: "", dead: "0", symptoms: "", sample_date: "", lab_name: "", lab_result: "",
    culled_head: "0", culled_kg: "0", chemical_liters: "0", buffer_note: "", prevention_note: "", as_of: "", progress_note: "",
  };
}

/** Giờ máy dạng datetime-local ("YYYY-MM-DDTHH:mm") của một thời điểm ISO. */
export function toLocalInput(iso: string): string {
  const d = new Date(iso);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}

/** Biểu nhập từ bản hiện hành (cập nhật diễn biến: gửi lại đủ trường). */
export function formOf(o: Outbreak): OutbreakForm {
  const s = (v: number | string | null | undefined) => (v === null || v === undefined ? "" : String(v));
  return {
    commune_code: o.commune_code, disease_code: o.disease_code, species_code: o.species_code, received_at: toLocalInput(o.received_at), source: o.source,
    locality: o.locality ?? "", lat: o.location?.lat ?? "", lon: o.location?.lon ?? "", accuracy_m: o.location?.accuracy_m ?? "",
    location_source: o.location?.source === "map" ? "map" : "gps", herd_size: s(o.herd_size), herd_age_days: s(o.herd_age_days), dead: s(o.dead),
    symptoms: o.symptoms, sample_date: o.sample_date ?? "", lab_name: o.lab_name ?? "", lab_result: o.lab_result ?? "", culled_head: s(o.culled_head),
    culled_kg: o.culled_kg ?? "", chemical_liters: o.chemical_liters ?? "", buffer_note: o.buffer_note ?? "", prevention_note: o.prevention_note ?? "",
    as_of: "", progress_note: "",
  };
}

/** Ô số của BM-10 (R-A1, như máy chủ): quy mô đàn nguyên > 0; số con chết, tiêu hủy, ngày tuổi nguyên ≥ 0; kg, lít, sai số ≥ 0, ≤ 3 số lẻ. */
export const OUTBREAK_RULES = {
  herd_size: { required: true, integer: true, positive: true, max: 999_999_999 },
  dead: { required: true, integer: true, max: 999_999_999 },
  culled_head: { integer: true, max: 999_999_999 },
  herd_age_days: { integer: true, max: 999_999_999 },
  culled_kg: { maxDecimals: 3 },
  chemical_liters: { maxDecimals: 3 },
  accuracy_m: { maxDecimals: 3 },
} satisfies Record<string, NumberRule>;
type NumKey = keyof typeof OUTBREAK_RULES;

/** Số đã chuẩn hóa ("1.234,5" → "1234.5"); rỗng hoặc sai → "" (đã chặn bởi `formErrors`). */
export function numValue(k: NumKey, v: string): string {
  const p = parseNumberInput(v, OUTBREAK_RULES[k]);
  return p.ok ? p.value : "";
}

const COORD = /^-?\d{1,3}(\.\d{1,7})?$/;

/** Lỗi phía máy khách trước khi gửi (máy chủ kiểm lại) — quy tắc chặn UR-XA-10. Khóa trường API → lý do. */
export function formErrors(f: OutbreakForm): Record<string, string> {
  const e: Record<string, string> = {};
  if (!f.commune_code) e.commune_code = "Chọn xã";
  if (!f.disease_code) e.disease_code = "Chọn bệnh (DB-01)";
  if (!f.species_code) e.species_code = "Chọn vật nuôi";
  if (!f.received_at) e.received_at = "Bắt buộc (DB-02)";
  if (!f.source) e.source = "Chọn nguồn tin (DB-03)";
  if (!f.locality.trim()) e.locality = "Ghi địa bàn (ấp, khu vực) — không ghi họ tên, số điện thoại";
  if (!f.lat.trim() || !f.lon.trim()) e.location = "Chưa có vị trí ổ dịch — lấy GPS hoặc nhập tọa độ (DB-05)";
  else if (!COORD.test(f.lat.trim()) || !COORD.test(f.lon.trim())) e.location = "Tọa độ độ thập phân, vd 10.25341 và 105.9721";
  else {
    const lat = Number(f.lat), lon = Number(f.lon);
    if (!(lat >= 8 && lat <= 24 && lon >= 102 && lon <= 118)) e.location = "Tọa độ ngoài lãnh thổ Việt Nam";
  }
  for (const k of Object.keys(OUTBREAK_RULES) as NumKey[]) {
    const p = parseNumberInput(f[k], OUTBREAK_RULES[k]);
    if (!p.ok) e[k] = p.error;
  }
  const herd = numValue("herd_size", f.herd_size);
  if (!e.herd_size && !e.dead && herd && compareDecimal(numValue("dead", f.dead), herd) > 0) e.dead = "Số con chết lớn hơn quy mô đàn (DB-07 > DB-06).";
  const culled = numValue("culled_head", f.culled_head);
  if (!e.herd_size && !e.culled_head && herd && culled && compareDecimal(culled, herd) > 0) e.culled_head = "Số con tiêu hủy lớn hơn quy mô đàn (DB-10 > DB-06).";
  if (!f.symptoms.trim()) e.symptoms = "Bắt buộc (DB-08)";
  return e;
}

const opt = (s: string) => (s.trim() === "" ? undefined : s.trim());
const optNum = (k: NumKey, s: string) => numValue(k, s) || null;
const optInt = (k: NumKey, s: string) => (numValue(k, s) === "" ? null : Number(numValue(k, s)));

/** Thân request từ biểu nhập. */
export function payload(f: OutbreakForm, baseVersion?: number): OutbreakInput {
  return {
    ...(baseVersion !== undefined ? { base_version: baseVersion } : {}),
    commune_code: f.commune_code,
    disease_code: f.disease_code,
    species_code: f.species_code,
    received_at: new Date(f.received_at).toISOString(),
    source: f.source || undefined,
    locality: f.locality.trim(),
    location: f.lat.trim() && f.lon.trim() ? { lat: f.lat.trim(), lon: f.lon.trim(), accuracy_m: optNum("accuracy_m", f.accuracy_m), source: f.location_source } : null,
    herd_size: Number(numValue("herd_size", f.herd_size)),
    herd_age_days: optInt("herd_age_days", f.herd_age_days),
    dead: Number(numValue("dead", f.dead) || "0"),
    symptoms: f.symptoms.trim(),
    sample_date: opt(f.sample_date),
    lab_name: opt(f.lab_name),
    lab_result: opt(f.lab_result),
    culled_head: optInt("culled_head", f.culled_head),
    culled_kg: optNum("culled_kg", f.culled_kg),
    chemical_liters: optNum("chemical_liters", f.chemical_liters),
    buffer_note: opt(f.buffer_note),
    prevention_note: opt(f.prevention_note),
    as_of: opt(f.as_of),
    progress_note: opt(f.progress_note),
  } as OutbreakInput;
}

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

export interface OutbreakQuery {
  status?: OutbreakStatus;
  commune?: string;
  disease?: string;
  from?: string;
  to?: string;
}

export function createOutbreakApi(api: ApiClient) {
  return {
    list: (q: OutbreakQuery = {}): Promise<OutbreakList> => unwrap(api.GET("/v1/extension/cnts/outbreaks", { params: { query: q } })),
    get: (ref: string): Promise<OutbreakDetail> => unwrap(api.GET("/v1/extension/cnts/outbreaks/{ref}", { params: { path: { ref } } })),
    report: (body: OutbreakInput): Promise<Outbreak> => unwrap(api.POST("/v1/extension/cnts/outbreaks", { params: idem(), body })),
    progress: (ref: string, body: OutbreakInput): Promise<Outbreak> =>
      unwrap(api.POST("/v1/extension/cnts/outbreaks/{ref}/progress", { params: { path: { ref }, ...idem() }, body })),
    close: (ref: string, base_version: number, reason: string): Promise<Outbreak> =>
      unwrap(api.POST("/v1/extension/cnts/outbreaks/{ref}/close", { params: { path: { ref }, ...idem() }, body: { base_version, reason } })),
    reopen: (ref: string, base_version: number, reason: string): Promise<Outbreak> =>
      unwrap(api.POST("/v1/extension/cnts/outbreaks/{ref}/reopen", { params: { path: { ref }, ...idem() }, body: { base_version, reason } })),
    suspects: async (status?: Suspect["status"]): Promise<Suspect[]> =>
      (await unwrap(api.GET("/v1/extension/cnts/suspects", { params: { query: status ? { status } : {} } }))).items,
    reportSuspect: (body: SuspectInput): Promise<Suspect> => unwrap(api.POST("/v1/extension/cnts/suspects", { params: idem(), body })),
    confirm: (ref: string, body: OutbreakInput): Promise<Outbreak> =>
      unwrap(api.POST("/v1/extension/cnts/suspects/{ref}/confirm", { params: { path: { ref }, ...idem() }, body })),
    dismiss: (ref: string, base_version: number, reason: string): Promise<Suspect> =>
      unwrap(api.POST("/v1/extension/cnts/suspects/{ref}/dismiss", { params: { path: { ref }, ...idem() }, body: { base_version, reason } })),
  };
}

export type OutbreakApi = ReturnType<typeof createOutbreakApi>;

export type WordRun = S["CNTSOutbreakWordRun"];

/** Word BM-10 (pf/export): tạo lần xuất (tự cấp số) rồi tải tệp; lịch sử lần xuất. */
export function createOutbreakWordApi(api: ApiClient) {
  return {
    export: (ref: string): Promise<WordRun> =>
      unwrap(api.POST("/v1/extension/cnts/outbreaks/{ref}/word", { params: { path: { ref }, ...idem() } })),
    history: async (ref: string): Promise<WordRun[]> =>
      (await unwrap(api.GET("/v1/extension/cnts/outbreaks/{ref}/words", { params: { path: { ref } } }))).items,
    download: async (ref: string, run: string): Promise<Blob> => {
      const r = await api.GET("/v1/extension/cnts/outbreaks/{ref}/words/{run}/download", { params: { path: { ref, run } }, parseAs: "blob" });
      if (r.error !== undefined || !r.data) await unwrap(Promise.resolve(r as never));
      return r.data as Blob;
    },
  };
}

/** Mã hiển thị của ổ dịch (UP-07 cột "Mã"): "OD-" + 6 ký tự cuối của mã ổ dịch (UUID, duy nhất). */
export function outbreakCode(ref: string): string {
  return "OD-" + ref.replace(/-/g, "").slice(-6).toUpperCase();
}

/** Thẻ thống kê UP-07: Tổng, Chờ xác nhận, Đang theo dõi, Đã kết thúc (đếm trước khi lọc trạng thái). */
export function statCards(byStatus: { key: string; count: number }[]): { key: OutbreakStatus | ""; label: string; count: number }[] {
  const n = (k: string) => byStatus.find((c) => c.key === k)?.count ?? 0;
  return [
    { key: "", label: "Tổng ổ dịch", count: byStatus.reduce((a, c) => a + c.count, 0) },
    { key: "reported", label: "Chờ xác nhận", count: n("reported") },
    { key: "monitoring", label: "Đang theo dõi", count: n("monitoring") },
    { key: "closed", label: "Đã kết thúc", count: n("closed") },
  ];
}

/** Tìm kiếm trong danh sách (mã, bệnh, vật nuôi, xã, địa bàn) — không phân biệt hoa thường, dấu. */
export function searchOutbreaks(items: Outbreak[], q: string, communeName: (c: string) => string): Outbreak[] {
  const fold = (s: string) => s.normalize("NFD").replace(/[\u0300-\u036f]/g, "").replace(/đ/g, "d").replace(/Đ/g, "D").toLowerCase();
  const k = fold(q.trim());
  if (!k) return items;
  return items.filter((o) =>
    fold([outbreakCode(o.ref), o.disease_name, o.species_name, communeName(o.commune_code), o.commune_code, o.locality ?? ""].join(" ")).includes(k),
  );
}
