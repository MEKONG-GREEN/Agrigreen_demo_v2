"use client";

import {useState} from "react";
import {DataTable, type Column} from "@/components/patterns/list";
import {LoadState, useLoad} from "@/kn/cycle/common";
import type {createArchiveApi, ArchivePeriod, ArchiveQuery} from "./archive-api";
import {REPORT_TYPES} from "../periods/api";

type Props = {api: ReturnType<typeof createArchiveApi>; year: number; kind: ArchiveQuery["kind"]; onChoose: (period: ArchivePeriod) => void; onClose: () => void};
const PAGE = 20;
// Independent pagination: a period is never omitted just because its exports are on another archive page.
export function ArchivePeriodPicker({api, year, kind, onChoose, onClose}: Props) {
 const [search,setSearch]=useState("");
 const [query,setQuery]=useState({q:"",offset:0});
 const rows=useLoad(()=>api.periods({year,kind,...query,limit:PAGE}),[api,year,kind,query]);
 const columns:Column<ArchivePeriod>[]=[
  {key:"kind",header:"Loại báo cáo",cell:p=>REPORT_TYPES.find(t=>t.value===p.report_type)?.label??p.report_type},
  {key:"period",header:"Kỳ",cell:p=>p.name},
  {key:"select",header:"Thao tác",cell:p=><button type="button" className="btn btn-o" onClick={()=>onChoose(p)} aria-label={`Lọc kỳ ${p.name}`}>Chọn kỳ</button>},
 ];
 return <section className="card sec" aria-label="Chọn kỳ đã xuất">
  <h2>Chọn kỳ đã xuất</h2>
  <form className="row" onSubmit={e=>{e.preventDefault();setQuery({q:search.trim(),offset:0});}}>
   <label>Tìm tên kỳ <input type="search" maxLength={200} value={search} onChange={e=>setSearch(e.target.value)}/></label>
   <button type="submit" className="btn">Tìm kỳ</button>
   <button type="button" className="btn btn-o" onClick={onClose}>Đóng chọn kỳ</button>
  </form>
  <LoadState s={rows}>{data=><>
   <DataTable caption="Kỳ có báo cáo đã xuất" columns={columns} rows={data.items} rowKey={p=>p.id} page={Math.floor(query.offset/PAGE)+1} pageSize={PAGE}/>
   <nav className="row" aria-label="Phân trang chọn kỳ">
    <button type="button" className="btn" disabled={query.offset===0||rows.loading} onClick={()=>setQuery(q=>({...q,offset:Math.max(0,q.offset-PAGE)}))}>Kỳ trước</button>
    <span>Trang kỳ {Math.floor(query.offset/PAGE)+1}</span>
    <button type="button" className="btn" disabled={!data.has_more||query.offset+PAGE>100000||rows.loading} onClick={()=>setQuery(q=>({...q,offset:q.offset+PAGE}))}>Kỳ tiếp</button>
   </nav>
  </>}</LoadState>
 </section>;
}
