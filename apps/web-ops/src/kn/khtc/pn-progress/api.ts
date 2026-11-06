import { unwrap, type ApiClient } from "@/api/client";
import { ApiProblem, parseProblemResponse } from "@/api/problem";
import { filenameFromDisposition } from "@/lib/files";
import type { components } from "@/api/generated/application";

type S = components["schemas"];
export type PNProgress = S["KHTCPNProgress"];
export type PNPeriod = S["KHTCPNPeriod"];
export type PNRow = S["KHTCPNProgressRow"];
export type PNDepartment = "trong_trot" | "chan_nuoi_thuy_san" | "thong_tin_truyen_thong" | "hanh_chinh_tong_hop";
export type PNLevel = "" | "achieved" | "progressing" | "slow" | "qualitative";
export const PN_DEPARTMENTS: Record<PNDepartment, string> = {
  trong_trot: "Phòng Trồng trọt", chan_nuoi_thuy_san: "Phòng Chăn nuôi - Thủy sản",
  thong_tin_truyen_thong: "Phòng Thông tin - Truyền thông", hanh_chinh_tong_hop: "Phòng Hành chính tổng hợp",
};
export const pnDepartment = (s: string): s is PNDepartment => Object.hasOwn(PN_DEPARTMENTS, s);
export function pnYear(raw: string): number {
  if (!/^[0-9]{4}$/.test(raw)) throw new ApiProblem({ type: "about:blank", status: 422, code: "invalid", title: "Năm phải có bốn chữ số từ 2000 đến 2100." });
  const year = Number(raw);
  if (year < 2000 || year > 2100) throw new ApiProblem({ type: "about:blank", status: 422, code: "invalid", title: "Năm phải từ 2000 đến 2100." });
  return year;
}
function mismatch(): never { throw new ApiProblem({ type: "about:blank", status: 502, code: "internal_error", title: "Dữ liệu tiến độ không khớp phòng hoặc kỳ đang chọn." }); }
export function createPNProgressApi(api: ApiClient) {
  return {
    async periods(department: PNDepartment, rawYear: string, after?: string) {
      const year = pnYear(rawYear);
      const out = await unwrap(api.GET("/v1/extension/khtc/departments/{department}/program-progress/periods", { params: { path: { department }, query: { year, ...(after ? { after } : {}) } } }));
      if (out.department !== department || out.items.some(p => p.year !== year) || out.has_more && (!out.next_after || out.next_after === after)) mismatch();
      return out;
    },
    async get(department: PNDepartment, periodID: string, program: string, level: PNLevel, snapshot_id?: string) {
      const out = await unwrap(api.GET("/v1/extension/khtc/departments/{department}/program-progress/{periodID}", { params: { path: { department, periodID }, query: { ...(program ? { program } : {}), ...(level ? { level } : {}), ...(snapshot_id ? { snapshot_id } : {}) } } }));
      if (out.department !== department || out.period.id !== periodID || out.rows.some(r => r.kind === "item" && r.department !== department) || out.status === "snapshot" && !out.snapshot || snapshot_id && out.snapshot?.id !== snapshot_id || out.status === "not_prepared" && (out.snapshot !== null || out.rows.length !== 0)) mismatch();
      return out;
    },
    async excel(department: PNDepartment, periodID: string, snapshot_id: string, program: string, level: PNLevel) {
      const out = await api.GET("/v1/extension/khtc/departments/{department}/program-progress/{periodID}/excel", { params: { path: { department, periodID }, query: { snapshot_id, ...(program ? { program } : {}), ...(level ? { level } : {}) } }, parseAs: "blob" });
      if (!out.response.ok || !out.data) throw await parseProblemResponse(out.response);
      return { blob: out.data, name: filenameFromDisposition(out.response.headers.get("Content-Disposition"), "tien-do-phong.xlsx") };
    },
  };
}
export type PNProgressApi = ReturnType<typeof createPNProgressApi>;
