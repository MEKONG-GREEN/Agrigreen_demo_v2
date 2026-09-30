// Tiện ích dùng chung của backend giả lập G4: tham số có phiên bản (người đặt ≠ người duyệt, R-56),
// ngày giờ Việt Nam, lý do 10..500 ký tự. Chỉ chạy trong trình duyệt/kiểm thử.
import { newId } from "./db";
import { created, forbidden, hasRole, json, nowIso, problem, route, selfApproval, str, validation, type Ctx, type Route } from "./http";

/** Ngày hôm nay theo giờ Việt Nam (YYYY-MM-DD). */
export function todayVn(offsetDays = 0): string {
  const t = new Date(Date.now() + 7 * 3_600_000 + offsetDays * 86_400_000);
  return t.toISOString().slice(0, 10);
}

/** Kỳ tháng (YYYY-MM) theo giờ Việt Nam, lùi `back` tháng. */
export function monthVn(back = 0): string {
  const t = new Date(Date.now() + 7 * 3_600_000);
  const d = new Date(Date.UTC(t.getUTCFullYear(), t.getUTCMonth() - back, 1));
  return d.toISOString().slice(0, 7);
}

/** Lý do 10..500 ký tự; trả Response lỗi hoặc chuỗi đã cắt khoảng trắng. */
export function reasonOf(body: Record<string, unknown>, field = "reason", required = true): string | Response {
  const r = str(body, field);
  if (!r && !required) return "";
  if (r.length < 10 || r.length > 500) return validation([{ field, reason: "10–500 ký tự" }]);
  return r;
}

export interface ParamVersionRec {
  key: string;
  version_id: string;
  version_seq: number;
  value: string;
  effective_from: string;
  provisional: boolean;
  status: "pending" | "approved" | "rejected";
  proposed_by: string;
  proposal_reason: string;
  decided_by: string | null;
  decided_at: string | null;
  decision_reason: string | null;
}

export interface ParamStoreOpts {
  prefix: string; // "/v1/logistics" | "/v1/orders"
  module: "logistics" | "orders";
  defaults: Record<string, { value: string; note: string; check: (v: string) => string | null }>;
  proposerRoles: string[];
  approverRoles: string[];
  readRoles: string[];
  /** logistics: {key, default, effective{value,param_version,provisional}, versions}; orders: {param_key, effective_value, …}. */
  shape: "logistics" | "orders";
}

/** Bộ tham số có phiên bản của một module + route GET / propose / decide. */
export function paramStore(opts: ParamStoreOpts) {
  const versions: Record<string, ParamVersionRec[]> = {};
  for (const k of Object.keys(opts.defaults)) versions[k] = [];

  function effective(key: string, on = todayVn()): { value: string; version: string; provisional: boolean } {
    const list = (versions[key] ?? []).filter((v) => v.status === "approved" && v.effective_from <= on).sort((a, b) => b.version_seq - a.version_seq);
    const v = list[0];
    if (v) return { value: v.value, version: `${key}:v${v.version_seq}`, provisional: v.provisional };
    return { value: opts.defaults[key]!.value, version: `${key}:default`, provisional: true };
  }

  const notFound = () => problem(404, `${opts.module}.param_not_found`, "Param not found");

  function view(key: string) {
    const e = effective(key);
    const list = [...(versions[key] ?? [])].sort((a, b) => b.version_seq - a.version_seq);
    if (opts.shape === "logistics") return { key, default: opts.defaults[key]!.value, effective: { value: e.value, param_version: e.version, provisional: e.provisional }, versions: list };
    return {
      param_key: key,
      effective_value: e.value,
      param_version: e.version,
      provisional: e.provisional,
      default_value: opts.defaults[key]!.value,
      note: opts.defaults[key]!.note,
      versions: list.map(({ key: k, ...rest }) => ({ param_key: k, ...rest })),
    };
  }

  function out(v: ParamVersionRec) {
    if (opts.shape === "logistics") return { param: v };
    const { key, ...rest } = v;
    return { param: { param_key: key, ...rest } };
  }

  const routes: Route[] = [
    route("GET", `${opts.prefix}/params/:key`, ({ params, persona }) => {
      if (!hasRole(persona, ...opts.readRoles)) return forbidden();
      const key = params[0]!;
      return opts.defaults[key] ? json(view(key)) : notFound();
    }),
    route("POST", `${opts.prefix}/params/:key/versions`, ({ params, body, persona }) => {
      if (!hasRole(persona, ...opts.proposerRoles)) return forbidden();
      const key = params[0]!;
      const def = opts.defaults[key];
      if (!def) return notFound();
      const value = str(body, "value");
      const bad = def.check(value);
      if (bad) return validation([{ field: "value", reason: bad }]);
      const reason = reasonOf(body);
      if (reason instanceof Response) return reason;
      const eff = str(body, "effective_from") || todayVn();
      if (!/^\d{4}-\d{2}-\d{2}$/.test(eff)) return validation([{ field: "effective_from", reason: "YYYY-MM-DD" }]);
      const list = versions[key]!;
      const v: ParamVersionRec = {
        key,
        version_id: newId(0x40),
        version_seq: list.length + 1,
        value,
        effective_from: eff,
        provisional: body.provisional === undefined ? opts.shape === "orders" : body.provisional === true,
        status: "pending",
        proposed_by: persona.actor,
        proposal_reason: reason,
        decided_by: null,
        decided_at: null,
        decision_reason: null,
      };
      list.push(v);
      return created(out(v));
    }),
    route("POST", `${opts.prefix}/params/:key/versions/:version/decision`, (ctx: Ctx) => {
      const { params, body, persona } = ctx;
      if (!hasRole(persona, ...opts.approverRoles)) return forbidden();
      const key = params[0]!;
      const v = (versions[key] ?? []).find((x) => x.version_id === params[1]);
      if (!v) return notFound();
      if (v.status !== "pending") return problem(409, opts.module === "logistics" ? "logistics.param_decided" : "orders.param_already_decided", "Already decided");
      if (v.proposed_by === persona.actor) return selfApproval();
      const d = str(body, "decision");
      if (d !== "approved" && d !== "rejected") return validation([{ field: "decision", reason: "approved | rejected" }]);
      if (d === "rejected") {
        const r = reasonOf(body);
        if (r instanceof Response) return r;
        v.decision_reason = r;
      }
      v.status = d;
      v.decided_by = persona.actor;
      v.decided_at = nowIso();
      return created(out(v));
    }),
  ];
  return { versions, effective, routes };
}

/** Số thập phân không âm (chuỗi) có tối đa `frac` số lẻ và ≤ max (so sánh chuỗi an toàn: số nguyên nhỏ). */
export function decimalCheck(frac: number, maxInt: number, integer = false): (v: string) => string | null {
  return (v) => {
    const re = integer ? /^\d+$/ : new RegExp(`^\\d+(\\.\\d{1,${frac}})?$`);
    if (!re.test(v)) return integer ? "Số nguyên không âm" : `Số không âm, tối đa ${frac} chữ số thập phân`;
    const intPart = v.split(".")[0] ?? "0";
    if (intPart.length > String(maxInt).length || BigInt(intPart) > BigInt(maxInt)) return `Tối đa ${maxInt}`;
    return null;
  };
}
