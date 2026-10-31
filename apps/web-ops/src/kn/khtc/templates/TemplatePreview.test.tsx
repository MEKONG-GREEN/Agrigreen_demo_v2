import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { TemplatePreviewPanel } from "./TemplatePreview";
import type { TemplateApi, TemplatePreview } from "./api";
import type { KhtcPeriod } from "../periods/api";
import { seedTemplate } from "@/api/mock/kn-khtc-template-render";
function result(name: string): TemplatePreview { return { scenario: "actual", definition_hash: "hash", context: { period_id: name, period_name: name, period_type: "week", period_start: "2026-10-01T00:00:00Z", plan_version_id: "plan", input_hash: "source", values: {}, lists: {}, texts: {}, provisional: [] }, document: { key: "mau_1", input_hash: "source", provisional: [], nodes: [] } }; }
describe("template preview request identity", () => {
 it("discards an old period response after selecting another period", async () => {
  let finish!: (r: TemplatePreview) => void;
  const api = { preview: vi.fn(() => new Promise<TemplatePreview>(resolve => { finish = resolve; })) } as unknown as TemplateApi;
  const periods = [{ id: "p1", name: "Kỳ một" }, { id: "p2", name: "Kỳ hai" }] as KhtcPeriod[];
  render(<TemplatePreviewPanel api={api} definition={seedTemplate("mau_1")!.definition} periods={periods} />);
  fireEvent.click(screen.getByRole("button", { name: "Xem trước toàn mẫu" }));
  fireEvent.change(screen.getByLabelText("Kỳ xem trước"), { target: { value: "p2" } });
  await act(async () => { finish(result("DỮ LIỆU KỲ CŨ")); });
  expect(screen.queryByText("DỮ LIỆU KỲ CŨ")).not.toBeInTheDocument();
 });
 it("discards an old scenario response after selecting another scenario", async () => {
  let finish!: (r: TemplatePreview) => void;
  const api = { preview: vi.fn(() => new Promise<TemplatePreview>(resolve => { finish = resolve; })) } as unknown as TemplateApi;
  render(<TemplatePreviewPanel api={api} definition={seedTemplate("mau_1")!.definition} periods={[{ id: "p1", name: "Kỳ" }] as KhtcPeriod[]} />);
  fireEvent.click(screen.getByRole("button", { name: "Xem trước toàn mẫu" }));
  fireEvent.change(screen.getByLabelText("Tình huống"), { target: { value: "no_send" } });
  await act(async () => { finish(result("TÌNH HUỐNG CŨ")); });
  expect(screen.queryByText("TÌNH HUỐNG CŨ")).not.toBeInTheDocument();
 });
});
