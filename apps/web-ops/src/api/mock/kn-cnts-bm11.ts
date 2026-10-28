// GIẢ LẬP Báo cáo phòng CNTS BM-11 (W11.45, BRD CNTS v3.7 UR-TINH-16, FN-16) theo hợp đồng /v1/extension/cnts/bm11/*: báo cáo
// tuần/tháng/quý/năm một bản mỗi kỳ, phần tự động là số tổng hợp mẫu (không phải thật), phần viết theo mục, khóa lạc quan
// expected_seq, vòng đời Đang soạn → Chờ duyệt → (Trả lại có ý kiến) → Đã gửi → Đã nhận từng nơi nhận, hộp "Báo cáo đến" của
// phòng nhận, Word giả (tệp có nhãn GIẢ LẬP — không phải bằng chứng bộ ghi .docx).
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, validation, type Ctx, type Route } from "./http";
import { knContext } from "./kn";

type S = components["schemas"];
type Report = S["CNTSBM11"];
type Type = Report["type"];

const REPORTS = new Map<string, Report>();
const WORDS = new Map<string, S["CNTSBM11WordRun"][]>();
let WORD_NO = 0;

/** Xóa trạng thái (kiểm thử). */
export function resetCntsBm11() {
  REPORTS.clear();
  WORDS.clear();
  WORD_NO = 0;
}

interface Who { role: string; dept: string | null; actor: string; readOnly: boolean; manager: boolean; group: string | null }
function who(ctx: Ctx): Who {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  return {
    role: kc?.role ?? (ctx.persona.me.roles.includes("platform_admin") ? "admin" : "kn_tinh"),
    dept: kc?.department_code ?? null,
    actor: ctx.persona.me.actor_ref,
    readOnly: !!kc?.read_only,
    manager: !!kc?.manager,
    group: kc?.province_group ?? null,
  };
}
const cnts = (w: Who) => w.role === "admin" || ((w.role === "kn_tinh" || w.role === "kn_tw") && (w.dept === null || w.dept === "chan_nuoi_thuy_san"));
const writer = (w: Who) => w.role === "kn_tinh" && (w.dept === null || w.dept === "chan_nuoi_thuy_san") && !w.readOnly;
const approver = (w: Who) => writer(w) && w.manager;
function recipient(w: Who): string | null {
  if (w.role !== "kn_tinh") return null;
  if (w.group === "ban_giam_doc") return "bgd";
  return w.dept === "hanh_chinh_tong_hop" ? "hcth" : w.dept === "ke_hoach_tai_chinh" ? "khtc" : null;
}
const sees = (w: Who, r: Report) => cnts(w) || (r.status === "sent" && r.recipients.includes(recipient(w) as "bgd"));

