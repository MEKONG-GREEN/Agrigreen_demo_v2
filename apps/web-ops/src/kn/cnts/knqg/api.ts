// Client mẫu biểu dự án KNQG (W11.53 — BRD CNTS v3.7 FN-19, UR-TINH-20…24, UR-QG-01): /v1/extension/cnts/knqg-specs,
// /knqg-docs*. Màn soạn biểu dựng theo đặc tả máy chủ (mục, ô, bảng của mẫu Word gốc). Quy tắc thuần ở đây để kiểm thử
// đơn vị; máy chủ kiểm lại (ô tự điền, thực tế từ nhật ký, điều kiện gửi Mẫu 07).
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";

type S = components["schemas"];
export type Spec = S["CNTSKNQGSpec"];
export type SpecField = S["CNTSKNQGField"];
export type SpecTable = S["CNTSKNQGTable"];
export type Doc = S["CNTSKNQGDoc"];
export type DocDetail = S["CNTSKNQGDocDetail"];
export type Content = S["CNTSKNQGContent"];
export type Create = S["CNTSKNQGCreate"];
export type WordRun = S["CNTSKNQGWordRun"];
export type Action = Doc["actions"][number];
export type Row = Record<string, string>;

export const FORM_LABEL: Record<Doc["form"], string> = {
  "01": "Mẫu 01 — Tiến độ tháng",
  "03": "Mẫu 03 — Kiểm tra, đánh giá",
  "04": "Mẫu 04 — Nghiệm thu tập huấn",
  "05": "Mẫu 05 — Nghiệm thu điểm trình diễn",
  "07": "Mẫu 07 — Nghiệm thu dự án năm",
};

export const STATUS_LABEL: Record<Doc["status"], string> = {
  draft: "Đang lập",
  submitted: "Chờ duyệt",
  approved: "Đã duyệt",
  returned: "Trả lại",
  awaiting_sign: "Chờ ký",
  signed: "Đã ký",
  sent: "Đã gửi Quốc gia",
  confirmed: "Đã xác nhận",
};

export const STATUS_TONE: Record<Doc["status"], "ok" | "info" | "neutral" | "bad" | "warn"> = {
  draft: "neutral",
  submitted: "info",
  approved: "ok",
  returned: "bad",
  awaiting_sign: "info",
  signed: "ok",
  sent: "info",
  confirmed: "ok",
};

export const ACTION_LABEL: Record<Action, string> = {
  submit: "Trình duyệt",
  approve: "Duyệt",
  return: "Trả lại",
  request_sign: "Trình ký",
  unrequest: "Sửa tiếp",
  sign: "Tải bản đã ký",
  send: "Gửi TTKN Quốc gia",
  confirm: "Xác nhận",
  adjust: "Lập bản điều chỉnh",
};

/** Thao tác cần ý kiến/lý do (FN-19: trả lại không ghi ý kiến → chặn; điều chỉnh có lý do). */
export const NEEDS_REASON: Action[] = ["return", "adjust"];

/** Nhãn kỳ: "2027-10" → "Tháng 10/2027"; "2027" → "Năm 2027". */
export function periodLabel(p: string | null | undefined): string {
  if (!p) return "—";
  const m = /^(\d{4})-(\d{2})$/.exec(p);
  if (m) return `Tháng ${Number(m[2])}/${m[1]}`;
  return /^\d{4}$/.test(p) ? `Năm ${p}` : p;
}

/** Dòng mới (rỗng) cho bảng: chỉ cột không tự điền. */
export function emptyRow(t: SpecTable): Row {
  return Object.fromEntries(t.columns.filter((c) => !c.auto && c.key !== "ma").map((c) => [c.key, ""]));
}

/** Lỗi phía máy khách theo kiểu ô (số không âm, số nguyên, ngày) — máy chủ kiểm lại. */
export function cellError(kind: SpecField["kind"], v: string): string | null {
  const s = v.trim();
  if (!s) return null;
  if (kind === "number" || kind === "int") {
    if (!/^\d+(\.\d+)?$/.test(s)) return "Số không âm, dấu chấm thập phân.";
    if (kind === "int" && s.includes(".")) return "Số nguyên.";
  }
  if (kind === "date" && !/^\d{4}-\d{2}-\d{2}$/.test(s)) return "Ngày dạng YYYY-MM-DD.";
  return null;
}

/** Lỗi phía máy khách của nội dung (khóa trường như máy chủ: content.fields.x / content.tables.t.i.c). */
export function contentErrors(spec: Spec, c: Content): Record<string, string> {
  const e: Record<string, string> = {};
  for (const f of spec.fields) {
    const err = cellError(f.kind, c.fields[f.key] ?? "");
    if (err) e[`content.fields.${f.key}`] = err;
  }
  for (const t of spec.tables) {
    const rows = c.tables[t.key] ?? [];
    if (rows.length > t.max_rows) e[`content.tables.${t.key}`] = `Tối đa ${t.max_rows} dòng.`;
    rows.forEach((r, i) =>
      t.columns.forEach((col) => {
        const err = cellError(col.kind, r[col.key] ?? "");
        if (err) e[`content.tables.${t.key}.${i}.${col.key}`] = err;
      }),
    );
  }
  return e;
}

