// Tham số có phê duyệt (Admin API pf/masterdata). Tên, đơn vị, kiểu, khoảng hợp lệ lấy từ sổ đăng ký
// GET /v1/masterdata/params (G-07); danh sách phiên bản GET /v1/masterdata/params/versions cho bản chờ duyệt,
// nháp và bản sắp hiệu lực. Web gom theo khóa để hiện "đang hiệu lực" + "chờ duyệt".
import type { AdminApiClient } from "@/api/client";
import { unwrap } from "@/api/client";
import { ApiProblem } from "@/api/problem";
import type { ParamProposalInput, ParamRegistryItem, ParamVersion } from "@/api/types";

export interface ParamView {
  key: string;
  label: string;
  unit: string;
  kind: ParamRegistryItem["kind"] | null;
  min: string | null;
  max: string | null;
  /** Bản đã duyệt có hiệu lực tại `now` (effective_from ≤ now, mới nhất; cùng ngày → số phiên bản lớn hơn). */
  current: ParamVersion | null;
  /** Bản đang hiệu lực theo sổ đăng ký khi danh sách phiên bản không có (vd phân trang). */
  registryCurrent: ParamRegistryItem["current"];
  /** Bản đã duyệt sẽ có hiệu lực sau `now`. */
  upcoming: ParamVersion[];
  /** Đã gửi duyệt, chờ người phê duyệt. */
  submitted: ParamVersion[];
  /** Nháp chưa gửi. */
  drafts: ParamVersion[];
}

function newer(a: ParamVersion, b: ParamVersion): number {
  return Date.parse(b.effective_from) - Date.parse(a.effective_from) || b.version - a.version;
}

/** Gom phiên bản theo khóa; khóa trong sổ đăng ký chưa có phiên bản nào vẫn hiện (để đề xuất lần đầu). */
export function groupParamVersions(
  versions: readonly ParamVersion[],
  registry: readonly ParamRegistryItem[] = [],
  now: number = Date.now(),
): ParamView[] {
  const byKey = new Map<string, ParamVersion[]>();
  for (const r of registry) byKey.set(r.key, []);
  for (const v of versions) byKey.set(v.key, [...(byKey.get(v.key) ?? []), v]);
  const reg = new Map(registry.map((r) => [r.key, r]));
  return Array.from(byKey.entries())
    .map(([key, list]): ParamView => {
      const approved = list.filter((v) => v.status === "approved").sort(newer);
      const current = approved.find((v) => Date.parse(v.effective_from) <= now) ?? null;
      const r = reg.get(key);
      return {
        key,
        label: r?.name ?? key,
        unit: r?.unit ?? current?.unit ?? list[0]?.unit ?? "",
        kind: r?.kind ?? null,
        min: r?.min ?? null,
        max: r?.max ?? null,
        current,
        registryCurrent: r?.current ?? null,
        upcoming: approved.filter((v) => Date.parse(v.effective_from) > now).reverse(),
        submitted: list.filter((v) => v.status === "submitted").sort((a, b) => a.version - b.version),
        drafts: list.filter((v) => v.status === "draft").sort((a, b) => a.version - b.version),
      };
    })
    .sort((a, b) => a.key.localeCompare(b.key));
}

/** Tên người đặt/duyệt (G-07); cán bộ chưa có trong danh bạ → mã tham chiếu. */
export function actorLabel(name: string | null | undefined, ref: string | null | undefined): string {
  return name ?? ref ?? "—";
}

export interface ProposeResult {
  draft: ParamVersion;
  /** Có khi đã gửi duyệt thành công. */
  submitted: ParamVersion | null;
  /** Lỗi ở bước gửi duyệt (bản nháp vẫn còn, gửi lại được từ danh sách nháp). */
  submitError: ApiProblem | null;
}

/**
 * Đề xuất = tạo nháp (POST /versions) rồi gửi duyệt (POST /versions/{id}/submit).
 * `idemKey` là khóa của MỘT thao tác: thử lại cùng dữ liệu dùng lại khóa → backend trả đúng bản nháp cũ,
 * không tạo trùng. Khóa bước gửi duyệt suy ra từ khóa đó (R-51).
 */
export async function proposeParam(admin: AdminApiClient, input: ParamProposalInput, idemKey: string): Promise<ProposeResult> {
  const draft = await unwrap(
    admin.POST("/v1/masterdata/params/versions", { params: { header: { "Idempotency-Key": idemKey } }, body: input }),
  );
  try {
    const submitted = await submitParam(admin, draft.id, `${idemKey}:submit`);
    return { draft, submitted, submitError: null };
  } catch (e) {
    return { draft, submitted: null, submitError: e instanceof ApiProblem ? e : null };
  }
}

export function submitParam(admin: AdminApiClient, id: string, idemKey: string): Promise<ParamVersion> {
  return unwrap(
    admin.POST("/v1/masterdata/params/versions/{id}/submit", { params: { path: { id }, header: { "Idempotency-Key": idemKey } } }),
  );
}
