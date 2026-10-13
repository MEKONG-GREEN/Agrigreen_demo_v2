// Client có kiểu cho Phòng Kế hoạch – Tài chính (W11.61, pf/extension — /v1/kn/khtc/*): Danh mục KHTC (FN-AD-04.01),
// danh sách CT/DA chủ (quyết định D20), Kế hoạch năm 6 tab có phiên bản theo QĐ (FN-TI-02.02). Bearer, X-AG-Context và
// Idempotency-Key do middleware của createApiClient gắn. Số lượng, kinh phí là CHUỖI thập phân (không qua số thực).
import { useMemo } from "react";
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { filenameFromDisposition, type UploadFile } from "@/lib/files";

type S = components["schemas"];
export type KhtcCatalogKind = S["KhtcCatalogKind"];
export type KhtcCatalogItem = S["KhtcCatalogItem"];
export type KhtcCatalogInput = S["KhtcCatalogInput"];
export type KhtcProgram = S["KhtcProgram"];
export type KhtcProgramInput = S["KhtcProgramInput"];
export type KhtcProgramKind = S["KhtcProgramKind"];
export type KhtcPlan = S["KhtcPlan"];
export type KhtcPlanVersion = S["KhtcPlanVersion"];
export type KhtcPlanEntry = S["KhtcPlanEntry"];
export type KhtcPlanSave = S["KhtcPlanSave"];
export type KhtcPlanDraft = S["KhtcPlanDraft"];
export type KhtcTab = S["KhtcPlanTab"];
export type KhtcCatalogList = { counts: { kind: KhtcCatalogKind; label: string; active: number; total: number }[]; items: KhtcCatalogItem[] };

export const KHTC_DEPT = "ke_hoach_tai_chinh";

export const CATALOG_KINDS: KhtcCatalogKind[] = ["report_type", "unit", "field", "funding_source", "signer", "recipient", "basis"];
export const KIND_LABEL: Record<KhtcCatalogKind, string> = {
  report_type: "Loại báo cáo",
  unit: "Đơn vị tính",
  field: "Lĩnh vực",
  funding_source: "Nguồn kinh phí",
  signer: "Người ký",
  recipient: "Nơi nhận",
  basis: "Căn cứ",
};
/** Thuộc tính theo loại (nhãn). */
export const KIND_ATTRS: Partial<Record<KhtcCatalogKind, { key: string; label: string; required?: boolean }[]>> = {
  signer: [
    { key: "title", label: "Chức vụ", required: true },
    { key: "authority", label: "Thẩm quyền ký" },
  ],
  unit: [{ key: "symbol", label: "Ký hiệu" }],
  basis: [
    { key: "document_no", label: "Số văn bản" },
    { key: "date", label: "Ngày" },
  ],
  recipient: [{ key: "address", label: "Địa chỉ" }],
};
export const TEMPLATED: KhtcCatalogKind[] = ["signer", "recipient", "basis"];
export const TEMPLATES = ["mau_1", "mau_2", "mau_3", "mau_4", "mau_5"] as const;
export const TEMPLATE_LABEL: Record<string, string> = {
  mau_1: "Mẫu 1 (tuần)",
  mau_2: "Mẫu 2 (tháng)",
  mau_3: "Mẫu 3 (quý, 6, 9 tháng)",
  mau_4: "Mẫu 4 (năm)",
  mau_5: "Mẫu 5 (tiến độ các CT)",
};

export const TABS: KhtcTab[] = ["programs", "prev_year", "central_project", "cooperation", "research", "advisory"];
export const TAB_LABEL: Record<KhtcTab, string> = {
  programs: "Các chương trình",
  prev_year: "CT, DA năm trước",
  central_project: "Dự án TW",
  cooperation: "Hợp tác",
  research: "Đề tài",
  advisory: "Dịch vụ tư vấn",
};
export const PROGRAM_KINDS: KhtcProgramKind[] = ["program", "project", "central_project", "cooperation", "research", "advisory"];
export const PROGRAM_KIND_LABEL: Record<KhtcProgramKind, string> = {
  program: "Chương trình",
  project: "Dự án",
  central_project: "Dự án KN TW",
  cooperation: "CT, DA hợp tác",
  research: "Đề tài KHCN",
  advisory: "Dịch vụ tư vấn",
};
/** Loại CT/DA gợi ý theo tab. */
export const TAB_KIND: Record<KhtcTab, KhtcProgramKind | null> = {
  programs: "program",
  prev_year: null,
  central_project: "central_project",
  cooperation: "cooperation",
  research: "research",
  advisory: "advisory",
};
export const ENTRY_KIND_LABEL = { program: "CT/DA", group: "Nhóm", item: "Hạng mục" } as const;
export const DEPT_LABEL: Record<string, string> = {
  trong_trot: "Trồng trọt",
  chan_nuoi_thuy_san: "Chăn nuôi – Thủy sản",
  ke_hoach_tai_chinh: "Kế hoạch – Tài chính",
  thong_tin_truyen_thong: "Thông tin – Truyền thông",
  hanh_chinh_tong_hop: "Hành chính – Tổng hợp",
};

