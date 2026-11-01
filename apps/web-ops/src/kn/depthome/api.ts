// Client Trang chủ phòng + kho văn bản phòng (W11.54 — D4 CN14; BRD CNTS v3.7 FN-17, FN-20): /v1/extension/dept-home*,
// /v1/extension/dept-docs*. Admin chọn phòng bằng department (màn /kn/phong-ban/{mã}/…); vai khác dùng phòng của ngữ cảnh.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { stableMultipart } from "@/api/multipart";
import { parseProblemResponse } from "@/api/problem";
import { filenameFromDisposition } from "@/lib/files";

type S = components["schemas"];
export type DeptHome = S["DeptHome"];
export type DeptKPI = S["DeptKPI"];
export type DeptDoc = S["DeptDoc"];
export type DeptDocInput = S["DeptDocInput"];
export type DeptDocFolder = S["DeptDocFolder"];
export type DeptDocList = S["DeptDocList"];

export const KIND_LABEL: Record<DeptDoc["kind"], string> = {
  quy_trinh_ky_thuat: "Quy trình kỹ thuật",
  van_ban_phap_quy: "Văn bản pháp quy, chính sách",
  van_ban_chi_dao: "Văn bản chỉ đạo",
  ke_hoach: "Kế hoạch",
  mau_bieu: "Mẫu biểu",
  tai_lieu_tap_huan: "Tài liệu tập huấn",
  khac: "Khác",
};
export const VALIDITY_LABEL: Record<DeptDoc["validity"], string> = { active: "Còn hiệu lực", pending: "Chưa có hiệu lực", expired: "Hết hiệu lực" };
export const VALIDITY_TONE: Record<DeptDoc["validity"], "ok" | "info" | "neutral"> = { active: "ok", pending: "info", expired: "neutral" };
export const DEPT_NAME: Record<string, string> = {
  trong_trot: "Trồng trọt",
  chan_nuoi_thuy_san: "Chăn nuôi – Thủy sản",
  ke_hoach_tai_chinh: "Kế hoạch – Tài chính",
  thong_tin_truyen_thong: "Thông tin – Truyền thông",
  hanh_chinh_tong_hop: "Hành chính – Tổng hợp",
};
export const MGMT_LABEL: Record<string, string> = {
  giam_doc: "Giám đốc",
  pho_giam_doc: "Phó Giám đốc",
  truong_phong: "Trưởng phòng",
  pho_truong_phong: "Phó Trưởng phòng",
  phu_trach: "Phụ trách",
  chuyen_vien: "Chuyên viên",
};

/** Màn chi tiết của thẻ chỉ số (FN-17 B3: bấm thẻ để mở màn chi tiết). */
export function kpiLink(key: string, dept?: string): string | null {
  const pb = (s: string) => (dept ? `/kn/phong-ban/${dept}/${s}` : null);
  const m: Record<string, string | null> = {
    alerts_open: "/kn/cnts/canh-bao",
    alerts_overdue: "/kn/cnts/canh-bao",
    outbreaks_active: "/kn/cnts/dich-benh",
    tasks_open: "/kn/nhiem-vu/cong-viec",
    tasks_overdue: "/kn/nhiem-vu/cong-viec",
    schedules_week: "/kn/nhiem-vu/lich-cong-tac",
    schedules_pending: "/kn/nhiem-vu/lich-cong-tac",
    khtc_periods_year: "/kn/khtc/ky-bao-cao",
    khtc_exports: "/kn/khtc/bao-cao-da-xuat",
    price_items: "/kn/bao-gia/bang-gia",
    price_tickets_week: "/kn/bao-gia/thong-ke",
    docs_total: dept ? pb("van-ban") : "/kn/van-ban",
    docs_expired: dept ? pb("van-ban") : "/kn/van-ban",
  };
  return m[key] ?? null;
}

const KPI_COLOR: Record<DeptKPI["tone"], string> = { ok: "green", info: "blue", warn: "orange", bad: "red", neutral: "violet" };
export const kpiColor = (t: DeptKPI["tone"]) => KPI_COLOR[t] ?? "blue";

