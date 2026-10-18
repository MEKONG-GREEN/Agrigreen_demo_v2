// ============================================================================
// BACKEND GIẢ LẬP — Hồ sơ nhân sự (W11.22, dom/hrm, BRD HCTH v3.0 FN-07, FN-08, FN-20). Đường dẫn, tham số, hình dạng
// phản hồi theo ĐÚNG contracts/openapi/application.yaml (/v1/hrm/access|units|staff*|permissions*). Quy tắc mô phỏng
// (rút gọn từ internal/platform/hrm/app/records.go, domain/staff.go, domain/perm.go):
//   - Nhóm vai từ ngữ cảnh phiên: Admin; Cấp Tỉnh ở phòng HCTH; lãnh đạo (manager) theo đơn vị nội bộ; còn lại không
//     xem hồ sơ (mặc định FN-07 BR-04). Ma trận ghi được (Admin), người dùng ưu tiên, Admin cố định.
//   - Quá trình có hiệu lực, phép chiếu as-of (trạng thái FN-08, đơn vị, chức vụ, ngạch – bậc; hệ số tra bảng của kn-hrm).
//   - Mỗi lần xem chi tiết/thẻ TK/xuất ghi nhật ký. Dữ liệu cá nhân ở đây là TỔNG HỢP (tên giả), trong bộ nhớ tab.
// ============================================================================
import type { components } from "../generated/application";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext } from "./kn";
import { hrmActiveItem, hrmScaleAt } from "./kn-hrm";

type S = components["schemas"];
type Matrix = S["HrmPermMatrix"];
type Scope = S["HrmScope"];
type EventIn = S["HrmEventInput"];
type Kind = EventIn["kind"];

interface Ev {
  id: string;
  kind: Kind;
  valid_from: string; // YYYY-MM-DD đầu kỳ
  precision: "day" | "month" | "year";
  valid_to: string | null;
  payload: Record<string, string>;
  void: boolean;
  supersedes: string | null;
  reason: string;
  version: number;
  recorded_at: string;
  actor_ref: string;
}
interface Staff {
  id: string;
  code: string;
  type: S["HrmStaffType"];
  account_ref: string | null;
  profile: S["HrmStaffProfile"];
  sensitive: Record<string, string>;
  history: S["HrmStaffHistory"][];
  events: Ev[];
}
interface PermRow {
  grantee_kind: "role" | "user";
  grantee: string;
  perms: Matrix;
  note: string;
  version: number;
  recorded_at: string;
  actor_ref: string;
}
interface Store {
  staff: Staff[];
  access: (S["HrmAccessEntry"] & { staff_ids: string[] })[];
  perms: PermRow[];
  seq: number;
}

const HCTH = "hanh_chinh_tong_hop";
const ACTOR = "01900000-0000-7000-8000-00000000a001";
const PERMS = [
  "profile.view",
  "profile.edit",
  "sensitive.view",
  "export",
  "audit.view",
  "docs.view", // W11.24 kho tài liệu
  "docs.manage",
] as const;
const GROUPS = ["admin", "hcth", "manager", "tinh", "xa", "tw"];
const UNITS = [
  { code: "bgd", name: "Ban Giám đốc", kind: "bgd", active: true },
  {
    code: "hcth",
    name: "Phòng Hành chính - Tổng hợp",
    kind: "phong",
    active: true,
  },
  { code: "trong_trot", name: "Phòng Trồng trọt", kind: "phong", active: true },
  {
    code: "chan_nuoi",
    name: "Phòng Chăn nuôi - Thủy sản",
    kind: "phong",
    active: true,
  },
  {
    code: "tram_chau_doc",
    name: "Trạm Khuyến nông Châu Đốc",
    kind: "tram",
    active: true,
  },
];
const now = () => new Date().toISOString();
const vnToday = () =>
  new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
const uuid = () => crypto.randomUUID();

let STORE: Store = seed();

/** Đặt lại dữ liệu giả lập (kiểm thử). */
export function resetHrmStaff(): void {
  STORE = seed();
}

function parseDate(s: unknown): { d: string; p: Ev["precision"] } | null {
  if (typeof s !== "string") return null;
  if (/^\d{4}-\d{2}-\d{2}$/.test(s) && !Number.isNaN(Date.parse(s)))
    return { d: s, p: "day" };
  if (/^\d{4}-(0[1-9]|1[0-2])$/.test(s)) return { d: `${s}-01`, p: "month" };
  if (/^\d{4}$/.test(s)) return { d: `${s}-01-01`, p: "year" };
  return null;
}
const fmt = (d: string, p: Ev["precision"]) =>
  p === "year" ? d.slice(0, 4) : p === "month" ? d.slice(0, 7) : d;

