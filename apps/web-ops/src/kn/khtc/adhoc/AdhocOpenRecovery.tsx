"use client";
import {useMemo,useRef,useState} from "react";
import {useAuth} from "@/auth/AuthProvider";
import {LoadState,dt,useAction,useLoad} from "@/kn/cycle/common";
import {TextField} from "@/components/form";
import {ApiProblem} from "@/api/problem";
import {createAdhocApi} from "./api";
export function AdhocOpenRecovery(){
 const{api}=useAuth();const requests=useMemo(()=>createAdhocApi(api),[api]);const list=useLoad(()=>requests.opens(),[requests]);
 const act=useAction();const[reasons,setReasons]=useState<Record<string,string>>({});
 const pending=useRef(new Map<string,string>());const[uncertain,setUncertain]=useState<Record<string,boolean>>({});
 const recover=(id:string,generation:number)=>act.run(async()=>{
  const key=id+":"+generation,reason=pending.current.get(key)??reasons[id]??"";
  pending.current.set(key,reason);setUncertain(xs=>({...xs,[key]:true}));
  try{await requests.recover(id,generation,reason);pending.current.delete(key);setUncertain(xs=>({...xs,[key]:false}));list.reload()}
  catch(e){if(e instanceof ApiProblem&&e.problem.status>=400 && e.problem.status<500){pending.current.delete(key);setUncertain(xs=>({...xs,[key]:false}))}throw e}
 },"Đã đối chiếu phát hành. Tải lại danh sách kỳ để tiếp tục soạn.");
 return <section aria-label="Khôi phục phát hành kỳ"><h2>Khôi phục phát hành kỳ</h2>
 <p>Mất kết nối khi mở kỳ: tải lại danh sách rồi đối chiếu đợt đã gửi. Nếu phiên cũ hết hạn, cùng người dùng và phạm vi có thể tiếp nhận với lý do. Đợt và các phiếu đã phát hành được giữ nguyên.</p>{act.view}
 <button type="button" className="btn" onClick={list.reload} disabled={act.busy}>Tải lại thao tác dang dở</button>
 <LoadState s={list}>{xs=>xs.length?<ul>{xs.map(x=><li key={x.id}>{x.name} · Hạn {dt(x.due_at)}
 {x.requires_adoption&&<><p>Phiên phát hành cũ hết hạn lúc {dt(x.lease_expires_at)}. {x.can_adopt?"Có thể tiếp nhận trong phạm vi hiện tại.":"Chờ phiên cũ hết hạn; giữ nguyên ngữ cảnh được giao."}</p>
 <TextField label={"Lý do tiếp nhận "+x.name} value={reasons[x.id]??""} maxLength={500} onChange={v=>setReasons(s=>({...s,[x.id]:v}))} disabled={act.busy||!x.can_adopt||uncertain[x.id+":"+x.generation]}/></>}
 {uncertain[x.id+":"+x.generation]&&<p role="status">Chưa xác nhận kết quả. Có thể gửi lại cùng nội dung hoặc tải lại trạng thái tiếp nhận.</p>}
 <button type="button" className="btn" disabled={act.busy||x.requires_adoption&&(!x.can_adopt||(reasons[x.id]??"").trim().length<10)}
 onClick={()=>recover(x.id,x.generation)}>{x.requires_adoption?"Tiếp nhận và đối chiếu đợt cũ":"Đối chiếu đợt đã gửi"}</button>
 </li>)}</ul>:<p>Không có thao tác phát hành dang dở trong ngữ cảnh hiện tại.</p>}</LoadState></section>;
}
