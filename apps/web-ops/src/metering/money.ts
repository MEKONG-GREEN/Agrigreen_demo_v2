// Tiền của bảng kê (pf/metering, ADR-0023): chuỗi thập phân numeric(18,4) / numeric(18,2) từ máy chủ.
// KHÔNG đổi sang float ở bất kỳ bước nào (R-10, agnofloat) — định dạng vi-VN và cộng/nhân bằng BigInt.

const DECIMAL = /^([+-]?)(\d+)(?:\.(\d+))?$/;

export interface ParsedDecimal {
  negative: boolean;
  int: string;
  frac: string;
}

/** Tách chuỗi thập phân ("1234567.8900", "-5", "0.50"). Chuỗi sai dạng (kể cả "1e5", "1,5") → null. */
export function parseDecimal(value: string): ParsedDecimal | null {
  const m = DECIMAL.exec(value.trim());
  if (!m) return null;
  const int = (m[2] ?? "0").replace(/^0+(?=\d)/, "");
  return { negative: m[1] === "-", int, frac: m[3] ?? "" };
}

/** Nhóm nghìn kiểu Việt Nam: 1234567 → "1.234.567". */
function groupThousands(int: string): string {
  return int.replace(/\B(?=(\d{3})+(?!\d))/g, ".");
}

export interface MoneyFormat {
  /** Số chữ số thập phân tối thiểu (mặc định 0). */
  minFraction?: number;
  /** Số chữ số thập phân tối đa — phần thừa được LÀM TRÒN nửa lên (mặc định 2). */
  maxFraction?: number;
  /** Thêm hậu tố " ₫" (mặc định có). */
  currency?: boolean;
}

/**
 * Định dạng tiền VND dạng chuỗi thập phân theo vi-VN, KHÔNG qua Number:
 * "1055518.50" → "1.055.518,5 ₫"; null/rỗng → "—"; sai dạng → "—".
 */
export function formatVnd(value: string | null | undefined, opts: MoneyFormat = {}): string {
  if (value === null || value === undefined || value.trim() === "") return "—";
  const p = parseDecimal(value);
  if (!p) return "—";
  const min = opts.minFraction ?? 0;
  const max = Math.max(opts.maxFraction ?? 2, min);
  let scaled = BigInt(p.int + p.frac.padEnd(max, "0").slice(0, max));
  // Làm tròn nửa lên theo chữ số bị cắt đầu tiên (giá trị tuyệt đối).
  const dropped = p.frac.slice(max);
  if ((dropped[0] ?? "0") >= "5") scaled += 1n;
  const digits = scaled.toString().padStart(max + 1, "0");
  const int = max > 0 ? digits.slice(0, -max) : digits;
  let frac = max > 0 ? digits.slice(-max) : "";
  while (frac.length > min && frac.endsWith("0")) frac = frac.slice(0, -1);
  const zero = /^0*$/.test(int + frac);
  const sign = p.negative && !zero ? "-" : "";
  const body = `${sign}${groupThousands(int)}${frac ? `,${frac}` : ""}`;
  return opts.currency === false ? body : `${body} ₫`;
}

/** Đơn giá (numeric(18,4)) — hiện tối đa 4 chữ số thập phân. */
export function formatUnitPrice(value: string | null | undefined): string {
  return formatVnd(value, { maxFraction: 4 });
}

/** Số nguyên (đơn vị, lời gọi) theo vi-VN — số nguyên an toàn, không phải tiền. */
export function formatCount(n: number | null | undefined): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return "—";
  const neg = n < 0;
  return `${neg ? "-" : ""}${groupThousands(String(Math.trunc(Math.abs(n))))}`;
}

/** Chuỗi thập phân → BigInt đã nhân 10^scale (cắt phần thừa). Sai dạng → ném lỗi. */
export function toScaledBig(value: string, scale: number): bigint {
  const p = parseDecimal(value);
  if (!p) throw new Error(`Số thập phân không hợp lệ: ${value}`);
  const v = BigInt(p.int + p.frac.padEnd(scale, "0").slice(0, scale));
  return p.negative ? -v : v;
}

/** BigInt đã nhân 10^scale → chuỗi thập phân cố định ("55518.50"). */
export function fromScaledBig(v: bigint, scale: number): string {
  const neg = v < 0n;
  const digits = (neg ? -v : v).toString().padStart(scale + 1, "0");
  const s = scale > 0 ? `${digits.slice(0, -scale)}.${digits.slice(-scale)}` : digits;
  return neg ? `-${s}` : s;
}

/**
 * Thành tiền = đơn giá numeric(18,4) × số đơn vị, làm tròn nửa lên về numeric(18,2) — như máy chủ
 * (decimal, không float). Dùng ở backend giả lập và để kiểm tra hiển thị.
 */
export function lineAmount(unitPrice4: string, units: number | bigint): string {
  const p = toScaledBig(unitPrice4, 4) * BigInt(units);
  const neg = p < 0n;
  const abs = neg ? -p : p;
  const rounded = (abs + 50n) / 100n;
  return fromScaledBig(neg ? -rounded : rounded, 2);
}

/** Tổng các chuỗi thập phân cùng thang 2 chữ số. */
export function sumMoney(values: readonly string[]): string {
  return fromScaledBig(
    values.reduce((acc, v) => acc + toScaledBig(v, 2), 0n),
    2,
  );
}