function seed(): Store {
  const st: Store = { staff: [], access: [], perms: [], seq: 0 };
  const ev = (
    kind: Kind,
    from: string,
    payload: Record<string, string>,
  ): Ev => {
    const pd = parseDate(from)!;
    return {
      id: uuid(),
      kind,
      valid_from: pd.d,
      precision: pd.p,
      valid_to: null,
      payload,
      void: false,
      supersedes: null,
      reason: "",
      version: ++st.seq,
      recorded_at: now(),
      actor_ref: ACTOR,
    };
  };
  const add = (
    code: string,
    type: Staff["type"],
    name: string,
    phone: string,
    gender: string,
    birth: string,
    evs: Ev[],
    sensitive: Record<string, string> = {},
  ) =>
    st.staff.push({
      id: uuid(),
      code,
      type,
      account_ref: null,
      profile: {
        full_name: name,
        phone,
        address: "TP. Long Xuyên, An Giang",
        birth_date: birth,
        birth_precision: "day",
        gender,
        email: `${code.toLowerCase()}@ttkn.example`,
        note: "",
      },
      sensitive,
      history: [
        {
          version: 1,
          change: "created",
          changed_fields: [],
          recorded_at: now(),
          actor_ref: ACTOR,
        },
      ],
      events: evs,
    });
  add(
    "VC-00001",
    "vien_chuc",
    "Nguyễn Thị Mẫu",
    "0901000001",
    "nu",
    "1988-05-01",
    [
      ev("work_start", "2012-03-01", {}),
      ev("unit", "2012-03-01", { unit: "trong_trot" }),
      ev("position", "2012-03-01", { position: "vien_chuc" }),
      ev("grade", "2018-01-01", {
        grade: "01.003",
        step: "5",
        step_since: "2018-01-01",
      }),
      ev("step", "2024-01-01", { step: "6", step_since: "2024-01-01" }),
      ev("degree", "2010", {
        level: "dai_hoc",
        major: "trong_trot",
        form: "chinh_quy",
        institution: "Trường Đại học Cần Thơ",
      }),
      ev("award", "2024", {
        form: "bang_khen_ubnd_tinh",
        title: "Bằng khen UBND tỉnh 2024",
      }),
    ],
    { ethnicity: "kinh", religion: "khong", hometown: "Xã Mỹ Hòa Hưng" },
  );
  add(
    "VC-00002",
    "vien_chuc",
    "Trần Văn Thử",
    "0901000002",
    "nam",
    "1975-09-12",
    [
      ev("work_start", "2000-06-01", {}),
      ev("unit", "2000-06-01", { unit: "hcth" }),
      ev("position", "2015-01-01", {
        position: "truong_phong",
        appointed_on: "2015-01-01",
        decision_no: "12/QĐ-TTKN",
      }),
      ev("grade", "2016-01-01", {
        grade: "01.002",
        step: "4",
        step_since: "2016-01-01",
      }),
    ],
  );
  add("XA-00001", "can_bo_xa", "Lê Thị Giả Lập", "0901000003", "nu", "1995", [
    ev("work_start", "2021-01-01", {}),
    ev("unit", "2021-01-01", { commune: "30292", mgmt_unit: "trong_trot" }),
    ev("position", "2021-01-01", { position: "vien_chuc" }),
  ]);
  return st;
}

function effective(evs: Ev[]): Ev[] {
  const dead = new Set(
    evs.filter((e) => e.supersedes).map((e) => e.supersedes!),
  );
  return evs
    .filter((e) => !e.void && !dead.has(e.id))
    .sort(
      (a, b) =>
        a.valid_from.localeCompare(b.valid_from) || a.version - b.version,
    );
}

function project(evs: Ev[], at: string): S["HrmSnapshot"] {
  const s: S["HrmSnapshot"] = {
    status: "none",
    started_on: null,
    ended_on: null,
    leave_reason: "",
    unit: "",
    commune: "",
    mgmt_unit: "",
    position: "",
    title: "",
    allowance: "",
    appointed_on: "",
    grade: "",
    step: 0,
    steps: 0,
    coefficient: "",
    step_since: "",
    reserved_coeff: "",
    disciplined: false,
    top_degree: "",
    secondments: [],
  };
  let hasGrade = false;
  for (const e of effective(evs)) {
    if (e.valid_from > at) continue;
    const p = e.payload;
    switch (e.kind) {
      case "work_start":
        Object.assign(s, {
          status: "active",
          started_on: e.valid_from,
          ended_on: null,
          leave_reason: "",
        });
        break;
      case "work_end":
        if (s.started_on && e.valid_from >= s.started_on)
          Object.assign(s, {
            status: "left",
            ended_on: e.valid_from,
            leave_reason: p.reason ?? "",
          });
        break;
      case "unit":
        Object.assign(s, {
          unit: p.unit ?? "",
          commune: p.commune ?? "",
          mgmt_unit: p.mgmt_unit ?? "",
        });
        break;
      case "position":
        Object.assign(s, {
          position: p.position ?? "",
          title: p.title ?? "",
          allowance: p.allowance ?? "",
          appointed_on: p.appointed_on ?? "",
        });
        break;
      case "grade":
        hasGrade = true;
        Object.assign(s, {
          grade: p.grade ?? "",
          step: Number(p.step ?? 0),
          step_since: p.step_since ?? "",
          reserved_coeff: p.reserved_coeff ?? "",
        });
        break;
      case "step":
        if (hasGrade)
          Object.assign(s, {
            step: Number(p.step ?? 0),
            step_since: p.step_since ?? "",
            reserved_coeff: p.reserved_coeff ?? "",
          });
        break;
      case "secondment":
        if (!e.valid_to || e.valid_to >= at)
          s.secondments.push({
            id: e.id,
            to_org: p.to_org ?? "",
            valid_from: fmt(e.valid_from, e.precision),
            valid_to: e.valid_to,
          });
        break;
      case "discipline":
        if (!e.valid_to || e.valid_to >= at) s.disciplined = true;
        break;
      case "degree":
        s.top_degree = p.level ?? s.top_degree;
        break;
      default:
        break;
    }
  }
  const sc = s.grade ? hrmScaleAt(s.grade, at) : null;
  if (sc) {
    s.steps = sc.coefficients.length;
    s.coefficient =
      s.step >= 1 && s.step <= sc.coefficients.length
        ? sc.coefficients[s.step - 1]!
        : "";
  }
  return s;
}

