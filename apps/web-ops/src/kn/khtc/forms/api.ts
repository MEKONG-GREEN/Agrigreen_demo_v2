// Client phiếu "Báo cáo cho KHTC" (W11.63 rút gọn — BRD KHTC v2.2 FN-PN-03.01, FN-PN-03.02, FN-TI-02.04): phiếu kỳ này
// của phòng, form phiếu theo mẫu nạp sẵn (lưới hạng mục theo Kế hoạch năm, cột "Từ Kế hoạch năm", "Lũy kế kỳ trước",
// khối "Điều chỉnh kỳ trước", mục lời văn), lưu nháp ở máy chủ, gửi KHTC, rà soát (/v1/extension/khtc/*).
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import {createRetryingMutation} from "../adhoc/retry";
import { newIdempotencyKey } from "@/api/idempotency";

type S = components["schemas"];
export type KhtcFormSheet = S["KHTCFormSheet"];
export type KhtcFormTask = S["KHTCFormTask"];
export type KhtcFormContent = S["KHTCFormContent"];
export type KhtcFormCell = S["KHTCFormCell"];
export type KhtcFormAdjustment = S["KHTCFormAdjustment"];
export type KhtcFormColumn = S["KHTCFormColumn"];
export type KhtcFormRow = S["KHTCFormRow"];
export type KhtcFormIssue = S["KHTCFormIssue"];

export const KHTC_DEPT = "ke_hoach_tai_chinh";

/** Nhãn loại cột (FN-AD-04.02 B3). */
export const COLUMN_KIND_LABEL: Record<KhtcFormColumn["kind"], string> = {
  fixed: "Cố định",
  from_plan: "Từ Kế hoạch năm",
  prior_cumulative: "Lũy kế kỳ trước",
  input: "Nhập",
  formula: "Công thức",
};

/** Nhãn quy tắc kiểm tra (FN-TI-02.04 — đúng 3 nhóm). */
export const RULE_LABEL: Record<KhtcFormIssue["rule"], string> = {
  template: "Yêu cầu của mẫu",
  negative_reason: "Số âm phải có lý do",
  over_plan: "Lũy kế vượt 100% KH",
};

/** Nút thao tác ở "Phiếu kỳ này" (FN-PN-03.01 B3). */
export function taskAction(
  state: KhtcFormTask["state"],
): "Nhập" | "Tiếp tục" | "Sửa, gửi lại" | "Xem" {
  switch (state) {
    case "not_started":
    case "late":
      return "Nhập";
    case "draft":
      return "Tiếp tục";
    case "supplement":
      return "Sửa, gửi lại";
    default:
      return "Xem";
  }
}

/** Nội dung rỗng chuẩn hóa. */
export function emptyContent(): KhtcFormContent {
  return { cells: {}, adjustments: [], texts: {} };
}

/** Bản sao sâu nội dung (để sửa cục bộ). */
export function cloneContent(
  c: KhtcFormContent | null | undefined,
): KhtcFormContent {
  const src = c ?? emptyContent();
  return {
    ...(src.adhoc_request_id ? {adhoc_request_id:src.adhoc_request_id,adhoc_values:{...src.adhoc_values},adhoc_text:src.adhoc_text??""}:{}),
    cells: Object.fromEntries(
      Object.entries(src.cells ?? {}).map(([k, v]) => [k, { ...v }]),
    ),
    adjustments: (src.adjustments ?? []).map((a) => ({ ...a })),
    texts: { ...(src.texts ?? {}) },
  };
}

const NUM = /^-?\d{1,15}(\.\d{1,6})?$/;

/** Đọc số thập phân của ô (chấp nhận dấu phẩy thập phân kiểu Việt); không phải số → null. */
export function parseNum(v: string | null | undefined): number | null {
  if (v === null || v === undefined) return null;
  const s = v.trim().replace(",", ".");
  if (s === "" || !NUM.test(s)) return null;
  return Number(s);
}

/** Làm tròn 1 chữ số, bỏ ",0" (BR-DL-05: 110,7%; 50%). */
export function pct(n: number): string {
  const r = Math.round(n * 10) / 10;
  return Number.isInteger(r) ? String(r) : r.toFixed(1).replace(".", ",");
}

/**
 * Ô tự tính xem trước khi lưu (máy chủ tính lại khi Lưu nháp / Gửi): G = Σ điều chỉnh cùng năm kế hoạch, H = E + F + G,
 * I = H / D × 100. Dòng nguồn module không tính (bộ đọc nguồn điền).
 */
