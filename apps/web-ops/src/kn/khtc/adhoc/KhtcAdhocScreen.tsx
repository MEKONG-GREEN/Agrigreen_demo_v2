"use client";
import {useState} from "react";
import {AdhocOpenRecovery} from "./AdhocOpenRecovery";
import Link from "next/link";
import {useAuth} from "@/auth/AuthProvider";
import {KhtcPeriodsScreen} from "../periods/KhtcPeriodsScreen";
import {KhtcReportsScreen} from "../reports/KhtcReportsScreen";
export function KhtcAdhocScreen(){
 const {kn}=useAuth();const c=kn.context;const[revision,setRevision]=useState(0);const reload=()=>setRevision(v=>v+1);
 if(!c || c.read_only || !(c.role==="admin" || c.role==="kn_tinh" && c.department_code==="ke_hoach_tai_chinh"))
 return <p>Báo cáo phát sinh dành cho Admin hoặc Cấp Tỉnh Phòng Kế hoạch – Tài chính đang có quyền soạn. <Link href="/kn/khtc/bao-cao-da-xuat">Xem báo cáo đã xuất</Link></p>;
 return <div key={JSON.stringify(c)}><AdhocOpenRecovery onRecovered={reload}/><details><summary>Mở kỳ phát sinh và giao phiếu</summary><KhtcPeriodsScreen key={"periods:"+revision} adhocOnly onOpened={reload}/></details><KhtcReportsScreen key={"reports:"+revision} adhoc/></div>;
}
