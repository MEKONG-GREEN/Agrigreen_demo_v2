// Số thập phân chính xác trên BigInt (R-10: không float) cho lưới mẫu báo cáo KN v7.7.
// Hình thức chuỗi chuẩn giống shopspring/decimal String() của máy chủ: "38.5", "-0.25", "126"
// (bỏ số 0 thừa sau dấu chấm). Định dạng hiển thị vi-VN ("1.234,5") làm trên chuỗi, không qua Number.

const TEN = 10n;

function pow10(n: number): bigint {
  return TEN ** BigInt(n);
}

const DEC_RE = /^([+-]?)(\d*)(?:\.(\d*))?(?:[eE]([+-]?\d+))?$/;

/** Giá trị = coef × 10^(−scale), scale ≥ 0. Bất biến. */
export class Decimal {
  readonly coef: bigint;
  readonly scale: number;

  private constructor(coef: bigint, scale: number) {
    if (scale < 0) {
      this.coef = coef * pow10(-scale);
      this.scale = 0;
    } else {
      this.coef = coef;
      this.scale = scale;
    }
  }

  static readonly ZERO = new Decimal(0n, 0);
  static readonly ONE = new Decimal(1n, 0);

  static of(coef: bigint, scale = 0): Decimal {
    return new Decimal(coef, scale);
  }

  static fromInt(n: number | bigint): Decimal {
    return new Decimal(BigInt(n), 0);
  }

  /** Đọc như decimal.NewFromString: "12", "-1.50", ".5", "1e3". Sai dạng → null. */
  static parse(s: string): Decimal | null {
    const m = DEC_RE.exec(s);
    if (!m) return null;
    const int = m[2] ?? "";
    const frac = m[3] ?? "";
    if (int === "" && frac === "") return null;
    const exp = m[4] ? Number.parseInt(m[4], 10) : 0;
    if (!Number.isFinite(exp) || Math.abs(exp) > 1000) return null;
    let coef = BigInt((int || "0") + frac);
    if (m[1] === "-") coef = -coef;
    return new Decimal(coef, frac.length - exp);
  }

  isZero(): boolean {
    return this.coef === 0n;
  }

  sign(): -1 | 0 | 1 {
    return this.coef === 0n ? 0 : this.coef < 0n ? -1 : 1;
  }

  private rescale(scale: number): bigint {
    return this.coef * pow10(scale - this.scale);
  }

  cmp(o: Decimal): -1 | 0 | 1 {
    const s = Math.max(this.scale, o.scale);
    const a = this.rescale(s);
    const b = o.rescale(s);
    return a < b ? -1 : a > b ? 1 : 0;
  }

  eq(o: Decimal): boolean {
    return this.cmp(o) === 0;
  }

  lt(o: Decimal): boolean {
    return this.cmp(o) < 0;
  }

  gt(o: Decimal): boolean {
    return this.cmp(o) > 0;
  }

  add(o: Decimal): Decimal {
    const s = Math.max(this.scale, o.scale);
    return new Decimal(this.rescale(s) + o.rescale(s), s);
  }

  sub(o: Decimal): Decimal {
    const s = Math.max(this.scale, o.scale);
    return new Decimal(this.rescale(s) - o.rescale(s), s);
  }

  mul(o: Decimal): Decimal {
    return new Decimal(this.coef * o.coef, this.scale + o.scale);
  }

  neg(): Decimal {
    return new Decimal(-this.coef, this.scale);
  }

  abs(): Decimal {
    return this.coef < 0n ? this.neg() : this;
  }

  /** Chia, làm tròn tới `places` chữ số thập phân, nửa xa số 0 (như DivRound). Chia 0 → ném lỗi. */
  divRound(o: Decimal, places: number): Decimal {
    if (o.coef === 0n) throw new RangeError("division by zero");
    // (c1/10^s1)/(c2/10^s2) = c1·10^s2 / (c2·10^s1); kết quả scale = places.
    let num = this.coef * pow10(o.scale);
    let den = o.coef * pow10(this.scale);
    if (places >= 0) num *= pow10(places);
    else den *= pow10(-places);
    return new Decimal(roundDiv(num, den), places);
  }

  /** Làm tròn tới `places` chữ số (âm = hàng chục, trăm…), nửa xa số 0 (như Round / Excel ROUND). */
  round(places: number): Decimal {
    return this.roundWith(places, "half");
  }

  /** Về phía +∞. */
  roundCeil(places: number): Decimal {
    return this.roundWith(places, "ceil");
  }

  /** Về phía −∞. */
  roundFloor(places: number): Decimal {
    return this.roundWith(places, "floor");
  }

  /** Bỏ phần sau `places` chữ số (về phía 0). */
  truncate(places: number): Decimal {
    return this.roundWith(places, "trunc");
  }

