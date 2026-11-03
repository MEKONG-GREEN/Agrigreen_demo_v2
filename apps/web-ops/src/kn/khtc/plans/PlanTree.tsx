"use client";

import { useState, type ReactNode } from "react";
import { DataTable } from "@/components/patterns/list";
import { DEPT_LABEL, ENTRY_KIND_LABEL, TABS, TAB_LABEL, treeRows, vnDecimal, type KhtcPlanEntry, type KhtcTab } from "../api";

/** Snapshot order and hierarchy, shared by workspace and immutable saved versions. */
export function PlanTree({ entries, initialTab = "programs", onTabChange, actions }: { entries: KhtcPlanEntry[]; initialTab?: KhtcTab; onTabChange?: (tab:KhtcTab)=>void; actions?: (entry: KhtcPlanEntry) => ReactNode }) {
  const [tab,setTab]=useState<KhtcTab>(initialTab);
  const rows=treeRows(entries,tab);
  return <><div className="row" role="tablist" aria-label="Nội dung kế hoạch năm" style={{flexWrap:"wrap",marginBottom:8}}>{TABS.map(t=><button key={t} type="button" role="tab" aria-selected={tab===t} className={tab===t?"btn btn-p btn-sm":"btn btn-o btn-sm"} onClick={()=>{setTab(t);onTabChange?.(t);}}>{TAB_LABEL[t]} ({entries.filter(e=>e.tab===t&&e.kind==="program").length})</button>)}</div>
    <DataTable caption={TAB_LABEL[tab]} rows={rows} rowKey={r=>r.e.ref} columns={[
      {key:"number",header:"Số chương trình",cell:r=>r.e.roman||"—"},
      {key:"name",header:"Nội dung",cell:r=><span style={{paddingLeft:r.depth*20,fontWeight:r.e.kind==="program"?600:undefined}}><span className="small mute">{ENTRY_KIND_LABEL[r.e.kind]} · </span>{r.e.name||r.e.program_name||"—"}</span>},
      {key:"unit",header:"ĐVT",cell:r=>r.e.unit_code||"—"},
      {key:"qty",header:"Khối lượng KH",cell:r=>vnDecimal(r.e.planned_qty)},
      {key:"budget",header:"Kinh phí KH",cell:r=>vnDecimal(r.e.budget)},
      {key:"dept",header:"Phòng phụ trách",cell:r=>DEPT_LABEL[r.e.department_code??""]||r.e.department_code||"—"},
      {key:"tracking",header:"Theo dõi",cell:r=>r.e.tracking==="stopped"?`Ngừng từ ${r.e.stopped_from}`:"Đang theo dõi"},
      ...(actions?[{key:"actions",header:"Thao tác",cell:(r:{e:KhtcPlanEntry;depth:number})=>actions(r.e)}]:[]),
    ]} />
  </>;
}
