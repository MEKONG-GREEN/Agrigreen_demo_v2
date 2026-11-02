"use client";

import Link from "next/link";
import { useId, useState } from "react";
import { TextField } from "@/components/form";
import { DetailPage, LabelValueBlock, LogPanel, type LogEntry } from "@/components/patterns/detail";
import { DataTable } from "@/components/patterns/list";
import { fieldErrorMap, type ApiProblem } from "@/api/problem";
import { LoadState, ReadOnlyNotice, useLoad } from "@/kn/cycle/common";
import { dmy, useKhtcApi, vnDecimal, type KhtcPlanVersion } from "../api";
import { planHref, type PlanRoute } from "./routes";
import { validateVersionHead, type VersionHead } from "./version-form";

export function planVersionLog(versions: KhtcPlanVersion[]): LogEntry[] {
  return versions.map(v => ({ event: `${v.basis === "adjust" ? "Điều chỉnh" : "Lập"} kế hoạch v${v.version}`, actor_display: v.actor.display || v.actor.ref, at: v.recorded_at, reason: v.note, changes: [{ field: "Quyết định", old: null, new: v.decision_no }, { field: "Hiệu lực", old: null, new: dmy(v.effective_from) }] }));
}
export function SaveVersionPage({ route, head, onChange, adjust, canEdit, dirty, busy, problem, log, onCancel, onSave }: {
  route: PlanRoute; head: VersionHead; onChange: (h: VersionHead) => void; adjust: boolean; canEdit: boolean; dirty: boolean; busy: boolean;
  problem: ApiProblem | null; log: LogEntry[]; onCancel: () => void; onSave: (head: VersionHead) => Promise<void>;
}) {
  const [errors, setErrors] = useState<Record<string, string>>({});
  const id = useId();
  const server = problem ? fieldErrorMap(problem) : {};
  const save = async () => {
    if (!canEdit || !dirty || busy) return;
    const checked = validateVersionHead(route.year, adjust, head);
    setErrors(checked.errors);
    if (checked.valid) await onSave(head);
  };
  const title = adjust ? `Điều chỉnh theo quyết định — năm ${route.year}` : `Lưu kế hoạch năm ${route.year} (phiên bản 1)`;
  return <DetailPage title={title} back={{ href: planHref(route.base, route.year), label: "Kế hoạch năm" }} log={<LogPanel entries={log} />} actions={<><button className="btn btn-o" type="button" disabled={busy} onClick={onCancel}>Hủy</button>{canEdit ? <button className="btn btn-p" type="submit" form={id} disabled={!dirty || busy}>Lưu phiên bản</button> : null}</>}>
    {!canEdit ? <ReadOnlyNotice>Chỉ xem — cần quyền lập kế hoạch trong ngữ cảnh hiện tại.</ReadOnlyNotice> : <>
      <p className="mute">Chỉ lưu khi bấm Lưu phiên bản. Bản nháp trong trang chưa thay đổi phiên bản đã lưu.</p>
      {!dirty ? <p role="status">Chưa có thay đổi chưa lưu trong bản nháp của năm này.</p> : null}
      <form id={id} noValidate onSubmit={e => { e.preventDefault(); void save(); }}>
        <TextField label="Số quyết định" value={head.decision_no} onChange={v => onChange({ ...head, decision_no: v })} required={adjust} maxLength={100} error={errors.decision_no || server.decision_no} disabled={busy} />
        <TextField label="Ngày quyết định" value={head.decision_date} onChange={v => onChange({ ...head, decision_date: v })} required={adjust} maxLength={10} placeholder="YYYY-MM-DD" error={errors.decision_date || server.decision_date} disabled={busy} />
        <TextField label="Ngày hiệu lực" value={head.effective_from} onChange={v => onChange({ ...head, effective_from: v })} required={adjust} maxLength={10} placeholder="YYYY-MM-DD" error={errors.effective_from || server.effective_from} disabled={busy} />
        <TextField label="Ghi chú" value={head.note} onChange={v => onChange({ ...head, note: v })} multiline maxLength={1000} error={errors.note || server.note} disabled={busy} />
      </form>
    </>}
  </DetailPage>;
}

export function VersionsPage({ route }: { route: PlanRoute }) {
  const api = useKhtcApi();
  const loaded = useLoad(() => api.versions(route.year), [api, route.year]);
  return <LoadState s={loaded}>{d => <DetailPage title={`Lịch sử phiên bản — năm ${route.year}`} back={{ href: planHref(route.base, route.year), label: "Kế hoạch năm" }} log={<LogPanel entries={planVersionLog([...d.items].reverse())} />}>
    <DataTable caption="Lịch sử kế hoạch năm" rows={[...d.items].reverse()} rowKey={v => v.id} columns={[
      { key: "version", header: "Phiên bản", cell: v => `v${v.version}` },
      { key: "decision", header: "Quyết định", cell: v => v.decision_no ? `${v.decision_no} (${dmy(v.decision_date)})` : "—" },
      { key: "effective", header: "Hiệu lực", cell: v => dmy(v.effective_from) },
      { key: "entries", header: "Số dòng", cell: v => v.entries_count },
      { key: "view", header: "Thao tác", cell: v => <Link href={planHref(route.base, route.year, "version", v.version)} aria-label={`Xem phiên bản ${v.version}`}>Xem</Link> },
    ]} />
  </DetailPage>}</LoadState>;
}

export function VersionDetailPage({ route }: { route: PlanRoute }) {
  const api = useKhtcApi();
  const loaded = useLoad(async () => ({ plan: await api.plan(route.year, route.version) }), [api, route.year, route.version]);
  return <LoadState s={loaded}>{({plan}) => {
    const back = { href: planHref(route.base, route.year, "history"), label: "Lịch sử phiên bản" };
    if (!plan || plan.year !== route.year || plan.version.version !== route.version) return <DetailPage title="Không có phiên bản kế hoạch" back={back}><p role="alert">Không tìm thấy đúng năm và phiên bản được yêu cầu.</p></DetailPage>;
    return <DetailPage title={`Kế hoạch năm ${route.year} — phiên bản ${route.version}`} back={back} log={<LogPanel entries={planVersionLog([plan.version])} />}>
      <ReadOnlyNotice>Chỉ xem phiên bản đã lưu.</ReadOnlyNotice>
      <LabelValueBlock title="Thông tin phiên bản" items={[{label:"Quyết định",value:plan.version.decision_no},{label:"Ngày quyết định",value:dmy(plan.version.decision_date)},{label:"Hiệu lực",value:dmy(plan.version.effective_from)},{label:"Số dòng",value:plan.version.entries_count},{label:"Ghi chú",value:plan.version.note}]} />
      <DataTable caption="Nội dung phiên bản kế hoạch" rows={plan.entries} rowKey={e => e.ref} columns={[
        {key:"name",header:"Nội dung",cell:e=>e.name}, {key:"qty",header:"Khối lượng KH",cell:e=>vnDecimal(e.planned_qty)},
        {key:"budget",header:"Kinh phí KH",cell:e=>vnDecimal(e.budget)}, {key:"department",header:"Phòng phụ trách",cell:e=>e.department_code||"—"},
      ]} />
    </DetailPage>;
  }}</LoadState>;
}
