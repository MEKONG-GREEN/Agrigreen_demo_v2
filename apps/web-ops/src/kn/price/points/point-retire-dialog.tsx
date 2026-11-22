"use client";

import { useState } from "react";
import { Dialog } from "@/components/Dialog";
import { TextField } from "@/components/form";
import type { PricePoint, PricePointApi, PriceRegion } from "./api";
import { validatePointFields, type FieldErrors } from "./point-form";
import { pointFieldErrors } from "./point-view";
import { usePointAction } from "./point-action";

export function RetireConfirmation({
  value,
  pa,
  canManage,
  onClose,
}: {
  value:
    | { kind: "point"; value: PricePoint }
    | { kind: "region"; value: PriceRegion };
  pa: PricePointApi;
  canManage: boolean;
  onClose: (changed: boolean) => void;
}) {
  const act = usePointAction();
  const [reason, setReason] = useState("");
  const [day, setDay] = useState("");
  const [local, setLocal] = useState<FieldErrors>({});
  const errors = pointFieldErrors(act.problem, local);
  const save = async () => {
    if (!canManage || act.busy) return;
    if (value.kind === "point") {
      const checked = validatePointFields(
        {
          admin_unit_code: "",
          region_code: "",
          valid_from: day,
          reason,
          note: "",
        },
        "retire",
      );
      setLocal(checked.errors);
      if (!checked.valid) return;
    }
    const result = await act.run(() =>
      value.kind === "point"
        ? pa.retirePoint(value.value.code, {
            reason: reason.trim(),
            valid_from: day || undefined,
          })
        : pa.retireRegion(value.value.code),
    );
    if (result) onClose(true);
  };
  return (
    <Dialog
      title={`Ngưng ${value.kind === "point" ? "xã" : "nhóm vùng"} ${value.value.code}`}
      onClose={() => onClose(false)}
      actions={
        <button
          type="button"
          className="btn btn-d"
          disabled={!canManage || act.busy}
          onClick={save}
        >
          Xác nhận ngưng
        </button>
      }
    >
      {act.view}
      <p>
        {value.value.name} sẽ ngưng theo quy tắc hiệu lực; dữ liệu lịch sử được
        giữ nguyên.
      </p>
      {value.kind === "point" && (
        <>
          <TextField
            label="Lý do ngưng"
            required
            multiline
            maxLength={500}
            value={reason}
            onChange={setReason}
            error={errors.reason}
          />
          <div className="field">
            <label htmlFor="retire-day">Ngưng từ ngày</label>
            <input
              id="retire-day"
              type="date"
              className="input"
              value={day}
              onChange={(e) => setDay(e.target.value)}
              aria-invalid={!!errors.valid_from}
            />
            {errors.valid_from && (
              <span className="err" role="alert">
                {errors.valid_from}
              </span>
            )}
            <small>Để trống = ngày báo giá kế tiếp.</small>
          </div>
        </>
      )}
    </Dialog>
  );
}
