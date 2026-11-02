"use client";

import Link from "next/link";
import { useParams, usePathname, useRouter, useSearchParams } from "next/navigation";
import { useMemo, useState } from "react";
import { fieldErrorMap } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { NumberField, TextField } from "@/components/form";
import { BackLink, DetailPage, LabelValueBlock, LogPanel, type LogEntry } from "@/components/patterns/detail";
import { DataTable } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { createHrmApi, dmy, vnDecimal, type HrmApi, type HrmItem, type HrmScale } from "./api";
import { catalogBase, catalogItemPath } from "./catalog-form";
import { effectiveScale, scaleListPath, scalePath, scaleToday, validScaleDay, validateScale, type ScaleValues } from "./scale-form";

function useApi() { const { api } = useAuth(); return useMemo(() => createHrmApi(api), [api]); }

export function scaleHistory(versions: HrmScale[]): LogEntry[] {
  const sorted = [...versions].sort((a, b) => b.version - a.version);
  return sorted.map((v, i) => ({ event: `Phiên bản ${v.version}${v.placeholder ? " · Mẫu" : ""}`, at: v.recorded_at,
    actor_display: "Chưa có thông tin người thực hiện", // API không trả tên actor; không suy từ vai hiện tại.
    changes: [
      { field: "Hiệu lực từ", old: sorted[i + 1] ? dmy(sorted[i + 1]!.valid_from) : null, new: dmy(v.valid_from) },
      { field: "Hệ số từng bậc", old: sorted[i + 1]?.coefficients.map(vnDecimal).join(" · ") ?? null, new: v.coefficients.map(vnDecimal).join(" · ") },
      { field: "Giữ bậc (tháng)", old: sorted[i + 1] ? String(sorted[i + 1]!.hold_months) : null, new: String(v.hold_months) },
      { field: "Ghi chú", old: sorted[i + 1]?.note || null, new: v.note || null },
    ].filter((change) => change.old !== change.new),
  }));
}

export function HrmScalesList({ ha, base, canManage, nonce, initialDay = scaleToday() }: { ha: HrmApi; base: string; canManage: boolean; nonce: number; initialDay?: string }) {
  const [day, setDay] = useState(initialDay);
  const [page, setPage] = useState(1);
  const router = useRouter();
  const list = useLoad(validScaleDay(day) ? () => ha.scales(day) : null, [day, nonce]);
  return <>
    <div className="row" style={{ marginBottom: 12 }}><h2 className="h3">Bảng hệ số theo bậc</h2>
      <label htmlFor="scale-day">Hiệu lực tại ngày</label><input id="scale-day" type="date" value={day} onChange={(e) => {
        const value = e.target.value; setDay(value); setPage(1); if (validScaleDay(value)) router.replace(scaleListPath(base, value));
      }} />
    </div>
    {!validScaleDay(day) ? <p role="alert">Ngày hiệu lực không hợp lệ.</p> : <LoadState s={list}>{(data) => <DataTable
      caption="Bảng hệ số các ngạch" rows={data.grades.slice((page - 1) * 20, page * 20)} rowKey={(r) => r.grade.code}
      page={page} pageSize={20} total={data.grades.length} onPage={setPage}
      columns={[
        { key: "code", header: "Mã ngạch", cell: ({ grade }) => <Link href={scalePath(base, grade.code, day)}><code>{grade.code}</code></Link> },
        { key: "name", header: "Tên ngạch/CDNN", cell: ({ grade }) => grade.name },
        { key: "group", header: "Nhóm", cell: ({ grade }) => grade.attrs.equiv_group ?? "—" },
        { key: "steps", header: "Số bậc", cell: ({ scale }) => scale?.steps ?? "—" },
        { key: "coef", header: "Hệ số bậc 1 → cuối", cell: ({ scale }) => scale ? `${vnDecimal(scale.coefficients[0])} → ${vnDecimal(scale.coefficients.at(-1))}${scale.placeholder ? " · Mẫu" : ""}` : "Chưa có" },
        { key: "hold", header: "Giữ bậc", cell: ({ scale }) => scale ? `${scale.hold_months} tháng` : "—" },
        { key: "from", header: "Hiệu lực từ", cell: ({ scale }) => scale ? dmy(scale.valid_from) : "—" },
        { key: "actions", header: "Thao tác", cell: ({ grade }) => <div className="row"><Link className="btn btn-o btn-sm" href={scalePath(base, grade.code, day)} aria-label={`Chi tiết hệ số ${grade.code}`}>Chi tiết</Link>{canManage ? <Link className="btn btn-p btn-sm" href={scalePath(base, grade.code, day, true)} aria-label={`Bảng hệ số mới ${grade.code}`}>Bảng mới</Link> : null}</div> },
      ]} empty="Chưa có ngạch." />}</LoadState>}
  </>;
}

