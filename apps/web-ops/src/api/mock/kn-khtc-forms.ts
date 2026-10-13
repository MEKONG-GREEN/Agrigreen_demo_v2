// GIẢ LẬP phiếu "Báo cáo cho KHTC" (W11.63 rút gọn) theo hợp đồng /v1/extension/khtc/{form-templates,my-forms,
// periods/{id}/forms/{dept}/*}: mẫu nạp sẵn 4 phòng, dòng = hạng mục phòng phụ trách (Kế hoạch năm giả lập), cột "Từ Kế
// hoạch năm", "Lũy kế kỳ trước", khối "Điều chỉnh kỳ trước", 3 quy tắc kiểm tra, nháp máy chủ, tối đa 3 lần gửi, yêu cầu
// bổ sung / xác nhận. Dùng chung kho kỳ của kn-khtc-periods.ts. Dữ liệu tổng hợp, không phải thật.
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, type Ctx, type Route } from "./http";
import { KHTC_DRAFTS, khtcMockStore, type Stored } from "./kn-khtc-periods";
import { knContext } from "./kn";

type S = components["schemas"];
type Sheet = S["KHTCFormSheet"];
type Tmpl = S["KHTCFormSheetTemplate"];
type Content = S["KHTCFormContent"];
type Issue = S["KHTCFormIssue"];
type Col = S["KHTCFormColumn"];

const KHTC = "ke_hoach_tai_chinh";
const GRID: Col[] = [
  { letter: "A", title: "STT", kind: "fixed", dtype: "text", source: "row_no" },
  {
    letter: "B",
    title: "Hạng mục",
    kind: "fixed",
    dtype: "text",
    source: "entry_name",
  },
  {
    letter: "C",
    title: "ĐVT",
    kind: "from_plan",
    dtype: "text",
    source: "unit",
  },
  {
    letter: "D",
    title: "Khối lượng KH năm",
    kind: "from_plan",
    dtype: "number",
    source: "planned_qty",
  },
  {
    letter: "E",
    title: "Lũy kế kỳ trước",
    kind: "prior_cumulative",
    dtype: "number",
  },
  {
    letter: "F",
    title: "Thực hiện trong kỳ",
    kind: "input",
    dtype: "number",
    input: "done",
    non_negative: true,
  },
  {
    letter: "G",
    title: "Điều chỉnh kỳ trước",
    kind: "formula",
    dtype: "number",
    formula: "adjust",
  },
  {
    letter: "H",
    title: "Lũy kế",
    kind: "formula",
    dtype: "number",
    formula: "cumulative",
  },
  {
    letter: "I",
    title: "% KH",
    kind: "formula",
    dtype: "number",
    formula: "percent",
    unit: "%",
  },
  {
    letter: "J",
    title: "Tình trạng",
    kind: "input",
    dtype: "text",
    input: "status",
  },
  {
    letter: "K",
    title: "Nội dung, xã/điểm thực hiện",
    kind: "input",
    dtype: "text",
    input: "content",
  },
  {
    letter: "L",
    title: "Số người tham gia",
    kind: "input",
    dtype: "integer",
    input: "people",
    non_negative: true,
  },
  {
    letter: "M",
    title: "Kinh phí",
    kind: "input",
    dtype: "number",
    input: "budget",
    non_negative: true,
    unit: "đồng",
  },
];
const common = (first: string) => [
  { key: "cong_tac_khac", title: first, required: true },
  { key: "kho_khan", title: "Khó khăn, vướng mắc, đề xuất" },
  { key: "nhiem_vu_ky_toi", title: "Nhiệm vụ kỳ tới" },
];
const REQ =
  "Cột F nhập số phát sinh thật của kỳ (không lũy kế, không số âm). Điều chỉnh kỳ đã khóa nhập ở khối bên dưới.";