function label(name: string): string {
  const w = name.split(/\s+/).filter(Boolean);
  if (!w.length) return "";
  return [w[0], ...w.slice(1).map((x) => `${x[0]!.toUpperCase()}.`)].join(" ");
}
const masked = (phone: string) =>
  phone.length === 10 ? `${phone.slice(0, 4)}•••${phone.slice(-3)}` : "";

// --- Quyền (FN-20). ---

function defaults(group: string): Matrix {
  const m = Object.fromEntries(PERMS.map((p) => [p, "none"])) as Matrix;
  if (group === "admin" || group === "hcth")
    PERMS.forEach((p) => (m[p] = "all"));
  if (group === "manager")
    Object.assign(m, {
      "profile.view": "unit",
      export: "unit",
      "docs.view": "unit",
    });
  return m;
}
const lastPerm = (kind: string, grantee: string) =>
  [...STORE.perms]
    .reverse()
    .find((r) => r.grantee_kind === kind && r.grantee === grantee);

export interface Caller {
  group: string;
  unit: string;
  ref: string;
  readOnly: boolean;
  m: Matrix;
}
function caller(ctx: Ctx): Caller | Response {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  if (!kc)
    return problem(
      403,
      "forbidden",
      "hồ sơ nhân sự: cần ngữ cảnh phiên Khuyến nông",
    );
  const group =
    kc.role === "admin"
      ? "admin"
      : kc.role === "kn_tinh" && kc.department_code === HCTH && !kc.read_only
        ? "hcth"
        : kc.role === "kn_tinh" && kc.manager
          ? "manager"
          : kc.role === "kn_tinh"
            ? "tinh"
            : kc.role === "kn_xa"
              ? "xa"
              : "tw";
  const ref = ctx.persona.me.actor_ref || ACTOR;
  const m = { ...defaults(group) };
  if (group !== "admin") {
    Object.assign(m, lastPerm("role", group)?.perms ?? {});
    Object.assign(m, lastPerm("user", ref)?.perms ?? {});
  }
  return {
    group,
    unit: kc.internal_unit_code ?? "",
    ref,
    readOnly: kc.role === "kn_tw" || !!kc.read_only,
    m,
  };
}
/** W11.24: người gọi + ma trận FN-20 cho kho tài liệu giả lập (kn-hrm-docs). */
export function hrmCaller(ctx: Ctx): Caller | Response {
  return caller(ctx);
}
/** W11.24: mã hồ sơ, tài khoản gắn, đơn vị hiện tại (kn-hrm-docs). */
export function hrmStaffBrief(): {
  id: string;
  code: string;
  label: string;
  account_ref: string | null;
  unit: string;
}[] {
  return STORE.staff.map((s) => ({
    id: s.id,
    code: s.code,
    label: label(s.profile.full_name),
    account_ref: s.account_ref,
    unit: project(s.events, vnToday()).unit,
  }));
}
function on(
  c: Caller,
  perm: keyof Matrix,
  st: Staff,
  snap: S["HrmSnapshot"],
): boolean {
  const s = c.m[perm] ?? "none";
  const self = !!st.account_ref && st.account_ref === c.ref;
  const unit = !!c.unit && (snap.unit === c.unit || snap.mgmt_unit === c.unit);
  return (
    s === "all" || (s === "unit" && (unit || self)) || (s === "self" && self)
  );
}
function need(c: Caller, perm: keyof Matrix, write = false): Response | null {
  if ((c.m[perm] ?? "none") === "none")
    return problem(
      403,
      "forbidden",
      `hồ sơ nhân sự: không có quyền ${perm} (FN-20)`,
    );
  if (write && c.readOnly)
    return problem(403, "forbidden", "ngữ cảnh chỉ xem không ghi được hồ sơ");
  return null;
}
function audit(
  c: Caller,
  action: "view" | "view_sensitive" | "export",
  ids: string[],
  scope: Scope,
  query: Record<string, string> = {},
) {
  STORE.access.unshift({
    id: uuid(),
    action,
    item_count: ids.length,
    scope: scope === "none" ? "self" : (scope as "self" | "unit" | "all"),
    query,
    actor_ref: c.ref,
    recorded_at: now(),
    staff_ids: ids,
  });
}

function findStaff(id: string | undefined): Staff | undefined {
  return STORE.staff.find((s) => s.id === id);
}

