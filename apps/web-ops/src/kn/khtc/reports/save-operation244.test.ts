import { describe, expect, it, vi } from "vitest";
import { ApiProblem } from "@/api/problem";
import type { ReportApi, ReportSaveReceipt } from "./api";
import { createReportSaveSession244 } from "./save-operation244";

const period="01927b3e-0000-7000-8000-000000000001", actor="01927b3e-0000-7000-8000-000000000002", draft="01927b3e-0000-7000-8000-000000000003";
function memory():Storage {const data=new Map<string,string>();return {get length(){return data.size;},clear:()=>data.clear(),getItem:key=>data.get(key)??null,key:i=>[...data.keys()][i]??null,removeItem:key=>{data.delete(key);},setItem:(key,value)=>{data.set(key,value);}};}
function applied(operation:string):ReportSaveReceipt{return {operation_id:operation,period_id:period,outcome:"applied",applied:{draft_id:draft,base_draft_id:null,base_version:4,version:5,request_hash:"a".repeat(64),actor_ref:actor,at:"2026-11-08T00:00:00Z",changes:[{field:"texts.B",before:"Ban đầu",after:"Bản đã lưu",before_default:false,after_default:false}]}};}
type SaveApi=Pick<ReportApi,"save"|"saveOperation">;
describe("report draft244 exact operation recovery",()=>{
 it("persists metadata before POST, reloads and reads the original committed save after lost ACK without retrying mutation",async()=>{
  const store=memory();let committed:ReportSaveReceipt|null=null;
  const api={save:vi.fn(async(_id,body)=>{expect(store.length).toBe(1);expect(store.getItem(store.key(0)!)!).not.toContain("Nội dung riêng tư");committed=applied(body.operation_id!);throw new TypeError("ACK lost after commit");}),saveOperation:vi.fn(async()=>committed!)} satisfies SaveApi;
  const first=createReportSaveSession244(api,period,actor,"current-authority",()=>store);first.load();
  await expect(first.save({base_version:4,texts:{B:"Nội dung riêng tư"}})).rejects.toThrow("ACK lost");
  const identity=first.getSnapshot().pending!.operationID;
  const reloaded=createReportSaveSession244(api,period,actor,"current-authority",()=>store);reloaded.load();
  expect(reloaded.getSnapshot().pending!.operationID).toBe(identity);
  await reloaded.reconcile();
  expect(api.save).toHaveBeenCalledTimes(1);expect(api.saveOperation).toHaveBeenCalledWith(period,identity);
  expect(reloaded.getSnapshot().receipt?.applied?.version).toBe(5);expect(store.length).toBe(0);
 });
 it("unknown is retained across reload and cannot permit a new mutation",async()=>{
  const store=memory();let id="";
  const api={save:vi.fn(async(_period,body)=>{id=body.operation_id!;throw new TypeError("network unknown");}),saveOperation:vi.fn(async()=>({operation_id:id,period_id:period,outcome:"unknown" as const}))} satisfies SaveApi;
  const first=createReportSaveSession244(api,period,actor,"authority",()=>store);first.load();
  await expect(first.save({base_version:4})).rejects.toThrow();await first.reconcile();
  const after=createReportSaveSession244(api,period,actor,"authority",()=>store);after.load();
  await expect(after.save({base_version:999})).rejects.toThrow("Đối chiếu");expect(api.save).toHaveBeenCalledTimes(1);expect(after.getSnapshot().pending).not.toBeNull();
 });
 it.each(["operation","period","actor","base","version","hash"])("a mismatched %s ACK cannot acknowledge or clear the original operation",async field=>{
  const store=memory();
  const api={save:vi.fn(async(_period,body)=>{const receipt=applied(body.operation_id!);switch(field){case "operation":receipt.operation_id=draft;break;case "period":receipt.period_id=draft;break;case "actor":receipt.applied!.actor_ref=draft;break;case "base":receipt.applied!.base_version=5;break;case "version":receipt.applied!.version=4;break;case "hash":receipt.applied!.request_hash="invalid";break;}return {save_receipt:receipt} as Awaited<ReturnType<ReportApi["save"]>>;}),saveOperation:vi.fn()} satisfies SaveApi;
  const op=createReportSaveSession244(api,period,actor,"authority",()=>store);op.load();await expect(op.save({base_version:4})).rejects.toThrow("chưa khớp");expect(op.getSnapshot().receipt).toBeNull();expect(store.length).toBe(1);
 });
 it("confirmed validation rejection frees the operation, while storage failure sends no POST and other authority reads no pending metadata",async()=>{
  const store=memory();const api={save:vi.fn(async()=>{throw new ApiProblem({type:"about:blank",title:"Không hợp lệ",status:422,code:"validation_failed"});}),saveOperation:vi.fn()} satisfies SaveApi;
  const op=createReportSaveSession244(api,period,actor,"authority",()=>store);op.load();await expect(op.save({base_version:4})).rejects.toBeInstanceOf(ApiProblem);expect(store.length).toBe(0);expect(op.getSnapshot().pending).toBeNull();
  const broken=memory();broken.setItem=()=>{throw new Error("storage unavailable");};const fail=createReportSaveSession244(api,period,actor,"authority",()=>broken);fail.load();await expect(fail.save({base_version:4})).rejects.toThrow("storage unavailable");expect(api.save).toHaveBeenCalledTimes(1);
  const reads=vi.fn(()=>store);const readonly=createReportSaveSession244(api,period,actor,"",reads);readonly.load();expect(reads).not.toHaveBeenCalled();expect(readonly.getSnapshot().blocked).toBe(true);
 });
});

it("a late old ACK after remount/reconcile cannot clear a successor's pending metadata",async()=>{
 const store=memory();let firstReceipt:ReportSaveReceipt|null=null;
 let releaseOld!:(v:Awaited<ReturnType<ReportApi["save"]>>)=>void;
 let rejectNew!:(e:Error)=>void;
 const oldResponse=new Promise<Awaited<ReturnType<ReportApi["save"]>>>(resolve=>{releaseOld=resolve;});
 const newResponse=new Promise<Awaited<ReturnType<ReportApi["save"]>>>((_resolve,reject)=>{rejectNew=reject;});
 const api={save:vi.fn((_id,body)=>{if(!firstReceipt){firstReceipt=applied(body.operation_id!);return oldResponse;}return newResponse;}),saveOperation:vi.fn(async()=>firstReceipt!)} satisfies SaveApi;
 const old=createReportSaveSession244(api,period,actor,"same-current-authority",()=>store);old.load();
 const oldSave=old.save({base_version:4});
 const current=createReportSaveSession244(api,period,actor,"same-current-authority",()=>store);current.load();await current.reconcile();
 const currentSave=current.save({base_version:5});
 const pending=current.getSnapshot().pending!;
 releaseOld({save_receipt:firstReceipt!} as Awaited<ReturnType<ReportApi["save"]>>);await oldSave;
 const afterReload=createReportSaveSession244(api,period,actor,"same-current-authority",()=>store);afterReload.load();
 expect(afterReload.getSnapshot().pending?.operationID).toBe(pending.operationID);
 rejectNew(new TypeError("successor ACK unknown"));await expect(currentSave).rejects.toThrow("unknown");
 expect(api.save).toHaveBeenCalledTimes(2);
});