export const TEMPLATES: Tmpl[] = [
  {
    code: "KH-TTTT",
    version: 1,
    department: "thong_tin_truyen_thong",
    title: "Phiếu Phòng Thông tin tuyên truyền",
    requirements: REQ,
    columns: GRID,
    sections: common("Công tác khác (thông tin, tuyên truyền)"),
  },
  {
    code: "KH-TT",
    version: 1,
    department: "trong_trot",
    title: "Phiếu Phòng Trồng trọt",
    requirements: REQ,
    columns: GRID,
    sections: common("Công tác khác; CT, DA năm trước; hợp tác, đề tài"),
  },
  {
    code: "KH-CNTS",
    version: 1,
    department: "chan_nuoi_thuy_san",
    title: "Phiếu Phòng Chăn nuôi - Thủy sản",
    requirements: REQ,
    columns: GRID,
    sections: common(
      "Công tác khác; dự án TW, hợp tác, đề tài, dịch vụ tư vấn",
    ),
  },
  {
    code: "KH-HCTH",
    version: 1,
    department: "hanh_chinh_tong_hop",
    title: "Phiếu Phòng Hành chính - Tổng hợp",
    requirements: "Nhập lời văn từng mục.",
    columns: [],
    sections: [
      {
        key: "cchc",
        title: "Cải cách hành chính, tổ chức cán bộ",
        required: true,
      },
      { key: "nhiem_vu_cchc", title: "Nhiệm vụ kỳ tới về CCHC" },
      { key: "kho_khan", title: "Khó khăn, vướng mắc, đề xuất" },
    ],
  },
];
const GENERIC: Tmpl = {
  code: "KH-CHUNG",
  version: 1,
  department: "",
  title: "Phiếu báo cáo cho KHTC",
  requirements: REQ,
  columns: GRID,
  sections: common("Kết quả thực hiện, công tác khác"),
};
const tmplOf = (dept: string) =>
  TEMPLATES.find((t) => t.department === dept) ?? GENERIC;

const NAMES: Record<string, string> = {
  trong_trot: "Phòng Trồng trọt",
  chan_nuoi_thuy_san: "Phòng Chăn nuôi - Thủy sản",
  thong_tin_truyen_thong: "Phòng Thông tin tuyên truyền",
  hanh_chinh_tong_hop: "Phòng Hành chính - Tổng hợp",
};

// Kế hoạch năm giả lập (hạng mục phòng phụ trách). Mã dòng cố định để nháp/gửi giữ qua các lần tải.
interface PlanRow {
  ref: string;
  tab: string;
  name: string;
  unit: string;
  qty: string;
  module?: boolean;
}
const PLAN: Record<string, PlanRow[]> = {
  trong_trot: [
    {
      ref: "0190a000-0000-7000-8000-000000000301",
      tab: "CT III: CT KN trồng trọt",
      name: "Mô hình lúa chất lượng cao",
      unit: "ha",
      qty: "100",
    },
    {
      ref: "0190a000-0000-7000-8000-000000000302",
      tab: "CT III: CT KN trồng trọt",
      name: "Tập huấn IPM",
      unit: "lớp",
      qty: "20",
    },
    {
      ref: "0190a000-0000-7000-8000-000000000303",
      tab: "CT IV: CT cơ giới hóa",
      name: "Diện tích gieo sạ (lấy từ module)",
      unit: "ha",
      qty: "5000",
      module: true,
    },
  ],
  chan_nuoi_thuy_san: [
    {
      ref: "0190a000-0000-7000-8000-000000000701",
      tab: "CT VII: CT KN chăn nuôi",
      name: "Mô hình bò thịt",
      unit: "mô hình",
      qty: "4",
    },
  ],
  thong_tin_truyen_thong: [
    {
      ref: "0190a000-0000-7000-8000-000000000101",
      tab: "CT I: CT thông tin tuyên truyền",
      name: "Số phiếu giá cả đã thu thập",
      unit: "phiếu",
      qty: "500",
      module: true,
    },
    {
      ref: "0190a000-0000-7000-8000-000000000102",
      tab: "CT I: CT thông tin tuyên truyền",
      name: "Tin bài",
      unit: "tin",
      qty: "50",
    },
  ],
};