function filtered(
  c: Caller,
  perm: keyof Matrix,
  url: URL,
): { day: string; rows: { st: Staff; snap: S["HrmSnapshot"] }[] } | Response {
  const at = url.searchParams.get("at");
  if (at && !/^\d{4}-\d{2}-\d{2}$/.test(at))
    return problem(422, "validation_failed", "at phải là ngày YYYY-MM-DD");
  const day = at ?? vnToday();
  const q = (url.searchParams.get("q") ?? "").trim();
  const f = (k: string) => url.searchParams.get(k) ?? "";
  const rows = STORE.staff
    .map((st) => ({ st, snap: project(st.events, day) }))
    .filter(({ st, snap }) => on(c, perm, st, snap))
    .filter(({ st }) => !f("type") || st.type === f("type"))
    .filter(
      ({ snap }) =>
        !f("unit") || snap.unit === f("unit") || snap.mgmt_unit === f("unit"),
    )
    .filter(({ snap }) => !f("status") || snap.status === f("status"))
    .filter(({ snap }) => !f("position") || snap.position === f("position"))
    .filter(({ snap }) => !f("grade") || snap.grade === f("grade"))
    .filter(
      ({ st }) =>
        !q ||
        (/^\+?[0-9][0-9 .-]{7,14}$/.test(q)
          ? st.profile.phone.replace(/\D/g, "") === q.replace(/\D/g, "")
          : st.code.startsWith(q.toUpperCase())),
    )
    .sort((a, b) => a.st.code.localeCompare(b.st.code));
  return { day, rows };
}

const item = (st: Staff, snap: S["HrmSnapshot"]): S["HrmStaffItem"] => ({
  id: st.id,
  code: st.code,
  type: st.type,
  label: label(st.profile.full_name),
  masked_phone: masked(st.profile.phone),
  current: snap,
});

const FIELDS: Record<Kind, string[]> = {
  work_start: ["decision_no"],
  work_end: ["reason", "decision_no"],
  unit: ["unit", "commune", "mgmt_unit"],
  position: ["position", "title", "allowance", "appointed_on", "decision_no"],
  secondment: ["to_org", "decision_no"],
  grade: ["grade", "step", "step_since", "reserved_coeff", "decision_no"],
  step: ["step", "step_since", "reserved_coeff", "decision_no"],
  degree: ["level", "major", "form", "institution"],
  training: ["type", "code", "level", "cert"],
  award: ["form", "title", "level", "decision_no"],
  discipline: ["form", "decision_no"],
};
const REQUIRED: Partial<Record<Kind, string[]>> = {
  work_end: ["reason"],
  position: ["position"],
  secondment: ["to_org"],
  grade: ["grade", "step"],
  step: ["step"],
  degree: ["level"],
  training: ["type"],
  award: ["form"],
  discipline: ["form"],
};

/** Kiểm + chuẩn hóa một sự kiện; trả lỗi Response hoặc sự kiện (chưa gán id/version). */
function normEvent(
  st: Pick<Staff, "type" | "events">,
  b: EventIn,
): Ev | Response {
  const pd = parseDate(b.valid_from);
  if (!pd)
    return validation([
      { field: "valid_from", reason: "YYYY-MM-DD | YYYY-MM | YYYY" },
    ]);
  const payload: Record<string, string> = {};
  for (const [k, v] of Object.entries(b.payload ?? {})) {
    if (!FIELDS[b.kind]?.includes(k))
      return validation([{ field: k, reason: `không thuộc ${b.kind}` }]);
    if (v) payload[k] = String(v).trim();
  }
  for (const k of REQUIRED[b.kind] ?? [])
    if (!payload[k]) return validation([{ field: k, reason: "bắt buộc" }]);
  if (b.kind === "unit") {
    if (
      st.type === "can_bo_xa"
        ? !/^\d{5}$/.test(payload.commune ?? "") || !payload.mgmt_unit
        : !payload.unit
    )
      return validation([
        {
          field: "unit",
          reason:
            st.type === "can_bo_xa"
              ? "chọn xã và đơn vị quản lý (BR-07)"
              : "thiếu đơn vị công tác",
        },
      ]);
    for (const k of ["unit", "mgmt_unit"])
      if (payload[k] && !UNITS.some((u) => u.code === payload[k] && u.active))
        return validation([{ field: k, reason: "đơn vị nội bộ không có" }]);
  }
  const refs: [string, string | undefined][] = [
    ["position", b.kind === "position" ? payload.position : undefined],
    ["grade", b.kind === "grade" ? payload.grade : undefined],
    ["leave_reason", b.kind === "work_end" ? payload.reason : undefined],
    ["award_form", b.kind === "award" ? payload.form : undefined],
    ["discipline_form", b.kind === "discipline" ? payload.form : undefined],
    ["education_level", b.kind === "degree" ? payload.level : undefined],
  ];
  for (const [kind, code] of refs)
    if (code && !hrmActiveItem(kind, code))
      return problem(
        422,
        "hrm.item_reference",
        "Tham chiếu tới mục danh mục không có hoặc đã ngừng sử dụng",
      );
  if (b.kind === "position") {
    const pos = hrmActiveItem("position", payload.position);
    if (pos?.attrs?.leader && !payload.appointed_on)
      return validation([
        {
          field: "appointed_on",
          reason: "chức vụ lãnh đạo phải có ngày quyết định bổ nhiệm (HS-20)",
        },
      ]);
  }
  if (b.kind === "grade" || b.kind === "step") {
    if (!payload.step_since) payload.step_since = fmt(pd.d, pd.p);
    const grade =
      b.kind === "grade" ? payload.grade! : project(st.events, pd.d).grade;
    const sc = grade ? hrmScaleAt(grade, pd.d) : null;
    if (
      !sc ||
      Number(payload.step) > sc.coefficients.length ||
      Number(payload.step) < 1
    )
      return problem(
        422,
        "hrm.step_out_of_range",
        "Bậc vượt số bậc của ngạch hoặc ngạch chưa có bảng hệ số",
      );
  }
  const to = b.valid_to ? parseDate(b.valid_to) : null;
  return {
    id: "",
    kind: b.kind,
    valid_from: pd.d,
    precision: pd.p,
    valid_to: to?.d ?? null,
    payload,
    void: false,
    supersedes: null,
    reason: "",
    version: 0,
    recorded_at: now(),
    actor_ref: ACTOR,
  };
}

