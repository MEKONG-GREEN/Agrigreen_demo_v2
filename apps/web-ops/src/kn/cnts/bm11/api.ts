// Client Báo cáo phòng CNTS BM-11 (W11.45 — BRD CNTS v3.7 UR-TINH-16, FN-16): danh sách, tính thử, lập, soạn phần viết,
// làm mới số liệu, trình, duyệt (tự gửi), trả lại (bắt buộc ý kiến), hộp "Báo cáo đến" + "Đã nhận", Word qua pf/export.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";

type S = components["schemas"];
export type Bm11 = S["CNTSBM11"];
export type Bm11Summary = S["CNTSBM11Summary"];
export type Bm11List = S["CNTSBM11List"];
export type Bm11Preview = S["CNTSBM11Preview"];
export type Bm11Type = Bm11Summary["type"];
export type Bm11Status = Bm11Summary["status"];
export type Bm11WordRun = S["CNTSBM11WordRun"];

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

export const TYPE_LABEL: Record<Bm11Type, string> = { week: "Tuần", month: "Tháng", quarter: "Quý", year: "Năm" };

export const STATUS_LABEL: Record<Bm11Status, [string, "neutral" | "info" | "warn" | "ok"]> = {
  draft: ["Đang soạn", "neutral"],
  pending: ["Chờ duyệt", "info"],
  returned: ["Trả lại", "warn"],
  sent: ["Đã gửi", "ok"],
};

export const RECIPIENT_LABEL: Record<string, string> = { bgd: "Ban Giám đốc", hcth: "Phòng HCTH", khtc: "Phòng KHTC" };

export const EVENT_LABEL: Record<string, string> = {
  created: "Lập báo cáo",
  refreshed: "Làm mới số liệu",
  edited: "Sửa phần viết",
  submitted: "Trình duyệt",
  returned: "Trả lại",
  approved: "Duyệt và gửi",
  received: "Đã nhận",
};

/** Gợi ý khóa kỳ theo loại (máy chủ kiểm lại): tuần YYYY-Www, tháng YYYY-MM, quý YYYY-Qn, năm YYYY. */
export const KEY_PATTERN: Record<Bm11Type, RegExp> = {
  week: /^\d{4}-W\d{2}$/,
  month: /^\d{4}-(0[1-9]|1[0-2])$/,
  quarter: /^\d{4}-Q[1-4]$/,
  year: /^\d{4}$/,
};

export const KEY_HINT: Record<Bm11Type, string> = { week: "VD 2026-W40", month: "VD 2026-09", quarter: "VD 2026-Q3", year: "VD 2026" };

/** Kiểm khóa kỳ phía máy khách (rỗng = kỳ vừa kết thúc). */
export function periodKeyError(type: Bm11Type, key: string): string | null {
  const k = key.trim();
  if (!k) return null;
  return KEY_PATTERN[type].test(k) ? null : `Kỳ chưa đúng dạng (${KEY_HINT[type]}).`;
}

/** Ý kiến trả lại: bắt buộc, tối đa 2.000 ký tự (AC-04). */
export function returnCommentError(comment: string): string | null {
  const n = [...comment.trim()].length;
  if (n === 0) return "Trả lại phải ghi ý kiến.";
  return n > 2000 ? "Ý kiến tối đa 2.000 ký tự." : null;
}

export const MAX_TEXT = 8000;

/** Các mục/đoạn đã đổi so với bản đang xem (chỉ gửi phần đổi). */
export function changedTexts(orig: Record<string, string>, edit: Record<string, string>): Record<string, string> {
  return Object.fromEntries(Object.entries(edit).filter(([k, v]) => (orig[k] ?? "") !== v));
}

export function createBm11Api(api: ApiClient) {
  const path = (id: string) => ({ path: { id } });
  return {
    list: (q: { type?: Bm11Type; status?: Bm11Status } = {}): Promise<Bm11List> =>
      unwrap(api.GET("/v1/extension/cnts/bm11/reports", { params: { query: q } })),
    inbox: (): Promise<Bm11List> => unwrap(api.GET("/v1/extension/cnts/bm11/inbox")),
    preview: (type: Bm11Type, key?: string): Promise<Bm11Preview> =>
      unwrap(api.GET("/v1/extension/cnts/bm11/preview", { params: { query: { type, key: key || undefined } } })),
    get: (id: string): Promise<Bm11> => unwrap(api.GET("/v1/extension/cnts/bm11/reports/{id}", { params: path(id) })),
    create: (type: Bm11Type, period_key: string, recipients?: Bm11Summary["recipients"]): Promise<Bm11> =>
      unwrap(api.POST("/v1/extension/cnts/bm11/reports", { params: idem(), body: { type, period_key, recipients } })),
    edit: (id: string, expected_seq: number, sections: Record<string, string>, programs: Record<string, string>): Promise<Bm11> =>
      unwrap(api.PUT("/v1/extension/cnts/bm11/reports/{id}/sections", { params: { ...path(id), ...idem() }, body: { expected_seq, sections, programs } })),
    refresh: (id: string, expected_seq: number): Promise<Bm11> =>
      unwrap(api.POST("/v1/extension/cnts/bm11/reports/{id}/refresh", { params: { ...path(id), ...idem() }, body: { expected_seq } })),
    submit: (id: string, expected_seq: number): Promise<Bm11> =>
      unwrap(api.POST("/v1/extension/cnts/bm11/reports/{id}/submit", { params: { ...path(id), ...idem() }, body: { expected_seq } })),
    approve: (id: string, expected_seq: number): Promise<Bm11> =>
      unwrap(api.POST("/v1/extension/cnts/bm11/reports/{id}/approve", { params: { ...path(id), ...idem() }, body: { expected_seq } })),
    ret: (id: string, expected_seq: number, comment: string): Promise<Bm11> =>
      unwrap(api.POST("/v1/extension/cnts/bm11/reports/{id}/return", { params: { ...path(id), ...idem() }, body: { expected_seq, comment } })),
    receive: (id: string): Promise<Bm11> => unwrap(api.POST("/v1/extension/cnts/bm11/reports/{id}/receive", { params: { ...path(id), ...idem() } })),
    word: (id: string): Promise<Bm11WordRun> => unwrap(api.POST("/v1/extension/cnts/bm11/reports/{id}/word", { params: { ...path(id), ...idem() } })),
    download: async (id: string, run: string): Promise<Blob> => {
      const res = await api.GET("/v1/extension/cnts/bm11/reports/{id}/words/{run}/download", { params: { path: { id, run } }, parseAs: "blob" });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      return res.data as Blob;
    },
  };
}

export type Bm11Api = ReturnType<typeof createBm11Api>;
