"use client";

// Ô nhập dùng chung cho mọi form KN (PRD kn-bugfix-brd-alignment R-A1, R-A4):
// trường bắt buộc có dấu *, báo lỗi tiếng Việt ngay dưới từng trường, ô số không nhận số âm.
// Số giữ dạng CHUỖI thập phân máy chủ nhận ("1234.5"), không qua float (R-10). Máy chủ vẫn phải kiểm lại.
import { useId, type ReactNode } from "react";
import { normalizeNumberInput } from "@/lib/exactDecimal";

export interface NumberRule {
  /** Bắt buộc nhập. */
  required?: boolean;
  /** Chỉ số nguyên (số lượng, năm). */
  integer?: boolean;
  /** Phải > 0 thay vì ≥ 0 (số lượng máy, diện tích vùng dịch). */
  positive?: boolean;
  /** Cho phép số âm — chỉ dùng khi nghiệp vụ có nghĩa âm (vd chênh lệch). Mặc định không. */
  allowNegative?: boolean;
  /** Giá trị lớn nhất (vd năm sản xuất ≤ năm hiện tại). */
  max?: string | number;
  /** Số chữ số thập phân tối đa. */
  maxDecimals?: number;
}

export type ParsedNumber = { ok: true; value: string } | { ok: false; error: string };

/** Kiểm một ô số người gõ (kiểu vi-VN "1.234,5" hoặc "1234.5"). Rỗng mà không bắt buộc → value "". */
export function parseNumberInput(raw: string, rule: NumberRule = {}): ParsedNumber {
  if (raw.trim() === "") return rule.required ? { ok: false, error: "Bắt buộc nhập." } : { ok: true, value: "" };
  const v = normalizeNumberInput(raw);
  if (!/^-?\d+(\.\d+)?$/.test(v)) return { ok: false, error: "Không phải số hợp lệ." };
  if (rule.integer && v.includes(".")) return { ok: false, error: "Chỉ nhập số nguyên." };
  if (rule.maxDecimals !== undefined && (v.split(".")[1]?.length ?? 0) > rule.maxDecimals) {
    return { ok: false, error: `Tối đa ${rule.maxDecimals} chữ số thập phân.` };
  }
  const sign = compareExact(v, "0");
  if (!rule.allowNegative && sign < 0) return { ok: false, error: "Không được nhập số âm." };
  if (rule.positive && !(sign > 0)) return { ok: false, error: "Phải lớn hơn 0." };
  if (rule.max !== undefined) {
    const c = compareExact(v, String(rule.max));
    if (Number.isNaN(c) || c > 0) return { ok: false, error: `Không được lớn hơn ${rule.max}.` };
  }
  return { ok: true, value: v };
}

const DECIMAL_RE = /^([+-]?)(\d+)(?:\.(\d+))?$/;

/**
 * So sánh hai chuỗi thập phân ĐỦ độ chính xác (không cắt chữ số như `compareDecimal` 6 chữ số, không qua float):
 * "-0.0000001" < 0, "1.0000001" > "1". Chuỗi không hợp lệ → NaN để người gọi coi là sai.
 */
export function compareExact(a: string, b: string): number {
  const x = DECIMAL_RE.exec(a.trim());
  const y = DECIMAL_RE.exec(b.trim());
  if (!x || !y) return NaN;
  const scale = Math.max(x[3]?.length ?? 0, y[3]?.length ?? 0);
  const toBig = (m: RegExpExecArray) => {
    const n = BigInt(m[2]! + (m[3] ?? "").padEnd(scale, "0"));
    return m[1] === "-" ? -n : n;
  };
  const p = toBig(x);
  const q = toBig(y);
  return p < q ? -1 : p > q ? 1 : 0;
}

export type FieldRule = { required?: boolean; label?: string } & ({ kind: "text"; maxLength?: number } | ({ kind: "number" } & NumberRule));

/**
 * Kiểm cả form trước khi Lưu: trả lỗi theo từng trường (rỗng = hợp lệ) và giá trị số đã chuẩn hóa.
 * Màn hình hiện lỗi dưới trường và KHÔNG gọi API khi còn lỗi.
 */