/** Ngày YYYY-MM-DD → dd/mm/yyyy. */
export function dmy(d: string | null | undefined): string {
  if (!d) return "—";
  const [y, m, day] = d.slice(0, 10).split("-");
  return `${day}/${m}/${y}`;
}
/** "120.5" → "120,5" (hiển thị, không qua số thực). */
export function vnDecimal(s: string | null | undefined): string {
  return s ? s.replace(".", ",") : "—";
}
/** Ô nhập "120,5" → "120.5"; rỗng → null. */
export function normDecimal(s: string): string | null {
  const v = s.replace(/\s+/g, "").replace(",", ".");
  return v === "" ? null : v;
}
export function isDecimal(s: string | null, scale: number): boolean {
  return s === null || new RegExp(`^\\d{1,16}(\\.\\d{1,${scale}})?$`).test(s);
}

/** UUIDv7-like cho dòng mới (máy chủ chỉ cần UUID). */
export function newRef(): string {
  return typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : newIdempotencyKey();
}

/** Dòng con trực tiếp. */
export function childrenOf(entries: KhtcPlanEntry[], ref: string | null): KhtcPlanEntry[] {
  return entries.filter((e) => (e.parent_ref ?? null) === ref);
}

/** Thứ tự hiển thị cây (cha trước con, theo sort_order rồi thứ tự lưu) của một tab, kèm độ sâu. */
export function treeRows(entries: KhtcPlanEntry[], tab: KhtcTab): { e: KhtcPlanEntry; depth: number }[] {
  const inTab = entries.filter((e) => e.tab === tab);
  const order = (xs: KhtcPlanEntry[]) => [...xs].sort((a, b) => a.sort_order - b.sort_order || inTab.indexOf(a) - inTab.indexOf(b));
  const out: { e: KhtcPlanEntry; depth: number }[] = [];
  const walk = (parent: string | null, depth: number) => {
    for (const e of order(inTab.filter((x) => (x.parent_ref ?? null) === parent))) {
      out.push({ e, depth });
      walk(e.ref, depth + 1);
    }
  };
  walk(null, 0);
  return out;
}

/** Xóa dòng và mọi dòng con. */
export function removeSubtree(entries: KhtcPlanEntry[], ref: string): KhtcPlanEntry[] {
  const gone = new Set([ref]);
  let grew = true;
  while (grew) {
    grew = false;
    for (const e of entries)
      if (e.parent_ref && gone.has(e.parent_ref) && !gone.has(e.ref)) {
        gone.add(e.ref);
        grew = true;
      }
  }
  return entries.filter((e) => !gone.has(e.ref));
}

/** Sắp lại để cha luôn đứng trước con (máy chủ đòi) — giữ thứ tự cây của từng tab. */
export function saveOrder(entries: KhtcPlanEntry[]): KhtcPlanEntry[] {
  return TABS.flatMap((t) => treeRows(entries, t).map((r) => r.e));
}

/** Thân request "Lưu" từ cây đang sửa. */
export function toSave(entries: KhtcPlanEntry[], head: Omit<KhtcPlanSave, "entries">): KhtcPlanSave {
  return {
    ...head,
    entries: saveOrder(entries).map((e) => ({
      ref: e.ref,
      tab: e.tab,
      kind: e.kind,
      parent_ref: e.parent_ref,
      program_ref: e.kind === "program" ? e.program_ref : null,
      sort_order: e.sort_order,
      code: e.code ?? "",
      name: e.name,
      unit_code: e.unit_code ?? "",
      planned_qty: e.planned_qty,
      scale_note: e.scale_note ?? "",
      budget: e.budget,
      funding_code: e.funding_code ?? "",
      field_code: e.field_code ?? "",
      department_code: e.department_code ?? "",
      communes: e.communes,
      points_count: e.points_count,
      data_source: e.data_source ?? "",
      source_key: e.source_key ?? "",
      tracking: e.tracking,
      stopped_from: e.tracking === "stopped" ? e.stopped_from : null,
      stop_decision: e.stop_decision ?? "",
      note: e.note ?? "",
    })),
  };
}

/** Dòng trống mới. */
export function blankEntry(tab: KhtcTab, kind: KhtcPlanEntry["kind"], parent: KhtcPlanEntry | null, sort: number): KhtcPlanEntry {
  return {
    ref: newRef(),
    tab,
    kind,
    parent_ref: parent?.ref ?? null,
    program_ref: parent?.program_ref ?? null,
    program_code: parent?.program_code ?? null,
    program_name: parent?.program_name ?? null,
    roman: null,
    sort_order: sort,
    code: null,
    name: "",
    unit_code: null,
    planned_qty: null,
    scale_note: null,
    budget: null,
    funding_code: null,
    field_code: null,
    department_code: null,
    communes: [],
    points_count: null,
    data_source: kind === "item" ? "form" : null,
    source_key: null,
    tracking: "tracking",
    stopped_from: null,
    stop_decision: null,
    note: null,
  };
}

