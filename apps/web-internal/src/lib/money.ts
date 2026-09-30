// Tiền VND và đơn giá dạng CHUỖI THẬP PHÂN (R-10): tiền numeric(18,2), đơn giá numeric(18,4).
// Mọi phép tính dùng BigInt (xu = 1/100 đồng; đơn vị giá = 1/10 000 đồng) — KHÔNG BAO GIỜ qua float,
// kể cả khi định dạng hiển thị (Intl.NumberFormat chỉ nhận phần nguyên dạng BigInt).

const MONEY_RE = /^(-?)(\d+)(?:\.(\d{1,2}))?$/;
const PRICE_RE = /^(\d+)(?:\.(\d{1,4}))?$/;
const DEC_RE = /^(-?)(\d+)(?:\.(\d+))?$/;

/** Chuỗi tiền hợp lệ theo hợp đồng (Money: ^-?[0-9]+(\.[0-9]{1,2})?$). */
export function isMoney(s: string | null | undefined): boolean {
  return !!s && MONEY_RE.test(s.trim());
}

/** Tiền dương (> 0). */
export function isPositiveMoney(s: string | null | undefined): boolean {
  return isMoney(s) && cents(s!) > 0n;
}

/** Đơn giá hợp lệ (Price/UnitPrice: ^[0-9]+(\.[0-9]{1,4})?$), > 0. */
export function isPrice(s: string | null | undefined): boolean {
  return !!s && PRICE_RE.test(s.trim()) && priceUnits(s) > 0n;
}

/** Chuỗi tiền → xu (BigInt). Ném lỗi nếu sai dạng. */
export function cents(s: string): bigint {
  const m = MONEY_RE.exec(s.trim());
  if (!m) throw new Error(`Số tiền không hợp lệ: ${s}`);
  const v = BigInt(m[2] ?? "0") * 100n + BigInt((m[3] ?? "").padEnd(2, "0") || "0");
  return m[1] === "-" ? -v : v;
}

/** Xu → chuỗi tiền chuẩn 2 chữ số ("24813134.93"). */
export function moneyOf(c: bigint): string {
  const neg = c < 0n;
  const a = neg ? -c : c;
  return `${neg ? "-" : ""}${a / 100n}.${(a % 100n).toString().padStart(2, "0")}`;
}

/** Chuỗi đơn giá → đơn vị 1/10 000 đồng. */
export function priceUnits(s: string): bigint {
  const m = PRICE_RE.exec(s.trim());
  if (!m) throw new Error(`Đơn giá không hợp lệ: ${s}`);
  return BigInt(m[1] ?? "0") * 10_000n + BigInt((m[2] ?? "").padEnd(4, "0") || "0");
}

/** Đơn vị giá → chuỗi 4 chữ số ("650000.0000"). */
export function priceOf(u: bigint): string {
  return `${u / 10_000n}.${(u % 10_000n).toString().padStart(4, "0")}`;
}

/** Chia làm tròn nửa ra xa 0 (như decimal.Round của backend Go). */
export function divRound(n: bigint, d: bigint): bigint {
  if (d === 0n) throw new Error("chia cho 0");
  const neg = n < 0n !== d < 0n;
  const an = n < 0n ? -n : n;
  const ad = d < 0n ? -d : d;
  const q = (an * 2n + ad) / (ad * 2n);
  return neg ? -q : q;
}

export function addMoney(...xs: (string | null | undefined)[]): string {
  return moneyOf(xs.reduce<bigint>((s, x) => s + (x ? cents(x) : 0n), 0n));
}

export function subMoney(a: string, b: string): string {
  return moneyOf(cents(a) - cents(b));
}

export function cmpMoney(a: string, b: string): -1 | 0 | 1 {
  const x = cents(a);
  const y = cents(b);
  return x < y ? -1 : x > y ? 1 : 0;
}

/** Chuỗi kg (≤ 3 số lẻ) → gam. */
function gramsOf(kg: string): bigint {
  const m = /^(-?)(\d+)(?:\.(\d{1,3}))?$/.exec(kg.trim());
  if (!m) throw new Error(`Khối lượng không hợp lệ: ${kg}`);
  const v = BigInt(m[2] ?? "0") * 1000n + BigInt((m[3] ?? "").padEnd(3, "0") || "0");
  return m[1] === "-" ? -v : v;
}

/** Thành tiền theo khối lượng: kg / 1000 × đơn giá/tấn, làm tròn nửa lên 2 số (orders.LineAmount, costing PM-02). */
export function amountForKg(kg: string, unitPricePerTonne: string): string {
  // gam × đơn vị giá / (10^6 gam/tấn × 10^4) × 100 xu = gam × u / 10^8
  return moneyOf(divRound(gramsOf(kg) * priceUnits(unitPricePerTonne), 100_000_000n));
}

/** Thành tiền theo số lượng nguyên (cuộn): n × đơn giá/cuộn, làm tròn 2 số. */
export function amountForCount(n: number | bigint, unitPrice: string): string {
  const count = typeof n === "bigint" ? n : BigInt(Math.trunc(n));
  return moneyOf(divRound(count * priceUnits(unitPrice), 100n));
}

