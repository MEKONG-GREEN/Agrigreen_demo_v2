import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const storageKey="ag.kn.khtc.initial225.v1";
async function fixture225(){
 const {mockPlanWriter225}=await import("@/kn/khtc/plans/mock-plan-fixture225");
 const {createDraftApi225}=await import("@/kn/khtc/plans/draft-api225");
 const {createKhtcApi,blankEntry,toSave}=await import("@/kn/khtc/api");
 const client=await mockPlanWriter225();return {client,draft:createDraftApi225(client),plan:createKhtcApi(client),blankEntry,toSave};
}
beforeEach(()=>{vi.resetModules();window.sessionStorage.clear();vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2035-06-01T03:00:00Z"));});
afterEach(()=>{vi.restoreAllMocks();vi.useRealTimers();});

describe("initial225 mock persistence and immutable recovery",()=>{
 it("reloads the real saved tree, pinned catalog/program refs and log, then saves another revision",async()=>{
  const f=await fixture225();const program=await f.plan.createProgram({code:"P225",name:"Pinned program225",kind:"program"});
  const row={...f.blankEntry("programs","program",null,1),program_ref:program.ref,name:""};
  const body={expected_revision:0,entries:f.toSave([row],{base_version:0}).entries};const ack=await f.draft.save(2041,body,"persist225-create");
  await f.plan.reviseProgram(program.ref,{code:"P225",name:"Renamed live program225",kind:"program"});
  // Receipt replay is exact even after the live master changes.
  expect((await f.draft.save(2041,body,"persist225-create")).event_id).toBe(ack.event_id);
  vi.resetModules();const restored=await fixture225();const current=await restored.draft.current(2041);
  expect(current.draft?.draft_id).toBe(ack.draft_id);expect(current.revision).toBe(1);expect(current.draft?.entries_hash).toBe(ack.entries_hash);
  expect(current.draft?.entries[0]?.name).toBe("Pinned program225");expect((await restored.plan.programs()).items.some(x=>x.ref===program.ref&&x.name==="Renamed live program225")).toBe(true);
  expect((await restored.draft.history(2041)).items.map(x=>x.event_id)).toEqual([ack.event_id]);
  const saved=await restored.draft.save(2041,{draft_id:ack.draft_id,expected_revision:1,entries:body.entries},"persist225-save2");expect(saved.revision).toBe(2);
  // Old generation/revision is never silently promoted to the latest state.
  await expect(restored.draft.discard(2041,{draft_id:ack.draft_id,expected_revision:1},"persist225-stale-discard")).rejects.toMatchObject({status:409});
 });
 it("does not ACK or mutate RAM when storage rejects a candidate",async()=>{
  const f=await fixture225();const ack=await f.draft.save(2042,{expected_revision:0,entries:[]},"quota225-first");const before=window.sessionStorage.getItem(storageKey);
  const fail=vi.spyOn(Storage.prototype,"setItem").mockImplementation(()=>{throw new DOMException("Unavailable","QuotaExceededError");});
  await expect(f.draft.save(2042,{draft_id:ack.draft_id,expected_revision:1,entries:[]},"quota225-second")).rejects.toMatchObject({status:503,code:"extension.khtc_draft_storage_unavailable"});
  fail.mockRestore();expect(window.sessionStorage.getItem(storageKey)===before).toBe(true);expect((await f.draft.current(2042)).revision).toBe(1);
  expect((await f.draft.save(2042,{draft_id:ack.draft_id,expected_revision:1,entries:[]},"quota225-second")).revision).toBe(2);
 });
 for(const damaged of ["invalid-json","unknown-version","missing-envelope"]){it(`fails closed for ${damaged} instead of resetting the active year fence`,async()=>{
  const f=await fixture225();await f.draft.save(2043,{expected_revision:0,entries:[]},"damage225-first");const before=window.sessionStorage.getItem(storageKey)!;
  if(damaged==="missing-envelope")window.sessionStorage.removeItem(storageKey);else window.sessionStorage.setItem(storageKey,damaged==="invalid-json"?"{":JSON.stringify({version:99}));
  await expect(f.draft.current(2043)).rejects.toMatchObject({status:503});
  await expect(f.plan.save(2043,{base_version:0,entries:[],effective_from:"2043-01-01"})).rejects.toMatchObject({status:503});
  window.sessionStorage.setItem(storageKey,before);expect((await f.draft.current(2043)).revision).toBe(1);
  await expect(f.plan.save(2043,{base_version:0,entries:[],effective_from:"2043-01-01"})).rejects.toMatchObject({status:409,code:"extension.khtc_initial_draft_active"});
 });}
 it("fails closed when reading storage is unavailable, including a cached operation ACK",async()=>{
  const f=await fixture225();const body={expected_revision:0,entries:[]};await f.draft.save(2044,body,"read225-first");
  const fail=vi.spyOn(Storage.prototype,"getItem").mockImplementation(()=>{throw new DOMException("Unavailable","SecurityError");});
  await expect(f.draft.save(2044,body,"read225-first")).rejects.toMatchObject({status:503});fail.mockRestore();expect((await f.draft.current(2044)).revision).toBe(1);
 });
 it("replays a persisted receipt after master retirement and cache loss but rejects a fresh retired-ref write",async()=>{
  const f=await fixture225();const program=await f.plan.createProgram({code:"RETIRED225",name:"Originally active225",kind:"program"});
  const row={...f.blankEntry("programs","program",null,1),program_ref:program.ref,name:"Pinned active row225"};const body={expected_revision:0,entries:f.toSave([row],{base_version:0}).entries};
  const ack=await f.draft.save(2048,body,"retired225-own");await f.plan.reviseProgram(program.ref,{code:program.code,name:program.name,kind:"program",active:false});
  vi.resetModules();const restored=await fixture225();const replay=await restored.draft.save(2048,body,"retired225-own");expect(replay.event_id).toBe(ack.event_id);expect(replay.entries).toEqual(ack.entries);
  await expect(restored.draft.save(2048,{...body,draft_id:ack.draft_id,expected_revision:1},"retired225-fresh")).rejects.toMatchObject({status:422});expect((await restored.draft.current(2048)).revision).toBe(1);
 });
 it("rejects a persisted published bucket whose year differs from its actual version before Open",async()=>{
  const f=await fixture225();await f.plan.save(2046,{base_version:0,effective_from:"2046-01-01",entries:[]});
  const {createKhtcPeriodApi}=await import("@/kn/khtc/periods/api");const {createApiClient}=await import("@/api/client");const {mockToken}=await import("./personas");const {mockFetchNoDelay}=await import("./handler");
  const periodClient=()=>createApiClient({baseUrl:"http://mock",getAccessToken:()=>mockToken("tinh"),getContextId:()=>null,fetch:mockFetchNoDelay});
  const periods=createKhtcPeriodApi(periodClient());expect((await periods.open({report_type:"week",year:2046,no:1,departments:["trong_trot"]})).year).toBe(2046);
  const saved=JSON.parse(window.sessionStorage.getItem(storageKey)!) as {plans:[number,unknown][]};const tuple=saved.plans.find(([year])=>year===2046)!;tuple[0]=2047;window.sessionStorage.setItem(storageKey,JSON.stringify(saved));
  vi.resetModules();const restored=await fixture225();await expect(restored.draft.current(2047)).rejects.toMatchObject({status:503});
  const khtcMock=await import("./kn-khtc");expect(khtcMock.publishedOwnerPlanGuard225(2047,"2047-01-01")?.status).toBe(503);
  const restoredPeriods=createKhtcPeriodApi(periodClient());await expect(restoredPeriods.open({report_type:"week",year:2047,no:1,departments:["trong_trot"]})).rejects.toMatchObject({status:503});expect((await restoredPeriods.list(2047)).length).toBe(0);
 });
 it("imports/resets without a cycle and never invents a plan for an unknown future year",async()=>{
  const f=await fixture225();expect((await f.draft.current(2045)).revision).toBe(0);expect((await f.plan.versions(2045)).items).toHaveLength(0);
  const khtcMock=await import("./kn-khtc");expect(khtcMock.publishedOwnerPlanGuard225(2045,"2045-01-01")?.status).toBe(409);
  khtcMock.resetKhtcMock();expect((await f.plan.versions(2045)).items).toHaveLength(0);expect((await f.draft.current(2045)).draft).toBeNull();
 });
});
