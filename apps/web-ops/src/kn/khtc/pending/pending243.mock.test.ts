import { afterEach, expect, it, vi } from "vitest";
import { createApiClient } from "@/api/client";
import { mockFetchNoDelay } from "@/api/mock/handler";
import { khtcMockStore } from "@/api/mock/kn-khtc-periods";
import { uuidv7 } from "@/api/idempotency";
import { readPending243 } from "./api";

afterEach(()=>vi.useRealTimers());
it("pending243 mock uses latest native identity, preserves read-only Admin rights, pages empty windows and excludes decided/foreign/adhoc/terminal data",async()=>{
  vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2030-01-02T03:00:00Z"));
  const selected=await mockFetchNoDelay(new Request("http://mock/v1/me/context",{method:"POST",headers:{Authorization:"Bearer mock.kn-admin","Content-Type":"application/json","Idempotency-Key":"pending243-context"},body:JSON.stringify({assignment_id:"asg-admin",department_code:null})}));
  expect(selected.status).toBe(201);const context=await selected.json() as {context_id:string};
  const api=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.kn-admin",getContextId:()=>context.context_id,fetch:mockFetchNoDelay});
  const store=khtcMockStore(),original=structuredClone(store),sample=store.find(s=>s.p.report_type==="week")!;
  try{
    const initial=await readPending243(api,null,new AbortController().signal);
    const target=initial.rows.find(r=>r.department==="trong_trot")!;expect(target).toBeDefined();
    const sourceBefore=structuredClone(store);
    const denied=await api.POST("/v1/extension/khtc/periods/{id}/forms/{dept}/review",{params:{path:{id:target.period_id,dept:target.department}},
      headers:{"Idempotency-Key":uuidv7()},body:{decision:"accepted",note:"",expected_submission_id:target.submission_id,base_attempt:target.attempt}});
    expect(denied.response.status).toBe(403);
    expect((await readPending243(api,null,new AbortController().signal)).rows.some(r=>r.submission_id===target.submission_id)).toBe(true);
    expect(store).toEqual(sourceBefore);
    // Isolate the page fixtures below from the older seeded pending period.
    for(const entry of store) entry.terminal="closed";
    const pending=structuredClone(sample);pending.p.id=uuidv7(Date.now()+1);pending.terminal="";pending.p.forms_status="open";
    pending.submitted={trong_trot:{attempt:2,decision:"",at:"2030-01-01T02:00:00Z"}};store.push(pending);
    for(let i=0;i<26;i++){const empty=structuredClone(sample);empty.p.id=uuidv7(Date.now()+2+i);empty.terminal="";empty.p.forms_status="open";empty.submitted={};store.push(empty);}
    for(const kind of ["closed","cancelled","adhoc","foreign","accepted","returned"]){
      const hidden=structuredClone(pending);hidden.p.id=uuidv7(Date.now()+100+store.length);
      if(kind==="closed"||kind==="cancelled")hidden.terminal=kind;
      if(kind==="adhoc")hidden.p.report_type="adhoc";
      if(kind==="foreign")hidden.adhocTenant="different-tenant243";
      if(kind==="accepted"||kind==="returned")hidden.submitted.trong_trot!.decision=kind;
      store.push(hidden);
    }
    const before=structuredClone(store),first=await readPending243(api,null,new AbortController().signal);
    expect(first.rows).toEqual([]);expect(first.next_cursor).not.toBeNull();
    const last=await readPending243(api,first.next_cursor,new AbortController().signal);
    expect(last.rows).toHaveLength(1);expect(last.rows[0]!.period_id).toBe(pending.p.id);expect(last.rows[0]!.attempt).toBe(2);
    expect(last.next_cursor).toBeNull();expect(store).toEqual(before);
  }finally{store.splice(0,store.length,...original);}
});
it("pending243 mock requires current context and reports a denial rather than a fabricated empty page",async()=>{
  const response=await mockFetchNoDelay(new Request("http://mock/v1/extension/khtc/pending-review",{headers:{Authorization:"Bearer mock.kn-admin"}}));
  expect(response.status).toBe(403);expect(response.headers.get("Cache-Control")).toBe("no-store");expect(await response.json()).not.toHaveProperty("rows");
});
