import { afterEach, describe, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { khtcMockStore } from "@/api/mock/kn-khtc-periods";
import { createDashboardApi } from "./api";

afterEach(() => vi.useRealTimers());
describe("dashboard mock submission counts", () => {
 it("counts submitted/confirmed and excludes returned forms while retaining timestamps", async () => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-10-14T03:00:00Z"));
  const response = await mockFetchNoDelay(new Request("http://mock/v1/me/context", {
   method: "POST", headers: { Authorization: "Bearer mock.kn-admin", "Content-Type": "application/json", "Idempotency-Key": "dashboard-count-context" },
   body: JSON.stringify({ assignment_id: "asg-admin", department_code: null }),
  }));
  expect(response.status).toBe(201);
  const context = await response.json() as { context_id: string };
  const api = createDashboardApi(createApiClient({ baseUrl: "http://mock", getAccessToken: () => "mock.kn-admin", getContextId: () => context.context_id, fetch: mockFetchNoDelay }));
  const stored = khtcMockStore().find(s => s.p.report_type === "week" && s.p.year === 2026 && !s.terminal)!;
  expect(stored).toBeDefined();
  const dept = stored.p.departments[0]!.code, previous = stored.submitted[dept];
  try {
   for (const decision of ["", "accepted", "returned"] as const) {
    stored.submitted[dept] = { attempt: 1, decision, at: "2026-10-13T02:00:00Z" };
    const quarter = Math.floor((Number(stored.p.from_date.slice(5, 7)) - 1) / 3) + 1;
    const d = await api.get({ year: 2026, quarter });
    const row = d.status.find(s => s.period_id === stored.p.id && s.department === dept && s.kind === "form")!;
    expect(row.state).toBe(decision === "returned" ? "supplement" : decision === "accepted" ? "confirmed" : "submitted");
    expect(row.at).toBe("2026-10-13T02:00:00Z");
    expect(d.counts.sent).toBe(d.status.filter(s => s.kind === "form" && ["submitted", "confirmed"].includes(s.state)).length);
   }
  } finally { if (previous) stored.submitted[dept] = previous; else delete stored.submitted[dept]; }
 });
});
it("dashboard189 includes over 100 authorized adhoc periods and keeps department/tenant filters", async () => {
 vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2026-10-14T03:00:00Z"));
 const response=await mockFetchNoDelay(new Request("http://mock/v1/me/context",{method:"POST",headers:{Authorization:"Bearer mock.kn-admin","Content-Type":"application/json","Idempotency-Key":"dashboard189-context"},body:JSON.stringify({assignment_id:"asg-admin",department_code:null})}));
 expect(response.status).toBe(201);const context=await response.json() as {context_id:string};
 const api=createDashboardApi(createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.kn-admin",getContextId:()=>context.context_id,fetch:mockFetchNoDelay}));
 const store=khtcMockStore(),baseline=await api.get({year:2026,quarter:4});
 const original=store.find(s=>s.p.report_type==="week"&&s.p.year===2026&&!s.terminal)!;
 const inserted=[] as typeof store;
 for(let i=0;i<105;i++){
  const copy=structuredClone(original);copy.p.id=`ffffffff-ffff-7000-8000-${String(i).padStart(12,"0")}`;copy.p.report_type="adhoc";copy.p.name=`Dashboard189 ${i}`;copy.p.from_date="2026-10-01";copy.p.to_date="2026-10-13";copy.p.departments=[{code:"trong_trot",name:"Trồng trọt"}];copy.submitted={};copy.adhocTenant="01927b3e-0000-7000-8000-00000000a001";inserted.push(copy);
 }
 const foreign=structuredClone(inserted[0]!);foreign.p.id="ffffffff-ffff-7000-8000-999999999999";foreign.adhocTenant="01927b3e-0000-7000-8000-00000000ffff";inserted.push(foreign);
 store.push(...inserted);
 try{
  const d=await api.get({year:2026,quarter:4});expect(d.counts.open).toBe(baseline.counts.open+105);expect(d.counts.forms).toBe(baseline.counts.forms+105);
  const fresh=d.status.filter(s=>s.period_name.startsWith("Dashboard189"));expect(fresh).toHaveLength(105);expect(new Set(fresh.map(s=>s.period_id)).size).toBe(105);expect(fresh.some(s=>s.period_id===foreign.p.id)).toBe(false);
  const filtered=await api.get({year:2026,quarter:4,department:"hanh_chinh_tong_hop"});expect(filtered.status.some(s=>s.period_name.startsWith("Dashboard189"))).toBe(false);
 }finally{for(const row of inserted){const index=store.indexOf(row);if(index>=0)store.splice(index,1);}}
});