  private roundWith(places: number, mode: "half" | "ceil" | "floor" | "trunc"): Decimal {
    if (places >= this.scale) return this;
    const drop = this.scale - places; // > 0
    const d = pow10(drop);
    let q = this.coef / d; // về phía 0
    const r = this.coef - q * d;
    if (r !== 0n) {
      switch (mode) {
        case "half":
          if (2n * (r < 0n ? -r : r) >= d) q += this.coef < 0n ? -1n : 1n;
          break;
        case "ceil":
          if (r > 0n) q += 1n;
          break;
        case "floor":
          if (r < 0n) q -= 1n;
          break;
        case "trunc":
          break;
      }
    }
    return new Decimal(q, places);
  }

  /** Nhân 10^n (n âm = chia). */
  shift(n: number): Decimal {
    return new Decimal(this.coef, this.scale - n);
  }

  isInteger(): boolean {
    return this.truncate(0).eq(this);
  }

  /** Phần nguyên (về phía 0). */
  intPart(): bigint {
    return this.coef / pow10(this.scale);
  }

  /** Chuỗi chuẩn: không số 0 thừa, không dấu chấm khi nguyên ("38.5", "126", "-0.05"). */
  toString(): string {
    if (this.coef === 0n) return "0";
    const neg = this.coef < 0n;
    let digits = (neg ? -this.coef : this.coef).toString();
    let scale = this.scale;
    // bỏ số 0 cuối phần thập phân
    while (scale > 0 && digits.endsWith("0")) {
      digits = digits.slice(0, -1);
      scale--;
    }
    let out: string;
    if (scale === 0) out = digits;
    else {
      const pad = digits.padStart(scale + 1, "0");
      out = `${pad.slice(0, pad.length - scale)}.${pad.slice(pad.length - scale)}`;
    }
    return neg ? `-${out}` : out;
  }
}

function roundDiv(num: bigint, den: bigint): bigint {
  let q = num / den;
  const r = num - q * den;
  if (r !== 0n) {
    const ar = r < 0n ? -r : r;
    const ad = den < 0n ? -den : den;
    if (2n * ar >= ad) q += (num < 0n) !== (den < 0n) ? -1n : 1n;
  }
  return q;
}

// ---------------------------------------------------------------------------
// Nhập liệu & hiển thị vi-VN (chỉ chuỗi)
// ---------------------------------------------------------------------------

/**
 * Chuẩn hóa chuỗi số người gõ hoặc dán từ Excel tiếng Việt về dạng máy chủ nhận ("1234.5"):
 * bỏ khoảng trắng (kể cả NBSP); có dấu phẩy → kiểu vi-VN (dấu chấm là phân cách nghìn, phẩy là thập phân);
 * không có dấu phẩy → dấu chấm là thập phân. Không đọc được → trả nguyên văn (máy chủ báo lỗi kiểu tại ô).
 */
export function normalizeNumberInput(raw: string): string {
  let s = raw.replace(/[\s  ]/g, "");
  if (s === "") return "";
  if (s.includes(",")) {
    s = s.replace(/\./g, "").replace(",", ".");
  }
  const d = Decimal.parse(s);
  if (!d || /[eE]/.test(s)) return raw.trim();
  return d.toString();
}

/** Ngày người gõ: "dd/mm/yyyy", "d/m/yyyy" hoặc "yyyy-mm-dd" → "yyyy-mm-dd". Sai dạng → nguyên văn. */
export function normalizeDateInput(raw: string): string {
  const s = raw.trim();
  const iso = /^(\d{4})-(\d{1,2})-(\d{1,2})$/.exec(s);
  const vn = /^(\d{1,2})[/.-](\d{1,2})[/.-](\d{4})$/.exec(s);
  const parts = iso ? [iso[1], iso[2], iso[3]] : vn ? [vn[3], vn[2], vn[1]] : null;
  if (!parts) return s;
  const [y, m, d] = parts as [string, string, string];
  return `${y}-${m.padStart(2, "0")}-${d.padStart(2, "0")}`;
}

/** "2026-09-01" → "01/09/2026"; khác dạng → nguyên văn. */
export function formatDateVi(iso: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  return m ? `${m[3]}/${m[2]}/${m[1]}` : iso;
}

/**
 * Hiển thị số kiểu Việt Nam ("1.234,5"): phân cách nghìn ".", thập phân ",". `maxDecimals` làm tròn nửa xa
 * số 0 rồi bỏ số 0 thừa; `minDecimals` giữ tối thiểu. Chuỗi không phải số → nguyên văn.
 */
export function formatDecimalVi(value: string, opts: { maxDecimals?: number; minDecimals?: number; group?: boolean } = {}): string {
  const d = Decimal.parse(value.trim());
  if (!d) return value;
  const r = opts.maxDecimals === undefined ? d : d.round(opts.maxDecimals);
  let s = r.toString();
  const neg = s.startsWith("-");
  if (neg) s = s.slice(1);
  let [int = "0", frac = ""] = s.split(".");
  if (opts.minDecimals && frac.length < opts.minDecimals) frac = frac.padEnd(opts.minDecimals, "0");
  if (opts.group !== false) int = int.replace(/\B(?=(\d{3})+(?!\d))/g, ".");
  const out = frac ? `${int},${frac}` : int;
  return neg && out !== "0" ? `-${out}` : out;
}
