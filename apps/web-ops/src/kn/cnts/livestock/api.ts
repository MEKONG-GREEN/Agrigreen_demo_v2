// Client danh sách hộ chăn nuôi CNTS (CNTS-V4-HOUSEHOLD-LIST #251, ADR-0056 — BRD CNTS v4.0 FN-10, bảng trường
// HCN-01…HCN-09): /v1/extension/cnts/livestock-households*. Quy tắc thuần ở đây để kiểm thử đơn vị; máy chủ kiểm lại.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseNumberInput } from "@/components/form";
import type { PillTone } from "@/components/patterns/list";

type S = components["schemas"];
export type Household = S["CNTSLivestockHousehold"];
export type HouseholdDetail = S["CNTSLivestockHouseholdDetail"];
export type HouseholdSave = S["CNTSLivestockHouseholdSave"];
export type Checkin = S["CNTSLivestockCheckin"];
export type LivestockType = Household["livestock_type"];
export type Status = Household["status"];

export const LIST_ROUTE = "/kn/cnts/so-lieu-xa/ho-chan-nuoi";
export const PAGE_SIZE = 20;

export const TYPE_LABEL: Record<LivestockType, string> = {
  nong_ho: "Nông hộ",
  trang_trai_nho: "Trang trại nhỏ",
  trang_trai_vua: "Trang trại vừa",
  trang_trai_cong_nghiep: "Trang trại công nghiệp",
};
export const TYPE_OPTIONS = (Object.keys(TYPE_LABEL) as LivestockType[]).map((v) => ({ value: v, label: TYPE_LABEL[v] }));

export const STATUS_LABEL: Record<Status, string> = { active: "Đang dùng", pending_location: "Chờ bổ sung vị trí", deactivated: "Vô hiệu hóa" };
export const STATUS_TONE: Record<Status, PillTone> = { active: "ok", pending_location: "warn", deactivated: "neutral" };
export const STATUS_OPTIONS = (Object.keys(STATUS_LABEL) as Status[]).map((v) => ({ value: v, label: STATUS_LABEL[v] }));

export const EVENT_LABEL: Record<HouseholdDetail["event"], string> = {
  created: "Thêm hộ (check-in tại nhà hộ)",
  created_pending: "Tạo từ báo cáo dịch bệnh (chờ bổ sung vị trí)",
  updated: "Sửa thông tin",
  checked_in: "Check-in tại nhà hộ — chuyển Đang dùng",
  relocated: "Đổi vị trí (check-in lại)",
  deactivated: "Vô hiệu hóa",
};

export const FIELD_LABEL: Record<string, string> = {
  owner_name: "Họ tên chủ hộ / tên trang trại",
  owner_phone: "Số điện thoại",
  commune_code: "Xã",
  hamlet: "Ấp, khu phố",
  animals: "Vật nuôi, số con",
  livestock_type: "Loại chăn nuôi",
  location: "Vị trí",
  status: "Trạng thái",
};

export const WARNING_LABEL: Record<string, string> = {
  gps_accuracy: "Sai số GPS vượt ngưỡng — vị trí vẫn được lưu, nên check-in lại khi tín hiệu tốt hơn.",
  mocked_location: "Thiết bị báo vị trí giả lập — đã đánh dấu.",
};

/** Dòng vật nuôi đang nhập (chuỗi thô của ô số). */
export interface AnimalRow {
  species_code: string;
  head_count: string;
}

/** Biểu mẫu thêm/sửa hộ. */
export interface HouseholdForm {
  owner_name: string;
  owner_phone: string;
  commune_code: string;
  hamlet: string;
  livestock_type: LivestockType | "";
  animals: AnimalRow[];
}

export const emptyForm = (commune = ""): HouseholdForm => ({
  owner_name: "",
  owner_phone: "",
  commune_code: commune,
  hamlet: "",
  livestock_type: "",
  animals: [{ species_code: "", head_count: "" }],
});

/** Biểu mẫu sửa từ chi tiết: họ tên đầy đủ (người sửa được hộ), SĐT để trống = giữ nguyên (máy chủ chỉ trả SĐT che). */
export function formOf(d: HouseholdDetail): HouseholdForm {
  return {
    owner_name: d.owner_name ?? "",
    owner_phone: "",
    commune_code: d.commune_code,
    hamlet: d.hamlet ?? "",
    livestock_type: d.livestock_type,
    animals: d.animals.map((a) => ({ species_code: a.species_code, head_count: String(a.head_count) })),
  };
}

const HEAD_RULE = { integer: true, max: 1_000_000_000 } as const;

