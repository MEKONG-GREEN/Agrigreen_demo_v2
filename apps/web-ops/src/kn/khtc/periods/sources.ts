// Client bộ đọc nguồn module của Kỳ KHTC (W11.64 — BRD KHTC v2.2 BR-DL-02…04; ADR-0053 §8): trạng thái nguồn (mốc dữ
// liệu, "Nguồn đã thay đổi", số đã chụp), "Cập nhật số liệu", lịch sử (/v1/extension/khtc/periods/{id}/sources*).
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";

type S = components["schemas"];
export type KhtcSourceStatus = S["KHTCSourceStatus"];
export type KhtcSourceValue = S["KHTCSourceValue"];
export type KhtcSourceAsOf = S["KHTCSourceAsOf"];
export type KhtcSourceChange = S["KHTCSourceChange"];
export type KhtcSourceHistoryItem = S["KHTCSourceHistoryItem"];

export const DEPT_NAME: Readonly<Record<string, string>> = {
  trong_trot: "Phòng Trồng trọt",
  chan_nuoi_thuy_san: "Phòng Chăn nuôi - Thủy sản",
  thong_tin_truyen_thong: "Phòng Thông tin Tuyên truyền (Báo giá)",
  hanh_chinh_tong_hop: "Phòng Hành chính - Tổng hợp",
};

export const FIELD_LABEL: Readonly<Record<string, string>> = {
  cumulative: "Lũy kế",
  increment: "Trong kỳ",
  sum: "Số phát sinh",
  price_min: "Giá từ",
  price_max: "Giá đến",
};

export const CHANGE_LABEL: Readonly<Record<KhtcSourceChange["kind"], string>> = {
  added: "kỳ nguồn mới",
  removed: "kỳ nguồn không còn",
  figures: "số đã chốt đổi",
};

/** Số thập phân dạng chuỗi → kiểu Việt Nam (chấm ngăn nghìn, phẩy thập phân); giữ dấu âm. */
export function vnNum(s: string | null | undefined): string {
  if (s == null || s === "") return "—";
  const neg = s.startsWith("-");
  const [int, frac] = (neg ? s.slice(1) : s).split(".");
  const grouped = int!.replace(/\B(?=(\d{3})+(?!\d))/g, ".");
  return `${neg ? "−" : ""}${grouped}${frac ? `,${frac}` : ""}`;
}

/** "2026-W48" → "tuần 48/2026". */
export function weekText(key: string): string {
  const m = /^(\d{4})-W(\d{2})$/.exec(key);
  return m ? `tuần ${Number(m[2])}/${m[1]}` : key;
}

/** Gốc của số trong kỳ (khtc-week-mapping Q4). */
export function baseRefText(ref: string | undefined): string {
  if (!ref) return "";
  const [kind, week] = ref.split(":");
  switch (kind) {
    case "frozen":
      return `lũy kế đã đóng ${weekText(week ?? "")}`;
    case "current":
      return `lũy kế hiện tại ${weekText(week ?? "")}`;
    case "reset":
      return "chuỗi lũy kế mới (gốc 0)";
    default:
      return "đầu năm đọc";
  }
}

/** Số hiển thị của một khóa nguồn. */
export function valueText(v: KhtcSourceValue): string {
  if (v.invalid) return "Khóa nguồn sai dạng";
  switch (v.mode) {
    case "price":
      return v.price_min == null
        ? "Chưa có bảng giá đã duyệt"
        : `${vnNum(v.price_min)} – ${vnNum(v.price_max)}`;
    case "sum":
      return vnNum(v.sum);
    default:
      return v.cumulative == null
        ? "—"
        : `Lũy kế ${vnNum(v.cumulative)} · trong kỳ ${vnNum(v.increment)}`;
  }
}

/** Nhãn trạng thái số (BR-DL-03; D2). */
export function valueBadges(v: KhtcSourceValue): string[] {
  const out: string[] = [];
  if (v.invalid) return ["Sai khóa nguồn"];
  out.push(v.provisional ? "Tạm tính" : "Đã chốt");
  if (v.no_source) out.push("Không phát sinh");
  if (v.frozen) out.push("Giữ số kỳ đã đóng");
  if (v.fallback) out.push("Ngày duyệt gần nhất trước kỳ");
  if (v.unavailable) out.push("Nguồn chưa công bố");
  if (v.unconfirmed && v.unconfirmed !== "0") out.push(`Chưa xác nhận ${vnNum(v.unconfirmed)}`);
  return out;
}

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

export function createKhtcSourceApi(api: ApiClient) {
  return {
    status: (id: string): Promise<KhtcSourceStatus> =>
      unwrap(api.GET("/v1/extension/khtc/periods/{id}/sources", { params: { path: { id } } })),
    refresh: (id: string): Promise<KhtcSourceStatus> =>
      unwrap(api.POST("/v1/extension/khtc/periods/{id}/sources/refresh", { params: { path: { id }, ...idem() } })),
    history: async (id: string): Promise<KhtcSourceHistoryItem[]> =>
      (await unwrap(api.GET("/v1/extension/khtc/periods/{id}/sources/history", { params: { path: { id } } }))).items,
  };
}

export type KhtcSourceApi = ReturnType<typeof createKhtcSourceApi>;