const SECTIONS: Record<"week" | "period", [string, string][]> = {
  week: [["nhiem_vu_khac", "Một số nhiệm vụ khác có liên quan"], ["han_che", "Đánh giá hạn chế, vướng mắc"], ["phoi_hop", "Công tác phối hợp"],
    ["ke_hoach", "Kế hoạch thực hiện nhiệm vụ trong tuần tới"], ["de_xuat", "Đề xuất, kiến nghị"]],
  period: [["phoi_hop", "Các công tác phối hợp khác"], ["tien_do", "Tiến độ thực hiện chương trình, dự án"], ["thuan_loi", "Những thuận lợi"],
    ["kho_khan", "Những khó khăn"], ["nguyen_nhan", "Nguyên nhân khách quan và chủ quan"], ["phuong_huong", "Phương hướng, nhiệm vụ kỳ tới"],
    ["de_xuat", "Đề xuất, kiến nghị"]],
};
const GROUPS = [{ key: "tong_dan", label: "Tổng đàn" }, { key: "tiem_phong", label: "Tiêm phòng" }, { key: "hoa_chat", label: "Hóa chất" }, { key: "ksgm", label: "Kiểm soát giết mổ" }];
const LINES: S["CNTSBM11Line"][] = [
  ["tong_dan", "tong_dan_heo", "Heo", "con", "467495"], ["tong_dan", "tong_dan_de", "Dê", "con", "14338"], ["tong_dan", "tong_dan_trau", "Trâu", "con", "3271"],
  ["tong_dan", "tong_dan_bo", "Bò", "con", "41678"], ["tong_dan", "tong_dan_vit", "Vịt", "con", "5523052"], ["tong_dan", "tong_dan_ga", "Gà", "con", "5806464"],
  ["tong_dan", "tong_dan_yen", "Yến", "nhà", "3963"], ["tong_dan", "tong_dan_cho_meo", "Chó, mèo", "con", "95609"],
  ["tiem_phong", "tiem_heo", "Heo", "liều", "1427"], ["tiem_phong", "tiem_bo", "Bò", "liều", "1042"], ["tiem_phong", "tiem_cum_gia_cam", "Cúm gia cầm", "liều", "191700"],
  ["tiem_phong", "tiem_dai", "Dại chó, mèo", "liều", "639"], ["hoa_chat", "hoa_chat_gia_suc", "Gia súc", "lít", "696"], ["hoa_chat", "hoa_chat_gia_cam", "Gia cầm", "lít", "159"],
  ["ksgm", "ksgm_gia_suc", "Gia súc", "con", "9933"], ["ksgm", "ksgm_gia_cam", "Gia cầm", "con", "20890"],
].map(([group, key, label, unit, value]) => ({ group: group!, key: key!, label: label!, unit: unit!, value: value!, sub: false }));
const FISHERY: S["CNTSBM11Fishery"] = { catch_cur: "2858.5", catch_ytd: "103318.86", aqua_cur: "38632.42", aqua_ytd: "1698301.69",
  total_cur: "41490.92", total_ytd: "1801620.55", groups: { ca: "1900", tom: "500", muc: "300", khac: "158.5" } };
const COVERAGE: S["CNTSBM11Coverage"][] = [
  { form: "CNTS-BM04", expected: 102, reported: 99, missing: ["X1", "X2", "X3"], missing_names: ["Xã Hòa Thuận", "Xã Ngọc Chúc", "Xã Vĩnh Hòa"], periods: 1, closed: 0 },
  { form: "CNTS-BM17", expected: 40, reported: 40, missing: [], missing_names: [], periods: 1, closed: 0 },
];
const KEY: Record<Type, RegExp> = { week: /^\d{4}-W\d{2}$/, month: /^\d{4}-(0[1-9]|1[0-2])$/, quarter: /^\d{4}-Q[1-4]$/, year: /^\d{4}$/ };

function label(t: Type, k: string): string {
  if (t === "week") return `Tuần ${Number(k.slice(6))}/${k.slice(0, 4)}`;
  if (t === "month") return `Tháng ${Number(k.slice(5))}/${k.slice(0, 4)}`;
  if (t === "quarter") return `Quý ${["I", "II", "III", "IV"][Number(k.slice(6)) - 1]}/${k.slice(0, 4)}`;
  return `Năm ${k}`;
}
// Kỳ vừa kết thúc theo ngày nghiệp vụ VN (UTC+7), neo ngày 1 trước khi lùi tháng/quý — như bm11PrevKey phía máy chủ.
export function bm11DefaultKey(t: Type, nowMs: number = Date.now()): string {
  const vn = new Date(nowMs + 7 * 3_600_000);
  const y = vn.getUTCFullYear(), m = vn.getUTCMonth();
  if (t === "week") {
    const d = new Date(vn.getTime() - 7 * 86_400_000), dy = d.getUTCFullYear();
    return `${dy}-W${String(Math.max(1, Math.ceil((d.getTime() - Date.UTC(dy, 0, 1)) / 604_800_000))).padStart(2, "0")}`;
  }
  if (t === "month") {
    const p = new Date(Date.UTC(y, m - 1, 1));
    return `${p.getUTCFullYear()}-${String(p.getUTCMonth() + 1).padStart(2, "0")}`;
  }
  if (t === "quarter") {
    const p = new Date(Date.UTC(y, Math.floor(m / 3) * 3 - 3, 1));
    return `${p.getUTCFullYear()}-Q${Math.floor(p.getUTCMonth() / 3) + 1}`;
  }
  return String(y - 1);
}

