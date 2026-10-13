// Client "Báo cáo cho KHTC" phía phòng (W11.11 — BRD KHTC v2.2 Mục IV.3 FN-PN-03.01; D4 X5, CN11, BG10): danh sách phiếu
// KHTC giao cho phòng theo kỳ KHTC (GET /v1/extension/khtc/department-forms). Phòng lấy từ ngữ cảnh phiên; Admin truyền mã
// phòng. Form nhập/gửi phiếu (FN-PN-03.02) là màn của W11.63 — mở qua `entry_route` của dòng.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";

type S = components["schemas"];
export type DeptForms = S["KHTCDepartmentForms"];
export type DeptForm = S["KHTCDepartmentForm"];
export type DeptFormsView = DeptForms["view"];

export type Tone = "neutral" | "ok" | "warn" | "bad" | "info";

export const STATE_TONE: Record<DeptForm["state"], Tone> = {
  not_started: "neutral",
  draft: "neutral",
  late: "bad",
  submitted: "info",
  supplement: "warn",
  confirmed: "ok",
  locked: "neutral",
  not_sent: "bad",
};

/** Bộ lọc trạng thái của FN-PN-03.01 ("Cần xử lý" là mặc định của "Phiếu kỳ này"). */
export const STATE_FILTERS: { value: string; label: string }[] = [
  { value: "needs_action", label: "Cần xử lý" },
  { value: "", label: "Tất cả trạng thái" },
  { value: "not_started", label: "Chưa nhập" },
  { value: "draft", label: "Lưu nháp" },
  { value: "late", label: "Trễ hạn" },
  { value: "supplement", label: "Yêu cầu bổ sung" },
  { value: "submitted", label: "Đã gửi" },
  { value: "confirmed", label: "Đã xác nhận" },
  { value: "locked", label: "Đã khóa" },
  { value: "not_sent", label: "Không gửi" },
];

/** Lọc dòng theo loại báo cáo và trạng thái ("needs_action" = Cần xử lý). */
export function filterRows(
  rows: DeptForm[],
  type: string,
  state: string,
): DeptForm[] {
  return rows.filter(
    (r) =>
      (!type || r.report_type === type) &&
      (!state ||
        (state === "needs_action" ? r.needs_action : r.state === state)),
  );
}

/** "Lần gửi": "—" khi chưa gửi, "n/max" (max = 0 → không giới hạn). */
export function attemptText(
  r: Pick<DeptForm, "attempt" | "max_submissions">,
): string {
  if (r.attempt === 0) return "—";
  return r.max_submissions > 0
    ? `${r.attempt}/${r.max_submissions}`
    : String(r.attempt);
}

/** Yêu cầu bổ sung mới nhất (dải trên bảng): dòng "Yêu cầu bổ sung" xem xét gần nhất. */
export function latestSupplement(rows: DeptForm[]): DeptForm | undefined {
  return rows
    .filter((r) => r.state === "supplement" && r.supplement_note)
    .sort(
      (a, b) =>
        Date.parse(b.reviewed_at ?? "") - Date.parse(a.reviewed_at ?? ""),
    )[0];
}

export function createDeptFormsApi(api: ApiClient) {
  return {
    list: (view: DeptFormsView, department?: string): Promise<DeptForms> =>
      unwrap(
        api.GET("/v1/extension/khtc/department-forms", {
          params: { query: { view, ...(department ? { department } : {}) } },
        }),
      ),
  };
}

export type DeptFormsApi = ReturnType<typeof createDeptFormsApi>;
