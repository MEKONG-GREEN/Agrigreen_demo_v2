import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { resetKnAdmin } from "@/api/mock/kn-admin";
import { resetCntsOutbreaks } from "@/api/mock/kn-cnts-outbreak";
import { ApiProblem } from "@/api/problem";
import { createKnAdminApi } from "@/kn/cycle/api";
import { createOutbreakApi, emptyForm, formErrors, formOf, payload, type OutbreakForm } from "./api";

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

function client(personaId: string, ctx: string | null) {
  return createOutbreakApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${personaId}`, getContextId: () => ctx, fetch: mockFetchNoDelay }));
}

const code = async (p: Promise<unknown>) => p.then(() => "ok", (e: unknown) => (e instanceof ApiProblem ? e.problem.code : String(e)));

const filled = (patch: Partial<OutbreakForm> = {}): OutbreakForm => ({
  ...emptyForm("91301"), disease_code: "ASF", species_code: "HEO", received_at: "2026-10-12T08:30", source: "can_bo", locality: "Ấp Hòa Thuận A",
  lat: "10.25341", lon: "105.9721", accuracy_m: "8", herd_size: "120", dead: "5", symptoms: "Sốt cao, bỏ ăn", ...patch,
});

describe("Báo cáo dịch bệnh BM-10 — hàm phía máy khách", () => {
  it("quy tắc chặn UR-XA-10 trước khi gửi", () => {
    expect(formErrors(filled())).toEqual({});
    expect(formErrors(filled({ lat: "", lon: "" })).location).toMatch(/DB-05/);
    expect(formErrors(filled({ dead: "121" })).dead).toMatch(/DB-07 > DB-06/);
    expect(formErrors(filled({ culled_head: "200" })).culled_head).toMatch(/DB-10 > DB-06/);
    expect(formErrors(filled({ lat: "0", lon: "0" })).location).toMatch(/ngoài lãnh thổ/);
    expect(formErrors(filled({ culled_kg: "1,5" })).culled_kg).toBeUndefined(); // dấu phẩy thập phân vi-VN
    expect(formErrors(filled({ culled_kg: "1,23456" })).culled_kg).toBe("Tối đa 3 chữ số thập phân.");
    expect(formErrors(filled({ herd_size: "0" })).herd_size).toBe("Phải lớn hơn 0.");
    expect(formErrors(filled({ herd_size: "-5" })).herd_size).toBe("Không được nhập số âm.");
    expect(formErrors(filled({ dead: "-1" })).dead).toBe("Không được nhập số âm.");
    expect(formErrors(filled({ herd_age_days: "1,5" })).herd_age_days).toBe("Chỉ nhập số nguyên.");
    expect(formErrors(filled({ chemical_liters: "-2" })).chemical_liters).toBe("Không được nhập số âm.");
    expect(payload(filled({ culled_kg: "1.234,5" })).culled_kg).toBe("1234.5");
    const p = payload(filled({ culled_head: "", chemical_liters: "20.5" }), 3);
    expect(p).toMatchObject({ base_version: 3, herd_size: 120, dead: 5, culled_head: null, chemical_liters: "20.5", location: { lat: "10.25341", source: "gps" } });
  });
});

describe("Báo cáo dịch bệnh BM-10 — backend giả lập /v1/extension/cnts/outbreaks*, /suspects*", () => {
  beforeEach(() => {
    resetKnAdmin();
    resetCntsOutbreaks();
  });

  it("Xã lập, cập nhật diễn biến (khóa lạc quan); Tỉnh xem, kết thúc; tin báo xác nhận thành ổ dịch", async () => {
    const adminCtx = await contextOf("kn-admin", "asg-admin", null);
    const adm = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => adminCtx, fetch: mockAdminFetchNoDelay });
    const cn = (await adm.departments()).items.find((d) => d.code === "chan_nuoi_thuy_san")!;
    await adm.departmentStatus(cn.id, "active");
    const xa = client("kn-an", await contextOf("kn-an", "asg-an-xa", "chan_nuoi_thuy_san"));
    const tinh = client("kn-binh", null);

    expect(await code(xa.report(payload(filled({ lat: "", lon: "" }))))).toBe("extension.cnts_outbreak_invalid");
    expect(await code(tinh.report(payload(filled())))).toBe("forbidden");
    const o = await xa.report(payload(filled()));
    expect(o).toMatchObject({ status: "reported", version: 1, disease_name: "Dịch tả heo Châu Phi", as_of: "2026-10-12" });

    const up = payload({ ...formOf(o), dead: "30", progress_note: "Thêm 25 con chết" }, 1);
    const v2 = await xa.progress(o.ref, up);
    expect(v2).toMatchObject({ version: 2, status: "monitoring", dead: 30 });
    expect(await code(xa.progress(o.ref, up))).toBe("extension.cnts_outbreak_version_conflict");

    const list = await tinh.list({ status: "monitoring" });
    expect(list.items.map((x) => x.ref)).toEqual([o.ref]);
    expect(list.by_status.find((c) => c.key === "monitoring")?.count).toBe(1);
    expect(await code(xa.close(o.ref, 2, "Hết dịch"))).toBe("forbidden");
    expect((await tinh.close(o.ref, 2, "Hết 21 ngày")).status).toBe("closed");
    expect(await code(xa.progress(o.ref, payload(formOf(v2), 3)))).toBe("extension.cnts_outbreak_closed");
    expect((await tinh.get(o.ref)).versions.map((v) => v.event)).toEqual(["closed", "updated", "reported"]);

    const s = await xa.reportSuspect({ commune_code: "91301", reporter_kind: "nong_ho", observed_at: "2026-10-13T00:00:00Z", locality: "Ấp 3",
      description: "Heo bỏ ăn", location: { lat: "10.1", lon: "105.8" } });
    const fromS = await xa.confirm(s.ref, { ...payload(filled({ lat: "", lon: "" })), base_version: 1 } as never);
    expect(fromS.suspect_ref).toBe(s.ref);
    expect(fromS.location?.lat).toBe("10.1");
    expect((await xa.suspects()).find((x) => x.ref === s.ref)?.status).toBe("confirmed");
    expect(await code(xa.dismiss(s.ref, 2, "x"))).toBe("extension.cnts_suspect_resolved");
  });
});