/** Nội dung gửi lên: bỏ ô/cột tự điền và bảng tự dựng (máy chủ đặt lại), bỏ ô rỗng. */
export function contentForSave(spec: Spec, c: Content): Content {
  const fields: Record<string, string> = {};
  for (const f of spec.fields) {
    const v = (c.fields[f.key] ?? "").trim();
    if (!f.auto && v) fields[f.key] = v;
  }
  const tables: Record<string, Row[]> = {};
  for (const t of spec.tables) {
    if (t.auto) continue;
    const keep = new Set(t.columns.filter((col) => !col.auto).map((col) => col.key));
    tables[t.key] = (c.tables[t.key] ?? [])
      .map((r) => Object.fromEntries(Object.entries(r).filter(([k, v]) => keep.has(k) && v.trim() !== "")))
      .filter((r) => Object.keys(r).length > 0);
  }
  return { fields, tables };
}

/** Mẫu được phép lập theo dự án đã chọn: 05 cần điểm; 01 cần tháng; 07 cần năm. */
export function createErrors(spec: Spec | undefined, v: { project_ref?: string; site_ref?: string; period?: string }): Record<string, string> {
  const e: Record<string, string> = {};
  if (!spec) e.form = "Chọn mẫu.";
  if (!v.project_ref) e.project_ref = "Chọn dự án KNQG.";
  if (spec?.per_site && !v.site_ref) e.site_ref = "Chọn điểm trình diễn.";
  if (spec?.period === "month" && !/^\d{4}-(0[1-9]|1[0-2])$/.test(v.period ?? "")) e.period = "Chọn tháng.";
  if (spec?.period === "year" && !/^\d{4}$/.test(v.period ?? "")) e.period = "Nhập năm (YYYY).";
  return e;
}

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

export function createKnqgApi(api: ApiClient) {
  return {
    specs: async (): Promise<Spec[]> => (await unwrap(api.GET("/v1/extension/cnts/knqg-specs"))).items,
    list: async (q: { project_ref?: string; form?: Doc["form"]; status?: string } = {}): Promise<Doc[]> =>
      (await unwrap(api.GET("/v1/extension/cnts/knqg-docs", { params: { query: q } }))).items,
    get: (ref: string): Promise<DocDetail> => unwrap(api.GET("/v1/extension/cnts/knqg-docs/{ref}", { params: { path: { ref } } })),
    create: (body: Create): Promise<DocDetail> => unwrap(api.POST("/v1/extension/cnts/knqg-docs", { params: idem(), body })),
    update: (ref: string, base_version: number, content: Content): Promise<DocDetail> =>
      unwrap(api.POST("/v1/extension/cnts/knqg-docs/{ref}", { params: { path: { ref }, ...idem() }, body: { base_version, content } })),
    act: (ref: string, action: Exclude<Action, "sign">, base_version: number, reason?: string): Promise<DocDetail> =>
      unwrap(
        api.POST("/v1/extension/cnts/knqg-docs/{ref}/actions/{action}", {
          params: { path: { ref, action }, ...idem() },
          body: reason ? { base_version, reason } : { base_version },
        }),
      ),
    sign: (ref: string, base_version: number, file: File): Promise<DocDetail> =>
      unwrap(
        api.POST("/v1/extension/cnts/knqg-docs/{ref}/signed", {
          params: { path: { ref }, ...idem() },
          body: { meta: JSON.stringify({ base_version }), file: file as unknown as string },
          bodySerializer: (b: { meta: string; file: string }) => {
            const fd = new FormData();
            fd.append("meta", b.meta);
            fd.append("file", b.file as unknown as Blob, file.name);
            return fd;
          },
        }),
      ),
    async scan(ref: string, version?: number): Promise<Blob> {
      const res = await api.GET("/v1/extension/cnts/knqg-docs/{ref}/signed", { params: { path: { ref }, query: version ? { version } : {} }, parseAs: "blob" });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      return res.data as Blob;
    },
    exportWord: (ref: string): Promise<WordRun> => unwrap(api.POST("/v1/extension/cnts/knqg-docs/{ref}/word", { params: { path: { ref }, ...idem() } })),
    words: async (ref: string): Promise<WordRun[]> =>
      (await unwrap(api.GET("/v1/extension/cnts/knqg-docs/{ref}/words", { params: { path: { ref } } }))).items,
    async download(ref: string, run: string): Promise<Blob> {
      const res = await api.GET("/v1/extension/cnts/knqg-docs/{ref}/words/{run}/download", { params: { path: { ref, run } }, parseAs: "blob" });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      return res.data as Blob;
    },
  };
}
export type KnqgApi = ReturnType<typeof createKnqgApi>;

/** Lưu tệp về máy. */
export function saveBlob(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}
