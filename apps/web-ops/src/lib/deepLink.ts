// Ánh xạ `deep_link` của thông báo (pf/notify, đường dẫn trong app — không phải URL web) sang trang web vận hành.
// Đường dẫn lạ hoặc sai dạng → null: giao diện chỉ mở hộp thư, không điều hướng theo dữ liệu tuỳ ý.
//
// Một số đường dẫn dùng chung cho nhiều loại thông báo (vd /cropcycle/farm-logs/{id} cho cả "chờ duyệt"
// và "đã duyệt") nên loại thông báo (`kind`) quyết định trang đích khi có.

const UUID = "[0-9a-fA-F-]{8,64}";
const KNQG_FORMS = "/kn/cnts/du-an-mau-bieu";
const APPRAISALS = "/kn/nhan-su/danh-gia";

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
  // integration_dead_lettered (GIS US-SYNC-02): sự kiện đồng bộ vào hàng chờ lỗi → màn Giám sát đồng bộ.
  { re: new RegExp(`^/sync/integrations/(${UUID})$`), to: () => "/quan-tri/giam-sat-dong-bo" },
  // area_feed_stale (CGH FN-07): dữ liệu App HTX quá ngưỡng độ tươi → Bản đồ CGH (banner + nhật ký đồng bộ).
  { re: /^\/mechanization\/(area-sync-log)$/, to: () => "/cgh" },
];

/** Trang đích theo loại thông báo khi `deep_link` trống hoặc không nhận dạng được. */
const KIND_FALLBACK: Readonly<Record<string, string>> = {
  farm_log_pending: "/htx/phe-duyet",
  farm_log_reviewed: "/htx/nhat-ky",
  season_opened: "/htx/mua-vu",
  household_registered: "/htx",
  household_membership_changed: "/htx",
  integration_dead_lettered: "/quan-tri/giam-sat-dong-bo",
  area_feed_stale: "/cgh",
  // W11.53b (#158): biểu dự án KNQG gửi Cấp TW / được xác nhận, trả lại / trình duyệt → màn Mẫu biểu dự án KNQG.
  cnts_knqg_sent: KNQG_FORMS,
  cnts_knqg_reviewed: KNQG_FORMS,
  cnts_knqg_submitted: KNQG_FORMS,
  hrm_appraisal: APPRAISALS,
  hrm_promotion_due: "/kn/nhan-su/nang-bac",
  hrm_plan_reminder: "/kn/nhan-su/ke-hoach-phong",
};

/** Mã biểu trong `?doc=` phải là UUID đầy đủ (không nhận chuỗi tuỳ ý); sai dạng → mở màn danh sách. */
const STRICT_UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** `deep_link` của biểu KNQG mang mã biểu ở tham số `doc` (pf/extension module_notify_knqg.go). */
function knqgHref(deepLink: string): string {
  const q = deepLink.split("#")[0]?.split("?")[1] ?? "";
  const doc = new URLSearchParams(q).get("doc");
  return doc && STRICT_UUID.test(doc) ? `${KNQG_FORMS}?doc=${doc.toLowerCase()}` : KNQG_FORMS;
}

export function deepLinkHref(deepLink: string | null | undefined, kind?: string | null): string | null {
  const k = kind ?? undefined;
  const raw = (deepLink ?? "").trim();
  const path = raw.split(/[?#]/)[0] ?? "";
  if (path === KNQG_FORMS) return knqgHref(raw);
  if (path.startsWith(`${APPRAISALS}/dot/`)) {
    const [period, marker, staff, extra] = path.slice(`${APPRAISALS}/dot/`.length).split("/");
    if (period && STRICT_UUID.test(period)) {
      if (!marker) return `${APPRAISALS}/dot/${period.toLowerCase()}`;
      if (marker === "phieu" && staff && STRICT_UUID.test(staff) && extra === undefined) return `${APPRAISALS}/dot/${period.toLowerCase()}/phieu/${staff.toLowerCase()}`;
    }
  }
  if (path) {
    for (const r of RULES) {
      const m = r.re.exec(path);
      if (m?.[1]) return r.to(encodeURIComponent(m[1]), k);
    }
  }
  return (k && Object.hasOwn(KIND_FALLBACK, k) ? KIND_FALLBACK[k] : null) ?? null;
}