export function HrmScaleScreen({ create = false }: { create?: boolean }) {
  const context = useAuth().kn?.context?.context_id ?? "unknown";
  const { grade } = useParams<{ grade: string }>();
  const base = catalogBase(usePathname());
  const day = useSearchParams().get("at") ?? scaleToday();
  if (!validScaleDay(day)) return <><BackLink href={`${base}/he-so`}>Danh sách bảng hệ số</BackLink><p role="alert">Ngày hiệu lực không hợp lệ.</p></>;
  return <ScaleAccess key={`${context}/${grade}/${day}/${create}`} grade={grade} base={base} day={day} create={create} />;
}

function ScaleAccess({ grade, base, day, create }: { grade: string; base: string; day: string; create: boolean }) {
  const ha = useApi();
  const status = useLoad(() => ha.status(), []);
  const allowed = status.data?.can_manage === true;
  const data = useLoad(!create || allowed ? async () => {
    const history = await ha.scaleHistory(grade);
    const catalog = await ha.history("grade", grade);
    return { versions: history.versions, item: [...catalog.versions].sort((a, b) => b.version - a.version)[0] };
  } : null, [grade, create, allowed]);
  return <LoadState s={status}>{() => create && !allowed ? <><BackLink href={scaleListPath(base, day)}>Danh sách bảng hệ số</BackLink><p role="alert">Bạn không có quyền thêm bảng hệ số.</p></> : <LoadState s={data}>{({ versions, item }) => {
    if (!item) return <p role="alert">Không tìm thấy ngạch.</p>;
    return create ? <ScaleEditor ha={ha} item={item} versions={versions} base={base} day={day} /> : <ScaleDetail item={item} versions={versions} base={base} day={day} canManage={allowed} />;
  }}</LoadState>}</LoadState>;
}

function ScaleDetail({ item, versions, base, day, canManage }: { item: HrmItem; versions: HrmScale[]; base: string; day: string; canManage: boolean }) {
  const current = effectiveScale(versions, day);
  return <DetailPage back={{ href: scaleListPath(base, day), label: "Danh sách bảng hệ số" }} title={`Bảng hệ số — ${item.name} (${item.code})`}
    actions={canManage ? <Link className="btn btn-p btn-sm" href={scalePath(base, item.code, day, true)}>Bảng mới</Link> : undefined}
    log={<LogPanel entries={scaleHistory(versions)} empty="Chưa có phiên bản bảng hệ số." />}>
    <p className="mute">Hiệu lực tại {dmy(day)}. Thay đổi bảng không làm đổi quyết định nâng bậc đã ban hành.</p>
    <Link href={catalogItemPath(base, "grade", item.code)}>Mục ngạch {item.code}</Link>
    {current ? <>
      <LabelValueBlock title="Bảng đang có hiệu lực" items={[{ label: "Phiên bản", value: current.version }, { label: "Hiệu lực từ", value: dmy(current.valid_from) }, { label: "Số bậc", value: current.steps }, { label: "Giữ bậc (tháng)", value: current.hold_months }, { label: "Ghi chú", value: current.note }, { label: "Dữ liệu mẫu", value: current.placeholder ? "Mẫu — chờ HCTH xác nhận (Q-070)" : "Không" }]} />
      <LabelValueBlock title="Hệ số từng bậc" items={current.coefficients.map((c, i) => ({ label: `Bậc ${i + 1}`, value: vnDecimal(c) }))} />
    </> : <p>Chưa có bảng hiệu lực tại ngày này.</p>}
    <DataTable caption="Lịch sử bảng hệ số" rows={[...versions].sort((a, b) => b.version - a.version)} rowKey={(v) => `${v.valid_from}/${v.version}`} columns={[
      { key: "version", header: "Phiên bản", cell: (v) => v.version }, { key: "from", header: "Hiệu lực từ", cell: (v) => dmy(v.valid_from) },
      { key: "coefficients", header: "Hệ số từng bậc", cell: (v) => v.coefficients.map(vnDecimal).join(" · ") },
      { key: "hold", header: "Giữ bậc (tháng)", cell: (v) => v.hold_months }, { key: "note", header: "Ghi chú", cell: (v) => v.note || "—" },
    ]} />
  </DetailPage>;
}