function summary(r: Report): S["CNTSBM11Summary"] {
  const { id, province, type, period_key, label: l, from, to, recipients, status, seq, last_event, last_at, created_by } = r;
  return { id, province, type, period_key, label: l, from, to, recipients, status, seq, last_event, last_at, created_by };
}
function view(r: Report, w: Who): Report {
  const editable = r.status === "draft" || r.status === "returned";
  const rc = recipient(w);
  return {
    ...r, programs: r.programs.map((p) => ({ ...p, can_edit: editable && writer(w) })),
    can: { edit: editable && writer(w), refresh: editable && writer(w), submit: editable && writer(w), approve: r.status === "pending" && approver(w),
      return: r.status === "pending" && approver(w), receive: r.status === "sent" && !!rc && rc !== "bgd" && !w.readOnly &&
        r.recipient_list.some((x) => x.code === rc && !x.received), word: true },
  };
}
function step(r: Report, w: Who, event: string, status: Report["status"], extra: Partial<Report["history"][number]> = {}, content = false): Report {
  const at = new Date().toISOString();
  const seq = r.seq + 1;
  return { ...r, seq, status, last_event: event as Report["last_event"], last_at: at, version: content ? seq : r.version,
    history: [...r.history, { seq, event, status, comment: "", recipient: "", content_hash: content ? "0".repeat(64) : "", actor_name: "", at, ...extra }] };
}

function guard(ctx: Ctx, w: Who, need: (w: Who) => boolean, msg: string): Report | Response {
  const r = REPORTS.get(ctx.params[0]!);
  if (!r || !sees(w, r)) return problem(404, "extension.cnts_bm11_not_found", "Không tìm thấy báo cáo phòng BM-11");
  if (!need(w)) return problem(403, "forbidden", msg);
  const b = (ctx.body ?? {}) as { expected_seq?: number };
  if (b.expected_seq !== undefined && b.expected_seq !== r.seq) return problem(409, "extension.cnts_bm11_version_conflict", "Báo cáo đã có người cập nhật — tải lại bản mới");
  return r;
}
const save = (r: Report, w: Who) => (REPORTS.set(r.id, r), json(view(r, w)));
const notEditable = () => problem(409, "extension.cnts_bm11_state", "Báo cáo không ở trạng thái cho phép thao tác này");

