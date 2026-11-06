import { beforeEach, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { createPNProgressApi, pnYear, type PNProgress } from "./api";

const A = "01900000-0000-7000-8000-000000000215";
const B = "01900000-0000-7000-8000-000000000216";
const period = { id: A, name: "Kỳ Mẫu 5", year: 2030, from: "2030-01-01T00:00:00Z", to: "2030-06-04T00:00:00Z", cutoff: "2030-06-04T00:00:00Z" };
let body: unknown;
const fetcher = vi.fn(async (input: RequestInfo | URL) => { void input; return Response.json(body); });
const api = () => createPNProgressApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => null, fetch: fetcher }));
const view = (): PNProgress => ({ department: "trong_trot", period, status: "snapshot", snapshot: { id: B, version: 2, captured_at: "2030-06-05T03:00:00Z" }, programs: [], rows: [] });
beforeEach(() => { fetcher.mockClear(); body = view(); });

it.each(["1e100000", "1e-100000", "9".repeat(100000), "1999", "2101", "2030.0", " 2030"].map((raw, index) => ({ raw, index })))("rejects hostile or out-of-range year before HTTP ($index)", async ({ raw }) => {
  expect(() => pnYear(raw)).toThrow();
  await expect(api().periods("trong_trot", raw)).rejects.toMatchObject({ status: 422 });
  expect(fetcher).not.toHaveBeenCalled();
});
it("accepts both year boundaries and binds a page to department, year and advancing cursor", async () => {
  expect(pnYear("2000")).toBe(2000); expect(pnYear("2100")).toBe(2100);
  body = { department: "trong_trot", items: [period], has_more: true, next_after: A };
  const client = api(); expect((await client.periods("trong_trot", "2030")).items).toEqual([period]);
  await expect(client.periods("trong_trot", "2030", A)).rejects.toMatchObject({ status: 502 });
  body = { department: "chan_nuoi_thuy_san", items: [period], has_more: false, next_after: null };
  await expect(client.periods("trong_trot", "2030")).rejects.toMatchObject({ status: 502 });
  body = { department: "trong_trot", items: [{ ...period, year: 2029 }], has_more: false, next_after: null };
  await expect(client.periods("trong_trot", "2030")).rejects.toMatchObject({ status: 502 });
});
it.each(["department", "period", "snapshot", "missing_source", "foreign_item", "unprepared_data"])("rejects a mismatched response (%s)", async kind => {
  const data = view();
  if (kind === "department") data.department = "chan_nuoi_thuy_san";
  if (kind === "period") data.period = { ...period, id: B };
  if (kind === "snapshot") data.snapshot!.id = A;
  if (kind === "missing_source") data.snapshot = null;
  if (kind === "foreign_item") data.rows = [{ ref: B, parent_ref: null, kind: "item", roman: "", name: "FOREIGN", department: "chan_nuoi_thuy_san", unit: "ha", planned: "1", year_to_date: "1", percent: "100", level: "achieved", missing: false }];
  if (kind === "unprepared_data") data.status = "not_prepared";
  body = data;
  await expect(api().get("trong_trot", A, "", "", B)).rejects.toMatchObject({ status: 502 });
});
it("pins both filters and Excel to the displayed snapshot without a write", async () => {
  await api().get("trong_trot", A, A, "slow", B);
  let request = fetcher.mock.calls.at(-1)![0] as unknown as Request;
  expect(request.method).toBe("GET");
  expect(new URL(request.url).searchParams.toString()).toBe(`program=${A}&level=slow&snapshot_id=${B}`);
  fetcher.mockImplementationOnce(async () => new Response("excel", { headers: { "Content-Type": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "Content-Disposition": "attachment; filename=phong.xlsx" } }));
  expect((await api().excel("trong_trot", A, B, A, "slow")).name).toBe("phong.xlsx");
  request = fetcher.mock.calls.at(-1)![0] as unknown as Request;
  expect(request.method).toBe("GET");
  expect(new URL(request.url).searchParams.get("snapshot_id")).toBe(B);
});
