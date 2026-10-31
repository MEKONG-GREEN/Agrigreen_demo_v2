import { validateForm } from "@/components/form";
import type { LogEntry } from "@/components/patterns/detail";
import type { TemplateDefinition, TemplateDetail, TemplateKey, TemplateNode } from "./api";
import { TEMPLATE_KEYS } from "./model";

export const TEMPLATE_BASE = "/kn/phong-ban/ke_hoach_tai_chinh/muc-luc-mau";
export const templateHref = (key: TemplateKey) => `${TEMPLATE_BASE}/${key}`;
export const itemHref = (key: TemplateKey, id: string) => `${templateHref(key)}/muc/${encodeURIComponent(id)}/sua`;
export function isTemplateKey(key: string): key is TemplateKey { return TEMPLATE_KEYS.includes(key as TemplateKey); }
export { SOURCE_LABELS, FIELDS, FIELD_LABELS, REPEAT_OPTIONS } from "./schema";
import { FIELDS } from "./schema";

export function outline(d: TemplateDefinition): (TemplateNode & { depth: number; number: string })[] {
  const result: (TemplateNode & { depth: number; number: string })[] = [];
  const seen = new Set<string>();
  const visit = (parent: string, prefix: string, depth: number) => {
    if (depth > 6) return;
    d.nodes.filter((n) => n.parent === parent).sort((a, b) => a.order - b.order).forEach((n, i) => {
      if (seen.has(n.id)) return; seen.add(n.id);
      const number = /^[A-Z]+(?:\.[A-Z0-9]+)*$/.test(n.id) || n.id === "phu_luc" ? n.id : `${prefix}.${i + 1}`;
      result.push({ ...n, depth, number }); visit(n.id, number, depth + 1);
    });
  }; visit("", "", 0); return result;
}

export function validateNode(node: TemplateNode, order: string, definition: TemplateDefinition): { errors: Record<string, string>; node: TemplateNode | null } {
  // Bound raw input before validateForm calls Decimal.parse/normalization.
  const hostile = order.length > 12 || /[eE]/.test(order);
  const hasChildren = definition.nodes.some((n) => n.parent === node.id);
  const checked = validateForm({ title: node.title, body: node.body, order: hostile ? "" : order }, {
    title: { kind: "text", required: true, maxLength: 500 }, body: { kind: "text", required: !hasChildren, maxLength: 12000 },
    order: { kind: "number", required: true, integer: true, positive: true, max: 10000, maxInputLength: 12 },
  });
  const errors: Record<string, string> = {};
  for (const [key, value] of Object.entries(checked.errors)) if (value) errors[key] = value;
  if (hostile) errors.order = "Nhập số nguyên từ 1 đến 10000; không dùng số mũ hoặc chuỗi dài.";
  if (!node.parent && !definition.nodes.some((n) => n.id === node.id)) errors.parent = "Chọn mục cha trong năm mẫu cố định.";
  if (!errors.order && definition.nodes.some((n) => n.id !== node.id && n.parent === node.parent && n.order === Number(checked.values.order))) errors.order = "Thứ tự trùng trong cùng mục cha.";
  const seen = new Set<string>();
  for (const binding of node.bindings) {
    if (!/^[a-z][a-z0-9_]{0,39}$/.test(binding.variable) || seen.has(binding.variable) || !FIELDS[binding.source]?.includes(binding.field)) errors.bindings = "Mỗi biến cần tên duy nhất và một nguồn dữ liệu hợp lệ.";
    if (binding.source === "text" && !node.text_slot) errors.bindings = "Bật vùng lời văn tự do trước khi dùng biến lời văn.";
    seen.add(binding.variable);
  }
  for (const match of (node.body + (node.preamble ?? "")).replace(/\{\{|\}\}/g, "").matchAll(/\{([^{}]+)\}/g)) if (!seen.has(match[1]!)) errors.body = `Biến {${match[1]}} chưa gắn nguồn.`;
  return { errors, node: Object.keys(errors).length ? null : { ...node, title: checked.values.title.trim(), order: Number(checked.values.order) } };
}

export function templateLog(detail: TemplateDetail): LogEntry[] {
  const history = [...detail.history].sort((a, b) => a.version - b.version);
  const short = (value: unknown) => typeof value === "string" ? value.slice(0, 160) : JSON.stringify(value).slice(0, 160);
  const entries: LogEntry[] = history.map((v, i) => {
    const previous = history[i - 1]?.definition;
    const currentLabels = new Map(outline(v.definition).map(node => [node.id, `${node.number}. ${node.title}`]));
    const previousLabels = new Map(previous ? outline(previous).map(node => [node.id, `${node.number}. ${node.title}`]) : []);
    const before = new Map(previous?.nodes.map((n) => [n.id, n]) ?? []);
    const changes: NonNullable<LogEntry["changes"]> = [];
    for (const node of v.definition.nodes) {
      const old = before.get(node.id); before.delete(node.id);
      if (!old) { changes.push({ field: currentLabels.get(node.id) ?? node.title, old: null, new: "Thêm mục" }); continue; }
      const labels = { title: "Tên mục", order: "Thứ tự", body: "Khuôn câu", repeat: "Phạm vi lặp", preamble: "Câu mở đầu", text_slot: "Vùng lời văn", bindings: "Biến và nguồn", locked_admin: "Ô hành chính khóa thêm" };
      for (const key of ["title", "order", "body", "repeat", "preamble", "text_slot", "bindings", "locked_admin"] as const) if (JSON.stringify(old[key]) !== JSON.stringify(node[key])) changes.push({ field: `${currentLabels.get(node.id) ?? node.title} / ${labels[key]}`, old: short(old[key] ?? ""), new: short(node[key] ?? "") });
    }
    for (const node of before.values()) changes.push({ field: previousLabels.get(node.id) ?? node.title, old: "Có mục", new: null });
    return { event: `${v.action === "restore" ? "Khôi phục thành bản nháp" : "Lưu bản nháp"} v${v.version}`, actor_display: v.actor_display || "—", at: v.at, changes };
  });
  for (const p of detail.publications) entries.push({ event: `Phát hành v${p.version}`, actor_display: p.actor_display || "—", at: p.at, changes: [{ field: "Áp dụng từ ngày bắt đầu kỳ", old: null, new: p.applies_from.slice(0, 10) }] });
  return entries.sort((a, b) => b.at.localeCompare(a.at));
}
