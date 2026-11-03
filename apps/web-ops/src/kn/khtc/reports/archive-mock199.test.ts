import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {createApiClient} from "@/api/client";
import {mockFetchNoDelay} from "@/api/mock/handler";
import {createKhtcPeriodApi} from "../periods/api";
import {createReportApi} from "./api";
import {createArchiveApi} from "./archive-api";
let context="", key=0;
beforeEach(async()=>{
 vi.useFakeTimers({toFake:["Date"]});vi.setSystemTime(new Date("2030-01-01T00:00:00Z"));
 const response=await mockFetchNoDelay(new Request("http://mock/v1/me/context",{method:"POST",headers:{Authorization:"Bearer mock.kn-admin","Content-Type":"application/json","Idempotency-Key":`archive199-context-${++key}`},body:JSON.stringify({assignment_id:"asg-admin",department_code:null})}));
 context=((await response.json()) as {context_id:string}).context_id;
});
afterEach(()=>vi.useRealTimers());
it("mock retains explicit Word bytes, does not infer another snapshot, and ZIPs mixed files",async()=>{
 const legacy=createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.tinh",getContextId:()=>null,fetch:mockFetchNoDelay});
 const reports=createReportApi(legacy), periods=createKhtcPeriodApi(legacy);
 const p=await periods.open({report_type:"week",year:2039,no:17,departments:["trong_trot"]});
 let d=await reports.save(p.id,{base_version:0,admin:{document_no:"199"},texts:{B:"Frozen Word input"}});
 const x=await reports.export(p.id,d.version,true,{}), w=await reports.word(p.id,x.id), original=await reports.downloadWord(p.id,w.id);
 const reader=createArchiveApi(createApiClient({baseUrl:"http://mock",getAccessToken:()=>"mock.kn-admin",getContextId:()=>context,fetch:mockFetchNoDelay}));
 let list=await reader.words(p.id,x.id);expect(list.items.map(x=>x.run_id)).toEqual([w.id]);
 d=await reports.save(p.id,{base_version:d.version,admin:{},texts:{B:"Changed live draft"}});
 const second=await reports.export(p.id,d.version,true,{});
 expect((await reader.words(p.id,second.id)).items).toHaveLength(0);
 await expect(reader.word(p.id,second.id,w.id)).rejects.toBeTruthy();
 const saved=await reader.word(p.id,x.id,w.id);expect(await saved.blob.text()).toBe(await original.blob.text());
 expect(await saved.blob.text()).toContain("GIẢ LẬP — không phải tệp Word thật");
 list=await reader.words(p.id,x.id);expect(list.items[0]?.actor_ref).toBe(w.actor_ref);expect(list.items[0]?.at).toBe(w.at);
 const zip=await reader.zip([{period_id:p.id,export_id:x.id},{period_id:p.id,export_id:x.id,format:"word",run_id:w.id}]);
 const bytes=new Uint8Array(await zip.blob.arrayBuffer());expect(Array.from(bytes.slice(0,2))).toEqual([80,75]);expect(new TextDecoder().decode(bytes)).toContain(await saved.blob.text());
 await expect(reader.zip([{period_id:p.id,export_id:x.id},{period_id:p.id,export_id:x.id}])).rejects.toBeTruthy();
 for(const query of ["limit=1e100000","offset=1e-100000","limit="+"9".repeat(100000),"offset=-1","limit=51","limit=1&limit=2"]){
  const res=await mockFetchNoDelay(new Request(`http://mock/v1/extension/khtc/periods/${p.id}/report/exports/${x.id}/words?${query}`,{headers:{Authorization:"Bearer mock.kn-admin","X-AG-Context":context}}));expect(res.status).toBe(422);
 }
});
