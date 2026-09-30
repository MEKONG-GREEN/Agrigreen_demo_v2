// So sánh và định dạng số thập phân dạng chuỗi mà KHÔNG qua float (R-10):
// ngưỡng 85% phải so đúng với "84.9999" và "85.0000".

const SCALE = 6;

/** Chuỗi thập phân → BigInt đã nhân 10^6. Trả null nếu không hợp lệ. */
export function toScaled(value: string | number): bigint | null {
  const s = typeof value === "number" ? (Number.isFinite(value) ? String(value) : "") : value.trim();
  const m = /^([+-]?)(\d*)(?:\.(\d*))?$/.exec(s);
  if (!m || (m[2] === "" && (m[3] ?? "") === "")) return null;
  const sign = m[1] === "-" ? -1n : 1n;
  const intPart = m[2] || "0";
  const frac = (m[3] ?? "").padEnd(SCALE, "0").slice(0, SCALE);
  return sign * (BigInt(intPart) * 10n ** BigInt(SCALE) + BigInt(frac || "0"));
}

/** -1 / 0 / 1 như a <=> b. Ném lỗi nếu chuỗi không hợp lệ. */
export function compareDecimal(a: string | number, b: string | number): -1 | 0 | 1 {
  const x = toScaled(a);
  const y = toScaled(b);
  if (x === null || y === null) throw new Error(`Số thập phân không hợp lệ: ${String(x === null ? a : b)}`);
  return x < y ? -1 : x > y ? 1 : 0;
}

const nf = (digits: number) =>
  new Intl.NumberFormat("vi-VN", { maximumFractionDigits: digits, minimumFractionDigits: 0 });

/** Định dạng hiển thị kiểu Việt Nam (312.480,5). Chỉ để HIỂN THỊ. */
export function formatDecimal(value: string | number | null | undefined, digits = 1): string {
  if (value === null || value === undefined || value === "") return "—";
  const n = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(n)) return "—";
  return nf(digits).format(n);
}

export function formatInt(value: number | null | undefined): string {
  if (value === null || value === undefined) return "—";
  return nf(0).format(value);
}
