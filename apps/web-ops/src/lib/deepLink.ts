// Ánh xạ `deep_link` của thông báo (pf/notify, đường dẫn trong app — không phải URL web) sang trang web vận hành.
// Đường dẫn lạ hoặc sai dạng → null: giao diện chỉ mở hộp thư, không điều hướng theo dữ liệu tuỳ ý.
//
// Một số đường dẫn dùng chung cho nhiều loại thông báo (vd /cropcycle/farm-logs/{id} cho cả "chờ duyệt"
// và "đã duyệt") nên loại thông báo (`kind`) quyết định trang đích khi có.

const UUID = "[0-9a-fA-F-]{8,64}";

type Rule = { re: RegExp; to: (id: string, kind: string | undefined) => string };

const RULES: readonly Rule[] = [
  { re: new RegExp(`^/extension/directives/(${UUID})$`), to: (id) => `/chi-dao?id=${id}` },
  { re: new RegExp(`^/documents/(${UUID})$`), to: (id) => `/thu-vien?doc=${id}` },
  // task_escalated, task_assigned (KN US-TASK-01): trang nhiệm vụ mở sẵn nhiệm vụ.
  { re: new RegExp(`^/extension/tasks/(${UUID})$`), to: (id) => `/nhiem-vu?task=${id}` },
  { re: new RegExp(`^/sync/conflicts/(${UUID})$`), to: (id) => `/quan-tri/xung-dot?id=${id}` },
  { re: new RegExp(`^/orgs/(${UUID})$`), to: (id) => `/quan-tri/to-chuc?org=${id}` },
  // farm_log_pending → quản lý HTX duyệt (HTX US-LOG-05); farm_log_reviewed → người ghi xem nhật ký (AC-3).
  { re: new RegExp(`^/cropcycle/farm-logs/(${UUID})$`), to: (_id, kind) => (kind === "farm_log_reviewed" ? "/htx/nhat-ky" : "/htx/phe-duyet") },
  // season_opened: nông dân tự mở vụ → quản lý HTX (HTX US-SEASON-01).
  { re: new RegExp(`^/cropcycle/seasons/(${UUID})$`), to: () => "/htx/mua-vu" },
  // household_registered / household_membership_changed (KN US-HTX-05): thông báo chỉ mang mã nông hộ
  // và mã xã (không có mã HTX) → danh sách HTX & nông hộ trong địa bàn của cán bộ.
  { re: new RegExp(`^/households/(${UUID})$`), to: () => "/htx" },
];

/** Trang đích theo loại thông báo khi `deep_link` trống hoặc không nhận dạng được. */
const KIND_FALLBACK: Readonly<Record<string, string>> = {
  farm_log_pending: "/htx/phe-duyet",
  farm_log_reviewed: "/htx/nhat-ky",
  season_opened: "/htx/mua-vu",
  household_registered: "/htx",
  household_membership_changed: "/htx",
};

export function deepLinkHref(deepLink: string | null | undefined, kind?: string | null): string | null {
  const k = kind ?? undefined;
  const path = (deepLink ?? "").trim().split(/[?#]/)[0] ?? "";
  if (path) {
    for (const r of RULES) {
      const m = r.re.exec(path);
      if (m?.[1]) return r.to(encodeURIComponent(m[1]), k);
    }
  }
  return (k && Object.hasOwn(KIND_FALLBACK, k) ? KIND_FALLBACK[k] : null) ?? null;
}
