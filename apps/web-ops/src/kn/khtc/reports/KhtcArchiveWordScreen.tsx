"use client";
import { useMemo, useState } from "react";
import { useParams } from "next/navigation";
import { useAuth } from "@/auth/AuthProvider";
import { DetailPage, LogPanel, LabelValueBlock } from "@/components/patterns/detail";
import { DataTable, type Column } from "@/components/patterns/list";
import { LoadState, dt, useAction, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import { archiveFieldSelector, archiveFilterKey, createArchiveApi, type ArchiveWord } from "./archive-api";
import { ArchiveSelectionBoundary, ArchiveBatchBar, archiveFileKey, useArchiveSelection, useArchiveAlive } from "./archive-selection";
import {archiveFieldStatusLabel} from "./ArchiveFieldPicker";
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
export function KhtcArchiveWordScreen() {
  const auth=useAuth(), params=useParams<{period: string; export: string}>();
  if (!uuid.test(params.period ?? "") || !uuid.test(params.export ?? "") || /^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(params.period) || /^0{8}-0{4}-0{4}-0{4}-0{12}$/.test(params.export)) return <p role="alert">Đường dẫn bản xuất không hợp lệ.</p>;
  return <ArchiveSelectionBoundary><WordBody key={JSON.stringify([auth.principal,auth.kn?.context,auth.kn?.support,params])} period={params.period} exportID={params.export}/></ArchiveSelectionBoundary>;
}
function WordBody({period,exportID}: {period: string; exportID: string}) {
  const auth=useAuth(), api=useMemo(()=>auth.api ? createArchiveApi(auth.api): null,[auth.api]);
  const [offset,setOffset]=useState(0), {files,toggle,filter}=useArchiveSelection(), alive=useArchiveAlive(archiveFilterKey(filter),auth.api), action=useAction();
  const [info,setInfo]=useState<ArchiveWord|null>(null);
  const rows=useLoad(api ? ()=>api.words(period,exportID,offset,archiveFieldSelector(filter)):null,[api,period,exportID,offset,filter]);
  const columns: Column<ArchiveWord>[] = [
    {key:"select",header:"Chọn",cell:r=>{const target={period_id:period,export_id:exportID,format:"word" as const,run_id:r.run_id}, checked=files.some(x=>archiveFileKey(x)===archiveFileKey(target));return <input type="checkbox" aria-label={`Chọn Word lần ${r.version}`} checked={checked} disabled={!checked && files.length>=10} onChange={e=>toggle({...target,label:`Word ${rows.data?.export_name ?? ""} · Excel v${rows.data?.export_version ?? ""} · lần ${r.version}`},e.target.checked)}/>;}},
    {key:"version",header:"Lần xuất Word",cell:r=>r.version},
    {key:"number",header:"Số văn bản đã lưu",cell:r=><span title={r.doc_number || undefined}>{r.doc_number.length>25 ? r.doc_number.slice(0,24)+"…" : r.doc_number || "—"}</span>},
    {key:"at",header:"Xuất lúc",cell:r=>dt(r.at)},
    {key:"download",header:"Thao tác",cell:r=><><button type="button" className="btn btn-o btn-sm" disabled={!api || action.busy} onClick={()=>{if(api)void action.run(async()=>{const file=await api.word(period,exportID,r.run_id,archiveFieldSelector(filter)); if(!alive())return false;saveBlob(file.blob,r.file_name);return true;},saved=>saved?"Đã nhận tệp Word đã lưu.":"");}} aria-label={`Tải Word lần ${r.version}`}>Tải Word</button> <button type="button" className="btn btn-o btn-sm" onClick={()=>setInfo(r)} aria-label={`Thông tin Word lần ${r.version}`}>Thông tin</button></>},
  ];
  return <DetailPage back={{href:"/kn/khtc/bao-cao-da-xuat",label:"Báo cáo đã xuất"}} title="Word đã lưu của bản xuất" log={<LogPanel title="Log xuất Word" entries={(rows.data?.items ?? []).map(r=>({event:`Xuất Word lần ${r.version}`,actor_display:r.actor_ref,at:r.at}))} empty="Chưa có Word được xác minh liên kết với bản xuất này."/>}>
    <LoadState s={rows}>{data=><><h2>{data.export_name} · Excel v{data.export_version}</h2><p>Lĩnh vực: {data.field_codes?.join(", ") || "—"} · {archiveFieldStatusLabel(data.field_status)}</p><p>Chỉ tải tệp Word đã lưu và được xác minh thuộc đúng bản xuất. Tệp Word cũ chưa có liên kết xác minh không được suy đoán từ số phiên bản.</p>
    <DataTable caption="Word đã lưu" columns={columns} rows={data.items} rowKey={r=>r.run_id} page={Math.floor(offset/20)+1} pageSize={20}/>
    <nav className="row" aria-label="Phân trang Word"><button className="btn" disabled={offset===0 || rows.loading} onClick={()=>{setInfo(null);setOffset(x=>Math.max(0,x-20));}}>Trước</button><span>Trang {Math.floor(offset/20)+1}</span><button className="btn" disabled={!data.has_more || offset+20>100000 || rows.loading} onClick={()=>{setInfo(null);setOffset(x=>x+20);}}>Tiếp</button></nav></>}</LoadState>
    {info && rows.data?.items.some(x=>x.run_id===info.run_id) && <section aria-label={`Thông tin Word lần ${info.version}`}>
      <LabelValueBlock title={`Tệp Word lần ${info.version}`} items={[
        {label:"Số văn bản",value:info.doc_number || "—"},{label:"Lần xuất Word",value:info.version},
        {label:"Tên tệp",value:info.file_name},{label:"Dung lượng",value:`${info.size.toLocaleString("vi-VN")} byte`},
        {label:"Người xuất",value:info.actor_ref},{label:"Xuất lúc",value:dt(info.at)},
        {label:"Mẫu đã ghim",value:`${info.template} v${info.template_version}`},{label:"Băm mẫu",value:<code>{info.template_hash}</code>},
        {label:"SHA-256 tệp",value:<code>{info.body_hash}</code>},{label:"Băm dữ liệu vào",value:<code>{info.input_hash}</code>},
      ]}/><button type="button" className="btn btn-o" onClick={()=>setInfo(null)}>Đóng thông tin tệp</button>
    </section>}
    <ArchiveBatchBar/>{action.view}
  </DetailPage>;
}
