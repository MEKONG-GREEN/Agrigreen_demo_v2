// Client Báo cáo dịch bệnh BM-10 và báo nghi dịch (W11.49 — BRD CNTS v3.7 FN-11, UR-XA-10): ổ dịch có phiên bản diễn
// biến (/v1/extension/cnts/outbreaks*), lớp điểm cho bản đồ (/outbreak-points), tin báo nghi dịch (/suspects*).
// openapi-fetch sinh từ contracts/openapi/application.yaml; Bearer, X-AG-Context, Idempotency-Key do createApiClient gắn.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";

type S = components["schemas"];
export type Outbreak = S["CNTSOutbreak"];
export type OutbreakInput = S["CNTSOutbreakInput"];
export type OutbreakList = S["CNTSOutbreakList"];
export type OutbreakDetail = S["CNTSOutbreakDetail"];
export type Suspect = S["CNTSSuspect"];
export type SuspectInput = S["CNTSSuspectInput"];
export type OutbreakStatus = Outbreak["status"];

export const STATUS_LABEL: Record<OutbreakStatus, string> = { reported: "Đã gửi", monitoring: "Đang theo dõi", closed: "Đã kết thúc" };
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
    location_source: "gps", herd_size: "", herd_age_days: "", dead: "0", symptoms: "", sample_date: "", lab_name: "", lab_result: "",
    culled_head: "", culled_kg: "", chemical_liters: "", buffer_note: "", prevention_note: "", as_of: "", progress_note: "",
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

const INT = /^\d{1,9}$/;
const DEC3 = /^\d{1,11}(\.\d{1,3})?$/;
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
  if (!INT.test(f.herd_size.trim()) || Number(f.herd_size) < 1) e.herd_size = "Số con ≥ 1 (DB-06)";
  if (!INT.test(f.dead.trim())) e.dead = "Số nguyên ≥ 0";
  else if (!e.herd_size && Number(f.dead) > Number(f.herd_size)) e.dead = "Số con chết lớn hơn quy mô đàn (DB-07 > DB-06)";
  if (f.culled_head.trim()) {
    if (!INT.test(f.culled_head.trim())) e.culled_head = "Số nguyên ≥ 0";
    else if (!e.herd_size && Number(f.culled_head) > Number(f.herd_size)) e.culled_head = "Số con tiêu hủy lớn hơn quy mô đàn (DB-10 > DB-06)";
  }
  if (f.herd_age_days.trim() && !INT.test(f.herd_age_days.trim())) e.herd_age_days = "Số ngày tuổi";
  for (const k of ["culled_kg", "chemical_liters", "accuracy_m"] as const) if (f[k].trim() && !DEC3.test(f[k].trim())) e[k] = "Số ≥ 0, tối đa 3 số lẻ (dấu chấm)";
  if (!f.symptoms.trim()) e.symptoms = "Bắt buộc (DB-08)";
  return e;
}

const opt = (s: string) => (s.trim() === "" ? undefined : s.trim());
const optInt = (s: string) => (s.trim() === "" ? null : Number(s));

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
    location: f.lat.trim() && f.lon.trim() ? { lat: f.lat.trim(), lon: f.lon.trim(), accuracy_m: opt(f.accuracy_m) ?? null, source: f.location_source } : null,
    herd_size: Number(f.herd_size),
    herd_age_days: optInt(f.herd_age_days),
    dead: Number(f.dead || "0"),
    symptoms: f.symptoms.trim(),
    sample_date: opt(f.sample_date),
    lab_name: opt(f.lab_name),
    lab_result: opt(f.lab_result),
    culled_head: optInt(f.culled_head),
    culled_kg: opt(f.culled_kg) ?? null,
    chemical_liters: opt(f.chemical_liters) ?? null,
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
