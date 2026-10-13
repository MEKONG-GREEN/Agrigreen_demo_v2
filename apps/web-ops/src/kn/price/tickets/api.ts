// Client có kiểu cho Phiếu báo giá + xác minh (W11.75, cap/price — /v1/price/tickets*, /v1/price/alerts*) trên openapi-fetch
// sinh từ hợp đồng. Giá, % là chuỗi decimal (R-10: không float — tính Δ% ở trình duyệt bằng Decimal BigInt).
import { unwrap, type ApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";
import type { components } from "@/api/generated/application";
import { Decimal, formatDecimalVi, normalizeNumberInput } from "@/kn/grid/decimal";
import { filenameFromDisposition } from "@/lib/files";

type S = components["schemas"];
export type PriceSheet = S["PriceSheet"];
export type PriceSheetLine = S["PriceSheetLine"];
export type PriceTicketDay = S["PriceTicketDay"];
export type PriceTicketRow = S["PriceTicketRow"];
export type PriceTicketHead = S["PriceTicketHead"];
export type PriceAlert = S["PriceAlert"];
export type PriceAlertRow = S["PriceAlertRow"];
export type PriceAlertView = S["PriceAlertView"];
export type PriceTicketView = S["PriceTicketView"];
export type PriceHistoryRow = S["PriceHistoryRow"];
export type PriceTicketLog = S["PriceTicketLog"];
export type PriceLineInput = S["PriceLineInput"];
export type PriceEntryInput = S["PriceEntryInput"];
export type PriceBoard = S["PriceBoard"];
export type PriceBoardLine = S["PriceBoardLine"];
export type PriceBoardState = S["PriceBoardState"];
export type PriceBoardAction = "lock" | "unlock" | "approve" | "reopen" | "cancel-reopen";

/** Trạng thái bảng giá ngày (Mục II.3.3 BRD Báo giá). */
export const BOARD_STATE_LABEL: Record<PriceBoardState, string> = {
  open: "Chưa duyệt",
  approving: "Đang duyệt",
  approved: "Đã duyệt",
  reopened: "Đang mở duyệt",
};

export const BOARD_STATE_TONE: Record<PriceBoardState, "neutral" | "ok" | "warn" | "bad" | "info"> = {
  open: "warn",
  approving: "info",
  approved: "ok",
  reopened: "warn",
};

export const BOARD_ACTION_LABEL: Record<S["PriceBoardLog"]["action"], string> = {
  lock: "Bắt đầu duyệt",
  unlock: "Hủy duyệt",
  approve: "Duyệt",
  reopen: "Mở duyệt",
  cancel_reopen: "Hủy mở duyệt",
};

export const TICKET_VIEW_LABEL: Record<PriceTicketView, string> = {
  not_entered: "Chưa nhập",
  draft: "Bản nháp",
  submitted: "Đã gửi",
  locked: "Đã khóa",
  missed: "Không nộp",
  late: "Nộp muộn",
  approved: "Đã duyệt",
  deleted: "Đã xóa",
};

export const TICKET_VIEW_TONE: Record<PriceTicketView, "neutral" | "ok" | "warn" | "bad" | "info"> = {
  not_entered: "neutral",
  draft: "warn",
  submitted: "ok",
  locked: "info",
  missed: "bad",
  late: "warn",
  approved: "ok",
  deleted: "neutral",
};

export const ALERT_VIEW_LABEL: Record<PriceAlertView, string> = {
  provisional: "Tạm tính",
  pending: "Chờ rà soát",
  requested: "Đã yêu cầu xác minh",
  responded: "Xã đã phản hồi",
  overdue: "Quá hạn phản hồi",
  adjusted: "Đã hiệu chỉnh",
  kept: "Giữ nguyên",
  excluded: "Đã loại",
  cancelled: "Tự hủy",
  reopened: "Chờ rà soát",
};

/** Cảnh báo đã xử lý (BR-02 FN-TI-02.03). */
export const alertResolved = (v: PriceAlertView) => v === "adjusted" || v === "kept" || v === "excluded" || v === "cancelled";

/** "1.234,5" từ chuỗi decimal; null/"" → "—". */
export const money = (v: string | null | undefined) => (v === null || v === undefined || v === "" ? "—" : formatDecimalVi(v, { maxDecimals: 2 }));

/** "+55,6%" / "−12,3%". */
export function pctLabel(v: string | null | undefined): string {
  if (v === null || v === undefined || v === "") return "—";
  const d = Decimal.parse(v);
  if (!d) return v;
  const s = formatDecimalVi(d.abs().toString(), { maxDecimals: 1 });
  return `${d.sign() > 0 ? "+" : d.sign() < 0 ? "−" : ""}${s}%`;
}

/** "08:00 dd/mm" giờ VN từ ISO UTC. */
export function vnTime(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(Date.parse(iso) + 7 * 3_600_000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getUTCHours())}:${p(d.getUTCMinutes())} ${p(d.getUTCDate())}/${p(d.getUTCMonth() + 1)}`;
}

export function vnToday(): string {
  return new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
}

/** Dòng đang soạn ở màn nhập (chuỗi người gõ, chưa chuẩn hóa). */
export interface Draft {
  min: string;
  max: string;
  noGoods: boolean;
  note: string;
}

export function draftOf(l: PriceSheetLine): Draft {
  return {
    min: l.min ? formatDecimalVi(l.min, { group: false }) : "",
    max: l.max ? formatDecimalVi(l.max, { group: false }) : "",
    noGoods: l.no_goods,
    note: l.note ?? "",
  };
}

/**
 * Giá đã chuẩn hóa ("1234.5") hoặc null khi trống; "bad" khi không đọc được. Giá VND gõ kiểu Việt "13.000" (nhóm 3 số
 * sau dấu chấm, không dấu phẩy) là mười ba nghìn; còn lại theo normalizeNumberInput ("12,5" = 12.5).
 */
export function priceOf(raw: string): string | null | "bad" {
  const t = raw.replace(/[\s ]/g, "");
  const s = /^\d{1,3}(\.\d{3})+$/.test(t) ? t.replace(/\./g, "") : normalizeNumberInput(raw);
  if (s === "") return null;
  const d = Decimal.parse(s);
  return d && !/[eE]/.test(s) ? d.toString() : "bad";
}

/** Lỗi của một dòng (BR-02 FN-XA-03.01) — rỗng = hợp lệ. */
export function draftProblem(d: Draft): string {
  if (d.noGoods) return "";
  const lo = priceOf(d.min);
  const hi = priceOf(d.max);
  if (lo === null && hi === null) return "";
  if (lo === "bad" || hi === "bad") return "Giá không đọc được";
  if (lo === null || hi === null) return "Nhập đủ giá thấp nhất và cao nhất (một mức giá → hai ô bằng nhau)";
  const a = Decimal.parse(lo)!;
  const b = Decimal.parse(hi)!;
  if (a.sign() <= 0 || b.sign() <= 0) return "Giá phải là số dương";
  if (a.gt(b)) return "Giá thấp nhất lớn hơn giá cao nhất";
  return "";
}

export const filled = (d: Draft) => d.noGoods || (priceOf(d.min) !== null && priceOf(d.max) !== null);

/** So dòng đang soạn với giá tham chiếu (CT-01, CT-03): hướng + Δ%; null khi chưa đủ giá / Giá mới. */
export function evalDraft(d: Draft, l: Pick<PriceSheetLine, "ref" | "threshold_pct">): { direction: "" | "high" | "low"; pct: string } | null {
  if (d.noGoods || !l.ref || draftProblem(d)) return null;
  const lo = priceOf(d.min);
  const hi = priceOf(d.max);
  if (typeof lo !== "string" || typeof hi !== "string" || lo === "bad" || hi === "bad") return null;
  const p = Decimal.parse(lo)!.add(Decimal.parse(hi)!).divRound(Decimal.fromInt(2), 4);
  const ref = Decimal.parse(l.ref.mid)!;
  if (ref.sign() <= 0) return null;
  const pct = p.sub(ref).mul(Decimal.fromInt(100)).divRound(ref, 4);
  const t = Decimal.parse(l.threshold_pct)!;
  const direction = !pct.lt(t) ? "high" : !pct.gt(t.neg()) ? "low" : "";
  return { direction, pct: pct.toString() };
}

/** Dòng gửi lên từ bản soạn. */
export function lineInput(code: string, d: Draft): PriceLineInput {
  const lo = priceOf(d.min);
  const hi = priceOf(d.max);
  return {
    item_code: code,
    min: d.noGoods || lo === "bad" ? null : lo,
    max: d.noGoods || hi === "bad" ? null : hi,
    no_goods: d.noGoods,
    note: d.note.trim() || null,
  };
}

/** Nhóm dòng theo nhóm hàng (giữ thứ tự máy chủ). */
export function byGroup<T extends { group_code: string; group_name: string }>(rows: T[]): { code: string; name: string; rows: T[] }[] {
  const out: { code: string; name: string; rows: T[] }[] = [];
  for (const r of rows) {
    let g = out.find((x) => x.code === r.group_code);
    if (!g) {
      g = { code: r.group_code, name: r.group_name, rows: [] };
      out.push(g);
    }
    g.rows.push(r);
  }
  return out;
}

export function createPriceTicketApi(api: ApiClient) {
  const header = () => ({ "Idempotency-Key": newIdempotencyKey() });
  return {
    day: (q: { day?: string; region?: string; mine?: boolean }): Promise<PriceTicketDay> =>
      unwrap(api.GET("/v1/price/tickets", { params: { query: q } })),
    sheet: (q: { point?: string; day?: string }): Promise<PriceSheet> =>
      unwrap(api.GET("/v1/price/tickets/sheet", { params: { query: q } })),
    ticket: (id: string): Promise<PriceSheet> => unwrap(api.GET("/v1/price/tickets/{id}", { params: { path: { id } } })),
    log: (id: string): Promise<PriceTicketLog> => unwrap(api.GET("/v1/price/tickets/{id}/log", { params: { path: { id } } })),
    save: (body: PriceEntryInput): Promise<PriceSheet> =>
      unwrap(api.POST("/v1/price/tickets/sheet", { params: { header: header() }, body })),
    proxy: (body: PriceEntryInput): Promise<PriceSheet> =>
      unwrap(api.POST("/v1/price/tickets/proxy", { params: { header: header() }, body })),
    correct: (id: string, body: S["PriceCorrectInput"]): Promise<PriceSheet> =>
      unwrap(api.POST("/v1/price/tickets/{id}/correct", { params: { header: header(), path: { id } }, body })),
    redate: (id: string, body: S["PriceRedateInput"]): Promise<PriceSheet> =>
      unwrap(api.POST("/v1/price/tickets/{id}/redate", { params: { header: header(), path: { id } }, body })),
    remove: (id: string, body: S["PriceDeleteInput"]): Promise<PriceSheet> =>
      unwrap(api.POST("/v1/price/tickets/{id}/delete", { params: { header: header(), path: { id } }, body })),
    history: (q: { point?: string; from?: string; to?: string }): Promise<{ rows: PriceHistoryRow[] }> =>
      unwrap(api.GET("/v1/price/tickets/history", { params: { query: q } })),
    alerts: (q: { from?: string; to?: string; point?: string; mine?: boolean; view?: PriceAlertView }): Promise<{ alerts: PriceAlertRow[] }> =>
      unwrap(api.GET("/v1/price/alerts", { params: { query: q } })),
    request: (body: S["PriceVerificationRequest"]): Promise<S["PriceWritten"]> =>
      unwrap(api.POST("/v1/price/alerts/request", { params: { header: header() }, body })),
    respond: (body: S["PriceVerificationResponse"]): Promise<S["PriceWritten"]> =>
      unwrap(api.POST("/v1/price/alerts/respond", { params: { header: header() }, body })),
    decide: (body: S["PriceAlertDecision"]): Promise<S["PriceWritten"]> =>
      unwrap(api.POST("/v1/price/alerts/decide", { params: { header: header() }, body })),
    /** Bảng giá ngày — màn Kiểm duyệt & công bố (W11.76). */
    board: (day: string): Promise<PriceBoard> => unwrap(api.GET("/v1/price/boards/{day}", { params: { path: { day } } })),
    boards: (q: { from?: string; to?: string }): Promise<{ boards: S["PriceBoardDay"][] }> =>
      unwrap(api.GET("/v1/price/boards", { params: { query: q } })),
    /** Thao tác bảng giá (khóa lạc quan theo seq): lock = "Duyệt bảng giá", approve = xác nhận, reopen = Mở duyệt. */
    boardOp: (day: string, action: PriceBoardAction, body: S["PriceBoardOp"]): Promise<PriceBoard> => {
      const opts = { params: { header: header(), path: { day } }, body };
      switch (action) {
        case "lock":
          return unwrap(api.POST("/v1/price/boards/{day}/lock", opts));
        case "unlock":
          return unwrap(api.POST("/v1/price/boards/{day}/unlock", opts));
        case "approve":
          return unwrap(api.POST("/v1/price/boards/{day}/approve", opts));
        case "reopen":
          return unwrap(api.POST("/v1/price/boards/{day}/reopen", opts));
        default:
          return unwrap(api.POST("/v1/price/boards/{day}/cancel-reopen", opts));
      }
    },
    /** Excel chi tiết báo giá của ngày (Cấp Tỉnh). */
    exportDay: async (q: { day?: string; region?: string; mine?: boolean }): Promise<{ blob: Blob; name: string }> => {
      const res = await api.GET("/v1/price/tickets/export", { params: { query: q }, parseAs: "blob" });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      return { blob: res.data as Blob, name: filenameFromDisposition(res.response.headers.get("Content-Disposition"), "chi-tiet-bao-gia.xlsx") };
    },
    /** In phiếu (PDF / Excel) — gộp nhiều phiếu một tệp. */
    print: async (ticket: string[], format: "pdf" | "xlsx"): Promise<{ blob: Blob; name: string }> => {
      const res = await api.GET("/v1/price/tickets/print", { params: { query: { ticket, format } }, parseAs: "blob" });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      return { blob: res.data as Blob, name: filenameFromDisposition(res.response.headers.get("Content-Disposition"), `phieu-bao-gia.${format}`) };
    },
  };
}

export type PriceTicketApi = ReturnType<typeof createPriceTicketApi>;

/** Mặc định nội dung yêu cầu xác minh (FN-TI-02.03 B3). */
export function defaultRequestMessage(item: string, point: string, pct: string): string {
  return `Giá ${item} tại ${point} chênh ${pctLabel(pct).replace("+", "")} so với giá tham chiếu, đề nghị xã kiểm tra lại`;
}
