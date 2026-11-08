import { beforeEach, describe, expect, it } from "vitest";
import { createApiClient } from "@/api/client";
import { mockAdminFetchNoDelay, mockFetchNoDelay } from "@/api/mock/handler";
import { createKnAdminApi } from "@/kn/cycle/api";
import { resetKnAdmin } from "@/api/mock/kn-admin";
import { previewDesignRows } from "@/api/mock/kn-cnts-design";
import { DESIGN_HEADERS, createDesignApi, designFieldErrors, designFileError } from "./api";
async function context(persona: string, assignment: string, department: string | null) {
  const r = await mockFetchNoDelay(new Request("http://mock/v1/me/context", { method: "POST", headers: { Authorization: `Bearer mock.${persona}`, "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() }, body: JSON.stringify({ assignment_id: assignment, department_code: department }) })); if (!r.ok) throw new Error(`Context failed: ${r.status}`); return (await r.json()).context_id as string;
}
describe("FN07 phần xem/kiểm cấu trúc", () => {
  beforeEach(() => resetKnAdmin());
  it("kiểm tệp rỗng, quá dung lượng và đuôi xls", () => {
    expect(designFileError({ name: "a.xlsx", size: 100 })).toBeNull(); expect(designFileError({ name: "a.xls", size: 100 })).toBeTruthy(); expect(designFileError({ name: "a.csv", size: 0 })).toBeTruthy(); expect(designFileError({ name: "a.xlsx", size: 2097153 })).toBeTruthy();
  });
  it("kiểm header, lỗi dòng, không trả trường có thể lưu khi lỗi; chặn số mũ/chuỗi dài ở cờ", () => {
    const valid = ["a", "Số con", "integer", "true", "con", "Đàn", "true", ""];
    expect(previewDesignRows([DESIGN_HEADERS, valid]).valid).toBe(true);
    expect(previewDesignRows([["bad"], valid]).issues[0]?.line).toBe(1);
    for (const value of ["1e100000", "1e-100000", "9".repeat(100000), "99999999999999999999999999999"]) { for (const col of [3, 6]) { const row = [...valid]; row[col] = value; const r = previewDesignRows([DESIGN_HEADERS, row]); expect(r.valid).toBe(false); expect(r.fields).toEqual([]); expect(r.issues[0]?.line).toBe(2); } }
    expect(previewDesignRows([DESIGN_HEADERS, valid, valid]).valid).toBe(false);
  });
  it("Admin xem cấu trúc, kiểm CSV không ghi; phiên Xã không được xem", async () => {
    const admin = await context("kn-admin", "asg-admin", null);
    const controls = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => admin, fetch: mockAdminFetchNoDelay });
    const department = (await controls.departments()).items.find(x => x.code === "chan_nuoi_thuy_san")!;
    await controls.departmentStatus(department.id, "active");
    const make = (persona: string, ctx: string) => createDesignApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => `mock.${persona}`, getContextId: () => ctx, fetch: mockFetchNoDelay }));
    const api = make("kn-admin", admin); const before = await api.list(); expect(before).toHaveLength(5);
    const raw = new TextEncoder().encode(DESIGN_HEADERS.join(",") + "\na,Số con,integer,true,con,Đàn,true,\n");
    expect((await api.preview(raw)).valid).toBe(true); expect(await api.list()).toEqual(before);
    const bad = new TextEncoder().encode("bad,header\na,b\n"); const result = await api.preview(bad); expect(result.valid).toBe(false); expect(result.fields).toEqual([]);
    // Phiên Xã CNTS thật được chọn thành công: 403 phải do quyền thiết kế, không phải phòng chưa mở/thiếu phiên.
    const xa = await context("kn-an", "asg-an-xa", "chan_nuoi_thuy_san"); await expect(make("kn-an", xa).list()).rejects.toMatchObject({ problem: { status: 403 } }); await expect(make("kn-an", xa).preview(raw)).rejects.toMatchObject({ problem: { status: 403 } });
  });
  it("#192: kiểm chỉ thêm chỉ tiêu phía máy khách", () => {
    const cur = [{ key: "ca", label: "Cá", type: "decimal" as const, required: true }];
    expect(designFieldErrors([...cur, { key: "cua", label: "Cua", type: "decimal", required: false }], cur)).toEqual([]);
    expect(designFieldErrors([{ key: "tom", label: "Tôm", type: "decimal", required: false }], cur)[0]).toMatch(/Không xóa/);
    expect(designFieldErrors([{ ...cur[0]!, type: "integer" }], cur)[0]).toMatch(/Không đổi kiểu/);
    expect(designFieldErrors([{ key: "a", label: "A", type: "choice", required: false }])).toHaveLength(1);
    expect(designFieldErrors([])).toHaveLength(1);
  });
  it("#192: Tỉnh CNTS lưu phiên bản BM-17, tạo biểu điều tra; KNQG 403; base cũ 409", async () => {
    const admin = await context("kn-admin", "asg-admin", null);
    const controls = createKnAdminApi({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => admin, fetch: mockAdminFetchNoDelay });
    const department = (await controls.departments()).items.find(x => x.code === "chan_nuoi_thuy_san")!;
    if (department.status !== "active") await controls.departmentStatus(department.id, "active");
    const ctx = await context("kn-cnts", "asg-cnts-tinh", "chan_nuoi_thuy_san");
    const api = createDesignApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-cnts", getContextId: () => ctx, fetch: mockFetchNoDelay }));
    const items = await api.list(); const bm17 = items.find(x => x.code === "CNTS-BM17")!; const bm04 = items.find(x => x.code === "CNTS-BM04")!;
    expect(bm17.can_edit).toBe(true); expect(bm04).toMatchObject({ locked: true, can_edit: false });
    const fields = [...bm17.fields, { key: "cua", label: "Cua", type: "decimal" as const, required: false }];
    const base = bm17.version ?? 0;
    const saved = await api.save("CNTS-BM17", base, "BM-17 có cua", fields); expect(saved.version.version).toBe(base + 1);
    await expect(api.save("CNTS-BM17", base, "cũ", fields)).rejects.toMatchObject({ problem: { status: 409 } });
    await expect(api.save("CNTS-BM04", bm04.version ?? 0, "x", bm04.fields)).rejects.toMatchObject({ problem: { status: 403 } });
    const copy = await api.create({ code_suffix: "tongdan", title: "Điều tra tổng đàn", source_code: "CNTS-BM17", source_version: saved.version.version });
    expect(copy).toMatchObject({ code: "CNTS-DT-TONGDAN", version: { version: 1 } });
    expect((await api.list()).find(x => x.code === "CNTS-DT-TONGDAN")).toMatchObject({ kind: "survey", can_edit: true });
    expect((await api.history("CNTS-BM17"))[0]?.version).toBe(base + 1);
  });
});
