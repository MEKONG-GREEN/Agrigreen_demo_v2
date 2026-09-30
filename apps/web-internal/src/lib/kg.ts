// Khối lượng kg dạng chuỗi thập phân tối đa 3 chữ số (R-10) — tính bằng BigInt gam, KHÔNG qua float.
// Dùng cho kiểm dữ liệu vào trên form và cho backend giả lập; hiển thị qua formatKg/formatTons.

const KG_RE = /^(-?)(\d+)(?:\.(\d{1,3}))?$/;
const PCT_RE = /^\d{1,3}(?:\.\d{1,2})?$/;

/** Chuỗi kg hợp lệ theo hợp đồng (Kg: ^-?[0-9]+(\.[0-9]{1,3})?$). */
export function isKg(s: string): boolean {
  return KG_RE.test(s.trim());
}

/** Phần trăm hợp lệ theo hợp đồng (Pct: 0..100, tối đa 2 chữ số). */
export function isPct(s: string): boolean {
  const t = s.trim();
  if (!PCT_RE.test(t)) return false;
  return grams(t) <= 100_000n;
}

/** Chuỗi kg → gam (BigInt). Ném lỗi nếu sai dạng. */
export function grams(kg: string): bigint {
  const m = KG_RE.exec(kg.trim());
  if (!m) throw new Error(`Khối lượng không hợp lệ: ${kg}`);
  const v = BigInt(m[2] ?? "0") * 1000n + BigInt((m[3] ?? "").padEnd(3, "0") || "0");
  return m[1] === "-" ? -v : v;
}

/** Gam → chuỗi kg chuẩn 3 chữ số ("96800.000"). */
export function kgOf(g: bigint): string {
  const neg = g < 0n;
  const a = neg ? -g : g;
  return `${neg ? "-" : ""}${a / 1000n}.${(a % 1000n).toString().padStart(3, "0")}`;
}

/** Chuẩn hoá chuỗi kg về 3 chữ số. */
export function normKg(kg: string): string {
  return kgOf(grams(kg));
}

export function addKg(...xs: string[]): string {
  return kgOf(xs.reduce((s, x) => s + grams(x), 0n));
}

export function subKg(a: string, b: string): string {
  return kgOf(grams(a) - grams(b));
}

export function cmpKg(a: string, b: string): -1 | 0 | 1 {
  const x = grams(a);
  const y = grams(b);
  return x < y ? -1 : x > y ? 1 : 0;
}

/** Chia làm tròn nửa ra xa 0 (như decimal.Round của backend). */
function divRound(n: bigint, d: bigint): bigint {
  if (d === 0n) throw new Error("chia cho 0");
  const neg = n < 0n !== d < 0n;
  const an = n < 0n ? -n : n;
  const ad = d < 0n ? -d : d;
  const q = (an * 2n + ad) / (ad * 2n);
  return neg ? -q : q;
}

/** Phần trăm (cơ số 100) dạng BigInt ×100 → chuỗi "10.00" / "-0.40". */
function pctString(hundredths: bigint): string {
  const neg = hundredths < 0n;
  const a = neg ? -hundredths : hundredths;
  return `${neg ? "-" : ""}${a / 100n}.${(a % 100n).toString().padStart(2, "0")}`;
}

/** (a − b) / b × 100, làm tròn 2 chữ số (domain.pctOf). */
export function pctDiff(a: string, b: string): string {
  return pctString(divRound((grams(a) - grams(b)) * 10_000n, grams(b)));
}

/** part / whole × 100, làm tròn 2 chữ số. */
export function pctOfWhole(part: string, whole: string): string {
  return pctString(divRound(grams(part) * 10_000n, grams(whole)));
}

/** kg × pct / 100, làm tròn 3 chữ số (gam). */
export function applyPct(kg: string, pct: string): string {
  const p = grams(pct); // pct × 1000
  return kgOf(divRound(grams(kg) * p, 100_000n));
}

/** kg × n (số nguyên). */
export function mulKg(kg: string, n: number): string {
  return kgOf(grams(kg) * BigInt(Math.trunc(n)));
}

/** kg / n làm tròn 3 chữ số (sản lượng quy đổi kg/cuộn). */
export function perUnit(kg: string, n: number): string {
  return kgOf(divRound(grams(kg), BigInt(Math.trunc(n))));
}

/** So sánh phần trăm dạng chuỗi (giá trị tuyệt đối) với ngưỡng. */
export function absPctGreater(pct: string, threshold: string): boolean {
  const a = grams(pct.startsWith("-") ? pct.slice(1) : pct);
  return a > grams(threshold);
}

const nf = (min: number, max: number) => new Intl.NumberFormat("vi-VN", { minimumFractionDigits: min, maximumFractionDigits: max });

/** Hiển thị kg kiểu Việt Nam ("96.800 kg"). Chỉ để HIỂN THỊ. */
export function formatKg(kg: string | null | undefined, digits = 0): string {
  if (kg === null || kg === undefined || kg === "" || !isKg(kg)) return "—";
  return `${nf(0, digits).format(Number(kg))} kg`;
}

/** Hiển thị tấn ("96,8 t"). */
export function formatTons(kg: string | null | undefined): string {
  if (kg === null || kg === undefined || kg === "" || !isKg(kg)) return "—";
  return `${nf(1, 1).format(Number(kg) / 1000)} t`;
}

/** Hiển thị phần trăm có dấu ("+10,00 %"). */
export function formatPct(p: string | null | undefined, signed = false): string {
  if (p === null || p === undefined || p === "") return "—";
  const n = Number(p);
  if (!Number.isFinite(n)) return "—";
  const s = nf(2, 2).format(n);
  return `${signed && n > 0 ? "+" : ""}${s} %`;
}
