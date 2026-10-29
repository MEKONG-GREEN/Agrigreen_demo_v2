// Client lịch trực KSGM BM-03 và lịch tiêm phòng BM-07 (W11.46 — BRD CNTS v3.7 FN-15, UR-TINH-10, UR-TINH-11, UR-XA-03):
// /v1/extension/cnts/duty-rosters/{month}, /my-duties, /vaccination-batches*. Quy tắc thuần ở đây để kiểm thử đơn vị.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";
import { parseNumberInput, type NumberRule } from "@/components/form";
import { filenameFromDisposition } from "@/lib/files";

type S = components["schemas"];
export type DutyRoster = S["CNTSDutyRoster"];
export type DutyCell = S["CNTSDutyCell"];
export type Shift = DutyCell["shift"];
export type MyDuties = S["CNTSMyDuties"];
export type DutyRosterImport = S["CNTSDutyRosterImport"];
export type VaccinationBatch = S["CNTSVaccinationBatch"];
export type VaccinationBatchDetail = S["CNTSVaccinationBatchDetail"];
export type VaccinationDose = S["CNTSVaccinationDose"];
export type BatchCreate = S["CNTSVaccinationBatchCreate"];
export type BatchStatus = VaccinationBatch["status"];
export type DosesUpdate = S["CNTSVaccinationDosesUpdate"];
export type VaccinationModel = S["CNTSVaccinationModel"];
export type VaccinationMatrix = S["CNTSVaccinationMatrix"];
export type MatrixRow = S["CNTSVaccinationMatrixRow"];
export type MatrixColumn = S["CNTSVaccinationMatrixColumn"];
export type MatrixQuery = { model_type_code: string; project_ref?: string; site_ref?: string; commune_code?: string; received_from?: string; received_to?: string };

/** Nhãn ca (UR-TINH-10): N ngày, Đ đêm, NĐ cả ngày đêm; trống = nghỉ. */
export const SHIFT_LABEL: Record<Shift, string> = { N: "N", D: "Đ", ND: "NĐ" };
export const SHIFT_TITLE: Record<Shift, string> = { N: "Ca ngày", D: "Ca đêm (qua nửa đêm thuộc ngày bắt đầu ca)", ND: "Cả ngày đêm" };
const CYCLE: (Shift | "")[] = ["", "N", "D", "ND"];

/** Bấm ô lưới: trống → N → Đ → NĐ → trống. */
export function nextShift(s: Shift | "" | undefined): Shift | "" {
  return CYCLE[(CYCLE.indexOf(s ?? "") + 1) % CYCLE.length]!;
}

export const cellKey = (facility: string, officer: string, day: number) => `${facility}|${officer}|${day}`;

/** Ô lưới dạng bảng tra (khóa cơ sở|viên chức|ngày → ca). */
export function toGrid(cells: readonly DutyCell[]): Map<string, Shift> {
  const m = new Map<string, Shift>();
  for (const c of cells) m.set(cellKey(c.facility_ref, c.officer_ref, c.day), c.shift);
  return m;
}

/** Bảng tra → danh sách ô gửi máy chủ (đã sắp, bỏ ngày ngoài tháng). */
export function fromGrid(grid: ReadonlyMap<string, Shift>, daysInMonth: number): DutyCell[] {
  const out: DutyCell[] = [];
  for (const [k, shift] of grid) {
    const [facility_ref, officer_ref, d] = k.split("|");
    const day = Number(d);
    if (day >= 1 && day <= daysInMonth) out.push({ facility_ref: facility_ref!, officer_ref: officer_ref!, day, shift });
  }
  return out.sort((a, b) => a.facility_ref.localeCompare(b.facility_ref) || a.officer_ref.localeCompare(b.officer_ref) || a.day - b.day);
}