function ScaleEditor({ ha, item, versions, base, day }: { ha: HrmApi; item: HrmItem; versions: HrmScale[]; base: string; day: string }) {
  const effective = effectiveScale(versions, day);
  const [values, setValues] = useState<ScaleValues>({ from: day, hold: String(effective?.hold_months ?? 36), coefficients: effective?.coefficients.map(vnDecimal) ?? [""], note: "" });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const act = useAction();
  const router = useRouter();
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  const error = (key: string) => errors[key] || server[key === "from" ? "valid_from" : key === "hold" ? "hold_months" : key];
  const set = (patch: Partial<ScaleValues>) => setValues((v) => ({ ...v, ...patch }));
  const save = async () => {
    const checked = validateScale(values);
    setErrors(checked.errors);
    if (!checked.valid) return;
    const saved = await act.run(() => ha.addScale(item.code, checked.body));
    if (saved) router.push(scalePath(base, item.code, values.from));
  };
  return <DetailPage back={{ href: scaleListPath(base, day), label: "Danh sách bảng hệ số" }} title={`Bảng hệ số mới — ${item.name} (${item.code})`}
    log={<LogPanel entries={scaleHistory(versions)} empty="Lịch sử xuất hiện sau khi lưu bảng đầu tiên." />}
    actions={<><Link className="btn btn-o btn-sm" href={scaleListPath(base, day)}>Hủy</Link><button type="submit" form="hrm-scale" className="btn btn-p btn-sm" disabled={act.busy}>Lưu</button></>}>
    {act.view}<p>Bảng mới áp dụng từ ngày hiệu lực; giữ nguyên bảng cũ và các quyết định đã ban hành.</p>
    <form id="hrm-scale" noValidate onSubmit={(e) => { e.preventDefault(); if (!act.busy) void save(); }}>
      <div className="grid2">
        <div className="field"><label htmlFor="scale-from">Ngày hiệu lực <span className="req" aria-hidden="true">*</span></label><input id="scale-from" type="date" value={values.from} aria-required="true" aria-invalid={error("from") ? true : undefined} aria-describedby={error("from") ? "scale-from-error" : undefined} onChange={(e) => set({ from: e.target.value })} />{error("from") ? <span id="scale-from-error" className="err" role="alert">{error("from")}</span> : null}</div>
        <NumberField label="Thời gian giữ bậc (tháng)" required value={values.hold} onChange={(hold) => set({ hold })} rule={{ integer: true, positive: true, max: 120, maxInputLength: 32 }} error={error("hold")} onBlurError={(message) => setErrors((e) => ({ ...e, hold: message ?? "" }))} hint="Số nguyên từ 1 đến 120." />
      </div>
      <fieldset><legend>Hệ số từng bậc — tăng dần, từ 1 đến 20 bậc</legend><div className="grid2">{values.coefficients.map((coefficient, i) => <NumberField key={i} label={`Hệ số bậc ${i + 1}`} required value={coefficient}
        onChange={(value) => set({ coefficients: values.coefficients.map((v, j) => j === i ? value : v) })}
        rule={{ positive: true, max: "99.99", maxDecimals: 2, maxInputLength: 32 }} error={error(`coefficient_${i}`) || server[`coefficients.${i}`] || server[`coefficients[${i}]`]}
        onBlurError={(message) => setErrors((e) => ({ ...e, [`coefficient_${i}`]: message ?? "" }))} hint="Lớn hơn 0, không quá 99,99; tối đa 2 chữ số thập phân." />)}</div>
        {error("coefficients") ? <p className="err" role="alert">{error("coefficients")}</p> : null}
        <div className="row"><button type="button" className="btn btn-o btn-sm" disabled={values.coefficients.length >= 20} onClick={() => set({ coefficients: [...values.coefficients, ""] })}>+ Bậc</button><button type="button" className="btn btn-o btn-sm" disabled={values.coefficients.length <= 1} onClick={() => set({ coefficients: values.coefficients.slice(0, -1) })}>− Bậc</button></div>
      </fieldset>
      <TextField label="Ghi chú (căn cứ)" value={values.note} maxLength={500} onChange={(note) => set({ note })} error={error("note")} />
    </form>
  </DetailPage>;
}
