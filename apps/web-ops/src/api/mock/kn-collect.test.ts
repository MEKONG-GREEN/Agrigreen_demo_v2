// Thu thập dữ liệu Cấp Xã (P7 /v1/kn/*) qua client web (src/kn/cycle/api.ts) trên backend giả lập:
// phạm vi xã phụ trách, SĐT che, vô hiệu hóa thay xóa, máy nguyên ≥ 0 không trùng loại, tổng xã = Σ HTX, vật tư + tổng hợp.
import { beforeEach, describe, expect, it } from "vitest";
import { ApiProblem } from "@/api/problem";
import { createCollectApi } from "@/kn/cycle/api";
import { mockFetchNoDelay } from "./handler";
import { resetKnCollect } from "./kn-collect";

async function clientFor(personaId: string, assignmentId: string) {
  const r = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: { Authorization: `Bearer mock.${personaId}`, "Content-Type": "application/json", "Idempotency-Key": `ctx-${Math.random()}` },
      body: JSON.stringify({ assignment_id: assignmentId, department_code: "trong_trot" }),
    }),
  );
  const ctx = ((await r.json()) as { context_id: string }).context_id;
  return createCollectApi({ baseUrl: "http://mock", getAccessToken: () => `mock.${personaId}`, getContextId: () => ctx, fetch: mockFetchNoDelay });
}
const code = (p: Promise<unknown>) => p.then(() => "ok", (e: unknown) => (e instanceof ApiProblem ? `${e.status} ${e.code}` : String(e)));

beforeEach(() => resetKnCollect());

describe("thu thập dữ liệu Cấp Xã giả lập (P7)", () => {
  it("FN-XA-03.03: HTX trong xã phụ trách, SĐT che, thêm + vô hiệu hóa thay xóa; Cấp Tỉnh chỉ xem", async () => {
    const xa = await clientFor("kn-an", "asg-an-xa");
    const list = (await xa.cooperatives()).items;
    expect(new Set(list.map((c) => c.commune_code))).toEqual(new Set(["91301", "91302"]));
    expect(list[0]!.representative?.masked_phone).toMatch(/•••/);
    expect(await code(xa.cooperatives({ commune: "91304" }))).toBe("403 party.out_of_scope");
    const c = await xa.createCooperative({ name: "HTX Mới Bình Đức", commune_code: "91302", area_ha: "12.5", representative: { full_name: "Nguyễn Văn Tư", phone: "0912000111" } });
    expect(c.representative).toEqual({ label: "Nguyễn V. T.", masked_phone: "0912•••111" });
    expect(await code(xa.createCooperative({ name: "htx mới bình đức", commune_code: "91302" }))).toBe("409 party.cooperative_name_taken");
    await xa.addHousehold(c.ref, { holder: { full_name: "Lê Thị Năm", phone: "0987000222" } });
    expect(await code(xa.deactivate(c.ref, "ngắn"))).toBe("422 validation_failed");
    const off = await xa.deactivate(c.ref, "HTX giải thể theo nghị quyết đại hội");
    expect(off.warnings.households).toBe(1);
    expect((await xa.cooperatives({ status: "inactive" })).items.map((x) => x.name)).toEqual(["HTX Mới Bình Đức"]);
    const tinh = await clientFor("kn-binh", "asg-binh-tinh");
    expect((await tinh.cooperatives()).items.length).toBeGreaterThan(list.length);
    expect(await code(tinh.createCooperative({ name: "X", commune_code: "91301" }))).toBe("403 party.read_only");
  });

  it("FN-XA-03.04 / 03.06: khai báo máy (nguyên, không trùng loại, tổng xã = Σ HTX); vật tư + tổng hợp theo xã", async () => {
    const xa = await clientFor("kn-an", "asg-an-xa");
    const coop = (await xa.cooperatives({ commune: "91301" })).items[0]!;
    expect(await code(xa.putMachines(coop.ref, [{ machine_type: "may_cay", quantity: 1.5 }]))).toBe("422 mechanization.machine_quantity_invalid");
    expect(await code(xa.putMachines(coop.ref, [{ machine_type: "may_cay", quantity: 1 }, { machine_type: "may_cay", quantity: 2 }]))).toBe("422 mechanization.machine_type_duplicate");
    expect((await xa.putMachines(coop.ref, [{ machine_type: "may_cay", quantity: 3 }])).total).toBe(3);
    const ov = await xa.machines("91301");
    const sum = ov.cooperatives.flatMap((c) => c.lines).filter((l) => l.machine_type === "may_cay").reduce((s, l) => s + l.quantity, 0);
    expect(ov.commune_totals.find((t) => t.machine_type === "may_cay")?.quantity).toBe(sum);
    expect(await code(xa.recordMaterials({ commune_code: "91301", recorded_on: "2026-09-30", lines: [{ material_code: "bay_den", quantity: "1.5" }] }))).toBe("422 extension.material_quantity_invalid");
    await xa.recordMaterials({ commune_code: "91301", recorded_on: "2026-09-30", lines: [{ material_code: "phan_ure", quantity: "2.25" }, { material_code: "bay_den", quantity: "10" }] });
    const tinh = await clientFor("kn-binh", "asg-binh-tinh");
    const s = await tinh.materialSummary();
    expect(s.items.map((x) => [x.commune_code, x.material_code, x.quantity])).toEqual([
      ["91301", "phan_ure", "2.25"],
      ["91301", "bay_den", "10"],
    ]);
    expect((await xa.plots()).items.every((p) => ["91301", "91302"].includes(p.commune_code))).toBe(true);
  });
});
