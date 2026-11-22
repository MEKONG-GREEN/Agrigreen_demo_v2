"use client";
import { useState } from "react";
import { TextField } from "@/components/form";
import { DataTable, type Column } from "@/components/patterns/list";
import type { components } from "@/api/generated/application";
type S=components["schemas"];
export type AdhocSelection=S["KHTCReportAdhocSelection"];
type Choice={id:string;kind:"rows"|"metrics";name:string;unit:string;source:string;value:string};
export function AdhocSelector({data,value,onChange,disabled}:{data:S["KHTCReportData"];value?:AdhocSelection;onChange:(v:AdhocSelection)=>void;disabled:boolean}) {
 const [query,setQuery]=useState("");const[page,setPage]=useState(1);
 const selection=value??{rows:[],metrics:[]};
 const count=selection.rows.length+selection.metrics.length;
 const choices:Choice[]=[
  ...data.rows.filter(r=>r.kind==="item").map(r=>({id:r.ref,kind:"rows" as const,name:r.name,unit:r.unit,source:r.department,value:r.in_period})),
  ...data.metrics.map(m=>({id:m.key,kind:"metrics" as const,name:m.label,unit:m.unit,source:m.department,value:m.text??m.value.Increment??m.value.Sum??m.value.Cumulative??"—"}))
 ];
 const filtered=choices.filter(c=>(c.name+" "+c.source).toLocaleLowerCase().includes(query.toLocaleLowerCase()));
 const toggle=(c:Choice,checked:boolean)=>onChange({...selection,[c.kind]:checked?[...selection[c.kind],c.id]:selection[c.kind].filter(id=>id!==c.id)});
 const columns:Column<Choice>[]=[
  {key:"choose",header:"Chọn",cell:c=><input type="checkbox" aria-label={"Chọn "+c.name} checked={selection[c.kind].includes(c.id)} disabled={disabled||count>=200&&!selection[c.kind].includes(c.id)} onChange={e=>toggle(c,e.target.checked)}/>},
  {key:"name",header:"Chỉ tiêu",cell:c=>c.name},{key:"unit",header:"ĐVT",cell:c=>c.unit},
  {key:"value",header:"Trong khoảng đã chọn",cell:c=>c.value},{key:"source",header:"Nguồn",cell:c=>c.source}
 ];
 return <section aria-label="Chọn chỉ tiêu báo cáo phát sinh"><h3>Chỉ tiêu báo cáo phát sinh</h3><p>Đã chọn {count}/200. Số liệu chỉ đọc; sửa ở nguồn rồi cập nhật ảnh chụp.</p>
 <TextField label="Tìm chỉ tiêu" value={query} maxLength={200} onChange={v=>{setQuery(v);setPage(1);}}/>
 <DataTable caption="Chỉ tiêu sẵn có" columns={columns} rows={filtered.slice((page-1)*20,page*20)} rowKey={r=>r.kind+":"+r.id} page={page} pageSize={20} total={filtered.length} onPage={setPage}/>
 </section>;
}