interface Sub {
  id: string;
  attempt: number;
  at: string;
  decision: "" | "accepted" | "returned";
  note: string;
  content: Content;
}
const SUBS = new Map<string, Sub[]>(); // `${kỳ}:${phòng}` → mới trước
const DRAFTS = new Map<
  string,
  { content: Content; base: number; at: string }
>();
const empty = (): Content => ({ cells: {}, adjustments: [], texts: {} });

function subsOf(s: Stored, dept: string): Sub[] {
  const k = `${s.p.id}:${dept}`;
  let list = SUBS.get(k);
  const seeded = s.submitted[dept];
  if (!list) {
    list = [];
    if (seeded)
      // kỳ giả lập có sẵn lần gửi (không có số liệu)
      for (let i = seeded.attempt; i >= 1; i--)
        list.push({
          id: uuidv7(),
          attempt: i,
          at: seeded.at,
          decision: i === seeded.attempt ? seeded.decision : "returned",
          note:
            i === seeded.attempt && seeded.decision === "returned"
              ? "Bổ sung số liệu tuần"
              : "",
          content: empty(),
        });
    SUBS.set(k, list);
  }
  return list;
}

const num = (v?: string) =>
  v && /^-?\d{1,15}(\.\d{1,6})?$/.test(v) ? Number(v) : null;

function caller(
  ctx: Ctx,
  dept: string,
): { member: boolean; review: boolean } | Response {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  if (!kc) return { member: true, review: true };
  if (kc.role === "admin") return { member: false, review: false };
  if (
    kc.department_code === KHTC &&
    (kc.role === "kn_tinh" || kc.role === "kn_tw")
  )
    return { member: false, review: kc.role === "kn_tinh" && !kc.read_only };
  if (kc.role === "kn_tinh" && kc.department_code === dept)
    return { member: !kc.read_only, review: false };
  return problem(403, "forbidden", "Không có quyền", {
    detail:
      "phiếu Báo cáo cho KHTC chỉ dành cho phòng được giao, Phòng KHTC và Admin",
  });
}

function find(id: string): Stored | undefined {
  return khtcMockStore().find((s) => s.p.id === id);
}

function stateOf(
  s: Stored,
  dept: string,
): { state: Sheet["state"]; attempt: number; decision: string } {
  const subs = subsOf(s, dept);
  const last = subs[0];
  const overdue = Date.now() > Date.parse(s.p.due_at);
  const draft = DRAFTS.get(`${s.p.id}:${dept}`);
  let state: Sheet["state"];
  if (s.terminal === "cancelled") state = "withdrawn";
  else if (s.terminal === "closed")
    state = !last
      ? "not_sent"
      : last.decision === "accepted"
        ? "confirmed"
        : "locked";
  else if (!last)
    state = overdue
      ? "late"
      : draft && draft.base === 0
        ? "draft"
        : "not_started";
  else
    state =
      last.decision === "accepted"
        ? "confirmed"
        : last.decision === "returned"
          ? "supplement"
          : "submitted";
  return { state, attempt: last?.attempt ?? 0, decision: last?.decision ?? "" };
}

const LABEL: Record<Sheet["state"], string> = {
  not_started: "Chưa nhập",
  draft: "Lưu nháp",
  late: "Trễ hạn",
  submitted: "Đã gửi",
  supplement: "Yêu cầu bổ sung",
  confirmed: "Đã xác nhận",
  locked: "Đã khóa",
  not_sent: "Không gửi",
  withdrawn: "Đã gỡ",
  not_available: "Không đọc được",
};

/** Kỳ tuần đã đóng trước kỳ (được chọn ở "Điều chỉnh kỳ trước"). */
function closedBefore(s: Stored): Stored[] {
  return khtcMockStore().filter(
    (x) =>
      x.p.report_type === "week" &&
      x.terminal === "closed" &&
      x.p.cutoff_date < s.p.from_date &&
      x.p.year >= s.p.year - 1,
  );
}

