// Thu thập dữ liệu Cấp Xã (P7 /v1/kn/*) qua client web (src/kn/cycle/api.ts) trên backend giả lập:
// phạm vi xã phụ trách, SĐT che, vô hiệu hóa thay xóa, máy nguyên ≥ 0 không trùng loại, tổng xã = Σ HTX, vật tư + tổng hợp.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiProblem } from "@/api/problem";
import { createCollectApi, RawFile } from "@/kn/cycle/api";
import { materialTemplateCsv } from "@/kn/cycle/screens/MaterialsScreen";
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
  it("FN-XA-03.03 / #67: hồ sơ KN riêng HTX, Nông hộ, THT trong xã phụ trách, SĐT che, thêm + sửa + vô hiệu hóa; Cấp Tỉnh chỉ xem", async () => {
    const xa = await clientFor("kn-an", "asg-an-xa");
    const list = (await xa.records({ kind: "cooperative" })).items;
    expect(new Set(list.map((c) => c.commune_code))).toEqual(new Set(["91301", "91302"]));
    expect(list[0]!.representative?.masked_phone).toMatch(/•••/);
    expect(list[0]!.htx_ref).toBeTruthy(); // hồ sơ demo liên kết HTX đã công bố (chỉ đọc)
    expect(await code(xa.records({ kind: "cooperative", commune: "91304" }))).toBe("403 extension.out_of_scope");
    const c = await xa.createRecord({ kind: "cooperative", commune_code: "91302", name: "HTX Mới Bình Đức", area_ha: "12.5", representative: { full_name: "Nguyễn Văn Tư", phone: "0912000111" } });
    expect(c.representative).toEqual({ label: "Nguyễn V. T.", masked_phone: "0912•••111" });
    expect(c.version).toBe(1);
    expect(await code(xa.createRecord({ kind: "cooperative", commune_code: "91302", name: "htx mới  bình đức", representative: { full_name: "Lê Văn B", phone: "0912000999" } }))).toBe("409 extension.kn_record_name_taken");
    expect(await code(xa.createRecord({ kind: "cooperative", commune_code: "91302", name: "HTX Âm", area_ha: "-1", representative: { full_name: "Lê Văn B", phone: "0912000998" } }))).toBe("422 validation_failed");
    expect(await code(xa.createRecord({ kind: "cooperative", commune_code: "91302", name: "HTX Thiếu" }))).toBe("422 validation_failed");
    expect(await code(xa.createRecord({ kind: "household", commune_code: "91302", parent_ref: c.ref, area_ha: "-2", holder: { full_name: "Lê Thị Năm", phone: "0987000222" } }))).toBe("422 validation_failed");
    const h = await xa.createRecord({ kind: "household", commune_code: "91302", parent_ref: c.ref, holder: { full_name: "Lê Thị Năm", phone: "0987000222" } });
    expect(h.parent_name).toBe("HTX Mới Bình Đức");
    expect(await code(xa.createRecord({ kind: "household", commune_code: "91302", holder: { full_name: "Lê Thị Năm", phone: "0987000222" } }))).toBe("409 extension.kn_record_household_exists");
    expect((await xa.record(c.ref)).member_count).toBe(1);
    const u = await xa.updateRecord(c.ref, { kind: "cooperative", commune_code: "91302", name: "HTX Bình Đức Mới", area_ha: "13", base_version: 1 });
    expect([u.name, u.version, u.representative?.masked_phone]).toEqual(["HTX Bình Đức Mới", 2, "0912•••111"]);
    expect(await code(xa.updateRecord(c.ref, { kind: "cooperative", commune_code: "91302", name: "X", base_version: 1 }))).toBe("409 extension.kn_record_version_conflict");
    expect(await code(xa.setRecordStatus(c.ref, true, { reason: "ngắn", base_version: 2 }))).toBe("422 validation_failed");
    const off = await xa.setRecordStatus(c.ref, true, { reason: "HTX giải thể theo nghị quyết đại hội", base_version: 2 });
    expect(off.status).toBe("inactive");
    expect(off.log.map((e) => e.event)).toEqual(["deactivated", "updated", "created"]);
    expect((await xa.records({ kind: "cooperative", status: "inactive" })).items.map((x) => x.name)).toEqual(["HTX Bình Đức Mới"]);
    expect((await xa.records({ kind: "coop_group" })).items.length).toBeGreaterThan(0);
    expect((await xa.records({ kind: "household", parent_ref: c.ref })).items.map((x) => x.ref)).toEqual([h.ref]);
    const tinh = await clientFor("kn-binh", "asg-binh-tinh");
    expect((await tinh.records({ kind: "cooperative", status: "all" })).items.length).toBeGreaterThan(list.length);
    expect(await code(tinh.createRecord({ kind: "cooperative", commune_code: "91301", name: "X", representative: { full_name: "A B", phone: "0912000777" } }))).toBe("403 forbidden");
  });

  it("#67: liên kết HTX đã công bố chỉ đọc — ứng viên trong phạm vi, khác xã bị chặn", async () => {
    const xa = await clientFor("kn-an", "asg-an-xa");
    const cands = (await xa.htxCandidates("91301")).items;
    expect(cands.length).toBeGreaterThan(0);
    expect(cands.every((x) => x.commune_code === "91301" && x.linked_ref)).toBe(true); // hồ sơ demo đã liên kết
    const other = (await xa.htxCandidates("91302")).items[0]!;
    expect(await code(xa.createRecord({ kind: "cooperative", commune_code: "91301", name: "HTX Liên Kết Sai", htx_ref: other.htx_ref, representative: { full_name: "Lê Văn C", phone: "0912000555" } }))).toBe("422 validation_failed");
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

// BF-TT-MAT (#105): bảng nhập vật tư — danh mục, lọc ngày, Nạp Excel (chỉ đọc), log bản ghi.
describe("vật tư dạng bảng nhập giả lập (TT-12)", () => {
  it("danh mục material khớp sổ; lọc from/to; nạp CSV không ghi; log bản ghi; Cấp Tỉnh không nạp", async () => {
    const xa = await clientFor("kn-an", "asg-an-xa");
    const cat = (await xa.materialCatalog()).items.map((m) => [m.code, m.attrs.unit]);
    expect(cat).toEqual(expect.arrayContaining([["phan_ure", "tấn"], ["bay_den", "cái"]]));
    const [rec] = (await xa.recordMaterials({ commune_code: "91301", recorded_on: "2026-09-30", lines: [{ material_code: "phan_ure", quantity: "2" }] })).items;
    await xa.recordMaterials({ commune_code: "91301", recorded_on: "2026-09-01", lines: [{ material_code: "phan_ure", quantity: "1" }] });
    expect((await xa.materials({ commune: "91301", from: "2026-09-30", to: "2026-09-30" })).items.map((m) => m.quantity)).toEqual(["2"]);
    expect((await xa.materials({ commune: "91301" })).items).toHaveLength(2);
    const log = await xa.materialLog(rec!.id);
    expect(log.items.map((l) => l.event)).toEqual(["recorded"]);
    const csv = ["Vật tư,Số lượng,Thời điểm,Ghi chú", "Phân urê,1.5,30/09/2026,Đợt 1", "bay_den,2.5,,", "khong_co,1,,", ""].join("\n");
    const r = await xa.importMaterials(new RawFile(csv, "text/csv; charset=utf-8"));
    expect([r.accepted, r.rejected]).toEqual([1, 2]);
    expect(r.lines[0]).toMatchObject({ row: 2, material_code: "phan_ure", unit: "tấn", quantity: "1.5", recorded_on: "2026-09-30", note: "Đợt 1", status: "accepted" });
    expect(r.lines[1]).toMatchObject({ material_code: "bay_den", quantity: null, status: "rejected" });
    expect((await xa.materials({ commune: "91301" })).items).toHaveLength(2); // nạp tệp không ghi sổ
    expect(await code(xa.importMaterials(new RawFile(["Ghi chú", "x", ""].join("\n"), "text/csv")))).toBe("422 extension.material_import_invalid");
    const tw = await clientFor("kn-binh", "asg-binh-tinh");
    expect(await code(tw.importMaterials(new RawFile(csv, "text/csv")))).toBe("403 extension.read_only");
  });

  it("Codex P2: tệp mẫu tải xuống nạp lại được — ô \"1,5\" bọc nháy, nhận 1 dòng số lượng 1.5", async () => {
    const xa = await clientFor("kn-an", "asg-an-xa");
    const r = await xa.importMaterials(new RawFile(materialTemplateCsv(), "text/csv; charset=utf-8"));
    expect([r.accepted, r.rejected]).toEqual([1, 0]);
    expect(r.lines[0]).toMatchObject({ row: 2, material_code: "phan_ure", unit: "tấn", quantity: "1.5", recorded_on: "2026-09-30", status: "accepted" });
  });
});

// KN-VN-DATE (#149): ngày vào HTX và "không ở tương lai" của vật tư theo ngày lịch VN (00:00–07:00 VN ngày UTC còn là hôm qua).
describe("thu thập Cấp Xã theo ngày VN", () => {
  afterEach(() => vi.useRealTimers());

  it("Thứ Hai 00:30 VN: vật tư ghi hôm nay hợp lệ, ngày mai bị chặn", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: Date.parse("2026-10-25T17:30:00Z") });
    const xa = await clientFor("kn-an", "asg-an-xa");
    const coop = (await xa.cooperatives({ commune: "91301" })).items[0]!;
    // #67: nông hộ là hồ sơ KN riêng (không còn since_date của HTX); phần ngày VN còn lại là vật tư.
    expect(coop.ref).toBeTruthy();
    const line = [{ material_code: "phan_ure", quantity: "1" }];
    expect(await code(xa.recordMaterials({ commune_code: "91301", recorded_on: "2026-10-26", lines: line }))).toBe("ok");
    expect(await code(xa.recordMaterials({ commune_code: "91301", recorded_on: "2026-10-27", lines: line }))).toBe("422 validation_failed");
  });

  it("Chủ Nhật 23:59 VN: vật tư ghi Thứ Hai còn ở tương lai", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: Date.parse("2026-10-25T16:59:00Z") });
    const xa = await clientFor("kn-an", "asg-an-xa");
    const line = [{ material_code: "phan_ure", quantity: "1" }];
    expect(await code(xa.recordMaterials({ commune_code: "91301", recorded_on: "2026-10-26", lines: line }))).toBe("422 validation_failed");
    expect(await code(xa.recordMaterials({ commune_code: "91301", recorded_on: "2026-10-25", lines: line }))).toBe("ok");
  });
});