/** Hàng của lưới: các cặp (cơ sở, viên chức) có ô hoặc được thêm. */
export function rowsOf(cells: readonly DutyCell[], extra: readonly [string, string][] = []): [string, string][] {
  const seen = new Set<string>();
  const out: [string, string][] = [];
  for (const p of [...cells.map((c) => [c.facility_ref, c.officer_ref] as [string, string]), ...extra]) {
    const k = p.join("|");
    if (!seen.has(k)) {
      seen.add(k);
      out.push(p);
    }
  }
  return out;
}

/** "YYYY-MM" của tháng trước/sau. */
export function shiftMonth(month: string, delta: number): string {
  const [y, m] = month.split("-").map(Number);
  const d = new Date(Date.UTC(y!, m! - 1 + delta, 1));
  return `${d.getUTCFullYear()}-${String(d.getUTCMonth() + 1).padStart(2, "0")}`;
}

/** "YYYY-MM" hiện tại (giờ máy). */
export function currentMonth(d = new Date()): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

/** Thứ trong tuần (T2…CN) của một ngày trong tháng. */
export function weekdayLabel(month: string, day: number): string {
  const [y, m] = month.split("-").map(Number);
  return ["CN", "T2", "T3", "T4", "T5", "T6", "T7"][new Date(Date.UTC(y!, m! - 1, day)).getUTCDay()]!;
}

/** Ngày cơ sở chưa có ai trực theo lưới đang sửa (như cảnh báo facility_uncovered của máy chủ) — để tô ô lưới. */
export function uncoveredDays(grid: ReadonlyMap<string, Shift>, facilities: readonly string[], daysInMonth: number): Map<string, Set<number>> {
  const covered = new Map<string, Set<number>>();
  for (const k of grid.keys()) {
    const [f, , d] = k.split("|");
    if (!covered.has(f!)) covered.set(f!, new Set());
    covered.get(f!)!.add(Number(d));
  }
  const out = new Map<string, Set<number>>();
  for (const f of facilities) {
    const miss = new Set<number>();
    for (let d = 1; d <= daysInMonth; d++) if (!covered.get(f)?.has(d)) miss.add(d);
    if (miss.size) out.set(f, miss);
  }
  return out;
}

/** Liên kết "Cấp quyền" từ cảnh báo viên chức chưa được cấp quyền cơ sở: chọn sẵn viên chức, vai KSGM, xã, cơ sở. */
export function grantHref(officer: string, facility: string, commune?: string | null): string {
  const q = new URLSearchParams({ vien_chuc: officer, co_so: facility });
  if (commune) q.set("xa", commune);
  return `/kn/cnts/cap-quyen?${q.toString()}`;
}

/** Dòng lỗi của kết quả nạp tệp, dạng "Dòng 12: …". */
export function issueText(i: DutyRosterImport["issues"][number]): string {
  return i.line ? `Dòng ${i.line}: ${i.message}` : i.message;
}

/** Tệp nạp: chỉ .xlsx (máy chủ đọc), tối đa 4 MB. Trả lỗi hiển thị hoặc null. */
export function importFileError(f: { kind: string; size: number }): string | null {
  if (f.kind === "xls") return "Tệp .xls cũ — mở bằng Excel rồi lưu lại dạng .xlsx.";
  if (f.kind !== "xlsx") return "Chọn tệp Excel .xlsx theo mẫu Lịch trực KSGM.";
  if (f.size > 4 * 1024 * 1024) return "Tệp vượt 4 MB.";
  return null;
}

export const BATCH_STATUS_LABEL: Record<BatchStatus, string> = { open: "Đang theo lịch", overdue: "Có mũi quá hạn", done: "Đã tiêm đủ", cancelled: "Đã hủy" };
export const BATCH_STATUS_TONE: Record<BatchStatus, "neutral" | "ok" | "warn" | "bad" | "info"> = { open: "info", overdue: "bad", done: "ok", cancelled: "neutral" };

