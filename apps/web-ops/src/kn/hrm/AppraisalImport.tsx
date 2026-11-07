"use client";
import { useState } from "react";
import { useAction } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import { type AppraisalPeriod } from "./appraisal-api";
import { useAppraisalApi } from "./appraisal-ui";
export function AppraisalImport({period,kinds,onChanged}:{period:AppraisalPeriod;kinds:Array<"MS05"|"MS06">;onChanged:()=>void}) {
  const api=useAppraisalApi(),act=useAction(),[file,setFile]=useState<File|null>(null);
  return <section className="card"><h2>Nhập đề xuất từ Excel</h2><p>Tải mẫu tám cột của đợt, giữ dòng tiêu đề rồi nhập hai mức đề xuất. Nếu trùng họ tên, dùng mã hồ sơ ở cột Họ và tên. Toàn bộ tệp được kiểm tra trước khi lưu.</p>{act.view}
    <div className="row">{kinds.map(kind=><button type="button" key={kind} className="btn btn-o" disabled={act.busy} onClick={()=>void act.run(async()=>{const f=await api.importTemplate(period.id,kind);saveBlob(f.blob,f.name);})}>Tải mẫu nhập {kind}</button>)}</div>
    <label className="field">Tệp đề xuất Excel<input aria-label="Tệp đề xuất Excel" type="file" accept=".xlsx" disabled={act.busy} onChange={e=>setFile(e.target.files?.[0]??null)}/></label>
    <button className="btn btn-p" disabled={!file||act.busy} onClick={()=>{if(file)void act.run(()=>api.importExcel(period.id,period.version,file),x=>`Đã nhập ${x.count} đề xuất.`).then(x=>{if(x)onChanged();});}}>Nhập toàn bộ đề xuất</button>
  </section>;
}