function evaluate(
  s: Stored,
  dept: string,
  c: Content,
  submit: boolean,
): { issues: Issue[]; rows: S["KHTCFormRow"][] } {
  const t = tmplOf(dept);
  const grid = s.p.report_type === "week" && t.columns.length > 0;
  const issues: Issue[] = [];
  const add = (i: Partial<Issue> & { rule: Issue["rule"]; message: string }) =>
    issues.push({ severity: "error", adjustment: -1, ...i });
  if (submit)
    for (const sec of t.sections)
      if (sec.required && !(c.texts[sec.key] ?? "").trim())
        add({
          rule: "template",
          section: sec.key,
          message: `"${sec.title}" bắt buộc`,
        });
  const plan = grid ? (PLAN[dept] ?? []) : [];
  if (!grid && (Object.keys(c.cells).length || c.adjustments.length))
    add({
      rule: "template",
      message: "phiếu tháng, quý, năm chỉ nhập lời văn (BR-DL-06)",
    });
  // Lũy kế kỳ trước: lần gửi cuối của các kỳ tuần đã đóng cùng năm.
  const prior: Record<string, number> = {};
  for (const x of closedBefore(s).filter((x) => x.p.year === s.p.year)) {
    const last = subsOf(x, dept)[0];
    if (!last) continue;
    for (const [ref, v] of Object.entries(last.content.cells))
      prior[ref] = (prior[ref] ?? 0) + (num(v.done) ?? 0);
    for (const a of last.content.adjustments)
      if (a.period_year === s.p.year)
        prior[a.entry_ref] = (prior[a.entry_ref] ?? 0) + (num(a.amount) ?? 0);
  }
  const allowed = new Map(closedBefore(s).map((x) => [x.p.id, x.p.year]));
  const g: Record<string, number> = {};
  c.adjustments.forEach((a, i) => {
    const row = plan.find((r) => r.ref === a.entry_ref);
    if (!row || row.module)
      return add({
        rule: "template",
        adjustment: i,
        message: "chọn hạng mục nhập của phiếu",
      });
    if (!a.period_id)
      add({
        rule: "template",
        adjustment: i,
        entry_ref: a.entry_ref,
        message: "bắt buộc chọn kỳ được điều chỉnh",
      });
    else if (!allowed.has(a.period_id))
      add({
        rule: "template",
        adjustment: i,
        entry_ref: a.entry_ref,
        message: "kỳ được điều chỉnh phải là kỳ KHTC tuần đã khóa",
      });
    const v = num(a.amount);
    if (v === null || v === 0)
      add({
        rule: "template",
        adjustment: i,
        entry_ref: a.entry_ref,
        message: "số điều chỉnh phải là số khác 0",
      });
    if (!a.reason?.trim())
      add(
        v !== null && v < 0
          ? {
              rule: "negative_reason",
              adjustment: i,
              entry_ref: a.entry_ref,
              letter: "G",
              message: "số âm phải có lý do",
            }
          : {
              rule: "template",
              adjustment: i,
              entry_ref: a.entry_ref,
              message: "bắt buộc nhập lý do điều chỉnh",
            },
      );
    if (v !== null && allowed.get(a.period_id) === s.p.year)
      g[a.entry_ref] = (g[a.entry_ref] ?? 0) + v;
  });
  const tabs: string[] = [];
  const rows = plan.map((r) => {
    if (!tabs.includes(r.tab)) tabs.push(r.tab);
    const cell = c.cells[r.ref];
    if (cell?.done) {
      const v = num(cell.done);
      if (r.module)
        add({
          rule: "template",
          entry_ref: r.ref,
          letter: "F",
          message: "hạng mục lấy từ module — không nhập",
        });
      else if (v === null)
        add({
          rule: "template",
          entry_ref: r.ref,
          letter: "F",
          message: "phải là số",
        });
      else if (v < 0)
        add({
          rule: "template",
          entry_ref: r.ref,
          letter: "F",
          message: "không nhận số âm — nhập ở Điều chỉnh kỳ trước",
        });
    }
    const e = prior[r.ref] ?? 0;
    const h = e + (num(cell?.done) ?? 0) + (g[r.ref] ?? 0);
    const qty = Number(r.qty);
    const over = !r.module && h > qty;
    if (over)
      add({
        rule: "over_plan",
        severity: "warning",
        entry_ref: r.ref,
        letter: "H",
        message: "lũy kế vượt 100% kế hoạch — vẫn gửi được",
      });
    const cells: Record<string, string> = {
      A: "",
      B: r.name,
      C: r.unit,
      D: r.qty,
      E: String(e),
    };
    if (!r.module)
      Object.assign(cells, {
        H: String(h),
        I: String(Math.round((h * 1000) / qty) / 10),
        ...(g[r.ref] !== undefined ? { G: String(g[r.ref]) } : {}),
      });
    return {
      entry_ref: r.ref,
      no: 0,
      code: "",
      name: r.name,
      unit: r.unit,
      planned_qty: r.qty,
      scale_note: "",
      module_sourced: !!r.module,
      source_key: r.module ? "module" : "",
      cells,
      over_plan: over,
      _tab: r.tab,
    };
  });
  return { issues, rows: rows as S["KHTCFormRow"][] };
}

