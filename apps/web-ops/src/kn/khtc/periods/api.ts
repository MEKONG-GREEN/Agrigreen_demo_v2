// Client Kỳ KHTC (W11.62 — BRD KHTC v2.2 FN-TI-02.03, D4 KH2): mở kỳ bằng ô chọn, phiếu các phòng (kỳ native cap/forms
// profile kn_khtc), tiến độ phiếu + nguồn, sửa hạn, đóng/hủy kỳ (/v1/extension/khtc/*). openapi-fetch sinh từ
// contracts/openapi/application.yaml; Bearer, X-AG-Context do createApiClient gắn.
import { unwrap, type ApiClient } from "@/api/client";
import type { components } from "@/api/generated/application";
import { newIdempotencyKey } from "@/api/idempotency";

type S = components["schemas"];
export type KhtcPeriod = S["KHTCPeriod"];
export type KhtcDraft = S["KHTCDraft"];
export type KhtcOpenInput = S["KHTCOpenInput"];
export type KhtcWeek = S["KHTCWeek"];
export type KhtcFormTemplate = S["KHTCFormTemplate"];
export type KhtcReportType = KhtcOpenInput["report_type"];
export type KhtcState = KhtcPeriod["state"];
export type KhtcFormState = S["KHTCForm"]["state"];

export type Tone = "neutral" | "ok" | "warn" | "bad" | "info";

export const REPORT_TYPES: { value: KhtcReportType; label: string }[] = [
  { value: "week", label: "Tuần (Mẫu 1)" },
  { value: "month", label: "Tháng (Mẫu 2)" },
  { value: "quarter", label: "Quý (Mẫu 3)" },
  { value: "year", label: "Năm (Mẫu 4)" },
  { value: "program", label: "Tiến độ các CT (Mẫu 5)" },
  { value: "adhoc", label: "Phát sinh" },
];

export const PERIOD_TONE: Record<KhtcState, Tone> = {
  draft: "neutral",
  opened: "info",
  collecting: "info",
  late: "bad",
  complete: "ok",
  closed: "neutral",
  cancelled: "bad",
};

export const FORM_TONE: Record<KhtcFormState, Tone> = {
  not_started: "neutral",
  draft: "neutral",
  late: "bad",
  submitted: "info",
  supplement: "warn",
  confirmed: "ok",
  locked: "neutral",
  not_sent: "bad",
  withdrawn: "neutral",
  not_available: "warn",
};

/** Lựa chọn hợp lệ của ô "Số" theo loại báo cáo (tuần cần danh sách tuần của năm). */
export function numberChoices(
  type: KhtcReportType,
  weeks: KhtcWeek[],
): { value: number; label: string }[] {
  switch (type) {
    case "week":
      return weeks.map((w) => ({
        value: w.no,
        label: `Tuần ${w.no} (${vnDate(w.from_date)} – ${vnDate(w.to_date)})`,
      }));
    case "month":
      return Array.from({ length: 12 }, (_, i) => ({
        value: i + 1,
        label: `Tháng ${i + 1}`,
      }));
    case "quarter":
      return ["I", "II", "III", "IV"].map((r, i) => ({
        value: i + 1,
        label: `Quý ${r}`,
      }));
  }
  return [];
}

/** Phạm vi báo cáo quý hợp lệ: 6 tháng chỉ với quý II, 9 tháng chỉ với quý III. */
export function scopeChoices(
  quarter: number,
): { value: "quarter" | "half" | "nine_months"; label: string }[] {
  const out: { value: "quarter" | "half" | "nine_months"; label: string }[] = [
    { value: "quarter", label: "Quý" },
  ];
  if (quarter === 2) out.push({ value: "half", label: "6 tháng" });
  if (quarter === 3) out.push({ value: "nine_months", label: "9 tháng" });
  return out;
}

/** "2026-10-13" → "13/10/2026". */
export function vnDate(d: string): string {
  const [y, m, day] = d.split("-");
  return `${day}/${m}/${y}`;
}

/** Tiến độ nộp "đã gửi/tổng" kèm phần trăm (làm tròn). */
export function progressText(p: Pick<KhtcPeriod, "counts">): string {
  const { forms, forms_sent } = p.counts;
  return `${forms_sent}/${forms}${forms ? ` (${Math.round((forms_sent * 100) / forms)}%)` : ""}`;
}

const idem = () => ({ header: { "Idempotency-Key": newIdempotencyKey() } });

export function createKhtcPeriodApi(api: ApiClient) {
  return {
    weeks: async (year: number): Promise<KhtcWeek[]> =>
      (
        await unwrap(
          api.GET("/v1/extension/khtc/weeks", { params: { query: { year } } }),
        )
      ).items,
    template: (): Promise<KhtcFormTemplate> =>
      unwrap(api.GET("/v1/extension/khtc/form-template")),
    install: (): Promise<KhtcFormTemplate> =>
      unwrap(
        api.POST("/v1/extension/khtc/form-template/install", {
          params: idem(),
        }),
      ),
    list: async (year?: number): Promise<KhtcPeriod[]> =>
      (
        await unwrap(
          api.GET("/v1/extension/khtc/periods", {
            params: { query: year ? { year } : {} },
          }),
        )
      ).items,
    get: (id: string): Promise<KhtcPeriod> =>
      unwrap(
        api.GET("/v1/extension/khtc/periods/{id}", {
          params: { path: { id } },
        }),
      ),
    preview: (body: KhtcOpenInput): Promise<KhtcDraft> =>
      unwrap(api.POST("/v1/extension/khtc/periods/preview", { body })),
    open: (body: KhtcOpenInput): Promise<KhtcPeriod> =>
      unwrap(api.POST("/v1/extension/khtc/periods", { params: idem(), body })),
    deadline: (
      id: string,
      due_at: string,
      export_due_at?: string,
      reason?: string,
    ): Promise<KhtcPeriod> =>
      unwrap(
        api.POST("/v1/extension/khtc/periods/{id}/deadline", {
          params: { path: { id }, ...idem() },
          body: { due_at, export_due_at, reason },
        }),
      ),
    close: (id: string, reason?: string): Promise<KhtcPeriod> =>
      unwrap(
        api.POST("/v1/extension/khtc/periods/{id}/close", {
          params: { path: { id }, ...idem() },
          body: { reason },
        }),
      ),
    cancel: (id: string, reason: string): Promise<KhtcPeriod> =>
      unwrap(
        api.POST("/v1/extension/khtc/periods/{id}/cancel", {
          params: { path: { id }, ...idem() },
          body: { reason },
        }),
      ),
  };
}

export type KhtcPeriodApi = ReturnType<typeof createKhtcPeriodApi>;
