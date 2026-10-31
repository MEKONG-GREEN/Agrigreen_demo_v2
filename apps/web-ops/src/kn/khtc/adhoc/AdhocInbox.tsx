"use client";
import Link from "next/link";
import {useEffect,useMemo} from "react";
import {useAuth} from "@/auth/AuthProvider";
import {LoadState,dt,useLoad} from "@/kn/cycle/common";
import {createAdhocApi} from "./api";
export function AdhocInbox(){
 const {api,kn}=useAuth();const client=useMemo(()=>createAdhocApi(api),[api]);
 const allowed=kn.context?.role==="kn_tinh"&&!kn.context.read_only;
 const list=useLoad(allowed?()=>client.inbox():null,[client,allowed,kn.context?.context_id]);
 useEffect(()=>{if(!allowed)return;const timer=setInterval(list.reload,30000);return()=>clearInterval(timer)},[allowed,list.reload]);
 if(!allowed)return null;
 return <section aria-label="Yêu cầu phát sinh của phòng"><h2>Yêu cầu số liệu bổ sung</h2><LoadState s={list}>{items=>items.length?<ul>{items.map(r=><li key={r.id}><Link href={"/kn/khtc/phieu/"+r.period_id}>{r.instruction}</Link> · Hạn {dt(r.due_at)}{r.overdue?" · Quá hạn":""} · {r.state==="intent"?"Đang phát hành":"Cần trả lời"}</li>)}</ul>:<p>Không có yêu cầu bổ sung đang chờ.</p>}</LoadState></section>;
}
