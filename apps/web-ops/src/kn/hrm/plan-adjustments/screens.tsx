"use client";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useEffect, useId, useMemo, useRef, useState } from "react";
import { fieldErrorMap } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { NumberField, TextField } from "@/components/form";
import { DetailPage, LabelValueBlock, LogPanel, type LogEntry } from "@/components/patterns/detail";
import { DataTable } from "@/components/patterns/list";
import { LoadState, ReadOnlyNotice, useAction, useLoad } from "@/kn/cycle/common";
import { createPlanApi, planDemand, type HrmPlanUnit, type PlanApi } from "../plan-api";
import { COUNT_RULE, validateAdjustment, type AdjustmentForm } from "./form";
import { adjustmentHref, parseAdjustmentRoute, PLAN_BASE, planUnitHref, type AdjustmentRoute } from "./routes";

type Adjustment = HrmPlanUnit["adjustments"][number];
const clients = new WeakMap<object, number>();
let nextClient = 0;
function clientIdentity(api: object) { let id = clients.get(api); if (id === undefined) { id = ++nextClient; clients.set(api, id); } return id; }
export function adjustmentLog(rows: Adjustment[]): LogEntry[] {
  return [...rows].sort((a, b) => b.seq - a.seq).map(a => ({ event: `Điều chỉnh số ${a.seq}`, actor_display: a.actor_ref || "Chưa có thông tin người thực hiện", at: a.recorded_at, reason: a.reason,
    changes: [{ field: "Dự kiến nghỉ, chuyển công tác khác", old: null, new: String(a.values.leaving) }, { field: "Số cần bổ sung", old: null, new: String(a.values.additional) }, { field: "Nhu cầu theo kế hoạch", old: null, new: String(a.values.demand) }] }));
}
export function PlanAdjustmentScreen({ mode = "edit" }: { mode?: AdjustmentRoute["mode"] }) {
  const auth = useAuth();
  const p = useParams<{ year: string; unit: string; seq?: string }>();
  const router = useRouter();
  const route = parseAdjustmentRoute(p.year, p.unit, mode, p.seq);
  if (!route) return <DetailPage title="Đường dẫn điều chỉnh không hợp lệ" back={{ href: PLAN_BASE, label: "Kế hoạch nhân lực" }}><p role="alert">Kiểm tra năm, đơn vị và số điều chỉnh trong đường dẫn.</p></DetailPage>;
  if (auth.status !== "authenticated" || !auth.principal || !auth.kn.context) return <DetailPage title="Điều chỉnh kế hoạch nhân lực" back={{ href: PLAN_BASE, label: "Kế hoạch nhân lực" }}><ReadOnlyNotice>Chưa xác định phiên làm việc hiện hành.</ReadOnlyNotice></DetailPage>;
  const identity = JSON.stringify([auth.status, auth.principal, auth.kn.context, auth.kn.support, clientIdentity(auth.api), route]);
  const canWrite = !auth.kn.context.read_only && !(auth.kn.context.role === "admin" && !auth.principal.mfa);
  return <AdjustmentLoader key={identity} route={route} apiClient={auth.api} canWrite={canWrite} go={href => router.push(href)} />;
}
function AdjustmentLoader({ route, apiClient, canWrite, go }: { route: AdjustmentRoute; apiClient: Parameters<typeof createPlanApi>[0]; canWrite: boolean; go: (href: string) => void }) {
  const api = useMemo(() => createPlanApi(apiClient), [apiClient]);
  const s = useLoad(() => api.unit(route.year, route.unit), [api, route.year, route.unit]);
  return <LoadState s={s}>{d => d.period.year === route.year && d.row.unit === route.unit ? <AdjustmentBody route={route} data={d} api={api} canWrite={canWrite} go={go} /> : <p role="alert">Không tìm thấy đúng năm và đơn vị được yêu cầu.</p>}</LoadState>;
}
function AdjustmentBody({ route, data: d, api, canWrite, go }: { route: AdjustmentRoute; data: HrmPlanUnit; api: PlanApi; canWrite: boolean; go: (href: string) => void }) {
  const back = { href: planUnitHref(route.year, route.unit), label: "Kế hoạch của đơn vị" };
  if (route.mode === "edit") return <AdjustmentEditor route={route} data={d} api={api} canWrite={canWrite} go={go} />;
  if (route.mode === "history") return <DetailPage title={`Lịch sử điều chỉnh ${route.year} · ${d.row.unit_name}`} back={back} log={<LogPanel entries={adjustmentLog(d.adjustments)} />}>
    <DataTable caption="Các bản điều chỉnh đã ghi" rows={[...d.adjustments].sort((a, b) => b.seq - a.seq)} rowKey={a => String(a.seq)} columns={[
      { key: "seq", header: "Số điều chỉnh", cell: a => a.seq }, { key: "at", header: "Thời điểm", cell: a => a.recorded_at }, { key: "actor", header: "Người thực hiện", cell: a => a.actor_ref },
      { key: "reason", header: "Lý do", cell: a => a.reason }, { key: "view", header: "Thao tác", cell: a => <Link href={adjustmentHref(route.year, route.unit, a.seq)}>Xem điều chỉnh {a.seq}</Link> },
    ]} />
  </DetailPage>;
  const a = d.adjustments.find(x => x.seq === route.seq);
  if (!a) return <DetailPage title="Không có bản điều chỉnh" back={back}><p role="alert">Không tìm thấy đúng số điều chỉnh được yêu cầu.</p></DetailPage>;
  return <DetailPage title={`Điều chỉnh số ${a.seq} · ${route.year} · ${d.row.unit_name}`} back={{ href: adjustmentHref(route.year, route.unit, "history"), label: "Lịch sử điều chỉnh" }} log={<LogPanel entries={adjustmentLog([a])} />}>
    <ReadOnlyNotice>Bản điều chỉnh đã ghi; kế hoạch đã chốt và các bản cũ giữ nguyên.</ReadOnlyNotice>
    <LabelValueBlock title="Thông tin bản điều chỉnh" items={[{ label: "Số điều chỉnh", value: a.seq }, { label: "Người thực hiện", value: a.actor_ref }, { label: "Thời điểm", value: a.recorded_at }, { label: "Lý do", value: a.reason }]} />
    <Values values={a.values} />
  </DetailPage>;
}
function Values({ values: v }: { values: HrmPlanUnit["row"]["values"] }) {
  return <><LabelValueBlock title="Số liệu kế hoạch" items={[{ label: "Hiện có", value: v.current }, { label: "Dự kiến nghỉ hưu", value: v.retiring }, { label: "Dự kiến nghỉ, chuyển công tác khác", value: v.leaving }, { label: "Số cần bổ sung", value: v.additional }, { label: "Nhu cầu theo kế hoạch", value: v.demand }, { label: "Ghi chú", value: v.note || "—" }]} />
    <DataTable caption="Nhu cầu theo vị trí của bản đã ghi" rows={(v.positions ?? []).map((p, i) => ({ ...p, _key: String(i) }))} rowKey={p => p._key} columns={[{ key: "title", header: "Vị trí", cell: p => p.title }, { key: "level", header: "Trình độ yêu cầu", cell: p => p.level || "—" }, { key: "count", header: "Số lượng", cell: p => p.count }, { key: "when", header: "Thời điểm cần", cell: p => p.when || "—" }, { key: "reason", header: "Lý do", cell: p => p.reason || "—" }]} /></>;
}
function AdjustmentEditor({ route, data: d, api, canWrite, go }: { route: AdjustmentRoute; data: HrmPlanUnit; api: PlanApi; canWrite: boolean; go: (href: string) => void }) {
  const [form, setForm] = useState<AdjustmentForm>(() => ({ leaving: String(d.row.values.leaving), additional: String(d.row.values.additional), reason: "" }));
  const [errors, setErrors] = useState<Partial<Record<keyof AdjustmentForm, string>>>({});
  const [edited, setEdited] = useState<Set<string>>(() => new Set());
  const act = useAction();
  const id = useId();
  const active = useRef(false);
  const saving = useRef(false);
  useEffect(() => { active.current = true; return () => { active.current = false; }; }, []);
  const allowed = canWrite && d.can_adjust && d.period.status === "closed";
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  const error = (key: keyof AdjustmentForm) => errors[key] || (!edited.has(key) ? server[key] : undefined);
  const change = (key: keyof AdjustmentForm, value: string) => {
    const next = { ...form, [key]: value }; setForm(next); setEdited(old => new Set(old).add(key));
    setErrors(old => { if (!old[key]) return old; const fresh = { ...old }; const message = validateAdjustment(next).errors[key]; if (message) fresh[key] = message; else delete fresh[key]; return fresh; });
  };
  const checked = validateAdjustment(form);
  const current = d.row.source === "auto" ? d.figures.current : d.row.values.current;
  const retiring = d.row.source === "auto" ? d.figures.retiring : d.row.values.retiring;
  const demand = !checked.errors.leaving && !checked.errors.additional ? planDemand({ current, retiring, leaving: Number(checked.values.leaving), additional: Number(checked.values.additional) }) : null;
  const save = async () => {
    if (!allowed || saving.current) return;
    const result = validateAdjustment(form); setErrors(result.errors); setEdited(new Set());
    if (!result.valid) return;
    saving.current = true;
    try {
      const a = await act.run(() => api.adjust(route.year, route.unit, { leaving: Number(result.values.leaving), additional: Number(result.values.additional), reason: result.values.reason, positions: d.row.values.positions ?? [], note: d.row.values.note ?? "" }));
      if (a && active.current) go(adjustmentHref(route.year, route.unit, a.seq));
    } finally { saving.current = false; }
  };
  return <DetailPage title={`Điều chỉnh kế hoạch đã chốt ${route.year} · ${d.row.unit_name}`} back={{ href: planUnitHref(route.year, route.unit), label: "Kế hoạch của đơn vị" }} log={<LogPanel entries={adjustmentLog(d.adjustments)} />} actions={<><button type="button" className="btn btn-o" disabled={act.busy} onClick={() => go(planUnitHref(route.year, route.unit))}>Hủy</button>{allowed ? <button type="submit" form={id} className="btn btn-p" disabled={act.busy}>Ghi điều chỉnh</button> : null}</>}>
    {act.view}
    <p><Link href={adjustmentHref(route.year, route.unit, "history")}>Lịch sử điều chỉnh</Link></p>
    {!allowed ? <ReadOnlyNotice>Chỉ HCTH và Admin có quyền hiện hành mới điều chỉnh kế hoạch đã chốt.</ReadOnlyNotice> : <form id={id} noValidate onSubmit={e => { e.preventDefault(); void save(); }}>
      <LabelValueBlock title="Số liệu tại ngày chốt" items={[{ label: "Ngày chốt số liệu", value: d.period.cutoff_on }, { label: "Hiện có", value: current }, { label: "Dự kiến nghỉ hưu", value: retiring }]} />
      <NumberField label="Dự kiến nghỉ, chuyển công tác khác" value={form.leaving} required rule={COUNT_RULE} onChange={v => change("leaving", v)} error={error("leaving")} disabled={act.busy} />
      <NumberField label="Số cần bổ sung" value={form.additional} required rule={COUNT_RULE} onChange={v => change("additional", v)} error={error("additional")} disabled={act.busy} />
      <TextField label="Lý do điều chỉnh" value={form.reason} required multiline onChange={v => change("reason", v)} error={error("reason")} hint="Nhập từ 10 đến 500 ký tự." disabled={act.busy} />
      <p>Nhu cầu theo kế hoạch: <strong>{demand ?? "—"}</strong></p>
      <p className="mute">Giữ nhu cầu theo vị trí và ghi chú của bản hiện hành; chỉ ghi khi bấm Ghi điều chỉnh. Kế hoạch đã chốt không bị sửa.</p>
    </form>}
  </DetailPage>;
}
