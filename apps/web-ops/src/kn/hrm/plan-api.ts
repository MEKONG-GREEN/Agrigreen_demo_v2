// Client có kiểu cho Kế hoạch nhân lực năm (W11.27, dom/hrm — /v1/hrm/plans*; BRD HCTH v3.0 FN-11). Họ tên người dự kiến
// nghỉ hưu chỉ đi qua bộ nhớ của màn.
import { unwrap, type ApiClient } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { parseProblemResponse } from "@/api/problem";
import { filenameFromDisposition } from "@/lib/files";
import type { components } from "@/api/generated/application";

type S = components["schemas"];
export type HrmPlanPeriod = S["HrmPlanPeriod"];
export type HrmPlanSummary = S["HrmPlanSummary"];
export type HrmPlanRow = S["HrmPlanRow"];
export type HrmPlanUnit = S["HrmPlanUnit"];
export type HrmPlanInput = S["HrmPlanInput"];
export type HrmPlanValues = S["HrmPlanValues"];
export type HrmPlanPosition = S["HrmPlanPosition"];
export type HrmPlanStatus = S["HrmPlanStatus"];
export type HrmPlanReminder = S["HrmPlanReminder"];

/** Nhãn trạng thái kế hoạch của đơn vị (prototype HCTH: Chưa lập, Đang lập, Đã gửi, Trễ hạn, Đã chốt). */
export const PLAN_STATUS_LABEL: Record<HrmPlanStatus, string> = {
  not_started: "Chưa lập",
  draft: "Đang lập",
  submitted: "Đã gửi",
  returned: "Yêu cầu điều chỉnh",
  accepted: "Đã xác nhận",
  closed: "Đã chốt",
  missed: "Không gửi",
};

export const PERIOD_STATUS_LABEL: Record<string, string> = {
  open: "Đang thu thập",
  reopened: "Đang thu thập",
  closed: "Đã chốt",
  cancelled: "Đã hủy",
};

/** Nhu cầu theo kế hoạch = hiện có − dự kiến nghỉ hưu − dự kiến nghỉ/chuyển khác + cần bổ sung (khớp máy chủ). */
export function planDemand(v: {
  current: number;
  retiring: number;
  leaving: number;
  additional: number;
}): number {
  return v.current - v.retiring - v.leaving + v.additional;
}

const idem = () => ({ "Idempotency-Key": newIdempotencyKey() });

export function createPlanApi(api: ApiClient) {
  const unitPath = (year: number, unit: string) => ({
    path: { year, unit },
    header: idem(),
  });
  return {
    list: (): Promise<{ items: HrmPlanPeriod[] }> =>
      unwrap(api.GET("/v1/hrm/plans")),
    open: (body: {
      year: number;
      due_on: string;
      cutoff_on?: string;
      units?: string[];
    }): Promise<HrmPlanPeriod> =>
      unwrap(api.POST("/v1/hrm/plans", { params: { header: idem() }, body })),
    summary: (year: number): Promise<HrmPlanSummary> =>
      unwrap(api.GET("/v1/hrm/plans/{year}", { params: { path: { year } } })),
    close: (year: number): Promise<HrmPlanSummary> =>
      unwrap(
        api.POST("/v1/hrm/plans/{year}/close", {
          params: { path: { year }, header: idem() },
        }),
      ),
    /** "Nhắc phòng chưa lập" (NOTIFY-HCTH-KN #50): nhắc lãnh đạo các đơn vị chưa gửi qua pf/notify. */
    remind: (year: number): Promise<HrmPlanReminder> =>
      unwrap(
        api.POST("/v1/hrm/plans/{year}/remind", {
          params: { path: { year }, header: idem() },
        }),
      ),
    unit: (year: number, unit: string): Promise<HrmPlanUnit> =>
      unwrap(
        api.GET("/v1/hrm/plans/{year}/units/{unit}", {
          params: { path: { year, unit } },
        }),
      ),
    previous: (
      year: number,
      unit: string,
    ): Promise<{ from_year: number; values: HrmPlanInput }> =>
      unwrap(
        api.GET("/v1/hrm/plans/{year}/units/{unit}/previous", {
          params: { path: { year, unit } },
        }),
      ),
    draft: (year: number, unit: string, body: HrmPlanInput) =>
      unwrap(
        api.PUT("/v1/hrm/plans/{year}/units/{unit}/draft", {
          params: unitPath(year, unit),
          body,
        }),
      ),
    submit: (
      year: number,
      unit: string,
      body: HrmPlanInput & { base_version: number },
    ) =>
      unwrap(
        api.POST("/v1/hrm/plans/{year}/units/{unit}/submit", {
          params: unitPath(year, unit),
          body,
        }),
      ),
    review: (
      year: number,
      unit: string,
      decision: "accepted" | "returned",
      note: string,
    ) =>
      unwrap(
        api.POST("/v1/hrm/plans/{year}/units/{unit}/review", {
          params: unitPath(year, unit),
          body: { decision, note },
        }),
      ),
    adjust: (
      year: number,
      unit: string,
      body: HrmPlanInput & { reason: string },
    ) =>
      unwrap(
        api.POST("/v1/hrm/plans/{year}/units/{unit}/adjustments", {
          params: unitPath(year, unit),
          body,
        }),
      ),
    async exportFile(year: number): Promise<{ blob: Blob; name: string }> {
      const res = await api.GET("/v1/hrm/plans/{year}/export", {
        params: { path: { year } },
        parseAs: "blob",
      });
      if (!res.response.ok || !res.data)
        throw await parseProblemResponse(res.response);
      return {
        blob: res.data as Blob,
        name: filenameFromDisposition(
          res.response.headers.get("Content-Disposition"),
          `ke-hoach-nhan-luc-${year}.xlsx`,
        ),
      };
    },
  };
}

export type PlanApi = ReturnType<typeof createPlanApi>;
