import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createApiClient, type ApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { resetKhtcMock } from "@/api/mock/kn-khtc";
import { ApiProblem } from "@/api/problem";
import { blankEntry, createKhtcApi, isDecimal, normDecimal, removeSubtree, toSave, treeRows, type KhtcPlanEntry } from "./api";
import { KhtcCatalogScreen } from "./CatalogScreen";
import { KhtcPlanScreen } from "./PlanScreen";

vi.mock("next/navigation", () => ({
  usePathname: () => "/kn/khtc/ke-hoach-nam",
  useSearchParams: () => new URLSearchParams(),
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
}));

const auth: { api: ApiClient | null } = { api: null };
vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({ api: auth.api, kn: { context: { role: "admin" } } }),
}));

async function clientFor(personaId: string, assignmentId: string, department = "trong_trot") {
  const res = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: {
        Authorization: `Bearer mock.${personaId}`,
        "Content-Type": "application/json",
        "Idempotency-Key": `ctx-${personaId}-${Math.random()}`,
      },
      body: JSON.stringify({ assignment_id: assignmentId, department_code: department }),
    }),
  );
  const ctx = (await res.json()) as { context_id: string };
  const api = createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${personaId}`, getContextId: () => ctx.context_id, fetch: mockFetchNoDelay });
  return { api, k: createKhtcApi(api) };
}

async function code(p: Promise<unknown>): Promise<string> {
  try {
    await p;
  } catch (e) {
    return e instanceof ApiProblem ? e.problem.code : String(e);
  }
  return "ok";
}

const year = new Date(Date.now() + 7 * 3_600_000).getUTCFullYear();

describe("Kế hoạch năm — tiện ích cây", () => {
  it("cây theo tab: cha trước con, theo thứ tự; xóa kéo theo dòng con; thân Lưu giữ cha trước con", () => {
    const p = { ...blankEntry("programs", "program", null, 2), name: "CT B", program_ref: "p-b" };
    const q = { ...blankEntry("programs", "program", null, 1), name: "CT A", program_ref: "p-a" };
    const g = { ...blankEntry("programs", "group", p, 1), name: "Nhóm" };
    const i = { ...blankEntry("programs", "item", g, 1), name: "Hạng mục", department_code: "trong_trot" };
    const other = { ...blankEntry("research", "program", null, 1), name: "Đề tài", program_ref: "p-c" };
    const all: KhtcPlanEntry[] = [i, g, p, q, other];
    expect(treeRows(all, "programs").map((r) => [r.e.name, r.depth])).toEqual([
      ["CT A", 0],
      ["CT B", 0],
      ["Nhóm", 1],
      ["Hạng mục", 2],
    ]);
    const body = toSave(all, { base_version: 0 });
    expect(body.entries.map((e) => e.name)).toEqual(["CT A", "CT B", "Nhóm", "Hạng mục", "Đề tài"]);
    expect(body.entries[3]!.program_ref).toBeNull();
    expect(removeSubtree(all, p.ref).map((e) => e.name)).toEqual(["CT A", "Đề tài"]);
  });
  it("số thập phân dạng chuỗi, không qua số thực", () => {
    expect(normDecimal("120,5")).toBe("120.5");
    expect(normDecimal(" ")).toBeNull();
    expect(isDecimal("1.23456", 4)).toBe(false);
    expect(isDecimal("1.2345", 4)).toBe(true);
  });
});

describe("Kế hoạch năm — backend giả lập", () => {
  beforeEach(() => resetKhtcMock());

  it("điều chỉnh theo QĐ: bắt buộc QĐ, khóa lạc quan, phiên bản mới; phòng chuyên môn chỉ xem", async () => {
    const { k } = await clientFor("kn-admin", "asg-admin");
    const plan = (await k.plan(year))!;
    expect(plan.version.version).toBe(1);
    expect(plan.entries[0]!.roman).toBe("I");
    const head = { base_version: 1, decision_no: "", decision_date: `${year}-06-01`, effective_from: `${year}-07-01` };
    expect(await code(k.save(year, toSave(plan.entries, head)))).toBe("extension.khtc_effective_date_invalid");
    const v2 = await k.save(year, toSave(plan.entries, { ...head, decision_no: "456/QĐ-SNN" }));
    expect(v2.latest_version).toBe(2);
    expect(await code(k.save(year, toSave(plan.entries, { ...head, decision_no: "x" })))).toBe("extension.khtc_plan_version_conflict");
    expect((await k.plan(year, 1))!.version.version).toBe(1);
    expect((await k.copyDraft(year + 1)).entries.length).toBe(plan.entries.length);

    const tt = await clientFor("kn-binh", "asg-binh-tinh");
    expect((await tt.k.plan(year))!.latest_version).toBe(2);
    expect(await code(tt.k.createProgram({ code: "X", name: "X", kind: "program" }))).toBe("forbidden");
  });

  it("danh mục: trùng mã 409, một mẫu một người ký mặc định", async () => {
    const { k } = await clientFor("kn-admin", "asg-admin");
    expect(await code(k.createCatalog("unit", { code: "ha", name: "Ha" }))).toBe("extension.khtc_catalog_exists");
    expect(await code(k.createCatalog("signer", { code: "pgd", name: "B", default_for: ["mau_1"], attrs: { title: "Phó giám đốc" } }))).toBe(
      "extension.khtc_default_conflict",
    );
    const c = await k.catalog("unit");
    expect(c.counts.find((x) => x.kind === "unit")!.active).toBe(3);
  });
});

describe("màn hình", () => {
  beforeEach(async () => {
    resetKhtcMock();
    auth.api = (await clientFor("kn-admin", "asg-admin")).api;
  });
  it("Kế hoạch năm hiện 6 tab và cây CT của năm", async () => {
    render(<KhtcPlanScreen />);
    await waitFor(() => expect(screen.getByText("Mô hình lúa chất lượng cao")).toBeTruthy());
    expect(screen.getAllByRole("tab")).toHaveLength(6);
    expect(screen.getByRole("button", { name: "Điều chỉnh theo QĐ" })).toBeTruthy();
  });
  it("Danh mục KHTC hiện 7 loại", async () => {
    render(<KhtcCatalogScreen />);
    await waitFor(() => expect(screen.getByText("Héc ta")).toBeTruthy());
    expect(screen.getAllByRole("tab")).toHaveLength(7);
  });
});
