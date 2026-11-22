// TT-11 (#104) qua client web trên backend giả lập: tạo/sửa thửa, bắt buộc tên + chủ hộ, chồng lấn bị chặn,
// sửa ranh giới thay phiên bản có lý do, cây trồng ở hồ sơ KN riêng (#119); Cấp Tỉnh chỉ xem.
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
const sq = (lng: number, lat: number, d = 0.001) => ({ type: "Polygon" as const, coordinates: [[[lng, lat], [lng + d, lat], [lng + d, lat + d], [lng, lat + d], [lng, lat]]] });

beforeEach(() => resetKnCollect());

describe("thửa KN giả lập (TT-11)", () => {
  it("tạo thửa: bắt buộc tên + chủ hộ, diện tích từ hình, chồng lấn 409; cây trồng hồ sơ KN", async () => {
    const xa = await clientFor("kn-an", "asg-an-xa");
    const [first] = (await xa.plots({ commune: "91301" })).items;
    const shapes = await xa.plotShapes("91301");
    expect(shapes.features.length).toBeGreaterThan(0);
    const base = { commune_code: "91301", lot_name: "Lô mới", farmer_name: "Trần Văn Bảy", shape: "polygon" as const, geometry: sq(106, 10) };
    expect(await code(xa.createPlot({ ...base, lot_name: " " }))).toBe("422 validation_failed");
    expect(await code(xa.createPlot({ ...base, farmer_name: "" }))).toBe("422 validation_failed");
    const seeded = (await xa.plot(first!.ref)).geometry;
    expect(await code(xa.createPlot({ ...base, geometry: seeded as never }))).toBe("409 geo.plot_overlap");
    const p = await xa.createPlot(base);
    expect(Number(p.area_ha)).toBeGreaterThan(1);
    expect(p.farmer?.label).toBe("Trần V. B.");
    expect((await xa.plotProfile(p.ref)).crop_code).toBeNull();
    expect(await code(xa.putPlotCrop(p.ref, ""))).toBe("422 validation_failed");
    expect(await code(xa.putPlotCrop(p.ref, "KHONG_CO"))).toBe("422 extension.kn_attrs_crop_unknown");
    expect((await xa.putPlotCrop(p.ref, "RAU_MUONG")).crop_name).toBe("Rau muống");
    expect((await xa.plotCrops("91301")).items.map((c) => c.plot_ref)).toContain(p.ref);
  });

  it("sửa thửa: tên rỗng/chủ hộ rỗng 422; ranh giới mới cần lý do, đúng phiên bản, không chồng lấn", async () => {
    const xa = await clientFor("kn-an", "asg-an-xa");
    const [a, b] = (await xa.plots({ commune: "91301" })).items;
    expect(await code(xa.updatePlot(a!.ref, { lot_name: "" }))).toBe("422 validation_failed");
    expect(await code(xa.updatePlot(a!.ref, { farmer_name: "" }))).toBe("422 validation_failed");
    expect((await xa.updatePlot(a!.ref, { lot_name: "Lô đổi tên" })).lot_name).toBe("Lô đổi tên");
    expect((await xa.plotLog(a!.ref)).items[0]!.event).toBe("attributes_changed");
    const bGeom = (await xa.plot(b!.ref)).geometry;
    const req = { supersedes_boundary_id: a!.boundary_id, geometry: sq(106.5, 10.5), shape: "polygon" as const, reason: "Đo lại bằng GPS" };
    expect(await code(xa.correctPlotBoundary(a!.ref, { ...req, reason: "ngắn" }))).toBe("422 validation_failed");
    expect(await code(xa.correctPlotBoundary(a!.ref, { ...req, geometry: bGeom as never }))).toBe("409 geo.plot_overlap");
    const moved = await xa.correctPlotBoundary(a!.ref, req);
    expect(moved.boundary_id).not.toBe(a!.boundary_id);
    expect(await code(xa.correctPlotBoundary(a!.ref, req))).toBe("409 geo.version_conflict");
  });

  it("Cấp Tỉnh chỉ xem", async () => {
    const tinh = await clientFor("kn-binh", "asg-binh-tinh");
    const [p] = (await tinh.plots()).items;
    expect(await code(tinh.updatePlot(p!.ref, { lot_name: "X" }))).toBe("403 geo.read_only");
    expect(await code(tinh.putPlotCrop(p!.ref, "RAU_MUONG"))).toBe("403 extension.read_only");
  });
});