/** FN-08: ngày nghỉ không trước ngày vào làm; không hai lần nghỉ liên tiếp. */
function workOk(evs: Ev[]): boolean {
  let start: string | null = null;
  let ended = true;
  for (const e of effective(evs)) {
    if (e.kind === "work_start") {
      if (!ended) return false;
      start = e.valid_from;
      ended = false;
    } else if (e.kind === "work_end") {
      if (!start || ended || e.valid_from < start) return false;
      ended = true;
    }
  }
  return true;
}

function detail(c: Caller, st: Staff, day: string): S["HrmStaffDetail"] {
  const today = project(st.events, vnToday());
  const can: Record<string, boolean> = {};
  for (const p of PERMS)
    can[p] =
      on(c, p, st, today) &&
      (!c.readOnly ||
        p === "profile.view" ||
        p === "export" ||
        p === "audit.view");
  can["sensitive.view"] = !!can["sensitive.view"] && st.type !== "can_bo_xa";
  const live = new Set(effective(st.events).map((e) => e.id));
  const b = parseDate(st.profile.birth_date);
  const age = b
    ? Number(day.slice(0, 4)) -
      Number(b.d.slice(0, 4)) -
      (day.slice(5) < b.d.slice(5) ? 1 : 0)
    : null;
  return {
    id: st.id,
    code: st.code,
    type: st.type,
    account_ref: st.account_ref,
    day,
    erased: false,
    created_at: st.history[0]!.recorded_at,
    age,
    profile: st.profile,
    current: project(st.events, day),
    events: st.events.map((e) => ({
      ...e,
      valid_from: fmt(e.valid_from, e.precision),
      effective: live.has(e.id),
    })),
    history: st.history,
    can,
  };
}

function writeStaff(
  st: Staff,
  change: S["HrmStaffHistory"]["change"],
  fields: string[],
): S["HrmStaffWrite"] {
  st.history.push({
    version: st.history.length + 1,
    change,
    changed_fields: fields,
    recorded_at: now(),
    actor_ref: ACTOR,
  });
  return { id: st.id, code: st.code, version: st.history.length, warnings: [] };
}

function checkProfile(
  p: Partial<S["HrmStaffProfile"]>,
  create: boolean,
): Response | null {
  const errs: { field: string; reason: string }[] = [];
  if (
    (create || p.full_name !== undefined) &&
    (!p.full_name ||
      p.full_name.trim().length < 2 ||
      /[()[\]0-9]/.test(p.full_name))
  )
    errs.push({
      field: "full_name",
      reason: "bắt buộc, 2–100 ký tự, không chứa ghi chú/số (BR-05)",
    });
  if (
    (create || p.phone !== undefined) &&
    !/^0\d{9}$/.test((p.phone ?? "").replace(/[\s.-]/g, ""))
  )
    errs.push({ field: "phone", reason: "SĐT 10 số (HS-12)" });
  if (
    (create || p.gender !== undefined) &&
    p.gender !== "nam" &&
    p.gender !== "nu"
  )
    errs.push({ field: "gender", reason: "nam | nu" });
  if (create || p.birth_date !== undefined) {
    const b = parseDate(p.birth_date);
    if (!b || b.d > vnToday())
      errs.push({
        field: "birth_date",
        reason: "bắt buộc, không sau ngày hiện tại (HS-04)",
      });
  }
  if (p.email && !/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(p.email))
    errs.push({ field: "email", reason: "định dạng email (HS-35)" });
  return errs.length ? validation(errs) : null;
}

const id0 = (ctx: Ctx) => decodeURIComponent(ctx.params[0] ?? "");