export function liveCells(
  row: KhtcFormRow,
  content: KhtcFormContent,
  adjYears: Record<string, number>,
  planYear: number,
): { G: number | null; H: number | null; I: string; over: boolean } {
  if (row.module_sourced) return { G: null, H: null, I: "", over: false };
  const prior = parseNum(row.cells["E"]) ?? 0;
  const done = parseNum(content.cells[row.entry_ref]?.done) ?? 0;
  let g: number | null = null;
  for (const a of content.adjustments) {
    if (a.entry_ref !== row.entry_ref || adjYears[a.period_id] !== planYear)
      continue;
    const v = parseNum(a.amount);
    if (v !== null) g = (g ?? 0) + v;
  }
  const h = Math.round((prior + done + (g ?? 0)) * 1e6) / 1e6;
  const plan = parseNum(row.planned_qty);
  const i = plan && plan > 0 ? pct((h * 100) / plan) : "";
  return { G: g, H: h, I: i, over: !!plan && plan > 0 && h > plan };
}

/** Chuẩn hóa nội dung trước khi gửi: số dùng dấu chấm, bỏ ô trống. */
export function normalizeForSend(c: KhtcFormContent): KhtcFormContent {
  const fix = (v?: string) =>
    (v ?? "").trim().replace(/^(-?\d+),(\d+)$/, "$1.$2");
  const cells: KhtcFormContent["cells"] = {};
  for (const [k, v] of Object.entries(c.cells)) {
    const n = {
      done: fix(v.done),
      status: (v.status ?? "").trim(),
      content: (v.content ?? "").trim(),
      people: fix(v.people),
      budget: fix(v.budget),
    };
    if (Object.values(n).some((x) => x !== "")) cells[k] = n;
  }
  if(Object.keys(c.adhoc_values??{}).length>20 || Object.values(c.adhoc_values??{}).some(v=>v.length>23) || (c.adhoc_text?.length??0)>32000)throw new Error("Câu trả lời bổ sung quá lớn.");
  return {
    ...(c.adhoc_request_id ? {adhoc_request_id:c.adhoc_request_id,adhoc_values:Object.fromEntries(Object.entries(c.adhoc_values??{}).map(([k,v])=>[k,fix(v)])),adhoc_text:c.adhoc_text?.trim()??""}:{}),
    cells,
    adjustments: c.adjustments.map((a) => ({
      ...a,
      amount: fix(a.amount),
      reason: (a.reason ?? "").trim(),
    })),
    texts: Object.fromEntries(
      Object.entries(c.texts).filter(([, v]) => (v ?? "").trim() !== ""),
    ),
  };
}

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

export function createKhtcFormApi(api: ApiClient) {
  const mutate=createRetryingMutation();
  const path = (id: string, dept: string) => ({ path: { id, dept } });
  return {
    tasks: async (department: string): Promise<KhtcFormTask[]> =>
      (
        await unwrap(
          api.GET("/v1/extension/khtc/my-forms", {
            params: { query: { department } },
          }),
        )
      ).items,
    get: (id: string, dept: string): Promise<KhtcFormSheet> =>
      unwrap(
        api.GET("/v1/extension/khtc/periods/{id}/forms/{dept}", {
          params: path(id, dept),
        }),
      ),
    draft: (
      id: string,
      dept: string,
      sheet: KhtcFormContent,
      base_attempt: number,
    ): Promise<KhtcFormSheet> =>
      unwrap(
        api.POST("/v1/extension/khtc/periods/{id}/forms/{dept}/draft", {
          params: { ...path(id, dept), ...idem() },
          body: { sheet: normalizeForSend(sheet), base_attempt },
        }),
      ),
    submit: (
      id: string,
      dept: string,
      sheet: KhtcFormContent,
      base_attempt: number,
    ): Promise<KhtcFormSheet> => {
      const body={sheet:normalizeForSend(sheet),base_attempt};
      return mutate("submit:"+id+":"+dept+":"+base_attempt,body,key=>unwrap(api.POST("/v1/extension/khtc/periods/{id}/forms/{dept}/submit",{
        params:{...path(id,dept),header:{"Idempotency-Key":key}},body
      })));
    },
    review: (
      id: string,
      dept: string,
      decision: "returned" | "accepted",
      note?: string,
    ): Promise<KhtcFormSheet> =>
      unwrap(
        api.POST("/v1/extension/khtc/periods/{id}/forms/{dept}/review", {
          params: { ...path(id, dept), ...idem() },
          body: { decision, note },
        }),
      ),
  };
}

export type KhtcFormApi = ReturnType<typeof createKhtcFormApi>;