function sheet(
  s: Stored,
  dept: string,
  member: boolean,
  review: boolean,
): Sheet {
  const t = tmplOf(dept);
  const st = stateOf(s, dept);
  const subs = subsOf(s, dept);
  const key = `${s.p.id}:${dept}`;
  const draft = DRAFTS.get(key);
  const useDraft =
    draft && draft.base === st.attempt && (member || !subs.length);
  const content = useDraft ? draft!.content : (subs[0]?.content ?? empty());
  const { issues, rows } = evaluate(s, dept, content, true);
  const tabMap = new Map<string, S["KHTCFormRow"][]>();
  for (const r of rows as (S["KHTCFormRow"] & { _tab: string })[]) {
    const list = tabMap.get(r._tab) ?? [];
    list.push({
      ...r,
      no: list.length + 1,
      cells: { ...r.cells, A: String(list.length + 1) },
    });
    tabMap.set(r._tab, list);
  }
  const limit = !!s.p.max_submissions && st.attempt >= s.p.max_submissions;
  const editable =
    member && ["not_started", "draft", "late", "supplement"].includes(st.state);
  return {
    period: {
      id: s.p.id,
      name: s.p.name,
      report_type: s.p.report_type,
      year: s.p.year,
      from_date: s.p.from_date,
      to_date: s.p.to_date,
      cutoff_date: s.p.cutoff_date,
      due_at: s.p.due_at,
      max_submissions: s.p.max_submissions,
      terminal: s.terminal,
    },
    department: dept,
    department_name: NAMES[dept] ?? dept,
    state: st.state,
    state_label: LABEL[st.state],
    attempt: st.attempt,
    limit_reached: limit,
    can_request_supplement: review && st.state === "submitted" && !limit,
    can_confirm: review && st.state === "submitted",
    can_review: review && st.state === "submitted",
    editable,
    overdue: Date.now() > Date.parse(s.p.due_at),
    request_note: st.decision === "returned" ? (subs[0]?.note ?? "") : "",
    template: t,
    grid: s.p.report_type === "week" && t.columns.length > 0,
    plan_missing: false,
    plan: {
      version: 1,
      decision_no: "",
      effective_from: `${s.p.year}-01-01`,
      version_id: "0190a000-0000-7000-8000-000000000001",
      decision_date: null,
    },
    tabs: [...tabMap].map(([label, rs], i) => ({
      key: String(i),
      label,
      plan: "programs",
      rows: rs,
    })),
    content,
    content_source: useDraft ? "draft" : subs.length ? "submitted" : "empty",
    draft_at: useDraft ? draft!.at : null,
    valid: !issues.some((i) => i.severity === "error"),
    issues,
    adj_periods: closedBefore(s).map((x) => ({
      id: x.p.id,
      name: x.p.name,
      year: x.p.year,
    })),
    submissions: subs.map((x) => ({
      id: x.id,
      attempt: x.attempt,
      submitted_at: x.at,
      late: x.at > s.p.due_at,
      decision: x.decision,
      review_note: x.note,
      reviewed_at: x.decision ? x.at : null,
    })),
  };
}

