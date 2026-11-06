import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { khtcPNProgressRoutes215 } from "@/api/mock/kn-khtc-pn-progress215";
import { findPersona } from "@/api/mock/personas";
import { createKhtcPeriodApi } from "../periods/api";
import { createReportApi } from "../reports/api";
import { createPNProgressApi } from "./api";

let context = "", key = 0;
const legacy = () => createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.tinh", getContextId: () => null, fetch: mockFetchNoDelay });
const client = () => createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => context, fetch: mockFetchNoDelay });
beforeEach(async () => {
  vi.useFakeTimers({ toFake: ["Date"] }); vi.setSystemTime(new Date("2030-06-05T03:00:00Z"));
  const response = await mockFetchNoDelay(new Request("http://mock/v1/me/context", { method: "POST", headers: { Authorization: "Bearer mock.kn-admin", "Content-Type": "application/json", "Idempotency-Key": `pn215-context-${++key}` }, body: JSON.stringify({ assignment_id: "asg-admin", department_code: null }) }));
  expect(response.status).toBe(201); context = ((await response.json()) as { context_id: string }).context_id;
});
afterEach(() => vi.useRealTimers());
async function open(year: number) {
  return createKhtcPeriodApi(legacy()).open({ report_type: "program", year, as_of_date: `${year}-06-05`, due_at: `${year}-06-07T03:00:00Z`, departments: ["trong_trot", "chan_nuoi_thuy_san"] });
}
it("mock read never prepares an owner draft and exposes no export without a real saved M5", async () => {
  const p = await open(2031), pn = createPNProgressApi(client());
  expect((await pn.periods("trong_trot", "2031")).items.map(x => x.id)).toContain(p.id);
  expect(await pn.get("trong_trot", p.id, "", "")).toMatchObject({ status: "not_prepared", snapshot: null, rows: [] });
  expect((await createReportApi(legacy()).get(p.id)).version).toBe(0);
  await expect(pn.excel("trong_trot", p.id, "01900000-0000-7000-8000-000000000215", "", "")).rejects.toMatchObject({ status: 404 });
});
it("mock uses the actual owner's M5 values, pins the observed draft and produces an XLSX projection", async () => {
  const p = await open(2032), reports = createReportApi(legacy()), pn = createPNProgressApi(client());
  const d = await reports.save(p.id, { base_version: 0, admin: {}, texts: { I: "PRIVATE OWNER NARRATIVE" } });
  const displayed = await pn.get("trong_trot", p.id, "", "");
  expect(displayed.snapshot).toMatchObject({ id: d.draft_id, version: d.version });
  const own = d.content.data.rows.filter(r => r.kind === "item" && r.department === "trong_trot");
  expect(own.length).toBeGreaterThan(0);
  expect(displayed.rows.filter(r => r.kind === "item").map(r => [r.ref, r.planned, r.year_to_date, r.percent])).toEqual(own.map(r => [r.ref, r.planned, r.year_to_date, r.percent]));
  expect(JSON.stringify(displayed)).not.toContain("PRIVATE OWNER NARRATIVE");
  expect(displayed.rows.every(r => r.kind !== "item" || r.department === "trong_trot")).toBe(true);
  const next = await reports.save(p.id, { base_version: d.version, admin: {}, texts: { I: "OWNER ADVANCED" } });
  expect((await pn.get("trong_trot", p.id, "", "")).snapshot?.id).toBe(next.draft_id);
  expect(await pn.get("trong_trot", p.id, "", "", displayed.snapshot!.id)).toEqual(displayed);
  const file = await pn.excel("trong_trot", p.id, displayed.snapshot!.id, "", ""), bytes = new Uint8Array(await file.blob.arrayBuffer()), xml = new TextDecoder().decode(bytes);
  expect(Array.from(bytes.slice(0, 4))).toEqual([80, 75, 3, 4]);
  for (const part of ["[Content_Types].xml", "xl/workbook.xml", "xl/worksheets/sheet1.xml", "Tiến độ phòng"]) expect(xml).toContain(part);
  for (const row of own) expect(xml).toContain(row.name.replaceAll("&", "&amp;"));
  expect(xml).not.toContain("PRIVATE OWNER NARRATIVE");
});
it("mock rejects missing context, malformed numeric year and out-of-scope program", async () => {
  const p = await open(2033), pn = createPNProgressApi(client());
  await createReportApi(legacy()).save(p.id, { base_version: 0, admin: {}, texts: {} });
  await expect(pn.get("trong_trot", p.id, "01900000-0000-7000-8000-000000000215", "")).rejects.toMatchObject({ status: 403 });
  for (const raw of ["1e100000", "1e-100000", "9".repeat(100000)]) {
    const r = await mockFetchNoDelay(new Request(`http://mock/v1/extension/khtc/departments/trong_trot/program-progress/periods?year=${raw}`, { headers: { Authorization: "Bearer mock.kn-admin", "X-AG-Context": context } })); expect(r.status).toBe(422);
  }
  const denied = await mockFetchNoDelay(new Request(`http://mock/v1/extension/khtc/departments/trong_trot/program-progress/${p.id}`, { headers: { Authorization: "Bearer mock.tinh" } })); expect(denied.status).toBe(403);
});
it("mock's read projection stays tenant-bound even if a caller knows another period ID", async () => {
  const p = await open(2034);
  const url = new URL(`http://mock/v1/extension/khtc/departments/trong_trot/program-progress/${p.id}`);
  const persona = structuredClone(findPersona("kn-admin")!); persona.me.tenant_id = "01900000-0000-7000-8000-000000000999";
  const route = khtcPNProgressRoutes215[0]!;
  const response = route.handler({ url, persona, params: [], body: null, request: new Request(url, { headers: { "X-AG-Context": context } }) });
  expect(response.status).toBe(404);
});
