import { describe, expect, it, vi } from "vitest";
import { Decimal } from "@/lib/exactDecimal";
import seeds from "@/api/mock/fixtures/khtc-template-seeds.json";
import { type TemplateDetail, type TemplateNode } from "./api";
import { isTemplateKey, outline, templateLog, validateNode } from "./template";

const detail = seeds[0] as TemplateDetail;
const child = (): TemplateNode => ({ id: "test_child", parent: "B", order: 1, title: "Mục kiểm thử", repeat: "none", body: "Nội dung", bindings: [], text_slot: false, locked_admin: [] });
describe("Mục lục năm mẫu cố định", () => {
  it("chỉ nhận năm loại mẫu và giữ cây theo thứ tự", () => {
    expect(isTemplateKey("mau_1")).toBe(true); expect(isTemplateKey("mau_6")).toBe(false); expect(isTemplateKey("adhoc")).toBe(false);
    const rows = outline(detail.definition); expect(rows[0]?.id).toBe("A"); expect(rows.find((n) => n.id === "A.I.1")?.depth).toBe(2);
  });
  it.each(["1e100000", "1e-100000", "9".repeat(100000)])("chặn số thô độc hại trước Decimal.parse", (raw) => {
    const parse = vi.spyOn(Decimal, "parse");
    const result = validateNode(child(), raw, detail.definition);
    expect(result.node).toBeNull(); expect(result.errors.order).toBeTruthy(); expect(parse).not.toHaveBeenCalled(); parse.mockRestore();
  });
  it("bắt trường bắt buộc, STT và biến chưa gắn nguồn", () => {
    const node = child(); node.title = ""; node.body = "";
    expect(validateNode(node, "0", detail.definition).errors).toMatchObject({ title: expect.any(String), body: expect.any(String), order: expect.any(String) });
    node.title = "Mục"; node.body = "{chua_co_nguon}";
    expect(validateNode(node, "1", detail.definition).errors.body).toContain("chưa gắn nguồn");
    node.bindings = [{ variable: "chua_co_nguon", source: "module", field: "household_phone" }]; expect(validateNode(node, "1", detail.definition).errors.bindings).toBeTruthy();
  });
  it("log đọc các phiên bản bất biến và chỉ rõ thay đổi", () => {
    const first = { id: "v1", key: "mau_1" as const, rule: "khtc-template/v2.3-1", version: 1, action: "save" as const, definition: structuredClone(detail.definition), hash: "h1", actor: "actor", actor_display: "Người kiểm thử", at: "2026-10-01T00:00:00Z" };
    const second = { ...first, id: "v2", version: 2, definition: structuredClone(first.definition), at: "2026-10-02T00:00:00Z" }; second.definition.nodes.find((n) => n.id === "B")!.body = "Lời văn khác";
    const log = templateLog({ ...detail, history: [second, first] });
    expect(log[0]?.actor_display).toBe("Người kiểm thử"); expect(log[0]?.changes?.some((c) => c.field.includes("Khuôn câu") && c.new === "Lời văn khác")).toBe(true);
  });
});
