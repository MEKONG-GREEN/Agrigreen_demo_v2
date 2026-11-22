import { validateForm } from "@/components/form";
import { isCalendarDay } from "../calendar-routes";

export type PointFields = {
  admin_unit_code: string;
  region_code: string;
  valid_from: string;
  note: string;
  reason: string;
};
export type FieldErrors = Record<string, string | undefined>;
export function validatePointFields(
  b: PointFields,
  mode: "create" | "revise" | "retire",
) {
  const checked = validateForm(b, {
    admin_unit_code: {
      kind: "text",
      required: mode === "create",
      maxLength: 20,
    },
    region_code: { kind: "text", required: mode !== "retire", maxLength: 40 },
    valid_from: { kind: "text", maxLength: 10 },
    note: { kind: "text", maxLength: 500 },
    reason: { kind: "text", required: mode === "retire", maxLength: 500 },
  });
  if (b.valid_from && !isCalendarDay(b.valid_from))
    checked.errors.valid_from = "Ngày không hợp lệ.";
  return { ...checked, valid: Object.keys(checked.errors).length === 0 };
}
