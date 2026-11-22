"use client";
import {useState} from "react";
import {LoadState, useLoad} from "@/kn/cycle/common";
import type {ArchiveQuery, createArchiveApi} from "./archive-api";

type Props={api:ReturnType<typeof createArchiveApi>;query:ArchiveQuery;onChoose:(code:string)=>void;onClose:()=>void};
// Cursor pages are read from all matching frozen exports; no preload cutoff.
export function ArchiveFieldPicker({api,query,onChoose,onClose}:Props){
 const [cursors,setCursors]=useState<string[]>([""]);
 const after=cursors[cursors.length-1];
 const rows=useLoad(()=>api.fields({year:query.year,kind:query.kind,period_id:query.period_id,q:query.q,versions:query.versions,field_status:query.field_status,after,limit:20}),[api,query,after]);
 return <section className="card sec" aria-label="Chọn lĩnh vực đã xuất"><h2>Lĩnh vực trong bản xuất</h2>
  <p>Mã lĩnh vực được giữ từ các hàng thực xuất. Tệp chưa rõ metadata không được gán mã thay thế.</p>
  <button type="button" className="btn btn-o" onClick={onClose}>Đóng chọn lĩnh vực</button>
  <LoadState s={rows}>{data=><><ul>{data.items.map(x=><li key={x.code}><button type="button" className="btn btn-o" onClick={()=>onChoose(x.code)}>Chọn lĩnh vực {x.code}</button></li>)}</ul>
   {!data.items.length&&<p>Không có mã lĩnh vực trong bộ lọc hiện tại.</p>}
   <nav className="row" aria-label="Phân trang lĩnh vực"><button type="button" className="btn" disabled={cursors.length===1||rows.loading} onClick={()=>setCursors(c=>c.slice(0,-1))}>Lĩnh vực trước</button><span>Trang lĩnh vực {cursors.length}</span><button type="button" className="btn" disabled={!data.has_more||!data.next_cursor||rows.loading} onClick={()=>setCursors(c=>[...c,data.next_cursor])}>Lĩnh vực tiếp</button></nav>
  </>}</LoadState>
 </section>;
}
export const archiveFieldStatusLabel=(status?:string)=>status==="complete"?"Đủ metadata":status==="partial"?"Metadata một phần":"Chưa rõ metadata";
