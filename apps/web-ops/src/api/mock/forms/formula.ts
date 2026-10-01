// BẢN PORT TS (CHỈ CHO BACKEND GIẢ LẬP) của internal/platform/reports/formula: tập con công thức Excel
// trên decimal. Màn hình KHÔNG tính công thức — lưới gọi POST /v1/reports/form-versions/{id}/evaluate
// (máy chủ là nguồn đúng). Bản port giữ đúng hành vi để chế độ giả lập trùng máy chủ: kiểm bằng ca
// vàng do engine Go sinh (fixtures/eval-golden.json, ops-golden.json; engine.test.ts).
import { Decimal } from "@/kn/grid/decimal";
import { columnIndex, columnLetter } from "@/kn/grid/letters";

export const DIV_PRECISION = 20;

export class SyntaxError2 extends Error {
  constructor(
    readonly pos: number,
    readonly msg: string,
  ) {
    super(`công thức sai ở vị trí ${pos}: ${msg}`);
  }
}

/** Như %q của Go cho chuỗi. */
export function goQuote(s: string): string {
  return JSON.stringify(s);
}

type TokKind = "eof" | "num" | "str" | "ident" | "ref" | "op" | "lp" | "rp" | "comma" | "colon";
interface Token {
  kind: TokKind;
  text: string;
  pos: number;
}

const isSpace = (c: string) => /\s/u.test(c);
const isLetter = (c: string) => /\p{L}/u.test(c);
const isDigitU = (c: string) => /\p{Nd}/u.test(c);
const isAsciiDigit = (c: string | undefined) => c !== undefined && c >= "0" && c <= "9";

function lex(src: string): Token[] {
  const rs = Array.from(src);
  const out: Token[] = [];
  let i = 0;
  while (i < rs.length) {
    const c = rs[i]!;
    if (isSpace(c)) {
      i++;
    } else if (isAsciiDigit(c) || (c === "." && isAsciiDigit(rs[i + 1]))) {
      const start = i;
      while (i < rs.length && (isAsciiDigit(rs[i]) || rs[i] === ".")) i++;
      if (i < rs.length && (rs[i] === "e" || rs[i] === "E")) {
        let j = i + 1;
        if (j < rs.length && (rs[j] === "+" || rs[j] === "-")) j++;
        if (isAsciiDigit(rs[j])) {
          i = j;
          while (isAsciiDigit(rs[i])) i++;
        }
      }
      out.push({ kind: "num", text: rs.slice(start, i).join(""), pos: start });
    } else if (c === '"') {
      const start = i;
      i++;
      let b = "";
      let closed = false;
      while (i < rs.length) {
        if (rs[i] === '"') {
          if (rs[i + 1] === '"') {
            b += '"';
            i += 2;
            continue;
          }
          i++;
          closed = true;
          break;
        }
        b += rs[i];
        i++;
      }
      if (!closed) throw new SyntaxError2(start, "chuỗi chưa đóng nháy");
      out.push({ kind: "str", text: b, pos: start });
    } else if (c === "$" || isLetter(c) || c === "_") {
      const start = i;
      if (c === "$") i++;
      while (i < rs.length && (isLetter(rs[i]!) || isDigitU(rs[i]!) || rs[i] === "_" || rs[i] === ".")) i++;
      const word = rs.slice(start, i).join("");
      if (rs[i] === "#") {
        i++;
        out.push({ kind: "ref", text: `${word}#`, pos: start });
        continue;
      }
      if (word.startsWith("$")) throw new SyntaxError2(start, "tham chiếu cột phải có dạng F# (cùng dòng)");
      out.push({ kind: "ident", text: word, pos: start });
    } else if (c === "(") {
      out.push({ kind: "lp", text: "(", pos: i++ });
    } else if (c === ")") {
      out.push({ kind: "rp", text: ")", pos: i++ });
    } else if (c === "," || c === ";") {
      out.push({ kind: "comma", text: ",", pos: i++ });
    } else if (c === ":") {
      out.push({ kind: "colon", text: ":", pos: i++ });
    } else if (c === "<" || c === ">") {
      const n = rs[i + 1];
      if (n === "=" || (c === "<" && n === ">")) {
        out.push({ kind: "op", text: c + n, pos: i });
        i += 2;
      } else {
        out.push({ kind: "op", text: c, pos: i++ });
      }
    } else if ("+-*/^&%=".includes(c)) {
      out.push({ kind: "op", text: c, pos: i++ });
    } else {
      throw new SyntaxError2(i, `ký tự không hỗ trợ '${c}'`);
    }
  }
  out.push({ kind: "eof", text: "", pos: rs.length });
  return out;
}

