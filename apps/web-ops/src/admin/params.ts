// Tham số có phê duyệt (Admin API pf/masterdata). Backend trả DANH SÁCH PHIÊN BẢN
// (GET /v1/masterdata/params/versions); web gom theo khóa để hiện "đang hiệu lực" + "chờ duyệt".
// Backend chưa có sổ đăng ký tên/đơn vị tham số (web-backend-gaps.md G-07) → tên hiển thị ở đây.
import type { AdminApiClient } from "@/api/client";
import { unwrap } from "@/api/client";
import { ApiProblem } from "@/api/problem";
import type { ParamProposalInput, ParamVersion } from "@/api/types";

export interface ParamView {
  key: string;
  label: string;
  unit: string;
  /** Bản đã duyệt có hiệu lực tại `now` (effective_from ≤ now, mới nhất; cùng ngày → số phiên bản lớn hơn). */
  current: ParamVersion | null;
  /** Bản đã duyệt sẽ có hiệu lực sau `now`. */
  upcoming: ParamVersion[];
  /** Đã gửi duyệt, chờ người phê duyệt. */
  submitted: ParamVersion[];
  /** Nháp chưa gửi. */
  drafts: ParamVersion[];
}

const STAGE: Record<string, string> = {
  lam_dat: "làm đất",
  gieo_sa: "gieo sạ/cấy",
  cham_soc: "chăm sóc",
  thu_hoach: "thu hoạch",
  sau_thu_hoach: "sau thu hoạch",
};

const KNOWN: Record<string, string> = {
  "cgh.coverage.sufficient_min_pct": "Ngưỡng Đủ máy (mức đáp ứng)",
  "cgh.coverage.attention_min_pct": "Ngưỡng Cần chú ý",
  "cgh.coverage.surplus_min_pct": "Ngưỡng Thừa máy",
  "extension.task_sla_hours": "SLA xử lý nhiệm vụ hỗ trợ",
  "extension.duty_status_ttl_hours": "Thời hạn trạng thái trực",
  "geo.overlap_tolerance_pct": "Ngưỡng chồng lấn ranh giới thửa",
  "geo.area_deviation_max_pct": "Ngưỡng lệch diện tích khai báo",
  "sync.conflict_sla_hours": "SLA xử lý xung đột đồng bộ",
};

/** Tên tiếng Việt của khóa tham số; khóa lạ → chính khóa. */
export function paramLabel(key: string): string {
  const known = KNOWN[key];
  if (known) return known;
  const norm = /^cgh\.norm_ha_per_machine:([^:]+):([^:]+)$/.exec(key);
  if (norm) return `Định mức máy ${norm[1]} — khâu ${STAGE[norm[2] ?? ""] ?? norm[2]}`;
  return key;
}

function newer(a: ParamVersion, b: ParamVersion): number {
  return Date.parse(b.effective_from) - Date.parse(a.effective_from) || b.version - a.version;
}

export function groupParamVersions(versions: readonly ParamVersion[], now: number = Date.now()): ParamView[] {
  const byKey = new Map<string, ParamVersion[]>();
  for (const v of versions) byKey.set(v.key, [...(byKey.get(v.key) ?? []), v]);
  return Array.from(byKey.entries())
    .map(([key, list]): ParamView => {
      const approved = list.filter((v) => v.status === "approved").sort(newer);
      const current = approved.find((v) => Date.parse(v.effective_from) <= now) ?? null;
      return {
        key,
        label: paramLabel(key),
        unit: current?.unit ?? list[0]?.unit ?? "",
        current,
        upcoming: approved.filter((v) => Date.parse(v.effective_from) > now).reverse(),
        submitted: list.filter((v) => v.status === "submitted").sort((a, b) => a.version - b.version),
        drafts: list.filter((v) => v.status === "draft").sort((a, b) => a.version - b.version),
      };
    })
    .sort((a, b) => a.key.localeCompare(b.key));
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