export function validateForm<K extends string>(
  values: Record<K, string>,
  rules: Partial<Record<K, FieldRule>>,
): { errors: Partial<Record<K, string>>; values: Record<K, string>; valid: boolean } {
  const errors: Partial<Record<K, string>> = {};
  const out = { ...values };
  for (const key of Object.keys(rules) as K[]) {
    const rule = rules[key]!;
    const raw = values[key] ?? "";
    if (rule.kind === "number") {
      const p = parseNumberInput(raw, rule);
      if (p.ok) out[key] = p.value;
      else errors[key] = p.error;
    } else if (rule.required && raw.trim() === "") {
      errors[key] = "Bắt buộc nhập.";
    } else if (rule.maxLength !== undefined && raw.length > rule.maxLength) {
      errors[key] = `Tối đa ${rule.maxLength} ký tự.`;
    }
  }
  return { errors, values: out, valid: Object.keys(errors).length === 0 };
}

function FieldShell({ id, label, required, error, hint, children }: { id: string; label: string; required?: boolean; error?: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="field">
      <label htmlFor={id}>
        {label}
        {required ? (
          <span className="req" aria-hidden="true">
            {" "}*
          </span>
        ) : null}
      </label>
      {children}
      {hint && !error ? <span className="mute hint">{hint}</span> : null}
      {error ? (
        <span className="err" id={`${id}-err`} role="alert">
          {error}
        </span>
      ) : null}
    </div>
  );
}

interface BaseProps {
  label: string;
  value: string;
  onChange: (value: string) => void;
  required?: boolean;
  /** Lỗi từ `validateForm` hoặc từ máy chủ. */
  error?: string;
  hint?: ReactNode;
  disabled?: boolean;
  name?: string;
}

/** Ô số: nhận chuỗi, không nhận số âm (trừ `allowNegative`), báo lỗi ngay khi rời ô. */
export function NumberField({ unit, rule, onBlurError, ...p }: BaseProps & { unit?: string; rule?: NumberRule; onBlurError?: (error: string | undefined) => void }) {
  const id = useId();
  const r: NumberRule = { ...rule, required: p.required ?? rule?.required };
  return (
    <FieldShell id={id} label={unit ? `${p.label} (${unit})` : p.label} required={r.required} error={p.error} hint={p.hint}>
      <input
        id={id}
        name={p.name}
        className="input"
        inputMode={r.integer ? "numeric" : "decimal"}
        value={p.value}
        disabled={p.disabled}
        aria-required={r.required || undefined}
        aria-invalid={p.error ? true : undefined}
        aria-describedby={p.error ? `${id}-err` : undefined}
        onChange={(e) => p.onChange(e.target.value)}
        onBlur={() => {
          const res = parseNumberInput(p.value, r);
          onBlurError?.(res.ok ? undefined : res.error);
        }}
      />
    </FieldShell>
  );
}

export function TextField({ multiline, maxLength, placeholder, ...p }: BaseProps & { multiline?: boolean; maxLength?: number; placeholder?: string }) {
  const id = useId();
  const common = {
    id,
    name: p.name,
    value: p.value,
    disabled: p.disabled,
    maxLength,
    placeholder,
    "aria-required": p.required || undefined,
    "aria-invalid": p.error ? true : undefined,
    "aria-describedby": p.error ? `${id}-err` : undefined,
  } as const;
  return (
    <FieldShell
      id={id}
      label={p.label}
      required={p.required}
      error={p.error}
      hint={p.hint ?? (maxLength && multiline ? `${p.value.length}/${maxLength}` : undefined)}
    >
      {multiline ? (
        <textarea className="textarea" {...common} onChange={(e) => p.onChange(e.target.value)} />
      ) : (
        <input className="input" {...common} onChange={(e) => p.onChange(e.target.value)} />
      )}
    </FieldShell>
  );
}

export function SelectField({ options, placeholder = "Chọn…", ...p }: BaseProps & { options: readonly { value: string; label: string }[]; placeholder?: string }) {
  const id = useId();
  return (
    <FieldShell id={id} label={p.label} required={p.required} error={p.error} hint={p.hint}>
      <select
        id={id}
        name={p.name}
        className="select"
        value={p.value}
        disabled={p.disabled}
        aria-required={p.required || undefined}
        aria-invalid={p.error ? true : undefined}
        aria-describedby={p.error ? `${id}-err` : undefined}
        onChange={(e) => p.onChange(e.target.value)}
      >
        <option value="">{placeholder}</option>
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </FieldShell>
  );
}