/** Đơn giá sau chiết khấu: giá × (100 − ck) / 100, làm tròn 4 số (orders.NetPrice). */
export function netPrice(unitPrice: string, discountPct: string | null | undefined): string {
  const pct = discountPct ? percentHundredths(discountPct) : 0n;
  return priceOf(divRound(priceUnits(unitPrice) * (10_000n - pct), 10_000n));
}

/** "14.5" → 1450 (phần trăm × 100). */
export function percentHundredths(pct: string): bigint {
  const m = /^(\d{1,3})(?:\.(\d{1,2}))?$/.exec(pct.trim());
  if (!m) throw new Error(`Phần trăm không hợp lệ: ${pct}`);
  return BigInt(m[1] ?? "0") * 100n + BigInt((m[2] ?? "").padEnd(2, "0") || "0");
}

/** Số tiền × pct / 100, làm tròn 2 số. */
export function applyMoneyPct(amount: string, pct: string): string {
  return moneyOf(divRound(cents(amount) * percentHundredths(pct), 10_000n));
}

/** |a − b| / b × 100 của hai đơn giá, 2 số lẻ ("5.00"); b = 0 → null. */
export function priceDeltaPct(a: string, b: string): string | null {
  const x = priceUnits(a);
  const y = priceUnits(b);
  if (y === 0n) return null;
  const d = divRound((x > y ? x - y : y - x) * 10_000n, y);
  return `${d / 100n}.${(d % 100n).toString().padStart(2, "0")}`;
}

/**
 * Chia doanh thu carbon (proto carbonRevenueShare): giữ lại `retained` %, phần còn lại chia nông hộ / cơ quan;
 * VVB nhận PHẦN DƯ làm tròn để Σ = đúng số tiền.
 */
export function splitCarbon(amount: string, retainedPct: string, farmersPct: string, authorityPct: string) {
  const total = cents(amount);
  const retained = divRound(total * percentHundredths(retainedPct), 10_000n);
  const rest = total - retained;
  const farmers = divRound(rest * percentHundredths(farmersPct), 10_000n);
  const authority = divRound(rest * percentHundredths(authorityPct), 10_000n);
  const verifier = rest - farmers - authority;
  return { retained: moneyOf(retained), farmers: moneyOf(farmers), authority: moneyOf(authority), verifier: moneyOf(verifier) };
}

/** Σ Nợ = Σ Có (> 0) của dòng bút toán. */
export function journalTotals(lines: readonly { side?: string; amount?: string }[]): { debit: string; credit: string; balanced: boolean } {
  let d = 0n;
  let c = 0n;
  for (const l of lines) {
    const v = l.amount ? cents(l.amount) : 0n;
    if (l.side === "debit") d += v;
    else if (l.side === "credit") c += v;
  }
  return { debit: moneyOf(d), credit: moneyOf(c), balanced: d === c && d > 0n };
}

const intFmt = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 0 });

/** Nhóm phần nguyên kiểu Việt Nam + phần lẻ sau dấu phẩy, bỏ số 0 thừa ở phần lẻ. */
function groupDecimal(s: string, maxFrac: number): string | null {
  const m = DEC_RE.exec(s.trim());
  if (!m) return null;
  const neg = m[1] === "-";
  const frac = (m[3] ?? "").slice(0, maxFrac).replace(/0+$/, "");
  const intPart = intFmt.format(BigInt(m[2] ?? "0"));
  const isZero = /^0*$/.test(m[2] ?? "") && frac === "";
  return `${neg && !isZero ? "-" : ""}${intPart}${frac ? `,${frac}` : ""}`;
}

/** Hiển thị tiền VND theo vi-VN ("24.813.134,93 ₫", "1.250.000 ₫"). Không qua float. */
export function formatVnd(amount: string | null | undefined): string {
  if (amount === null || amount === undefined || amount === "") return "—";
  const g = groupDecimal(amount, 2);
  return g === null ? "—" : `${g} ₫`;
}

/** Hiển thị đơn giá ("650.000 ₫/tấn", "1.500,5 ₫/cuộn"). */
export function formatPrice(price: string | null | undefined, per?: "tấn" | "cuộn" | "t·km" | "chuyến" | "kg"): string {
  if (price === null || price === undefined || price === "") return "—";
  const g = groupDecimal(price, 4);
  return g === null ? "—" : `${g} ₫${per ? `/${per}` : ""}`;
}

/** Hiển thị số thập phân bất kỳ (km, tấn…) không qua float, tối đa `maxFrac` số lẻ. */
export function formatDecimalString(v: string | null | undefined, maxFrac = 3): string {
  if (v === null || v === undefined || v === "") return "—";
  return groupDecimal(v, maxFrac) ?? "—";
}

/** Băm rút gọn để hiển thị ("a1b2c3d4…e5f6"), đủ trong title. */
export function shortHash(h: string | null | undefined): string {
  if (!h) return "—";
  return h.length <= 16 ? h : `${h.slice(0, 8)}…${h.slice(-6)}`;
}