export const cntsBm11Routes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/bm11\/reports$/,
    handler: (ctx) => {
      const w = who(ctx);
      if (!cnts(w)) return problem(403, "forbidden", "Báo cáo phòng BM-11 dành cho Phòng Chăn nuôi – Thủy sản");
      const q = ctx.url.searchParams;
      const items = [...REPORTS.values()].filter((r) => (!q.get("type") || r.type === q.get("type")) && (!q.get("status") || r.status === q.get("status")))
        .sort((a, b) => b.to.localeCompare(a.to)).map(summary);
      return json({ items, can_create: writer(w) });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/bm11\/inbox$/,
    handler: (ctx) => {
      const w = who(ctx);
      const rc = recipient(w);
      if (!rc) return problem(403, "forbidden", "chỉ phòng nhận báo cáo");
      return json({ items: [...REPORTS.values()].filter((r) => r.status === "sent" && r.recipients.includes(rc as "bgd")).map(summary), can_create: false });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/bm11\/preview$/,
    handler: (ctx) => {
      const w = who(ctx);
      if (!cnts(w)) return problem(403, "forbidden", "Báo cáo phòng BM-11 dành cho Phòng Chăn nuôi – Thủy sản");
      const t = (ctx.url.searchParams.get("type") ?? "week") as Type;
      const k = ctx.url.searchParams.get("key") || bm11DefaultKey(t);
      if (!KEY[t]?.test(k)) return validation([{ field: "key", reason: "khóa kỳ không đúng dạng" }]);
      const existing = [...REPORTS.values()].find((r) => r.type === t && r.period_key === k);
      return json({ type: t, period_key: k, label: label(t, k), from: "2026-09-22", to: "2026-09-28", weeks: [k], due_at: new Date().toISOString(),
        min_pct: 80, ready: true, reasons: [], basis: "submitted", communes: "Tổng hợp từ 99/102 xã", coverage: COVERAGE, groups: GROUPS, lines: LINES,
        fishery: FISHERY, recipients: t === "week" ? ["bgd", "hcth"] : ["bgd", "khtc"], existing_id: existing?.id ?? null } satisfies S["CNTSBM11Preview"]);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/bm11\/reports$/,
    handler: (ctx) => {
      const w = who(ctx);
      if (!writer(w)) return problem(403, "forbidden", "chỉ Cấp Tỉnh Phòng Chăn nuôi – Thủy sản lập báo cáo phòng");
      const b = (ctx.body ?? {}) as S["CNTSBM11CreateInput"];
      if (!KEY[b.type]?.test(b.period_key ?? "")) return validation([{ field: "period_key", reason: "khóa kỳ không đúng dạng" }]);
      const old = [...REPORTS.values()].find((r) => r.type === b.type && r.period_key === b.period_key);
      if (old) return problem(409, "extension.cnts_bm11_exists", "Kỳ này đã có báo cáo phòng — mở báo cáo đã có");
      const recipients = (b.recipients?.length ? b.recipients : b.type === "week" ? ["bgd", "hcth"] : ["bgd", "khtc"]) as Report["recipients"];
      const at = new Date().toISOString();
      const r: Report = {
        id: uuidv7(), province: "89", type: b.type, period_key: b.period_key, label: label(b.type, b.period_key), from: "2026-09-22", to: "2026-09-28",
        recipients, status: "draft", seq: 1, last_event: "created", last_at: at, created_by: "", version: 1, content_hash: "0".repeat(64),
        basis: "submitted", communes: "Tổng hợp từ 99/102 xã", coverage: COVERAGE, groups: GROUPS, lines: LINES, fishery: FISHERY,
        programs: [{ ref: uuidv7(), code: "CT-CN-GS", name: "Chương trình phát triển chăn nuôi gia súc", kind: "tinh", manager_ref: w.actor, sites: 15,
          households: 15, text: "", manager_name: "", can_edit: false }],
        sections: SECTIONS[b.type === "week" ? "week" : "period"].map(([key, l]) => ({ key, label: l, text: "" })), supplement: [], return_comment: "",
        recipient_list: recipients.map((code) => ({ code, label: { bgd: "Ban Giám đốc TTKN", hcth: "Phòng Hành chính – Tổng hợp", khtc: "Phòng Kế hoạch – Tài chính" }[code], received: false, received_at: null, actor_name: "" })),
        history: [{ seq: 1, event: "created", status: "draft", comment: "", recipient: "", content_hash: "0".repeat(64), actor_name: "", at }],
        can: { edit: false, refresh: false, submit: false, approve: false, return: false, receive: false, word: true }, replayed: false,
      };
      REPORTS.set(r.id, r);
      return json(view(r, w), 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/bm11\/reports\/([^/]+)$/,
    handler: (ctx) => {
      const w = who(ctx);
      const r = REPORTS.get(ctx.params[0]!);
      if (!r || !sees(w, r)) return problem(404, "extension.cnts_bm11_not_found", "Không tìm thấy báo cáo phòng BM-11");
      return json(view(r, w));
    },
  },
  {
    method: "PUT",
    pattern: /^\/v1\/extension\/cnts\/bm11\/reports\/([^/]+)\/sections$/,
    handler: (ctx) => {
      const w = who(ctx);
      const r = guard(ctx, w, writer, "chỉ Cấp Tỉnh Phòng CNTS soạn báo cáo");
      if (r instanceof Response) return r;
      if (r.status !== "draft" && r.status !== "returned") return notEditable();
      const b = (ctx.body ?? {}) as S["CNTSBM11EditInput"];
      const errs = Object.entries(b.sections ?? {}).filter(([k, v]) => !r.sections.some((s) => s.key === k) || [...v].length > 8000)
        .map(([k]) => ({ field: `sections.${k}`, reason: "mục không có trong mẫu hoặc quá 8.000 ký tự" }));
      if (errs.length) return validation(errs);
      const next = step(r, w, "edited", "draft", {}, true);
      next.sections = r.sections.map((s) => ({ ...s, text: (b.sections?.[s.key] ?? s.text).trim() }));
      next.programs = r.programs.map((p) => ({ ...p, text: (b.programs?.[p.ref] ?? p.text).trim() }));
      return save(next, w);
    },
  },
  ...([
    ["refresh", "refreshed", "draft", writer],
    ["submit", "submitted", "pending", writer],
    ["approve", "approved", "sent", approver],
    ["return", "returned", "returned", approver],
  ] as const).map(([op, event, status, need]): Route => ({
    method: "POST",
    pattern: new RegExp(`^/v1/extension/cnts/bm11/reports/([^/]+)/${op}$`),
    handler: (ctx) => {
      const w = who(ctx);
      const r = guard(ctx, w, need, op === "approve" || op === "return" ? "chỉ Lãnh đạo phòng duyệt, trả lại" : "chỉ Cấp Tỉnh Phòng CNTS soạn, trình");
      if (r instanceof Response) return r;
      const editable = r.status === "draft" || r.status === "returned";
      if ((op === "refresh" || op === "submit") !== editable || ((op === "approve" || op === "return") && r.status !== "pending")) return notEditable();
      const comment = ((ctx.body ?? {}) as { comment?: string }).comment?.trim() ?? "";
      if (op === "return" && !comment) return validation([{ field: "comment", reason: "bắt buộc ghi ý kiến khi trả lại" }]);
      const next = step(r, w, event, status, { comment }, op === "refresh");
      next.return_comment = op === "return" ? comment : op === "submit" ? "" : r.return_comment;
      return save(next, w);
    },
  })),
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/bm11\/reports\/([^/]+)\/receive$/,
    handler: (ctx) => {
      const w = who(ctx);
      const rc = recipient(w);
      const r = REPORTS.get(ctx.params[0]!);
      if (!r || !sees(w, r)) return problem(404, "extension.cnts_bm11_not_found", "Không tìm thấy báo cáo phòng BM-11");
      if (!rc || w.readOnly || !r.recipients.includes(rc as "bgd")) return problem(403, "forbidden", "chỉ phòng nhận của báo cáo xác nhận Đã nhận");
      if (r.recipient_list.some((x) => x.code === rc && x.received)) return json(view(r, w));
      const next = step(r, w, "received", "sent", { recipient: rc });
      next.recipient_list = r.recipient_list.map((x) => (x.code === rc ? { ...x, received: true, received_at: next.last_at } : x));
      return save(next, w);
    },
  },
  {
    method: "POST",
    pattern: /^\/v1\/extension\/cnts\/bm11\/reports\/([^/]+)\/word$/,
    handler: (ctx) => {
      const w = who(ctx);
      const r = REPORTS.get(ctx.params[0]!);
      if (!r || !sees(w, r)) return problem(404, "extension.cnts_bm11_not_found", "Không tìm thấy báo cáo phòng BM-11");
      const runs = WORDS.get(r.id) ?? [];
      const prior = runs.find((x) => x.doc_number);
      const no = r.status === "sent" ? (prior?.doc_number ?? `${++WORD_NO}/BC-CNTS`) : "";
      const run = { id: uuidv7(), seq: runs.length + 1, template: r.type === "week" ? "cnts.bm11_tuan" : "cnts.bm11_ky", template_version: 1, doc_number: no,
        doc_number_auto: !!no, file_name: `bao-cao-phong-cnts-${r.type}-${r.period_key}-v${r.version}.docx`, body_hash: "0".repeat(64), size: 4,
        actor_ref: w.actor, at: new Date().toISOString() };
      WORDS.set(r.id, [run, ...runs]);
      return json(run, 201);
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/bm11\/reports\/([^/]+)\/words$/,
    handler: (ctx) => json({ items: WORDS.get(ctx.params[0]!) ?? [] }),
  },
  {
    method: "GET",
    pattern: /^\/v1\/extension\/cnts\/bm11\/reports\/([^/]+)\/words\/([^/]+)\/download$/,
    handler: (ctx) => {
      const run = (WORDS.get(ctx.params[0]!) ?? []).find((x) => x.id === ctx.params[1]);
      if (!run) return problem(404, "export.run_not_found", "Không có lần xuất này");
      return new Response(new Blob(["GIẢ LẬP BM-11"]), {
        status: 200,
        headers: { "Content-Type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "Content-Disposition": `attachment; filename="${run.file_name}"` },
      });
    },
  },
];