/** Lỗi phía máy khách của biểu lập lịch tiêm (máy chủ kiểm lại). */
/** Tuổi lúc nhận giống: số nguyên 0–3650 ngày; số con: số nguyên > 0 (R-A1, như máy chủ). */
export const AGE_RULE: NumberRule = { required: true, integer: true, max: 3650 };
export const HEAD_RULE: NumberRule = { integer: true, positive: true, max: 100_000_000 };

/** Ô số nguyên người gõ → số (rỗng → null) hoặc lỗi theo trường. */
export function intValue(raw: string, rule: NumberRule): { ok: true; value: number | null } | { ok: false; error: string } {
  const p = parseNumberInput(raw, rule);
  return p.ok ? { ok: true, value: p.value === "" ? null : Number(p.value) } : p;
}

export function batchErrors(b: Partial<BatchCreate>): Record<string, string> {
  const e: Record<string, string> = {};
  // Chọn hộ mô hình: xã, loại mô hình lấy theo hộ/điểm ở máy chủ (BF-CN-VACC).
  if (!b.household_ref && !b.commune_code) e.commune_code = "Bắt buộc nhập.";
  if (!b.household_ref && !b.model_type_code) e.model_type_code = "Bắt buộc nhập.";
  if (!b.household_ref && !b.locality?.trim()) e.locality = "Chọn hộ mô hình hoặc ghi địa bàn (không ghi họ tên, số điện thoại).";
  if (!b.received_date) e.received_date = "Bắt buộc nhập.";
  if (b.age_at_receipt === undefined || !Number.isInteger(b.age_at_receipt) || b.age_at_receipt < 0 || b.age_at_receipt > 3650) e.age_at_receipt = "0–3650 ngày";
  if (b.head_count !== undefined && b.head_count !== null && (!Number.isInteger(b.head_count) || b.head_count < 1)) e.head_count = "Số con ≥ 1";
  return e;
}