export function createKhtcApi(api: ApiClient) {
  const h = () => ({ "Idempotency-Key": newIdempotencyKey() });
  return {
    catalog: (kind?: KhtcCatalogKind, includeRetired = false): Promise<KhtcCatalogList> =>
      unwrap(api.GET("/v1/kn/khtc/catalogs", { params: { query: { ...(kind ? { kind } : {}), ...(includeRetired ? { include_retired: true } : {}) } } })) as Promise<KhtcCatalogList>,
    createCatalog: (kind: KhtcCatalogKind, body: KhtcCatalogInput): Promise<KhtcCatalogItem> =>
      unwrap(api.POST("/v1/kn/khtc/catalogs/{kind}", { params: { path: { kind }, header: h() }, body })),
    reviseCatalog: (kind: KhtcCatalogKind, code: string, body: KhtcCatalogInput): Promise<KhtcCatalogItem> =>
      unwrap(api.POST("/v1/kn/khtc/catalogs/{kind}/{code}/revise", { params: { path: { kind, code }, header: h() }, body })),
    catalogHistory: (kind: KhtcCatalogKind, code: string): Promise<{ items: KhtcCatalogItem[] }> =>
      unwrap(api.GET("/v1/kn/khtc/catalogs/{kind}/{code}/history", { params: { path: { kind, code } } })),
    programs: (includeRetired = false): Promise<{ items: KhtcProgram[] }> =>
      unwrap(api.GET("/v1/kn/khtc/programs", { params: { query: includeRetired ? { include_retired: true } : {} } })),
    createProgram: (body: KhtcProgramInput): Promise<KhtcProgram> => unwrap(api.POST("/v1/kn/khtc/programs", { params: { header: h() }, body })),
    reviseProgram: (ref: string, body: KhtcProgramInput): Promise<KhtcProgram> =>
      unwrap(api.POST("/v1/kn/khtc/programs/{ref}/revise", { params: { path: { ref }, header: h() }, body })),
    /** null = năm chưa có kế hoạch (404 extension.khtc_plan_not_found). */
    async plan(year: number, version?: number): Promise<KhtcPlan | null> {
      const res = await api.GET("/v1/kn/khtc/plans/{year}", { params: { path: { year }, query: version ? { version } : {} } });
      if (res.response.status === 404) return null;
      return unwrap(Promise.resolve(res));
    },
    versions: (year: number): Promise<{ items: KhtcPlanVersion[] }> => unwrap(api.GET("/v1/kn/khtc/plans/{year}/versions", { params: { path: { year } } })),
    save: (year: number, body: KhtcPlanSave): Promise<KhtcPlan> =>
      unwrap(api.POST("/v1/kn/khtc/plans/{year}/versions", { params: { path: { year }, header: h() }, body })),
    copyDraft: (year: number): Promise<KhtcPlanDraft> => unwrap(api.GET("/v1/kn/khtc/plans/{year}/copy-draft", { params: { path: { year } } })),
    async exportXlsx(year: number, version?: number): Promise<{ blob: Blob; name: string }> {
      const res = await api.GET("/v1/kn/khtc/plans/{year}/export", { params: { path: { year }, query: version ? { version } : {} }, parseAs: "blob" });
      if (!res.response.ok || !res.data) throw await parseProblemResponse(res.response);
      return { blob: res.data as unknown as Blob, name: filenameFromDisposition(res.response.headers.get("Content-Disposition"), `ke-hoach-nam-${year}.xlsx`) };
    },
    /** Thân là nội dung tệp nguyên văn (.xlsx hoặc CSV) — máy chủ đọc thành bản nháp, không ghi. */
    importDraft: (year: number, file: UploadFile): Promise<KhtcPlanDraft> =>
      unwrap(
        api.POST("/v1/kn/khtc/plans/{year}/import-draft", {
          params: { path: { year }, header: h() },
          body: file.body as unknown as string,
          bodySerializer: (b: unknown) => b as BodyInit,
          headers: { "Content-Type": file.contentType },
        }),
      ),
  };
}
export type KhtcApi = ReturnType<typeof createKhtcApi>;

export function useKhtcApi(): KhtcApi {
  const { api } = useAuth();
  return useMemo(() => createKhtcApi(api), [api]);
}

/** Quyền theo ngữ cảnh phiên: Admin sửa mọi thứ; Cấp Tỉnh Phòng KHTC lập kế hoạch + CT/DA; khác chỉ xem. */
export function useKhtcRights(): { catalog: boolean; plan: boolean } {
  const { kn } = useAuth();
  const c = kn.context;
  if (!c || c.read_only) return { catalog: false, plan: false };
  if (c.role === "admin") return { catalog: true, plan: true };
  return { catalog: false, plan: c.role === "kn_tinh" && c.department_code === KHTC_DEPT };
}