/** Kiểm biểu mẫu như máy chủ (HCN-02…HCN-06); trả lỗi theo trường (khóa như lỗi của máy chủ). */
export function validateHousehold(f: HouseholdForm, mode: "create" | "edit"): Record<string, string> {
  const e: Record<string, string> = {};
  const name = f.owner_name.trim().replace(/\s+/g, " ");
  if (mode === "create" || name !== "") {
    if (!name) e.owner_name = "Bắt buộc nhập.";
    else if ([...name].length > 200) e.owner_name = "Tối đa 200 ký tự.";
  }
  if (f.owner_phone.trim() && !/^[0-9]{1,15}$/.test(f.owner_phone.trim())) e.owner_phone = "Chỉ nhập chữ số.";
  if (!f.commune_code) e.commune_code = "Chọn xã.";
  if ([...f.hamlet.trim()].length > 200) e.hamlet = "Tối đa 200 ký tự.";
  if (!f.livestock_type) e.livestock_type = "Chọn loại chăn nuôi.";
  if (f.animals.length === 0) e.animals = "Nhập ít nhất một vật nuôi.";
  if (f.animals.length > 30) e.animals = "Tối đa 30 dòng.";
  const seen = new Set<string>();
  f.animals.forEach((a, i) => {
    if (!a.species_code) e[`animals[${i}].species_code`] = "Chọn vật nuôi.";
    else if (seen.has(a.species_code)) e[`animals[${i}].species_code`] = "Vật nuôi trùng dòng khác.";
    seen.add(a.species_code);
    const n = parseNumberInput(a.head_count, HEAD_RULE);
    if (!a.head_count.trim()) e[`animals[${i}].head_count`] = "Bắt buộc nhập.";
    else if (!n.ok) e[`animals[${i}].head_count`] = n.error;
  });
  return e;
}

/** Thân gửi máy chủ (đã kiểm bằng validateHousehold). Sửa: họ tên rỗng / SĐT rỗng = không đổi. */
export function payload(f: HouseholdForm, opts: { base_version?: number; checkin?: Checkin; confirm_distinct?: boolean }): HouseholdSave {
  const name = f.owner_name.trim().replace(/\s+/g, " ");
  const phone = f.owner_phone.trim();
  const body: HouseholdSave = {
    commune_code: f.commune_code,
    hamlet: f.hamlet.trim(),
    livestock_type: f.livestock_type as LivestockType,
    animals: f.animals.map((a) => ({ species_code: a.species_code, head_count: Number(a.head_count.trim()) })),
  };
  if (name) body.owner_name = name;
  if (phone) body.owner_phone = phone;
  if (opts.base_version !== undefined) body.base_version = opts.base_version;
  if (opts.checkin) body.checkin = opts.checkin;
  if (opts.confirm_distinct) body.confirm_distinct = true;
  return body;
}

/** Lấy vị trí GPS tại nhà hộ (BR-03: không nhập tay). Không bắt được GPS → lỗi "thử lại" (luồng ngoại lệ FN-10). */
export function captureCheckin(geo: Geolocation | undefined = typeof navigator === "undefined" ? undefined : navigator.geolocation): Promise<Checkin> {
  return new Promise((resolve, reject) => {
    if (!geo) return reject(new Error("Thiết bị không có GPS — không check-in được."));
    geo.getCurrentPosition(
      (p) =>
        resolve({
          lat: p.coords.latitude,
          lon: p.coords.longitude,
          accuracy_m: accuracyText(p.coords.accuracy),
          mocked: false,
          captured_at: new Date(p.timestamp || Date.now()).toISOString(),
        }),
      () => reject(new Error("Không bắt được GPS — ra chỗ thoáng và thử lại.")),
      { enableHighAccuracy: true, timeout: 20000, maximumAge: 0 },
    );
  });
}

/** Độ chính xác (m) → chuỗi thập phân thường ≤ 2 chữ số lẻ như máy chủ nhận. */
export function accuracyText(m: number): string {
  if (!Number.isFinite(m) || m < 0) return "0";
  return (Math.round(Math.min(m, 99_999_999) * 100) / 100).toFixed(2).replace(/\.?0+$/, "");
}

export function animalsText(as: HouseholdDetail["animals"]): string {
  return as.map((a) => `${a.species_name ?? a.species_code}: ${a.head_count.toLocaleString("vi-VN")}`).join("; ");
}

export function createLivestockApi(api: ApiClient) {
  const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });
  return {
    list: (q: { commune?: string; status?: Status | ""; livestock_type?: LivestockType | ""; page: number }) =>
      unwrap(
        api.GET("/v1/extension/cnts/livestock-households", {
          params: {
            query: {
              commune: q.commune || undefined,
              status: q.status || undefined,
              livestock_type: q.livestock_type || undefined,
              limit: PAGE_SIZE,
              offset: (q.page - 1) * PAGE_SIZE,
            },
          },
        }),
      ),
    get: (ref: string) => unwrap(api.GET("/v1/extension/cnts/livestock-households/{ref}", { params: { path: { ref } } })),
    create: (body: HouseholdSave) => unwrap(api.POST("/v1/extension/cnts/livestock-households", { params: idem(), body })),
    update: (ref: string, body: HouseholdSave) =>
      unwrap(api.POST("/v1/extension/cnts/livestock-households/{ref}", { params: { path: { ref }, ...idem() }, body })),
    checkin: (ref: string, base_version: number, checkin: Checkin, reason?: string) =>
      unwrap(
        api.POST("/v1/extension/cnts/livestock-households/{ref}/checkin", {
          params: { path: { ref }, ...idem() },
          body: reason ? { base_version, checkin, reason } : { base_version, checkin },
        }),
      ),
    deactivate: (ref: string, base_version: number, reason: string) =>
      unwrap(api.POST("/v1/extension/cnts/livestock-households/{ref}/deactivate", { params: { path: { ref }, ...idem() }, body: { base_version, reason } })),
  };
}
export type LivestockApi = ReturnType<typeof createLivestockApi>;
