"use client";

import Link from "next/link";
import { fieldErrorMap } from "@/api/problem";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { TextField, validateForm } from "@/components/form";
import { DetailPage, LogPanel } from "@/components/patterns/detail";
import { LoadState, ReadOnlyNotice, useAction, useLoad } from "@/kn/cycle/common";
import { localPeriodDate, periodDateISO } from "./form";
import { createKhtcPeriodApi, type KhtcPeriod } from "./api";
import { KhtcPeriodsScreen, PERIOD_BASE, periodHref, periodLog, type PeriodRoute } from "./KhtcPeriodsScreen";

/** Layout giữ bản nháp trong bộ nhớ khi đi lại giữa các route cùng phiên. */
export function KhtcPeriodWorkspace() {
  const { kn } = useAuth();
  const path = usePathname();
  const router = useRouter();
  const tail = path.slice(PERIOD_BASE.length).split("/").filter(Boolean);
  const id = tail[0] === "ky" ? tail[1] : undefined;
  const view: PeriodRoute["view"] = tail[0] === "them-moi" ? "new" : id ? tail[2] === "sua-han" ? "deadline" : "detail" : "list";
  // Không mang bản nháp sang ngữ cảnh, vai hoặc tenant khác.
  return <Workspace key={JSON.stringify(kn.context)} route={{ view, id, go: router.push }} />;
}
function Workspace({ route }: { route: PeriodRoute }) {
  return <>
    <KhtcPeriodsScreen route={route} />
    {route.view === "deadline" && route.id ? <PeriodDeadline key={route.id} id={route.id} onSaved={() => route.go(periodHref(route.id!))} /> : null}
  </>;
}

function PeriodDeadline({ id, onSaved }: { id: string; onSaved: () => void }) {
  const { api, kn } = useAuth();
  const kapi = useMemo(() => createKhtcPeriodApi(api), [api]);
  const canEdit = kn.context?.role === "kn_tinh" && kn.context.department_code === "ke_hoach_tai_chinh" && !kn.context.read_only;
  const loaded = useLoad(canEdit ? () => kapi.get(id) : null, [kapi, id, canEdit]);
  if (!canEdit) return <DetailPage title="Sửa hạn nộp" back={{ href: periodHref(id), label: "Chi tiết kỳ" }}><ReadOnlyNotice>Chỉ xem — Phòng KHTC đang có quyền soạn mới được sửa hạn.</ReadOnlyNotice></DetailPage>;
  return <LoadState s={loaded}>{p => p.id === id ? <DeadlineEditor key={`${id}:${p.events.length}`} period={p} onSaved={onSaved} /> : null}</LoadState>;
}
function DeadlineEditor({ period: p, onSaved }: { period: KhtcPeriod; onSaved: () => void }) {
  const { api, kn } = useAuth();
  const kapi = useMemo(() => createKhtcPeriodApi(api), [api]);
  const [due, setDue] = useState(localPeriodDate(p.due_at));
  const [reason, setReason] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const act = useAction();
  const formId = useId();
  const mounted = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  const closed = p.state === "closed" || p.state === "cancelled";
  const save = async () => {
    if (closed || act.busy || kn.context?.read_only || kn.context?.role !== "kn_tinh" || kn.context.department_code !== "ke_hoach_tai_chinh") return;
    const checked = validateForm({ due, reason }, { due: { kind: "text", required: true }, reason: { kind: "text", maxLength: 500 } });
    const next = { ...checked.errors };
    const dueISO = periodDateISO(due);
    if (!dueISO) next.due = "Nhập ngày giờ hợp lệ theo giờ Việt Nam.";
    setErrors(next);
    if (Object.keys(next).length || !dueISO) return;
    if (await act.run(() => kapi.deadline(p.id, dueISO, undefined, reason.trim() || undefined), "Đã sửa hạn nộp.") && mounted.current) onSaved();
  };
  return <DetailPage title={`Sửa hạn nộp — ${p.name}`} back={{ href: periodHref(p.id), label: "Chi tiết kỳ" }} log={<LogPanel entries={periodLog(p)} />} actions={<><Link className="btn btn-o" href={periodHref(p.id)}>Hủy</Link>{!closed ? <button className="btn btn-p" type="submit" form={formId} disabled={act.busy}>Lưu</button> : null}</>}>
    {act.view}
    {closed ? <ReadOnlyNotice>Kỳ đã đóng hoặc hủy; không sửa hạn.</ReadOnlyNotice> : <form id={formId} noValidate onSubmit={e => { e.preventDefault(); void save(); }}>
      <TextField label="Hạn nộp" required value={due} onChange={setDue} error={errors.due || server.due_at} hint="YYYY-MM-DDTHH:mm, giờ Việt Nam (UTC+7)." />
      <TextField label="Lý do" value={reason} onChange={setReason} maxLength={500} error={errors.reason || server.reason} />
    </form>}
  </DetailPage>;
}