export function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${(n / 1024 / 1024).toFixed(1).replace(".", ",")} MB`;
}

export const MAX_BYTES = 20 * 1024 * 1024;
export const EXTENSIONS = [".pdf", ".docx", ".xlsx"];

/** Kiểm tệp trước khi gửi (máy chủ kiểm lại theo nội dung). */
export function fileError(f: File | null): string | null {
  if (!f) return "Chọn tệp";
  if (f.size > MAX_BYTES) return "Tệp vượt 20 MB";
  const ext = /\.[^.]+$/.exec(f.name.toLowerCase())?.[0] ?? "";
  if (!EXTENSIONS.includes(ext)) return "Chỉ nhận PDF, DOCX, XLSX";
  return null;
}

/** Biểu nhập thuộc tính → thân DeptDocInput (bỏ trống thành chuỗi rỗng). */
export interface DocForm {
  folder_ref: string;
  title: string;
  kind: DeptDoc["kind"] | "";
  field: string;
  number: string;
  issuer: string;
  summary: string;
  issued_on: string;
  effective_from: string;
  effective_to: string;
  xa: boolean;
  nong_ho: boolean;
}

export const emptyDocForm = (folder = ""): DocForm => ({
  folder_ref: folder, title: "", kind: "", field: "", number: "", issuer: "", summary: "", issued_on: "", effective_from: "", effective_to: "",
  xa: true, nong_ho: false,
});

export function docFormOf(d: DeptDoc): DocForm {
  return {
    folder_ref: d.folder_ref ?? "", title: d.title, kind: d.kind, field: d.field ?? "", number: d.number ?? "", issuer: d.issuer ?? "",
    summary: d.summary ?? "", issued_on: d.issued_on ?? "", effective_from: d.effective_from ?? "", effective_to: d.effective_to ?? "",
    xa: d.audience.includes("xa"), nong_ho: d.audience.includes("nong_ho"),
  };
}

export function docFormErrors(f: DocForm): Record<string, string> {
  const e: Record<string, string> = {};
  if (!f.title.trim()) e.title = "Bắt buộc";
  if (!f.kind) e.kind = "Chọn loại văn bản";
  if (f.effective_from && f.effective_to && f.effective_to < f.effective_from) e.effective_to = "Trước ngày có hiệu lực";
  return e;
}

export function docPayload(f: DocForm, base?: number): DeptDocInput {
  const audience: DeptDocInput["audience"] = [];
  if (f.xa) audience.push("xa");
  if (f.nong_ho) audience.push("nong_ho");
  return {
    folder_ref: f.folder_ref || null, title: f.title.trim(), kind: f.kind as DeptDoc["kind"], field: f.field.trim(), number: f.number.trim(),
    issuer: f.issuer.trim(), summary: f.summary.trim(), issued_on: f.issued_on, effective_from: f.effective_from, effective_to: f.effective_to,
    audience, ...(base !== undefined ? { base_version: base } : {}),
  };
}

export interface DocQuery {
  folder?: string;
  q?: string;
  kind?: string;
  validity?: "active" | "pending" | "expired";
  include_hidden?: "1";
}

export function createDeptHomeApi(api: ApiClient, department?: string) {
  const header = () => ({ "Idempotency-Key": newIdempotencyKey() });
  const dq = department ? { department } : {};
  const clean = <T extends object>(o: T) => Object.fromEntries(Object.entries(o).filter(([, v]) => v !== undefined && v !== "")) as T;
  return {
    home: (): Promise<DeptHome> => unwrap(api.GET("/v1/extension/dept-home", { params: { query: dq } })),
    saveProfile: (intro: string, duties: string, base: number) =>
      unwrap(api.POST("/v1/extension/dept-home/profile", { params: { query: dq, header: header() }, body: { intro, duties, base_version: base } })),
    profileHistory: () => unwrap(api.GET("/v1/extension/dept-home/profile/history", { params: { query: dq } })),
    folders: (includeHidden = false) =>
      unwrap(api.GET("/v1/extension/dept-docs/folders", { params: { query: { ...dq, ...(includeHidden ? { include_hidden: "1" as const } : {}) } } })),
    createFolder: (name: string, parent: string | null) =>
      unwrap(api.POST("/v1/extension/dept-docs/folders", { params: { query: dq, header: header() }, body: { name, parent_ref: parent } })),
    renameFolder: (ref: string, name: string, base: number) =>
      unwrap(api.POST("/v1/extension/dept-docs/folders/{ref}/rename", { params: { path: { ref }, query: dq, header: header() }, body: { name, base_version: base } })),
    hideFolder: (ref: string, hidden: boolean, reason: string, base: number) =>
      unwrap(
        hidden
          ? api.POST("/v1/extension/dept-docs/folders/{ref}/hide", { params: { path: { ref }, query: dq, header: header() }, body: { reason, base_version: base } })
          : api.POST("/v1/extension/dept-docs/folders/{ref}/restore", { params: { path: { ref }, query: dq, header: header() }, body: { reason, base_version: base } }),
      ),
    list: (q: DocQuery): Promise<DeptDocList> => unwrap(api.GET("/v1/extension/dept-docs", { params: { query: clean({ ...dq, ...q }) } })),
    get: (ref: string): Promise<DeptDoc> => unwrap(api.GET("/v1/extension/dept-docs/{ref}", { params: { path: { ref }, query: dq } })),
    // Thân multipart tất định (src/api/multipart.ts, #172): bấm lại sau mất phản hồi trong cùng useAction gửi đúng từng
    // byte cũ → cùng Idempotency-Key, máy chủ phát lại văn bản đã tạo thay vì tạo bản thứ hai. Lời gọi api.POST phải
    // khởi phát đồng bộ trong act.run để bắt được phiên thao tác.
    create: (meta: DeptDocInput, file: File): Promise<DeptDoc> => {
      const m = JSON.stringify(meta);
      const mp = stableMultipart([
        ["meta", m],
        ["file", file],
      ]);
      return unwrap(
        api.POST("/v1/extension/dept-docs", {
          params: { query: dq, header: header() },
          headers: { "Content-Type": mp.contentType },
          body: { meta: m, file: file as unknown as string },
          bodySerializer: () => mp.body,
        }),
      );
    },
    update: (ref: string, meta: DeptDocInput): Promise<DeptDoc> =>
      unwrap(api.POST("/v1/extension/dept-docs/{ref}/update", { params: { path: { ref }, query: dq, header: header() }, body: meta })),
    // Như create: cùng tệp + cùng base_version → cùng thân → máy chủ phát lại, không ghi phiên bản tệp thứ hai (#172).
    replaceFile: (ref: string, file: File, base: number): Promise<DeptDoc> => {
      const mp = stableMultipart([
        ["base_version", String(base)],
        ["file", file],
      ]);
      return unwrap(
        api.POST("/v1/extension/dept-docs/{ref}/file", {
          params: { path: { ref }, query: dq, header: header() },
          headers: { "Content-Type": mp.contentType },
          body: { file: file as unknown as string, base_version: String(base) },
          bodySerializer: () => mp.body,
        }),
      );
    },
    hide: (ref: string, hidden: boolean, reason: string, base: number): Promise<DeptDoc> =>
      unwrap(
        hidden
          ? api.POST("/v1/extension/dept-docs/{ref}/hide", { params: { path: { ref }, query: dq, header: header() }, body: { reason, base_version: base } })
          : api.POST("/v1/extension/dept-docs/{ref}/restore", { params: { path: { ref }, query: dq, header: header() }, body: { reason, base_version: base } }),
      ),
    async content(ref: string, download: boolean, version?: number): Promise<{ blob: Blob; name: string }> {
      const res = await api.GET("/v1/extension/dept-docs/{ref}/content", {
        params: { path: { ref }, query: { ...dq, ...(download ? { download: "1" as const } : {}), ...(version ? { version } : {}) } },
        parseAs: "blob",
      });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      return { blob: res.data as Blob, name: filenameFromDisposition(res.response.headers.get("Content-Disposition"), "van-ban") };
    },
  };
}

export type DeptHomeApi = ReturnType<typeof createDeptHomeApi>;
