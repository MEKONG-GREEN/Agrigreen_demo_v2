import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetKnAdmin } from "@/api/mock/kn-admin";
import { resetCntsAggregate } from "@/api/mock/kn-cnts-aggregate";
import { resetCntsPeriods } from "@/api/mock/kn-cnts-periods";
import { ApiProblem } from "@/api/problem";
import { createKnAdminApi } from "@/kn/cycle/api";
import { createCntsPeriodApi } from "../periods/api";
import { adjustmentErrors, aquaValueError, createCntsAggApi } from "./api";

async function contextOf(personaId: string, assignmentId: string, dept: string | null) {
  const r = await mockFetchNoDelay(
    new Request("http://mock/v1/me/context", {
      method: "POST",
      headers: { Authorization: `Bearer mock.${personaId}`, "Content-Type": "application/json", "Idempotency-Key": `ctx-${Math.random()}` },
      body: JSON.stringify({ assignment_id: assignmentId, department_code: dept }),
    }),
  );
  return ((await r.json()) as { context_id: string }).context_id;
}

const apiOf = (personaId: string, ctx: string | null) =>
  createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${personaId}`, getContextId: () => ctx, fetch: mockFetchNoDelay });
const code = async (p: Promise<unknown>) => p.then(() => "ok", (e: unknown) => (e instanceof ApiProblem ? e.problem.code : String(e)));

describe("tổng hợp CNTS — kiểm phía máy khách", () => {
  it("dòng điều chỉnh, ô nuôi trồng", () => {
    expect(adjustmentErrors({ heo: "-3" }, "Trùng số ca đêm")).toEqual({});
    expect(adjustmentErrors({ heo: "0" }, "ngắn")).toEqual({ deltas: "Nhập ít nhất một số khác 0", reason: "Lý do 10–500 ký tự" });
    expect(adjustmentErrors({ heo: "1,5" }, "Lý do đủ dài ở đây")).toEqual({ heo: "Số (có thể âm, dấu chấm)" });
    expect(aquaValueError("")).toBeNull();
    expect(aquaValueError("7.25")).toBeNull();
    expect(aquaValueError("-1")).toBe("Số không âm (dấu chấm)");
  });
});

describe("tổng hợp CNTS — backend giả lập /v1/extension/cnts/bm18|aggregates|shares", () => {
  beforeEach(() => {
    resetKnAdmin();
    resetCntsPeriods();
    resetCntsAggregate();
  });

  it("Phụ lục 2: khai thác BM-17 + nuôi trồng, lũy kế; chốt cần mọi kỳ nguồn đã chốt; phiên bản chia sẻ", async () => {
    const adminCtx = await contextOf("kn-admin", "asg-admin", null);
    const adm = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => adminCtx, fetch: mockAdminFetchNoDelay });
    const cn = (await adm.departments()).items.find((d) => d.code === "chan_nuoi_thuy_san")!;
    await adm.departmentStatus(cn.id, "active");
    await createCntsPeriodApi(apiOf("kn-admin", adminCtx)).install();
    const tinhP = createCntsPeriodApi(apiOf("kn-binh", null));
    const xaP = createCntsPeriodApi(apiOf("kn-an", await contextOf("kn-an", "asg-an-xa", "chan_nuoi_thuy_san")));
    const agg = createCntsAggApi(apiOf("kn-binh", null));

    const s = await tinhP.publish({ code: "CNTS-BM17", from_date: "2026-09-22", to_date: "2026-10-05", units: ["91301"] });
    expect(s.periods.map((p) => p.key)).toEqual(["2026-W39", "2026-W40"]);
    const [w39, w40] = s.periods.map((p) => p.id!);
    const vals = (ca: string) => ({ ca, tom: "0", muc: "0", khac: "0" });
    await xaP.submit(w39!, { unit: "91301", values: vals("2"), base_version: 0, nil_report: false, explanation: "Gửi muộn" });
    await xaP.submit(w40!, { unit: "91301", values: vals("4"), base_version: 0, nil_report: false, explanation: "Gửi muộn" });

    let v = await agg.bm18({ week: "2026-W40" });
    expect(v.catch.tong).toEqual({ prev: "2", cur: "4", qtd: "4", ytd: "6" }); // tuần 40 kết thúc 05/10 → quý IV
    v = await agg.saveAqua("2026-W40", [{ object: "ca_tra", area: "10", output: "3.5" }, { object: "ngoc_trai", area: null, output: "2" }]);
    expect([v.aqua_output.cur, v.total.cur]).toEqual(["3.5", "7.5"]);

    expect(await code(agg.close("CNTS-BM18", "2026-W40"))).toBe("extension.cnts_aggregate_sources_open");
    await tinhP.close(w40!);
    expect((await agg.close("CNTS-BM18", "2026-W40")).state).toBe("closed");
    expect(await code(agg.saveAqua("2026-W40", [{ object: "ca_tra", area: null, output: "1" }]))).toBe("extension.cnts_aggregate_closed");

    const a = await agg.share("CNTS-BM18", "week", "2026-W40");
    expect([a.replayed, a.share.version, a.share.basis]).toEqual([false, 1, "closed"]);
    expect((await agg.share("CNTS-BM18", "week", "2026-W40")).replayed).toBe(true);
    expect((await agg.download(a.share.id)).size).toBeGreaterThan(0);

    const xaAgg = createCntsAggApi(apiOf("kn-an", await contextOf("kn-an", "asg-an-xa", "chan_nuoi_thuy_san")));
    expect(await code(xaAgg.reopen("CNTS-BM18", "2026-W40", "Lý do mở lại đủ dài"))).toBe("forbidden");
    expect((await agg.reopen("CNTS-BM18", "2026-W40", "Bổ sung số cá tra")).state).toBe("reopened");
  });
});