function load(
  ctx: Ctx,
):
  | { s: Stored; dept: string; who: { member: boolean; review: boolean } }
  | Response {
  const id = ctx.params[0] ?? "";
  const dept = ctx.params[1] ?? "";
  const s = find(id);
  const who = caller(ctx, dept);
  if (who instanceof Response) return who;
  if (
    !s ||
    !s.p.departments.some((d) => d.code === dept) ||
    (s.terminal === "cancelled" && who.member)
  )
    return problem(
      404,
      "extension.khtc_form_not_found",
      "Không có phiếu của phòng trong kỳ KHTC",
    );
  return { s, dept, who };
}

function sync(s: Stored, dept: string) {
  const last = subsOf(s, dept)[0];
  if (last)
    s.submitted[dept] = {
      attempt: last.attempt,
      decision: last.decision,
      at: last.at,
    };
  const d = DRAFTS.get(`${s.p.id}:${dept}`);
  if (d && d.base === (last?.attempt ?? 0))
    KHTC_DRAFTS.set(`${s.p.id}:${dept}`, d.base);
  else KHTC_DRAFTS.delete(`${s.p.id}:${dept}`);
}

const FORM = /^\/v1\/extension\/khtc\/periods\/([^/]+)\/forms\/([a-z_]+)$/;
const OP = (op: string) =>
  new RegExp(`^/v1/extension/khtc/periods/([^/]+)/forms/([a-z_]+)/${op}$`);

function save(ctx: Ctx, submit: boolean): Response {
  const l = load(ctx);
  if (l instanceof Response) return l;
  const { s, dept, who } = l;
  if (!who.member)
    return problem(403, "forbidden", "Không có quyền", {
      detail: "chỉ Cấp Tỉnh của phòng được giao nhập, lưu nháp, gửi phiếu",
    });
  const cur = sheet(s, dept, true, false);
  if (!cur.editable)
    return problem(
      409,
      "extension.khtc_form_locked",
      "Phiếu đã gửi hoặc kỳ đã đóng — không sửa được",
      { detail: `phiếu "${cur.state_label}"` },
    );
  const b = (ctx.body ?? {}) as { sheet?: Content; base_attempt?: number };
  if ((b.base_attempt ?? 0) !== cur.attempt)
    return problem(
      409,
      "forms.submission_conflict",
      "Đã có người cập nhật báo cáo — tải bản mới",
    );
  const c: Content = {
    cells: b.sheet?.cells ?? {},
    adjustments: (b.sheet?.adjustments ?? []).map((a) => ({
      ...a,
      period_year: find(a.period_id)?.p.year,
      period_name: find(a.period_id)?.p.name,
    })),
    texts: b.sheet?.texts ?? {},
  };
  const key = `${s.p.id}:${dept}`;
  if (!submit) {
    DRAFTS.set(key, {
      content: c,
      base: cur.attempt,
      at: new Date().toISOString(),
    });
  } else {
    const { issues } = evaluate(s, dept, c, true);
    const errs = issues.filter((i) => i.severity === "error");
    if (errs.length)
      return problem(
        422,
        "extension.khtc_form_invalid",
        "Phiếu chưa đạt quy tắc kiểm tra — chưa gửi được",
        {
          issues: errs,
          errors: errs.map((i) => ({
            field: i.section ? `texts.${i.section}` : "sheet",
            reason: i.message,
          })),
        },
      );
    subsOf(s, dept).unshift({
      id: uuidv7(),
      attempt: cur.attempt + 1,
      at: new Date().toISOString(),
      decision: "",
      note: "",
      content: c,
    });
    DRAFTS.delete(key);
  }
  sync(s, dept);
  return json(sheet(s, dept, true, false));
}