export const hrmStaffRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/access$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      return json({ enabled: true, group: c.group, unit: c.unit, perms: c.m });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/units$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      return need(c, "profile.view") ?? json({ items: UNITS });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/staff$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const n = need(c, "profile.view");
      if (n) return n;
      const r = filtered(c, "profile.view", ctx.url);
      if (r instanceof Response) return r;
      const offset = Math.max(
        Number(ctx.url.searchParams.get("offset") ?? 0) || 0,
        0,
      );
      const limit = Math.min(
        Math.max(Number(ctx.url.searchParams.get("limit") ?? 50) || 50, 1),
        100,
      );
      return json({
        day: r.day,
        total: r.rows.length,
        scope: c.m["profile.view"],
        items: r.rows
          .slice(offset, offset + limit)
          .map((x) => item(x.st, x.snap)),
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/staff$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const n = need(c, "profile.edit", true);
      if (n) return n;
      const b = (ctx.body ?? {}) as S["HrmStaffCreate"];
      if (!["vien_chuc", "hop_dong", "can_bo_xa"].includes(b.type))
        return validation([
          { field: "type", reason: "vien_chuc | hop_dong | can_bo_xa" },
        ]);
      const pe = checkProfile(b.profile ?? {}, true);
      if (pe) return pe;
      const phone = (b.profile.phone ?? "").replace(/[\s.-]/g, "");
      if (STORE.staff.some((s) => s.profile.phone === phone))
        return problem(
          409,
          "hrm.duplicate_person",
          "Số điện thoại đã thuộc một hồ sơ khác",
        );
      const kinds = new Set((b.events ?? []).map((e) => e.kind));
      if (!kinds.has("unit") || !kinds.has("position"))
        return validation([
          {
            field: "events",
            reason: "bắt buộc đơn vị công tác (HS-16) và chức vụ (HS-17)",
          },
        ]);
      if (b.sensitive && Object.keys(b.sensitive).length) {
        const ns = need(c, "sensitive.view", true);
        if (ns) return ns;
        if (b.type === "can_bo_xa")
          return problem(
            422,
            "hrm.not_applicable",
            "thẻ Thống kê BC Sở chỉ áp dụng viên chức, hợp đồng lao động",
          );
      }
      const draft: Staff = {
        id: uuid(),
        code: "",
        type: b.type,
        account_ref: b.account_ref ?? null,
        profile: {
          full_name: b.profile.full_name!.trim(),
          phone,
          address: b.profile.address ?? "",
          birth_date: fmt(
            parseDate(b.profile.birth_date)!.d,
            parseDate(b.profile.birth_date)!.p,
          ),
          birth_precision: parseDate(b.profile.birth_date)!.p,
          gender: b.profile.gender!,
          email: b.profile.email ?? "",
          note: b.profile.note ?? "",
        },
        sensitive: { ...(b.sensitive ?? {}) } as Record<string, string>,
        history: [],
        events: [],
      };
      for (const e of b.events) {
        const ev = normEvent(draft, e);
        if (ev instanceof Response) return ev;
        draft.events.push({ ...ev, id: uuid(), version: ++STORE.seq });
      }
      if (!workOk(draft.events))
        return validation([
          {
            field: "events",
            reason: "ngày vào làm/nghỉ việc không hợp lệ (FN-08)",
          },
        ]);
      const prefix =
        b.type === "hop_dong" ? "HD" : b.type === "can_bo_xa" ? "XA" : "VC";
      const n2 =
        STORE.staff.filter((s) => s.code.startsWith(prefix)).length + 1;
      draft.code = `${prefix}-${String(n2).padStart(5, "0")}`;
      STORE.staff.push(draft);
      return json(writeStaff(draft, "created", []), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/staff\/export$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const n = need(c, "export");
      if (n) return n;
      const r = filtered(c, "export", ctx.url);
      if (r instanceof Response) return r;
      audit(
        c,
        "export",
        r.rows.map((x) => x.st.id),
        c.m.export ?? "none",
        { at: r.day },
      );
      const lines = [
        [
          "Mã hồ sơ",
          "Họ tên (che bớt)",
          "Loại nhân sự",
          "Trạng thái",
          "Đơn vị",
          "Chức vụ",
          "Ngạch/CDNN",
          "Bậc",
          "Hệ số bậc",
        ].join(","),
      ];
      for (const { st, snap } of r.rows)
        lines.push(
          [
            st.code,
            label(st.profile.full_name),
            st.type,
            snap.status,
            snap.unit || snap.mgmt_unit,
            snap.position,
            snap.grade,
            snap.step ? `${snap.step}/${snap.steps}` : "",
            snap.coefficient,
          ].join(","),
        );
      return new Response(String.fromCharCode(0xfeff) + lines.join("\r\n"), {
        status: 200,
        headers: {
          "Content-Type": "text/csv; charset=utf-8",
          "Cache-Control": "no-store",
          "Content-Disposition": `attachment; filename="ho-so-nhan-su-${r.day}.csv"`,
        },
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/staff\/([^/]+)$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const n = need(c, "profile.view");
      if (n) return n;
      const st = findStaff(id0(ctx));
      const at = ctx.url.searchParams.get("at");
      const day = at && /^\d{4}-\d{2}-\d{2}$/.test(at) ? at : vnToday();
      if (
        !st ||
        (!on(c, "profile.view", st, project(st.events, vnToday())) &&
          !on(c, "profile.view", st, project(st.events, day)))
      )
        return problem(
          404,
          "hrm.staff_not_found",
          "Không có hồ sơ nhân sự này",
        );
      audit(c, "view", [st.id], c.m["profile.view"] ?? "none", { at: day });
      return json(detail(c, st, day), 200, { "Cache-Control": "no-store" });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/staff\/([^/]+)\/profile$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const n = need(c, "profile.edit", true);
      if (n) return n;
      const st = findStaff(id0(ctx));
      if (!st || !on(c, "profile.edit", st, project(st.events, vnToday())))
        return problem(
          404,
          "hrm.staff_not_found",
          "Không có hồ sơ nhân sự này",
        );
      const b = (ctx.body ?? {}) as S["HrmProfileUpdate"];
      const pe = checkProfile(b as Partial<S["HrmStaffProfile"]>, false);
      if (pe) return pe;
      if (
        b.phone &&
        STORE.staff.some((s) => s !== st && s.profile.phone === b.phone)
      )
        return problem(
          409,
          "hrm.duplicate_person",
          "Số điện thoại đã thuộc một hồ sơ khác",
        );
      const changed: string[] = [];
      for (const k of [
        "full_name",
        "phone",
        "address",
        "birth_date",
        "gender",
        "email",
        "note",
      ] as const) {
        const v = b[k];
        if (v !== undefined && v !== st.profile[k]) {
          changed.push(k);
          st.profile[k] = v;
        }
      }
      if ("account_ref" in b && (b.account_ref ?? null) !== st.account_ref) {
        st.account_ref = b.account_ref ?? null;
        changed.push("account_ref");
      }
      if (!changed.length)
        return json({
          id: st.id,
          code: st.code,
          version: st.history.length,
          warnings: [],
        });
      return json(
        writeStaff(
          st,
          changed.length === 1 && changed[0] === "account_ref"
            ? "account"
            : "profile",
          changed,
        ),
      );
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/staff\/([^/]+)\/sensitive$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const n = need(c, "sensitive.view");
      if (n) return n;
      const st = findStaff(id0(ctx));
      if (!st || !on(c, "sensitive.view", st, project(st.events, vnToday())))
        return problem(
          404,
          "hrm.staff_not_found",
          "Không có hồ sơ nhân sự này",
        );
      if (st.type === "can_bo_xa")
        return problem(
          422,
          "hrm.not_applicable",
          "thẻ Thống kê BC Sở chỉ áp dụng viên chức, hợp đồng lao động",
        );
      audit(c, "view_sensitive", [st.id], "all");
      return json({ id: st.id, fields: st.sensitive }, 200, {
        "Cache-Control": "no-store",
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/staff\/([^/]+)\/sensitive$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const n =
        need(c, "sensitive.view", true) ?? need(c, "profile.edit", true);
      if (n) return n;
      const st = findStaff(id0(ctx));
      if (!st)
        return problem(
          404,
          "hrm.staff_not_found",
          "Không có hồ sơ nhân sự này",
        );
      if (st.type === "can_bo_xa")
        return problem(
          422,
          "hrm.not_applicable",
          "thẻ Thống kê BC Sở chỉ áp dụng viên chức, hợp đồng lao động",
        );
      const f =
        ((ctx.body ?? {}) as { fields?: Record<string, string> }).fields ?? {};
      const allowed = [
        "ethnicity",
        "religion",
        "party_date",
        "hometown",
        "birthplace",
        "revolution_date",
      ];
      const bad = Object.keys(f).find((k) => !allowed.includes(k));
      if (bad)
        return validation([
          { field: bad, reason: "không thuộc thẻ Thống kê BC Sở" },
        ]);
      const changed = allowed.filter(
        (k) => (f[k] ?? "") !== (st.sensitive[k] ?? ""),
      );
      st.sensitive = Object.fromEntries(Object.entries(f).filter(([, v]) => v));
      return json(
        changed.length
          ? writeStaff(st, "sensitive", changed)
          : {
              id: st.id,
              code: st.code,
              version: st.history.length,
              warnings: [],
            },
      );
    },
  },
  {
    method: "POST",
    pattern:
      /^\/v1\/hrm\/staff\/([^/]+)\/events(?:\/([^/]+)\/(replace|void))?$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const n = need(c, "profile.edit", true);
      if (n) return n;
      const st = findStaff(id0(ctx));
      if (!st || !on(c, "profile.edit", st, project(st.events, vnToday())))
        return problem(
          404,
          "hrm.staff_not_found",
          "Không có hồ sơ nhân sự này",
        );
      const target = ctx.params[1] ? decodeURIComponent(ctx.params[1]) : null;
      const op = ctx.params[2] ?? "add";
      const b = (ctx.body ?? {}) as EventIn;
      const reason = (b.reason ?? "").trim();
      let old: Ev | undefined;
      if (target) {
        old = effective(st.events).find((e) => e.id === target);
        if (!old)
          return problem(
            404,
            "hrm.event_not_found",
            "Không có dòng quá trình này trong hồ sơ",
          );
        if (reason.length < 10 || reason.length > 500)
          return validation([
            {
              field: "reason",
              reason: "sửa/hủy dòng quá trình cần lý do 10–500 ký tự",
            },
          ]);
      }
      const rest = st.events.filter((e) => e !== old);
      let ev: Ev;
      if (op === "void") {
        ev = { ...old!, payload: {}, void: true };
      } else {
        const ne = normEvent({ type: st.type, events: rest }, b);
        if (ne instanceof Response) return ne;
        if (old && ne.kind !== old.kind)
          return validation([
            {
              field: "kind",
              reason: "dòng thay phải cùng loại với dòng bị thay",
            },
          ]);
        ev = ne;
      }
      ev = {
        ...ev,
        id: uuid(),
        version: ++STORE.seq,
        supersedes: old?.id ?? null,
        reason: old ? reason : "",
        recorded_at: now(),
      };
      if (!workOk([...st.events, ev]))
        return validation([
          {
            field: "valid_from",
            reason: "ngày vào làm/nghỉ việc không hợp lệ (FN-08)",
          },
        ]);
      st.events.push(ev);
      return json({ id: ev.id, version: ev.version }, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/staff\/([^/]+)\/access-log$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      const n = need(c, "audit.view");
      if (n) return n;
      const st = findStaff(id0(ctx));
      if (!st)
        return problem(
          404,
          "hrm.staff_not_found",
          "Không có hồ sơ nhân sự này",
        );
      return json({
        items: STORE.access
          .filter((a) => a.staff_ids.includes(st.id))
          .map((a) => ({
            id: a.id,
            action: a.action,
            item_count: a.item_count,
            scope: a.scope,
            query: a.query,
            actor_ref: a.actor_ref,
            recorded_at: a.recorded_at,
          })),
      });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/hrm\/permissions(\/history)?$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      if (c.group !== "admin")
        return problem(403, "forbidden", "phân quyền hồ sơ: chỉ Admin (FN-20)");
      const row = (r: PermRow, def: Matrix | null): S["HrmPermEntry"] => ({
        ...r,
        default: def,
        locked: false,
      });
      if (ctx.params[0])
        return json({
          items: STORE.perms.map((r) =>
            row(r, r.grantee_kind === "role" ? defaults(r.grantee) : null),
          ),
        });
      const items: S["HrmPermEntry"][] = GROUPS.map((g) => {
        const r = lastPerm("role", g);
        return {
          grantee_kind: "role",
          grantee: g,
          perms: { ...defaults(g), ...(g === "admin" ? {} : (r?.perms ?? {})) },
          default: defaults(g),
          version: r?.version ?? 0,
          note: r?.note ?? "",
          recorded_at: r?.recorded_at ?? null,
          actor_ref: r?.actor_ref ?? null,
          locked: g === "admin",
        };
      });
      const users = [
        ...new Set(
          STORE.perms
            .filter((r) => r.grantee_kind === "user")
            .map((r) => r.grantee),
        ),
      ];
      for (const u of users) items.push(row(lastPerm("user", u)!, null));
      return json({
        perms: [...PERMS],
        scopes: ["none", "self", "unit", "all"],
        groups: GROUPS,
        items,
      });
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/hrm\/permissions$/,
    handler: (ctx) => {
      const c = caller(ctx);
      if (c instanceof Response) return c;
      if (c.group !== "admin" || c.readOnly)
        return problem(403, "forbidden", "phân quyền hồ sơ: chỉ Admin (FN-20)");
      const b = (ctx.body ?? {}) as {
        grantee_kind?: string;
        grantee?: string;
        perms?: Record<string, string>;
        note?: string;
      };
      if (b.grantee_kind === "role" && b.grantee === "admin")
        return problem(
          422,
          "hrm.permission_locked",
          "Quyền của Admin cố định, không chỉnh được",
        );
      if (b.grantee_kind !== "role" && b.grantee_kind !== "user")
        return validation([{ field: "grantee_kind", reason: "role | user" }]);
      if (b.grantee_kind === "role" && !GROUPS.includes(b.grantee ?? ""))
        return validation([{ field: "grantee", reason: "nhóm vai" }]);
      if (b.grantee_kind === "user" && !/^[0-9a-f-]{36}$/.test(b.grantee ?? ""))
        return validation([{ field: "grantee", reason: "UUID" }]);
      for (const [k, v] of Object.entries(b.perms ?? {})) {
        if (
          !(PERMS as readonly string[]).includes(k) ||
          !["none", "self", "unit", "all"].includes(v)
        )
          return validation([{ field: k, reason: "nhóm quyền/phạm vi" }]);
        if (
          k === "sensitive.view" &&
          v !== "none" &&
          (v !== "all" || (b.grantee_kind === "role" && b.grantee !== "hcth"))
        )
          return validation([
            {
              field: k,
              reason:
                "thẻ Thống kê BC Sở chỉ cấp cho nhóm HCTH hoặc từng người (BR-03)",
            },
          ]);
      }
      const version = (lastPerm(b.grantee_kind, b.grantee!)?.version ?? 0) + 1;
      STORE.perms.push({
        grantee_kind: b.grantee_kind,
        grantee: b.grantee!,
        perms: (b.perms ?? {}) as Matrix,
        note: b.note ?? "",
        version,
        recorded_at: now(),
        actor_ref: c.ref,
      });
      return json({ version }, 201);
    },
  },
];

/** W11.28 (kn-hrm-reports.ts): báo cáo Sở và dashboard đọc cùng kho giả, phép chiếu, quyền và nhật ký của hồ sơ. */
export const hrmStaffInternals = {
  caller,
  project,
  on,
  audit,
  staff: (): readonly Staff[] => STORE.staff,
  units: () => UNITS,
};
