// GIẢ LẬP W10.17 (`cap/export`): sổ lần xuất + số văn bản {n}/<ký hiệu> theo năm, mẫu đăng ký. Tệp giả lập là văn bản
// có nhãn GIẢ LẬP (không phải .docx thật) — không dùng làm bằng chứng bộ ghi .docx (kiểm ở Go: internal/shared/docx).
import type { components } from "../generated/application";
import { uuidv7 } from "../idempotency";
import { json, problem, type Ctx, type Route } from "./http";
import { knContext } from "./kn";

type S = components["schemas"];
type Run = S["ExportRun"];
type Template = S["ExportTemplate"];

const RUNS: { meta: Run; body: string }[] = [];
const NUMBERS = new Map<string, number>(); // "<ký hiệu>:<năm>" → n cuối
const ASSIGNED = new Map<string, string>(); // "<ký hiệu>:<subject>" → số đã cấp
const RECORDED = new Map<string, string>(); // "<khóa>:v<n>" → lúc ghi sổ

const SECTIONS: Record<string, string[]> = {
  mau_1: ["A. KẾT QUẢ ĐẠT ĐƯỢC", "B. Đánh giá hạn chế, vướng mắc", "C. Nhiệm vụ kỳ tới", "D. Đề xuất, kiến nghị"],
  mau_2: ["I. Kết quả thực hiện", "II. Công tác cán bộ, CCHC", "III. Thuận lợi, khó khăn", "IV. Phương hướng kỳ tới", "V. Đề xuất và kiến nghị"],
  mau_5: ["I. Tiến độ thực hiện các chương trình", "II. Kế hoạch thực hiện kỳ tới"],
};
const TEMPLATES = ["mau_1", "mau_2", "mau_3", "mau_4", "mau_5"].map((m) => ({ key: `khtc.${m}`, title: `Báo cáo KHTC ${m.replace("_", " ").toUpperCase()}`, sections: SECTIONS[m] ?? SECTIONS.mau_2! }));

/** Năm theo giờ Việt Nam (UTC+7, không đổi giờ). */
const vnYear = (d: Date) => new Date(d.getTime() + 7 * 3600_000).getUTCFullYear();
/** "An Giang, ngày dd tháng mm năm yyyy" theo giờ Việt Nam. */
export function placeDateLine(place: string, d = new Date()): string {
  const v = new Date(d.getTime() + 7 * 3600_000), two = (n: number) => String(n).padStart(2, "0");
  const line = `ngày ${two(v.getUTCDate())} tháng ${two(v.getUTCMonth() + 1)} năm ${v.getUTCFullYear()}`;
  return place.trim() ? `${place.trim()}, ${line}` : line;
}

/** Ghi một lần xuất giả lập: cấp số nếu `number` và ô số trống (một số mỗi subject, tuần tự theo năm). */
export function mockExportRun(input: { template: string; subject: string; fileName: string; docNumber: string; series?: string; actor: string; lines: string[] }): Run {
  const now = new Date();
  RECORDED.set(`${input.template}:v1`, RECORDED.get(`${input.template}:v1`) ?? now.toISOString());
  let docNumber = input.docNumber.trim(), auto = false;
  if (!docNumber && input.series) {
    const key = `${input.series}:${input.subject}`;
    docNumber = ASSIGNED.get(key) ?? "";
    if (!docNumber) {
      const counter = `${input.series}:${vnYear(now)}`, n = (NUMBERS.get(counter) ?? 0) + 1;
      NUMBERS.set(counter, n);
      docNumber = `${n}/${input.series}`;
      ASSIGNED.set(key, docNumber);
    }
    auto = true;
  }
  const body = ["GIẢ LẬP — không phải tệp Word thật", `Số: ${docNumber || "…"}`, ...input.lines].join("\r\n");
  const seq = RUNS.filter((r) => r.meta.subject === input.subject).length + 1;
  const meta: Run = { id: uuidv7(), seq, subject: input.subject, template: input.template, template_version: 1, template_hash: "0".repeat(64), input_hash: "0".repeat(64),
    doc_number: docNumber, doc_number_auto: auto, file_name: input.fileName, body_hash: "0".repeat(64), size: body.length, actor_ref: input.actor, at: now.toISOString() };
  RUNS.push({ meta, body });
  return meta;
}
export const mockExportRuns = (subject: string) => RUNS.filter((r) => r.meta.subject === subject).map((r) => r.meta).reverse();
export const mockExportBody = (subject: string | null, id: string) => RUNS.find((r) => r.meta.id === id && (subject === null || r.meta.subject === subject));

function admin(ctx: Ctx): boolean {
  const kc = knContext(ctx.request.headers.get("X-AG-Context") ?? "");
  return kc ? kc.role === "admin" : true; // không ngữ cảnh KN: persona quản trị nền tảng của bản demo
}

export function downloadResponse(r: { meta: Run; body: string }): Response {
  return new Response(r.body, { headers: { "Content-Type": "text/plain; charset=utf-8", "Content-Disposition": `attachment; filename="${r.meta.file_name.replace(/\.docx$/, "-gia-lap.txt")}"` } });
}

export const exportRoutes: Route[] = [
  {
    method: "GET",
    pattern: /^\/v1\/exports\/(templates|runs)$/,
    handler: (ctx) => {
      if (!admin(ctx)) return problem(403, "forbidden", "Chỉ Admin xem mẫu văn bản và lịch sử xuất");
      if (ctx.params[0] === "templates") {
        const items: Template[] = TEMPLATES.map((t) => ({ key: t.key, title: t.title, owner: "pf/extension", latest: 1, vars: [{ key: "so_van_ban", label: "Số văn bản" }, { key: "dia_danh_ngay", label: "Địa danh, ngày", required: true }],
          lists: [{ key: "ct_trong_nam", label: "Chương trình triển khai trong năm", fields: ["tt", "ten", "dvt", "ke_hoach", "trong_ky", "luy_ke", "phan_tram", "tinh_trang", "nhom"] }],
          sections: t.sections, versions: [{ version: 1, hash: "0".repeat(64), registered: true, recorded_at: RECORDED.get(`${t.key}:v1`) ?? null, drift: false }] }));
        return json({ items });
      }
      const template = ctx.url.searchParams.get("template"), subject = ctx.url.searchParams.get("subject");
      return json({ items: RUNS.filter((r) => (!template || r.meta.template === template) && (!subject || r.meta.subject === subject)).map((r) => r.meta).reverse() });
    },
  },
  {
    method: "GET",
    pattern: /^\/v1\/exports\/runs\/([^/]+)\/download$/,
    handler: (ctx) => {
      if (!admin(ctx)) return problem(403, "forbidden", "Chỉ Admin tải lại");
      const r = mockExportBody(null, ctx.params[0] ?? "");
      return r ? downloadResponse(r) : problem(404, "export.run_not_found", "Không có lần xuất này");
    },
  },
];