export const khtcFormRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/extension\/khtc\/form-templates$/,
    handler: () =>
      json({
        column_kinds: [
          "fixed",
          "from_plan",
          "prior_cumulative",
          "input",
          "formula",
        ],
        items: [...TEMPLATES, GENERIC],
      }),
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/khtc\/my-forms$/,
    handler: (ctx) => {
      const dept = ctx.url.searchParams.get("department") ?? "";
      if (!dept)
        return problem(422, "validation_failed", "Validation failed", {
          errors: [{ field: "department", reason: "bắt buộc" }],
        });
      const who = caller(ctx, dept);
      if (who instanceof Response) return who;
      const items = khtcMockStore()
        .filter(
          (s) =>
            s.terminal !== "cancelled" &&
            s.p.departments.some((d) => d.code === dept),
        )
        .map((s) => {
          const st = stateOf(s, dept);
          const last = subsOf(s, dept)[0];
          return {
            period_id: s.p.id,
            name: s.p.name,
            report_type: s.p.report_type,
            year: s.p.year,
            from_date: s.p.from_date,
            to_date: s.p.to_date,
            due_at: s.p.due_at,
            overdue: Date.now() > Date.parse(s.p.due_at),
            state: st.state,
            state_label: LABEL[st.state],
            attempt: st.attempt,
            max_submissions: s.p.max_submissions,
            limit_reached:
              !!s.p.max_submissions && st.attempt >= s.p.max_submissions,
            request_note: st.decision === "returned" ? (last?.note ?? "") : "",
            last_submitted_at: last?.at ?? null,
            late: !!last && last.at > s.p.due_at,
            actionable: ["not_started", "draft", "late", "supplement"].includes(
              st.state,
            ),
          };
        });
      return json({ items });
    },
  },
  {
    method: "GET",
    pattern: FORM,
    handler: (ctx) => {
      const l = load(ctx);
      return l instanceof Response
        ? l
        : json(sheet(l.s, l.dept, l.who.member, l.who.review));
    },
  },
  { method: "POST", pattern: OP("draft"), handler: (ctx) => save(ctx, false) },
  { method: "POST", pattern: OP("submit"), handler: (ctx) => save(ctx, true) },
  {
    method: "POST",
    pattern: OP("review"),
    handler: (ctx) => {
      const l = load(ctx);
      if (l instanceof Response) return l;
      const { s, dept, who } = l;
      if (!who.review)
        return problem(403, "forbidden", "Không có quyền", {
          detail: "chỉ Cấp Tỉnh Phòng KHTC rà soát phiếu",
        });
      const b = (ctx.body ?? {}) as { decision?: string; note?: string };
      const f = sheet(s, dept, false, true);
      if (b.decision === "returned") {
        if (!f.can_request_supplement)
          return problem(
            409,
            "extension.khtc_form_not_reviewable",
            "Phiếu không ở trạng thái rà soát được",
          );
        if (!b.note?.trim())
          return problem(422, "validation_failed", "Validation failed", {
            errors: [{ field: "note", reason: "bắt buộc khi trả lại" }],
          });
      } else if (b.decision === "accepted") {
        if (!f.can_confirm)
          return problem(
            409,
            "extension.khtc_form_not_reviewable",
            "Phiếu không ở trạng thái rà soát được",
          );
      } else
        return problem(422, "validation_failed", "Validation failed", {
          errors: [{ field: "decision", reason: "returned | accepted" }],
        });
      const last = subsOf(s, dept)[0];
      if (!last)
        return problem(
          409,
          "extension.khtc_form_not_reviewable",
          "Phiếu không ở trạng thái rà soát được",
        );
      last.decision = b.decision as "returned" | "accepted";
      last.note = b.note?.trim() ?? "";
      sync(s, dept);
      return json(sheet(s, dept, false, true));
    },
  },
];