/** Ngày dự kiến = ngày nhận + ngày tuổi − tuổi lúc nhận (xem trước trên web, FN-15 AC-03). */
export function expectedDate(received: string, ageDays: number, ageAtReceipt: number): string {
  const d = new Date(`${received}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + ageDays - ageAtReceipt);
  return d.toISOString().slice(0, 10);
}

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

/** Nhãn hộ của một lứa: chủ hộ che bớt (hộ mô hình) hoặc địa bàn. */
export function householdText(b: Pick<VaccinationBatch, "model" | "locality" | "household_ref">): string {
  return b.model?.owner_label ?? b.locality ?? (b.household_ref ? `Hộ ${b.household_ref.slice(0, 8)}` : "—");
}

/** Cột của bảng BM-07 gom theo vaccine: [vaccine, số lần] theo thứ tự cột. */
export function vaccineGroups(cols: readonly MatrixColumn[]): { code: string; name: string; span: number }[] {
  const out: { code: string; name: string; span: number }[] = [];
  for (const c of cols) {
    const last = out.at(-1);
    if (last && last.code === c.vaccine_code) last.span++;
    else out.push({ code: c.vaccine_code, name: c.vaccine_name ?? c.vaccine_code, span: 1 });
  }
  return out;
}

/** Thay đổi của một hàng bảng BM-07 (ô đang sửa khác bản hiện hành); Cấp Xã/nông hộ không gửi ngày dự kiến. */
export function rowChanges(row: Pick<MatrixRow, "cells" | "can_plan">, edit: Readonly<Record<string, { expected?: string; actual?: string }>>): DosesUpdate["doses"] {
  const out: DosesUpdate["doses"] = [];
  for (const d of row.cells) {
    if (!d) continue;
    const e = edit[`${d.vaccine_code}/${d.shot}`];
    if (!e) continue;
    const u: DosesUpdate["doses"][number] = { vaccine_code: d.vaccine_code, shot: d.shot };
    if (row.can_plan && e.expected && e.expected !== d.expected_date) u.expected_date = e.expected;
    if (e.actual && e.actual !== d.actual_date) u.actual_date = e.actual;
    if (u.expected_date || u.actual_date) out.push(u);
  }
  return out;
}

export function createCntsCalApi(api: ApiClient) {
  return {
    roster: (month: string, province_code?: string): Promise<DutyRoster> => unwrap(api.GET("/v1/extension/cnts/duty-rosters/{month}", { params: { path: { month }, query: { province_code } } })),
    saveRoster: (month: string, body: S["CNTSDutyRosterSave"]): Promise<DutyRoster> =>
      unwrap(api.POST("/v1/extension/cnts/duty-rosters/{month}", { params: { path: { month }, ...idem() }, body })),
    /** Nạp Excel BM-03: máy chủ chỉ đọc, trả ô + lỗi theo dòng + cảnh báo; lưu bằng saveRoster(source=excel). */
    importRoster: (month: string, file: { body: string | Uint8Array; contentType: string }, province_code?: string): Promise<DutyRosterImport> =>
      unwrap(
        api.POST("/v1/extension/cnts/duty-rosters/{month}/import", {
          params: { path: { month }, query: { province_code }, ...idem() },
          body: file.body as unknown as string,
          bodySerializer: (b: unknown) => b as BodyInit,
          headers: { "Content-Type": file.contentType },
        }),
      ),
    /** Xuất .xlsx đúng mẫu BM-03 của bản đã lưu. */
    exportRoster: async (month: string, province_code?: string, basis?: string): Promise<{ blob: Blob; name: string }> => {
      const p = api.GET("/v1/extension/cnts/duty-rosters/{month}/export.xlsx", {
        params: { path: { month }, query: { province_code, ...(basis?.trim() ? { basis: basis.trim() } : {}) } },
        parseAs: "blob",
      });
      const blob = (await unwrap(p)) as Blob;
      const res = await p;
      return { blob, name: filenameFromDisposition(res.response.headers.get("Content-Disposition"), `lich-truc-ksgm-${month}.xlsx`) };
    },
    myDuties: (month: string, province_code?: string): Promise<MyDuties> => unwrap(api.GET("/v1/extension/cnts/my-duties", { params: { query: { month, province_code } } })),
    batches: async (q: { status?: BatchStatus; commune_code?: string; model_type_code?: string; project_ref?: string; site_ref?: string } = {}): Promise<VaccinationBatch[]> =>
      (await unwrap(api.GET("/v1/extension/cnts/vaccination-batches", { params: { query: q } }))).items,
    batch: (ref: string): Promise<VaccinationBatchDetail> =>
      unwrap(api.GET("/v1/extension/cnts/vaccination-batches/{ref}", { params: { path: { ref } } })),
    createBatch: (body: BatchCreate): Promise<VaccinationBatchDetail> =>
      unwrap(api.POST("/v1/extension/cnts/vaccination-batches", { params: idem(), body })),
    matrix: (q: MatrixQuery): Promise<VaccinationMatrix> => unwrap(api.GET("/v1/extension/cnts/vaccination-matrix", { params: { query: q } })),
    matrixXlsx: async (q: MatrixQuery): Promise<Blob> => {
      const res = await api.GET("/v1/extension/cnts/vaccination-matrix/xlsx", { params: { query: q }, parseAs: "blob" });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      return res.data as Blob;
    },
    updateDoses: (ref: string, body: DosesUpdate): Promise<VaccinationBatchDetail> =>
      unwrap(api.POST("/v1/extension/cnts/vaccination-batches/{ref}/doses", { params: { path: { ref }, ...idem() }, body })),
    cancelBatch: (ref: string, base_version: number, reason: string): Promise<VaccinationBatchDetail> =>
      unwrap(api.POST("/v1/extension/cnts/vaccination-batches/{ref}/cancel", { params: { path: { ref }, ...idem() }, body: { base_version, reason } })),
  };
}

export type CntsCalApi = ReturnType<typeof createCntsCalApi>;