export type Node =
  | { t: "num"; v: Decimal }
  | { t: "str"; v: string }
  | { t: "bool"; v: boolean }
  | { t: "ref"; col: number }
  | { t: "range"; from: number; to: number }
  | { t: "unary"; op: string; x: Node }
  | { t: "binary"; op: string; l: Node; r: Node }
  | { t: "call"; name: string; args: Node[] };

const FUNCS: Record<string, [number, number]> = {
  SUM: [1, -1],
  MIN: [1, -1],
  MAX: [1, -1],
  AVERAGE: [1, -1],
  COUNT: [1, -1],
  ROUND: [2, 2],
  ROUNDUP: [2, 2],
  ROUNDDOWN: [2, 2],
  ABS: [1, 1],
  IF: [2, 3],
  IFERROR: [2, 2],
  AND: [1, -1],
  OR: [1, -1],
  NOT: [1, 1],
  ISBLANK: [1, 1],
  ISNUMBER: [1, 1],
  XREF: [2, 2],
};

class Parser {
  i = 0;
  constructor(private readonly toks: Token[]) {}
  peek(): Token {
    return this.toks[this.i]!;
  }
  next(): Token {
    return this.toks[this.i++]!;
  }
  isOp(...ops: string[]): boolean {
    const t = this.peek();
    return t.kind === "op" && ops.includes(t.text);
  }
  comparison(): Node {
    let l = this.concat();
    while (this.isOp("=", "<>", "<", "<=", ">", ">=")) {
      const op = this.next().text;
      l = { t: "binary", op, l, r: this.concat() };
    }
    return l;
  }
  concat(): Node {
    let l = this.additive();
    while (this.isOp("&")) {
      this.next();
      l = { t: "binary", op: "&", l, r: this.additive() };
    }
    return l;
  }
  additive(): Node {
    let l = this.multiplicative();
    while (this.isOp("+", "-")) {
      const op = this.next().text;
      l = { t: "binary", op, l, r: this.multiplicative() };
    }
    return l;
  }
  multiplicative(): Node {
    let l = this.power();
    while (this.isOp("*", "/")) {
      const op = this.next().text;
      l = { t: "binary", op, l, r: this.power() };
    }
    return l;
  }
  power(): Node {
    let l = this.unary();
    while (this.isOp("^")) {
      this.next();
      l = { t: "binary", op: "^", l, r: this.unary() };
    }
    return l;
  }
  unary(): Node {
    if (this.isOp("-", "+")) {
      const op = this.next().text;
      return { t: "unary", op, x: this.unary() };
    }
    return this.postfix();
  }
  postfix(): Node {
    let x = this.primary();
    while (this.isOp("%")) {
      this.next();
      x = { t: "unary", op: "%", x };
    }
    return x;
  }
  primary(): Node {
    const t = this.next();
    switch (t.kind) {
      case "num": {
        const d = Decimal.parse(t.text);
        if (!d) throw new SyntaxError2(t.pos, `số không hợp lệ ${t.text}`);
        return { t: "num", v: d };
      }
      case "str":
        return { t: "str", v: t.text };
      case "ref": {
        let col = columnIndex(t.text.replace(/#$/, ""));
        if (col === 0) throw new SyntaxError2(t.pos, `chữ cái cột không hợp lệ ${t.text}`);
        if (this.peek().kind === "colon") {
          this.next();
          const t2 = this.next();
          if (t2.kind !== "ref") throw new SyntaxError2(t2.pos, "dải ô phải có dạng F#:L#");
          let col2 = columnIndex(t2.text.replace(/#$/, ""));
          if (col2 === 0) throw new SyntaxError2(t2.pos, `chữ cái cột không hợp lệ ${t2.text}`);
          if (col2 < col) [col, col2] = [col2, col];
          return { t: "range", from: col, to: col2 };
        }
        return { t: "ref", col };
      }
      case "ident": {
        const name = t.text.toUpperCase();
        if (this.peek().kind !== "lp") {
          if (name === "TRUE") return { t: "bool", v: true };
          if (name === "FALSE") return { t: "bool", v: false };
          throw new SyntaxError2(t.pos, `${goQuote(t.text)} không phải tham chiếu cùng dòng (viết F#) hay hàm`);
        }
        const arity = FUNCS[name];
        if (!arity) throw new SyntaxError2(t.pos, `hàm không hỗ trợ ${name}`);
        this.next();
        const args: Node[] = [];
        if (this.peek().kind !== "rp") {
          for (;;) {
            args.push(this.comparison());
            if (this.peek().kind === "comma") {
              this.next();
              continue;
            }
            break;
          }
        }
        const r = this.next();
        if (r.kind !== "rp") throw new SyntaxError2(r.pos, "thiếu dấu )");
        if (args.length < arity[0] || (arity[1] >= 0 && args.length > arity[1])) {
          throw new SyntaxError2(t.pos, `${name} nhận sai số đối số (${args.length})`);
        }
        if (name === "XREF" && args.some((a) => a.t !== "str")) throw new SyntaxError2(t.pos, 'XREF nhận hai chuỗi: XREF("T01","M")');
        return { t: "call", name, args };
      }
      case "lp": {
        const x = this.comparison();
        const r = this.next();
        if (r.kind !== "rp") throw new SyntaxError2(r.pos, "thiếu dấu )");
        return x;
      }
      case "eof":
        throw new SyntaxError2(t.pos, "công thức kết thúc đột ngột");
      default:
        throw new SyntaxError2(t.pos, `không mong đợi ${goQuote(t.text)}`);
    }
  }
}

export function parse(src: string): Node {
  let s = src.trim();
  if (s.startsWith("=")) s = s.slice(1);
  if (s.trim() === "") throw new SyntaxError2(0, "công thức rỗng");
  const p = new Parser(lex(s));
  const n = p.comparison();
  if (p.peek().kind !== "eof") throw new SyntaxError2(p.peek().pos, `thừa ${goQuote(p.peek().text)}`);
  return n;
}

function walk(n: Node, fn: (x: Node) => void): void {
  fn(n);
  if (n.t === "unary") walk(n.x, fn);
  else if (n.t === "binary") {
    walk(n.l, fn);
    walk(n.r, fn);
  } else if (n.t === "call") n.args.forEach((a) => walk(a, fn));
}

export function refs(n: Node): number[] {
  const set = new Set<number>();
  walk(n, (x) => {
    if (x.t === "ref") set.add(x.col);
    else if (x.t === "range") for (let c = x.from; c <= x.to; c++) set.add(c);
  });
  return [...set].sort((a, b) => a - b);
}

export function usesXRef(n: Node): boolean {
  let found = false;
  walk(n, (x) => {
    if (x.t === "call" && x.name === "XREF") found = true;
  });
  return found;
}

// ---------------------------------------------------------------------------
// In lại, dời tham chiếu (rewrite.go)
// ---------------------------------------------------------------------------

function prec(n: Node): number {
  if (n.t === "binary") {
    switch (n.op) {
      case "=":
      case "<>":
      case "<":
      case "<=":
      case ">":
      case ">=":
        return 1;
      case "&":
        return 2;
      case "+":
      case "-":
        return 3;
      case "*":
      case "/":
        return 4;
      case "^":
        return 5;
    }
  }
  if (n.t === "unary") return n.op === "%" ? 7 : 6;
  return 8;
}

function fmt(n: Node, parent: number): string {
  const p = prec(n);
  let s: string;
  switch (n.t) {
    case "num":
      s = n.v.toString();
      break;
    case "str":
      s = `"${n.v.replace(/"/g, '""')}"`;
      break;
    case "bool":
      s = n.v ? "TRUE" : "FALSE";
      break;
    case "ref":
      s = `${columnLetter(n.col)}#`;
      break;
    case "range":
      s = `${columnLetter(n.from)}#:${columnLetter(n.to)}#`;
      break;
    case "unary":
      s = n.op === "%" ? `${fmt(n.x, p)}%` : `${n.op}${fmt(n.x, p)}`;
      break;
    case "binary":
      s = `${fmt(n.l, p)}${n.op}${fmt(n.r, p + 1)}`;
      break;
    case "call":
      s = `${n.name}(${n.args.map((a) => fmt(a, 0)).join(",")})`;
      break;
  }
  return p < parent ? `(${s})` : s;
}

export function format(n: Node): string {
  return fmt(n, 0);
}

export class RefDeletedError extends Error {
  constructor() {
    super("formula: tham chiếu cột đã bị xóa");
  }
}

function shiftCol(c: number, at: number, delta: number): number {
  if (delta < 0) {
    if (c === at) throw new RefDeletedError();
    return c > at ? c - 1 : c;
  }
  return c >= at ? c + delta : c;
}

export function shift(n: Node, at: number, delta: number): Node {
  switch (n.t) {
    case "ref":
      return { t: "ref", col: shiftCol(n.col, at, delta) };
    case "range": {
      let { from, to } = n;
      if (delta < 0) {
        if (from === at && to === at) throw new RefDeletedError();
        if (from > at) from--;
        if (to >= at) to--;
        return { t: "range", from, to };
      }
      if (from >= at) from += delta;
      if (to >= at) to += delta;
      return { t: "range", from, to };
    }
    case "unary":
      return { t: "unary", op: n.op, x: shift(n.x, at, delta) };
    case "binary":
      return { t: "binary", op: n.op, l: shift(n.l, at, delta), r: shift(n.r, at, delta) };
    case "call":
      if (n.name === "XREF") return n;
      return { t: "call", name: n.name, args: n.args.map((a) => shift(a, at, delta)) };
    default:
      return n;
  }
}

export function shiftString(src: string, at: number, delta: number): string {
  return format(shift(parse(src), at, delta));
}

// ---------------------------------------------------------------------------
// Tính (eval.go)
// ---------------------------------------------------------------------------

export const ERR_DIV0 = "#DIV/0!";
export const ERR_VALUE = "#VALUE!";
export const ERR_NUM = "#NUM!";

export type Value =
  | { k: "blank" }
  | { k: "num"; n: Decimal }
  | { k: "text"; s: string }
  | { k: "bool"; b: boolean }
  | { k: "err"; e: string };

export const BLANK: Value = { k: "blank" };
export const num = (n: Decimal): Value => ({ k: "num", n });
export const text = (s: string): Value => ({ k: "text", s });
const bool = (b: boolean): Value => ({ k: "bool", b });
const err = (e: string): Value => ({ k: "err", e });

export function valueString(v: Value): string {
  switch (v.k) {
    case "num":
      return v.n.toString();
    case "text":
      return v.s;
    case "bool":
      return v.b ? "TRUE" : "FALSE";
    case "err":
      return v.e;
    default:
      return "";
  }
}

export interface Env {
  cell(col: number): Value;
  xref(form: string, letter: string): Value;
}

function toNumber(v: Value): [Decimal, string] {
  switch (v.k) {
    case "blank":
      return [Decimal.ZERO, ""];
    case "num":
      return [v.n, ""];
    case "bool":
      return [v.b ? Decimal.ONE : Decimal.ZERO, ""];
    case "text": {
      const d = Decimal.parse(v.s.trim());
      return d ? [d, ""] : [Decimal.ZERO, ERR_VALUE];
    }
    case "err":
      return [Decimal.ZERO, v.e];
  }
}

function toText(v: Value): string {
  return v.k === "blank" ? "" : valueString(v);
}

function toBool(v: Value): [boolean, string] {
  switch (v.k) {
    case "blank":
      return [false, ""];
    case "bool":
      return [v.b, ""];
    case "num":
      return [!v.n.isZero(), ""];
    case "text": {
      const u = v.s.trim().toUpperCase();
      if (u === "TRUE") return [true, ""];
      if (u === "FALSE") return [false, ""];
      return [false, ERR_VALUE];
    }
    case "err":
      return [false, v.e];
  }
}

function blankLike(o: Value): Value {
  if (o.k === "text") return text("");
  if (o.k === "bool") return bool(false);
  return num(Decimal.ZERO);
}

function compare(a0: Value, b0: Value): number {
  const a = a0.k === "blank" ? blankLike(b0) : a0;
  const b = b0.k === "blank" ? blankLike(a) : b0;
  const rank = (v: Value) => (v.k === "num" ? 0 : v.k === "text" ? 1 : 2);
  const ra = rank(a);
  const rb = rank(b);
  if (ra !== rb) return ra - rb;
  if (a.k === "num" && b.k === "num") return a.n.cmp(b.n);
  if (a.k === "text" && b.k === "text") {
    const x = a.s.toLowerCase();
    const y = b.s.toLowerCase();
    return x < y ? -1 : x > y ? 1 : 0;
  }
  const ab = a.k === "bool" && a.b;
  const bb = b.k === "bool" && b.b;
  if (ab === bb) return 0;
  return !ab ? -1 : 1;
}

function numbersOf(args: Node[], env: Env): [Decimal[], string] {
  const out: Decimal[] = [];
  for (const a of args) {
    if (a.t === "range" || a.t === "ref") {
      const from = a.t === "range" ? a.from : a.col;
      const to = a.t === "range" ? a.to : a.col;
      for (let c = from; c <= to; c++) {
        const v = env.cell(c);
        if (v.k === "err") return [[], v.e];
        if (v.k === "num") out.push(v.n);
      }
    } else {
      const v = evaluate(a, env);
      if (v.k === "err") return [[], v.e];
      if (v.k === "blank") continue;
      const [d, e] = toNumber(v);
      if (e) return [[], e];
      out.push(d);
    }
  }
  return [out, ""];
}

function numArg(n: Node, env: Env): [Decimal, string] {
  const v = evaluate(n, env);
  if (v.k === "err") return [Decimal.ZERO, v.e];
  return toNumber(v);
}

const orZero = (v: Value): Value => (v.k === "blank" ? num(Decimal.ZERO) : v);

function evalBinary(n: Extract<Node, { t: "binary" }>, env: Env): Value {
  const l = evaluate(n.l, env);
  if (l.k === "err") return l;
  const r = evaluate(n.r, env);
  if (r.k === "err") return r;
  switch (n.op) {
    case "&":
      return text(toText(l) + toText(r));
    case "=":
      return bool(compare(l, r) === 0);
    case "<>":
      return bool(compare(l, r) !== 0);
    case "<":
      return bool(compare(l, r) < 0);
    case "<=":
      return bool(compare(l, r) <= 0);
    case ">":
      return bool(compare(l, r) > 0);
    case ">=":
      return bool(compare(l, r) >= 0);
  }
  const [a, ea] = toNumber(l);
  if (ea) return err(ea);
  const [b, eb] = toNumber(r);
  if (eb) return err(eb);
  switch (n.op) {
    case "+":
      return num(a.add(b));
    case "-":
      return num(a.sub(b));
    case "*":
      return num(a.mul(b));
    case "/":
      return b.isZero() ? err(ERR_DIV0) : num(a.divRound(b, DIV_PRECISION));
    case "^": {
      if (!b.isInteger() || b.abs().gt(Decimal.fromInt(64))) return err(ERR_NUM);
      if (a.isZero() && b.sign() <= 0) return err(b.isZero() ? ERR_NUM : ERR_DIV0);
      const k = b.intPart();
      let res = Decimal.ONE;
      for (let i = 0n; i < (k < 0n ? -k : k); i++) res = res.mul(a);
      if (k < 0n) res = Decimal.ONE.divRound(res, DIV_PRECISION);
      return num(res);
    }
  }
  return err(ERR_VALUE);
}

function evalCall(c: Extract<Node, { t: "call" }>, env: Env): Value {
  switch (c.name) {
    case "SUM":
    case "MIN":
    case "MAX":
    case "AVERAGE":
    case "COUNT": {
      const [nums, e] = numbersOf(c.args, env);
      if (e) return c.name === "COUNT" ? num(Decimal.ZERO) : err(e);
      if (c.name === "COUNT") return num(Decimal.fromInt(nums.length));
      if (c.name === "SUM") return num(nums.reduce((s, d) => s.add(d), Decimal.ZERO));
      if (c.name === "AVERAGE") {
        if (!nums.length) return err(ERR_DIV0);
        return num(nums.reduce((s, d) => s.add(d), Decimal.ZERO).divRound(Decimal.fromInt(nums.length), DIV_PRECISION));
      }
      if (!nums.length) return num(Decimal.ZERO);
      let m = nums[0]!;
      for (const d of nums.slice(1)) if ((c.name === "MIN" && d.lt(m)) || (c.name === "MAX" && d.gt(m))) m = d;
      return num(m);
    }
    case "ROUND":
    case "ROUNDUP":
    case "ROUNDDOWN": {
      const [x, e1] = numArg(c.args[0]!, env);
      if (e1) return err(e1);
      const [p, e2] = numArg(c.args[1]!, env);
      if (e2) return err(e2);
      const places = Number(p.truncate(0).intPart());
      if (places > 30 || places < -30) return err(ERR_NUM);
      if (c.name === "ROUND") return num(x.round(places));
      if (c.name === "ROUNDUP") return num(x.sign() < 0 ? x.roundFloor(places) : x.roundCeil(places));
      return num(x.shift(places).truncate(0).shift(-places));
    }
    case "ABS": {
      const [x, e] = numArg(c.args[0]!, env);
      return e ? err(e) : num(x.abs());
    }
    case "IF": {
      const cond = evaluate(c.args[0]!, env);
      if (cond.k === "err") return cond;
      const [b, e] = toBool(cond);
      if (e) return err(e);
      if (b) return orZero(evaluate(c.args[1]!, env));
      if (c.args.length === 3) return orZero(evaluate(c.args[2]!, env));
      return bool(false);
    }
    case "IFERROR": {
      const v = evaluate(c.args[0]!, env);
      return v.k === "err" ? orZero(evaluate(c.args[1]!, env)) : orZero(v);
    }
    case "AND":
    case "OR": {
      let res = c.name === "AND";
      for (const a of c.args) {
        const v = evaluate(a, env);
        if (v.k === "err") return v;
        const [b, e] = toBool(v);
        if (e) return err(e);
        res = c.name === "AND" ? res && b : res || b;
      }
      return bool(res);
    }
    case "NOT": {
      const v = evaluate(c.args[0]!, env);
      if (v.k === "err") return v;
      const [b, e] = toBool(v);
      return e ? err(e) : bool(!b);
    }
    case "ISBLANK":
      return bool(evaluate(c.args[0]!, env).k === "blank");
    case "ISNUMBER":
      return bool(evaluate(c.args[0]!, env).k === "num");
    case "XREF": {
      const a = c.args[0];
      const b = c.args[1];
      return env.xref(a?.t === "str" ? a.v : "", b?.t === "str" ? b.v.toUpperCase() : "");
    }
  }
  return err(ERR_VALUE);
}

export function evaluate(n: Node, env: Env): Value {
  switch (n.t) {
    case "num":
      return num(n.v);
    case "str":
      return text(n.v);
    case "bool":
      return bool(n.v);
    case "ref":
      return env.cell(n.col);
    case "range":
      return n.from === n.to ? env.cell(n.from) : err(ERR_VALUE);
    case "unary": {
      const x = evaluate(n.x, env);
      if (x.k === "err") return x;
      const [d, e] = toNumber(x);
      if (e) return err(e);
      if (n.op === "-") return num(d.neg());
      if (n.op === "%") return num(d.divRound(Decimal.fromInt(100), DIV_PRECISION));
      return num(d);
    }
    case "binary":
      return evalBinary(n, env);
    case "call":
      return evalCall(n, env);
  }
}
