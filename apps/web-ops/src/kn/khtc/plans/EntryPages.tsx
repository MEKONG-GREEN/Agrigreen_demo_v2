"use client";

import { useId, useState } from "react";
import { NumberField, SelectField, TextField } from "@/components/form";
import { DetailPage, LabelValueBlock, LogPanel, type LogEntry } from "@/components/patterns/detail";
import { ReadOnlyNotice } from "@/kn/cycle/common";
import { DEPT_LABEL, ENTRY_KIND_LABEL, type KhtcCatalogItem, type KhtcPlanEntry } from "../api";
import { checkEntry, ENTRY_NUMBER_RULES, type EntryDraft } from "./entry-form";
import { planHref, type PlanRoute } from "./routes";

export function EntryPage({ route, draft, onChange, canEdit, catalog, log, onSave, onCancel }: {
  route: PlanRoute; draft: EntryDraft; onChange: (d: EntryDraft) => void; canEdit: boolean; catalog: KhtcCatalogItem[]; log: LogEntry[];
  onSave: (e: KhtcPlanEntry) => void; onCancel: () => void;
}) {
  const [errors, setErrors] = useState<Record<string, string>>({});
  const id = useId(); const e = draft.entry; const editing = canEdit && route.view !== "entry";
  const title = `${route.view === "entry" ? "Xem" : route.view === "new-entry" ? "Thêm" : "Sửa"} ${ENTRY_KIND_LABEL[e.kind].toLowerCase()}`;
  const update = (next: EntryDraft, field: string) => {
    onChange(next);
    setErrors(old => { if (!old[field]) return old; const out = { ...old }; const error = checkEntry(next, route.year).errors[field]; if (error) out[field] = error; else delete out[field]; return out; });
  };
  const patch = (field: keyof KhtcPlanEntry, value: string) => update({ ...draft, entry: { ...e, [field]: value || null } }, field);
  const text = (label: string, field: keyof KhtcPlanEntry, required = false) => <TextField label={label} value={String(e[field] ?? "")} required={required} disabled={!editing} error={errors[field]} onChange={v => patch(field, v)} />;
  const catalogOptions = (kind: string) => catalog.filter(c => c.kind === kind && c.active).map(c => ({value:c.code,label:c.name}));
  return <DetailPage title={title} back={{href:planHref(route.base,route.year),label:"Kế hoạch năm"}} log={<LogPanel entries={log} />} actions={<><button type="button" className="btn btn-o" onClick={onCancel}>{editing ? "Hủy" : "Quay lại"}</button>{editing ? <button type="submit" form={id} className="btn btn-p">Lưu vào nháp</button> : null}</>}>
    <p className="mute">Nháp trong workspace chỉ trở thành phiên bản lưu khi xác nhận Lưu phiên bản. Log dưới đây thuộc phiên bản đã lưu.</p>
    {!editing ? <ReadOnlyNotice>Chỉ xem nội dung kế hoạch trong ngữ cảnh hiện tại.</ReadOnlyNotice> : null}
    <LabelValueBlock title="Vị trí trong cây" items={[{label:"Tab",value:e.tab},{label:"Dòng cha",value:e.parent_ref || "CT/DA gốc"},{label:"Lĩnh vực hiện hữu",value:e.field_code || "—"}]} />
    <form id={id} noValidate onSubmit={event => { event.preventDefault(); if (!editing) return; const checked = checkEntry(draft,route.year); setErrors(checked.errors); if (checked.valid) onSave(checked.entry); }}>
      {text("Tên","name",true)}
      <div className="grid2">{text("Mã / số hiệu","code")}<NumberField label="Thứ tự trong cây" value={draft.sort_order} required disabled={!editing} rule={ENTRY_NUMBER_RULES.sort_order} error={errors.sort_order} onChange={v=>update({...draft,sort_order:v},"sort_order")} /></div>
      <div className="grid2"><SelectField label="ĐVT" value={e.unit_code??""} disabled={!editing} options={catalogOptions("unit")} onChange={v=>patch("unit_code",v)} /><NumberField label="Khối lượng KH" value={draft.planned_qty} disabled={!editing} rule={ENTRY_NUMBER_RULES.planned_qty} error={errors.planned_qty} onChange={v=>update({...draft,planned_qty:v},"planned_qty")} /></div>
      {text("Quy mô (ghi chú)","scale_note")}
      <div className="grid2"><NumberField label="Kinh phí KH" unit="đồng" value={draft.budget} disabled={!editing} rule={ENTRY_NUMBER_RULES.budget} error={errors.budget} onChange={v=>update({...draft,budget:v},"budget")} /><SelectField label="Nguồn kinh phí" value={e.funding_code??""} disabled={!editing} options={catalogOptions("funding_source")} onChange={v=>patch("funding_code",v)} /></div>
      <SelectField label="Phòng phụ trách" required={e.kind==="item"} disabled={!editing} value={e.department_code??""} error={errors.department_code} options={Object.entries(DEPT_LABEL).map(([value,label])=>({value,label}))} onChange={v=>patch("department_code",v)} />
      {e.kind==="item" ? <><SelectField label="Nguồn số liệu" disabled={!editing} value={e.data_source??"form"} options={[{value:"form",label:"Phiếu của phòng"},{value:"module",label:"Lấy từ module"}]} onChange={v=>patch("data_source",v)} />{e.data_source==="module" ? text("Khóa nguồn module","source_key",true) : null}<div className="grid2"><TextField label="Xã/điểm (mã, cách nhau dấu phẩy)" value={draft.communes} disabled={!editing} error={errors.communes} onChange={v=>update({...draft,communes:v},"communes")} /><NumberField label="Số điểm" value={draft.points_count} disabled={!editing} rule={ENTRY_NUMBER_RULES.points_count} error={errors.points_count} onChange={v=>update({...draft,points_count:v},"points_count")} /></div></> : null}
      <SelectField label="Theo dõi" disabled={!editing} value={e.tracking} options={[{value:"tracking",label:"Đang theo dõi"},{value:"stopped",label:"Ngừng theo dõi"}]} onChange={v=>patch("tracking",v)} />
      {e.tracking==="stopped" ? <>{text("Ngừng từ ngày (YYYY-MM-DD)","stopped_from",true)}{text("Số QĐ ngừng","stop_decision")}</> : null}{text("Ghi chú","note")}
    </form>
  </DetailPage>;
}
