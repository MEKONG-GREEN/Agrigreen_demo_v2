import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import createClient from "openapi-fetch";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import type { ApiPaths } from "@/api/client";
import { createKhtcApi } from "../api";
import { ProgramPage, type ProgramDraft } from "./ProgramPages";
import { PLAN_BASE, type PlanRoute } from "./routes";

// Exercise the real form and typed API serialization. The transport deliberately
// accepts every name: backend rejection must not manufacture a UI validation PASS.
function harness() {
  const requests: { method: string; path: string; body: unknown }[] = [];
  const transport = vi.fn(async (request: Request) => {
    const body = await request.json();
    requests.push({ method: request.method, path: new URL(request.url).pathname, body });
    return Response.json({ ...body, ref: "0190f1c4-7d3e-7000-8000-000000000206", active: true, version: 1 }, { status: 201 });
  });
  const api = createKhtcApi(createClient<ApiPaths>({ baseUrl: "https://api.test", fetch: transport }));
  const route: PlanRoute = { base: PLAN_BASE, year: 2030, view: "new-program", tab: "programs", go: vi.fn() };
  function Form() {
    const [draft, setDraft] = useState<ProgramDraft>({ code: " NAME500 ", name: "", kind: "program" });
    return <ProgramPage route={route} programs={[]} used={new Set()} draft={draft} onChange={setDraft}
      onCreate={async () => { await api.createProgram({ ...draft, code: draft.code.trim(), name: draft.name.trim() }); }}
      onPick={vi.fn()} busy={false} problem={null} canEdit log={[]} />;
  }
  render(<Form />);
  const name = screen.getByRole("textbox", { name: /Tên CT\/DA/ });
  const save = () => fireEvent.click(screen.getByRole("button", { name: "Lưu CT/DA chủ" }));
  return { requests, transport, name, save };
}

describe("206 program name matches the native 500-rune limit", () => {
  it("rejects 501 ASCII characters at the name field without a POST", async () => {
    const h = harness();
    fireEvent.change(h.name, { target: { value: "a".repeat(501) } });
    await act(async () => h.save());
    expect(h.transport).not.toHaveBeenCalled();
    expect(h.requests).toHaveLength(0);
    expect(await screen.findByText("Tối đa 500 ký tự.")).toBeVisible();
    expect(h.name).toHaveAttribute("aria-invalid", "true");
  });

  for (const [label, entered, sent] of [
    ["500 ASCII, with surrounding whitespace", `  ${"a".repeat(500)}  `, "a".repeat(500)],
    ["500 astral code points (1000 UTF-16 units)", "😀".repeat(500), "😀".repeat(500)],
  ]) {
    it(`sends ${label} unchanged after trimming`, async () => {
      const h = harness();
      expect(h.name).toHaveAttribute("maxlength", "1000");
      fireEvent.change(h.name, { target: { value: entered } });
      h.save();
      await waitFor(() => expect(h.requests).toHaveLength(1));
      expect(h.transport).toHaveBeenCalledTimes(1);
      expect(h.requests[0]).toEqual({ method: "POST", path: "/v1/kn/khtc/programs", body: { code: "NAME500", name: sent, kind: "program" } });
      expect(h.name).not.toHaveAttribute("aria-invalid", "true");
      expect(screen.queryByText("Tối đa 500 ký tự.")).toBeNull();
    });
  }
});
