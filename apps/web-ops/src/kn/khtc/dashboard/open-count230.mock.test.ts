import { afterEach, expect, it, vi } from "vitest";
import { createApiClient, unwrap } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { khtcMockStore } from "@/api/mock/kn-khtc-periods";
import { uuidv7 } from "@/api/idempotency";
import { createDashboardApi } from "./api";

afterEach(() => vi.useRealTimers());
it("open230 mock counts more than100 across years without modifying data; dashboard and no-store route agree", async () => {
 vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2030-01-02T03:00:00Z"));
 const selected=await mockFetchNoDelay(new Request("http://mock/v1/me/context",{method:"POST",headers:{Authorization:"Bearer mock.kn-admin","Content-Type":"application/json","Idempotency-Key":"open-count230-context"},body:JSON.stringify({assignment_id:"asg-admin",department_code:null})}));
 expect(selected.status).toBe(201);const context=await selected.json() as {context_id:string};
 const api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.kn-admin",getContextId:()=>context.context_id,fetch:mockFetchNoDelay});
 const route="/v1/extension/khtc/open-period-count";
 const baseline=await unwrap(api.GET(route));
 const store=khtcMockStore(),original=structuredClone(store),sample=store.find(s=>s.p.report_type==="week")!;
 expect(sample).toBeDefined();
 try{
  for(let i=0;i<110;i++){
   const row=structuredClone(sample);row.p.id=uuidv7();row.p.year=2020+i%3;row.submitted={};row.terminal=i===0?"closed":i===1?"cancelled":"";store.push(row);
  }
  const foreign=structuredClone(sample);foreign.p.id=uuidv7();foreign.terminal="";foreign.adhocTenant="different-tenant-open230";store.push(foreign);
  const before=structuredClone(store),count=await unwrap(api.GET(route));
  expect(count.open_count).toBe(baseline.open_count+108);
  const dashboard=await createDashboardApi(api).get({year:2026,quarter:4,department:"no_matching_department"});
  expect(dashboard.counts.open).toBe(0);expect(dashboard.counts.open_all).toBe(count.open_count);
  expect(store).toEqual(before);
  const response=await mockFetchNoDelay(new Request("http://mock"+route,{headers:{Authorization:"Bearer mock.kn-admin","X-AG-Context":context.context_id}}));
  expect(response.status).toBe(200);expect(response.headers.get("Cache-Control")).toBe("no-store");
 }finally{store.splice(0,store.length,...original);}
});
it("open230 mock denies requests without a selected context and never returns a false zero",async()=>{
 const response=await mockFetchNoDelay(new Request("http://mock/v1/extension/khtc/open-period-count",{headers:{Authorization:"Bearer mock.kn-admin"}}));
 expect(response.status).toBe(403);expect(response.headers.get("Cache-Control")).toBe("no-store");expect(await response.json()).not.toHaveProperty("open_count");
});
