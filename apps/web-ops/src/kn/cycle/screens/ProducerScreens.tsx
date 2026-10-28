"use client";

// KNSPLIT-2-KN-RECORDS (#67): màn hồ sơ KN riêng HTX / Nông hộ / Tổ hợp tác trên /v1/kn/producer-records — thay đường
// ghi legacy /v1/kn/cooperatives (KN không tạo/sửa HTX nền tảng). Danh sách, chi tiết (Log bên phải), thêm/sửa trên
// trang riêng (R-A2), bắt buộc theo TT-08 (R-A4), số không âm (R-A1), STT (R-A5). HTX đã công bố chỉ để liên kết đối
// chiếu; máy móc và thửa của HTX vẫn khai báo ở màn riêng (module nền), đọc theo HTX đã liên kết.
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useState, type ReactNode } from "react";
import { fieldErrorMap } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { NumberField, SelectField, TextField, validateForm } from "@/components/form";
import { ActionBar, BackLink, DetailLayout, DetailPage, LabelValueBlock, LogPanel, RelatedTabs, type LogEntry } from "@/components/patterns/detail";
import { DataTable, FilterBar, FilterChip, PageHeader, RowActions, SearchBox, StatusPill, type Column } from "@/components/patterns/list";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";
import { PiiHint } from "@/kn/pii";
import type { KnProducer, KnProducerDetail, KnProducerKind, KnProducerWrite } from "../api";
import { LoadState, num, useAction, useCommuneNames, useCycleReadOnly, useLoad } from "../common";
import { useCommuneFilter } from "./DataScreens";

/** Cấu hình theo loại: nhãn, tiêu đề danh sách, đường dẫn gốc (TT-09: ba danh sách riêng). */
export const PRODUCER_KINDS: Record<KnProducerKind, { label: string; list: string; base: string; nameLabel: string; empty: string }> = {
  cooperative: { label: "HTX", list: "Danh sách HTX", base: "/kn/du-lieu/htx", nameLabel: "Tên HTX", empty: "Xã phụ trách chưa có HTX." },
  household: { label: "Nông hộ", list: "Danh sách Nông hộ", base: "/kn/du-lieu/htx/nong-ho", nameLabel: "Chủ hộ", empty: "Xã phụ trách chưa có nông hộ." },
  coop_group: { label: "Tổ hợp tác", list: "Danh sách Tổ hợp tác", base: "/kn/du-lieu/htx/to-hop-tac", nameLabel: "Tên Tổ hợp tác", empty: "Xã phụ trách chưa có tổ hợp tác." },
};

const EVENT_LABEL: Record<string, string> = { created: "Tạo hồ sơ", updated: "Sửa hồ sơ", deactivated: "Vô hiệu hóa", reactivated: "Kích hoạt lại" };

/** Log chuẩn của DetailPage từ nhật ký phiên bản (mới nhất trước). */
export function producerLog(d: KnProducerDetail): LogEntry[] {
  return d.log.map((e) => ({ event: EVENT_LABEL[e.event] ?? e.event, actor_display: e.actor_display ?? "—", at: e.at, reason: e.status_reason }));
}

const person = (r: KnProducer) => r.representative ?? r.holder;
const statusPill = (r: Pick<KnProducer, "status">) => <StatusPill tone={r.status === "active" ? "ok" : "neutral"}>{r.status === "active" ? "Hoạt động" : "Ngừng hoạt động"}</StatusPill>;

/** Đường dẫn chi tiết của hồ sơ (BF-TT-HTX #102: mỗi loại có trang riêng dưới /kn/du-lieu/htx). */
export function producerHref(r: Pick<KnProducer, "kind" | "ref">): string {
  return `${PRODUCER_KINDS[r.kind].base}/${r.ref}`;
}

const KIND_ORDER: KnProducerKind[] = ["cooperative", "household", "coop_group"];

/** TT-09 (#102): ba danh sách riêng HTX / Nông hộ / Tổ hợp tác — thanh tab (liên kết tới ba trang, mỗi trang bộ lọc riêng). */
export function ProducerTabs({ current }: { current: KnProducerKind }) {
  return (
    <div className="tabs" role="tablist" aria-label="Loại hồ sơ">
      {KIND_ORDER.map((k) => (
        <Link key={k} href={PRODUCER_KINDS[k].base} role="tab" aria-selected={k === current}>
          {PRODUCER_KINDS[k].label}
        </Link>
      ))}
    </div>
  );
}

/** Danh sách một loại hồ sơ, bộ lọc riêng của loại (TT-09). */
export function ProducerListScreen({ kind, title, actions }: { kind: KnProducerKind; title?: string; actions?: ReactNode }) {
  const { collectApi } = useAuth();
  const readOnly = useCycleReadOnly();
  const f = useCommuneFilter();
  const cfg = PRODUCER_KINDS[kind];
  const [status, setStatus] = useState<"active" | "inactive" | "all">("active");
  const [q, setQ] = useState("");
  const [parent, setParent] = useState("");
  const [page, setPage] = useState(1);
  const s = useLoad(
    () => collectApi.records({ kind, commune: f.commune || undefined, status, q: kind === "household" ? undefined : q || undefined, parent_ref: kind === "household" ? parent || undefined : undefined }),
    [kind, f.commune, status, q, parent],
  );
  const parents = useLoad(
    kind === "household"
      ? async () => {
          const [a, b] = await Promise.all([collectApi.records({ kind: "cooperative", status: "all" }), collectApi.records({ kind: "coop_group", status: "all" })]);
          return [...a.items, ...b.items];
        }
      : null,
    [kind],
  );
  const name = useCommuneNames(s.data?.items.map((c) => c.commune_code) ?? []);
  const columns: Column<KnProducer>[] = [
    { key: "name", header: cfg.nameLabel, cell: (c) => c.display_name },
    { key: "commune", header: "Xã/phường", cell: (c) => name(c.commune_code) },
    ...(kind === "household"
      ? [{ key: "parent", header: "Thuộc HTX/THT", cell: (c: KnProducer) => c.parent_name ?? "—" }]
      : [{ key: "rep", header: "Người đại diện", cell: (c: KnProducer) => person(c)?.label ?? "—" }]),
    { key: "phone", header: "Điện thoại", cell: (c) => person(c)?.masked_phone ?? "—" },
    { key: "area", header: "Diện tích (ha)", cell: (c) => num(c.area_ha), className: "num" },
    ...(kind === "household" ? [] : [{ key: "hh", header: "Nông hộ", cell: (c: KnProducer) => c.member_count, className: "num" }]),
    { key: "st", header: "Trạng thái", cell: (c) => statusPill(c) },
    { key: "act", header: "Thao tác", cell: (c) => <RowActions label={`${cfg.label} ${c.display_name}`} viewHref={producerHref(c)} editHref={!readOnly && c.can_edit && c.status === "active" ? `${producerHref(c)}/sua` : undefined} /> },
  ];
  return (
    <>
      <ProducerTabs current={kind} />
      <PageHeader
        title={title ?? cfg.list}
        actions={
          actions !== undefined ? actions : readOnly ? null : (
            <Link className="btn btn-p" href={`${cfg.base}/them-moi`}>
              Thêm {cfg.label}
            </Link>
          )
        }
      />
      <FilterBar onReset={() => (setStatus("active"), setQ(""), setParent(""), setPage(1))}>
        {f.chip}
        <FilterChip
          label="Trạng thái"
          value={status}
          defaultValue="active"
          onChange={(v) => (setStatus(v as typeof status), setPage(1))}
          options={[
            { value: "active", label: "Hoạt động" },
            { value: "inactive", label: "Ngừng hoạt động" },
            { value: "all", label: "Tất cả" },
          ]}
        />
        {kind === "household" ? (
          <FilterChip label="Thuộc HTX/THT" value={parent} onChange={(v) => (setParent(v), setPage(1))} options={[{ value: "", label: "Tất cả" }, ...(parents.data ?? []).map((p) => ({ value: p.ref, label: `${p.kind_label}: ${p.display_name}` }))]} />
        ) : (
          <SearchBox value={q} onChange={(v) => (setQ(v), setPage(1))} placeholder={`Tìm theo tên ${cfg.label}`} />
        )}
      </FilterBar>
      <LoadState s={s}>
        {(d) => <DataTable caption={cfg.list} columns={columns} rows={d.items.slice((page - 1) * 10, page * 10)} rowKey={(c) => c.ref} page={page} pageSize={10} total={d.items.length} onPage={setPage} empty={cfg.empty} />}
      </LoadState>
    </>
  );
}

/** Chi tiết HTX / Tổ hợp tác: Thông tin chung, Nông hộ, (HTX đã liên kết) Máy móc, Thửa; Log bên phải. */
export function ProducerDetailScreen({ kind }: { kind: KnProducerKind }) {
  const { id } = useParams<{ id: string }>();
  const { collectApi } = useAuth();
  const readOnly = useCycleReadOnly();
  const act = useAction();
  const cfg = PRODUCER_KINDS[kind];
  const s = useLoad(async () => {
    const c = await collectApi.record(id);
    const [hh, mm, pl] = await Promise.all([
      c.kind === "household" ? Promise.resolve({ items: [] as KnProducer[] }) : collectApi.records({ kind: "household", parent_ref: id, status: "all" }),
      c.htx_ref ? collectApi.coopMachines(c.htx_ref).catch(() => null) : Promise.resolve(null),
      c.htx_ref ? collectApi.plots({ cooperative_ref: c.htx_ref }).catch(() => null) : Promise.resolve(null),
    ]);
    return { c, hh: hh.items, mm, pl: pl?.items ?? [] };
  }, [id, act.note]);
  const [dlg, setDlg] = useState<null | "off" | "on">(null);
  const [reason, setReason] = useState("");
  const [reasonErr, setReasonErr] = useState<string | undefined>();
  useBreadcrumbTail(s.data ? [{ label: s.data.c.display_name }] : []);
  const name = useCommuneNames(s.data ? [s.data.c.commune_code] : []);
  return (
    <>
      <BackLink href={cfg.base}>{cfg.list}</BackLink>
      <LoadState s={s}>
        {({ c, hh: households, mm, pl }) => {
          const p = person(c);
          const editable = !readOnly && c.can_edit;
          return (
            <>
              <PageHeader title={c.display_name} badge={statusPill(c)} />
              {editable ? (
                <ActionBar>
                  {c.status === "active" && c.kind !== "household" ? (
                    <Link className="btn btn-p btn-sm" href={`${cfg.base}/${id}/them-nong-ho`}>
                      Thêm nông hộ
                    </Link>
                  ) : null}
                  {c.status === "active" ? (
                    <Link className="btn btn-o btn-sm" href={`${cfg.base}/${id}/sua`}>
                      Sửa
                    </Link>
                  ) : null}
                  <button type="button" className="btn btn-o btn-sm" onClick={() => (setReason(""), setReasonErr(undefined), setDlg(c.status === "active" ? "off" : "on"))}>
                    {c.status === "active" ? "Vô hiệu hóa" : "Kích hoạt lại"}
                  </button>
                </ActionBar>
              ) : null}
              {act.view}
              <DetailLayout
                main={
                  <>
                    <LabelValueBlock
                      title="Thông tin chung"
                      items={[
                        { label: "Loại", value: c.kind_label },
                        { label: c.kind === "household" ? "Chủ hộ" : "Người đại diện", value: p ? `${p.label}${p.masked_phone ? ` · ${p.masked_phone}` : ""}` : "—" },
                        ...(c.kind === "cooperative" ? [{ label: "Mã số thuế", value: c.tax_code }, { label: "HTX đã công bố (liên kết)", value: c.htx_name }] : []),
                        ...(c.kind === "household" ? [{ label: "Thuộc HTX/THT", value: c.parent_name }] : []),
                        { label: "Lý do trạng thái", value: c.status_reason },
                      ]}
                    />
                    <LabelValueBlock title="Địa bàn – sản xuất" items={[{ label: "Xã/phường", value: name(c.commune_code) }, { label: "Diện tích (ha)", value: num(c.area_ha) }, ...(c.kind === "household" ? [] : [{ label: "Địa chỉ trụ sở", value: c.address }])]} />
                    {c.kind === "household" ? null : (
                      <RelatedTabs
                        tabs={[
                          {
                            id: "hh",
                            label: `Nông hộ (${households.filter((h) => h.status === "active").length})`,
                            content: households.length ? (
                              <ul className="list-plain">
                                {households.map((h) => (
                                  <li key={h.ref}>
                                    {h.display_name} · {h.holder?.masked_phone ?? "—"} · {num(h.area_ha)} ha {h.status === "active" ? "" : "(ngừng)"}
                                  </li>
                                ))}
                              </ul>
                            ) : (
                              <p className="mute">Chưa có nông hộ.</p>
                            ),
                          },
                          ...(c.htx_ref
                            ? [
                                { id: "mm", label: `Máy móc (${mm?.total ?? 0})`, content: <ul className="list-plain">{(mm?.lines ?? []).map((l) => <li key={l.machine_type}>{l.machine_type_name}: {l.quantity}</li>)}</ul> },
                                { id: "pl", label: `Thửa ruộng (${pl.length})`, content: <ul className="list-plain">{pl.map((x) => <li key={x.ref}>{x.lot_name ?? x.code} · {num(x.area_ha, 2)} ha</li>)}</ul> },
                              ]
                            : []),
                        ]}
                      />
                    )}
                  </>
                }
                log={<LogPanel entries={producerLog(c)} />}
              />
              {dlg ? (
                <Dialog
                  title={dlg === "off" ? `Vô hiệu hóa ${c.kind_label}` : `Kích hoạt lại ${c.kind_label}`}
                  onClose={() => setDlg(null)}
                  actions={
                    <button
                      type="button"
                      className="btn btn-p"
                      disabled={act.busy}
                      onClick={async () => {
                        // Nhập lý do ngắn vẫn làm trong pop-up (ui-kit.md). Máy chủ cũng kiểm 10–1000 ký tự, không SĐT/CCCD.
                        const t = reason.trim();
                        const err = t === "" ? "Bắt buộc nhập." : [...t].length < 10 ? "Tối thiểu 10 ký tự." : undefined;
                        setReasonErr(err);
                        if (err) return;
                        const off = dlg === "off";
                        if (await act.run(() => collectApi.setRecordStatus(id, off, { reason: t, base_version: c.version }), off ? "Đã vô hiệu hóa (không xóa)." : "Đã kích hoạt lại.")) setDlg(null);
                      }}
                    >
                      {dlg === "off" ? "Vô hiệu hóa" : "Kích hoạt lại"}
                    </button>
                  }
                >
                  {dlg === "off" ? <p className="small">Nông hộ thuộc {c.kind_label} vẫn giữ nguyên — vô hiệu hóa thay xóa.</p> : null}
                  <TextField label="Lý do (≥ 10 ký tự)" required multiline value={reason} onChange={setReason} error={reasonErr ?? (act.problem ? fieldErrorMap(act.problem).reason : undefined)} hint={<PiiHint text={reason} />} />
                </Dialog>
              ) : null}
            </>
          );
        }}
      </LoadState>
    </>
  );
}

type Form = { kind: string; commune_code: string; name: string; area_ha: string; tax_code: string; address: string; rep: string; phone: string; parent_ref: string; htx_ref: string };
const EMPTY: Form = { kind: "", commune_code: "", name: "", area_ha: "", tax_code: "", address: "", rep: "", phone: "", parent_ref: "", htx_ref: "" };
const AREA_RULE = { maxDecimals: 4, max: 1_000_000 } as const;

/** Thân gửi máy chủ từ form đã kiểm (trường tùy chọn rỗng → null; người đại diện chỉ gửi khi có nhập). */
export function producerBody(kind: KnProducerKind, b: Form, withPerson: boolean): KnProducerWrite {
  const out: KnProducerWrite = { kind, commune_code: b.commune_code, area_ha: b.area_ha || null };
  if (kind !== "household") {
    out.name = b.name.trim();
    out.address = b.address.trim() || null;
  }
  if (kind === "cooperative") {
    out.tax_code = b.tax_code.trim() || null;
    out.htx_ref = b.htx_ref || null;
  }
  if (kind === "household") out.parent_ref = b.parent_ref || null;
  if (withPerson) {
    const p = { full_name: b.rep.trim(), phone: b.phone.trim() };
    if (kind === "household") out.holder = p;
    else out.representative = p;
  }
  return out;
}

/**
 * Thêm / Sửa hồ sơ trên trang riêng (R-A2). Bắt buộc theo TT-08: Xã/phường, Loại, Tên (HTX, THT), Người đại diện / Chủ hộ,
 * Điện thoại. Sửa: để trống người đại diện = giữ người hiện tại (họ tên, SĐT ở kho định danh, không hiện rõ).
 * `kinds` > 1 → hiện ô "Loại" (trang Thêm chung); `parentRef` → Nông hộ thêm từ chi tiết HTX/THT.
 */
export function ProducerFormScreen({ kind: fixedKind, kinds, parentRef, editRef, back }: { kind?: KnProducerKind; kinds?: KnProducerKind[]; parentRef?: string; editRef?: string; back: { href: string; label: string } }) {
  const { collectApi } = useAuth();
  const router = useRouter();
  const readOnly = useCycleReadOnly();
  const { communes } = useCommuneFilter();
  const act = useAction();
  const cur = useLoad(editRef ? () => collectApi.record(editRef) : null, [editRef]);
  const parent = useLoad(parentRef ? () => collectApi.record(parentRef) : null, [parentRef]);
  const [form, setForm] = useState<Form>(() => ({ ...EMPTY, kind: fixedKind ?? "", commune_code: communes.length === 1 ? communes[0]!.code : "", parent_ref: parentRef ?? "" }));
  const [loaded, setLoaded] = useState<string | null>(null);
  if (cur.data && loaded !== cur.data.ref) {
    const d = cur.data;
    setLoaded(d.ref);
    setForm({ ...EMPTY, kind: d.kind, commune_code: d.commune_code, name: d.name ?? "", area_ha: d.area_ha ?? "", tax_code: d.tax_code ?? "", address: d.address ?? "", parent_ref: d.parent_ref ?? "", htx_ref: d.htx_ref ?? "" });
  }
  if (parent.data && !editRef && form.commune_code === "" && loaded !== parent.data.ref) {
    setLoaded(parent.data.ref);
    setForm((x) => ({ ...x, commune_code: parent.data!.commune_code }));
  }
  const kind = (cur.data?.kind ?? form.kind) as KnProducerKind | "";
  const candidates = useLoad(kind === "cooperative" && form.commune_code ? () => collectApi.htxCandidates(form.commune_code) : null, [kind, form.commune_code]);
  const parents = useLoad(
    kind === "household" && !parentRef
      ? async () => {
          const [a, b] = await Promise.all([collectApi.records({ kind: "cooperative" }), collectApi.records({ kind: "coop_group" })]);
          return [...a.items, ...b.items];
        }
      : null,
    [kind, parentRef],
  );
  const [errors, setErrors] = useState<Partial<Record<keyof Form, string>>>({});
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  const set = (k: keyof Form) => (v: string) => setForm((x) => ({ ...x, [k]: v }));
  const editing = !!editRef;
  const personLabel = kind === "household" ? "Chủ hộ" : "Người đại diện";
  const pk = kind === "household" ? "holder" : "representative";
  const save = async () => {
    const personTouched = !editing || form.rep.trim() !== "" || form.phone.trim() !== "";
    const v = validateForm(form, {
      kind: { kind: "text", required: true },
      commune_code: { kind: "text", required: true },
      ...(kind !== "household" ? { name: { kind: "text" as const, required: true, maxLength: 200 }, address: { kind: "text" as const, maxLength: 300 } } : {}),
      ...(kind === "cooperative" ? { tax_code: { kind: "text" as const, maxLength: 14 } } : {}),
      area_ha: { kind: "number", ...AREA_RULE },
      rep: { kind: "text", required: personTouched, maxLength: 120 },
      phone: { kind: "text", required: personTouched, maxLength: 20 },
    });
    const errs = { ...v.errors };
    if (!errs.phone && personTouched && !/^0[1-9]\d{8}$/.test(form.phone.replace(/[\s.()-]/g, ""))) errs.phone = "Số điện thoại 10 chữ số, bắt đầu bằng 0.";
    if (!errs.tax_code && kind === "cooperative" && form.tax_code.trim() && !/^\d{10}(-\d{3})?$/.test(form.tax_code.trim())) errs.tax_code = "Mã số thuế 10 chữ số (hoặc 10-3).";
    setErrors(errs);
    if (Object.values(errs).some(Boolean) || !kind) return;
    const body = producerBody(kind, v.values, personTouched);
    const r = await act.run(() => (editing && cur.data ? collectApi.updateRecord(editRef!, { ...body, base_version: cur.data.version }) : collectApi.createRecord(body)));
    if (r) router.push(parentRef ? back.href : producerHref(r));
  };
  const title = editing ? `Sửa ${cur.data?.kind_label ?? ""} ${cur.data?.display_name ?? ""}`.replace(/\s+/g, " ").trim() : kind === "household" ? "Thêm nông hộ" : kind ? `Thêm ${PRODUCER_KINDS[kind].label}` : "Thêm hồ sơ";
  const kindOptions = (kinds ?? []).map((k) => ({ value: k, label: PRODUCER_KINDS[k].label }));
  const communeOptions = communes.map((c) => ({ value: c.code, label: c.name }));
  if (form.commune_code && !communeOptions.some((o) => o.value === form.commune_code)) communeOptions.push({ value: form.commune_code, label: form.commune_code });
  return (
    <DetailPage
      back={{ href: back.href, label: parent.data?.display_name ?? back.label }}
      title={title}
      actions={
        <>
          <Link className="btn btn-o" href={back.href}>
            Hủy
          </Link>
          <button type="button" className="btn btn-p" disabled={readOnly || act.busy || (editing && !cur.data) || parent.data?.status === "inactive"} onClick={() => void save()}>
            Lưu
          </button>
        </>
      }
    >
      {act.view}
      <div className="grid2">
        {!editing && kindOptions.length > 1 ? <SelectField label="Loại" required value={form.kind} onChange={(v) => setForm((x) => ({ ...x, kind: v, parent_ref: "", htx_ref: "" }))} error={errors.kind ?? server.kind} options={kindOptions} /> : null}
        <SelectField label="Xã/phường" required value={form.commune_code} onChange={(v) => setForm((x) => ({ ...x, commune_code: v, htx_ref: "" }))} error={errors.commune_code ?? server.commune_code} options={communeOptions} />
        {kind && kind !== "household" ? <TextField label={PRODUCER_KINDS[kind].nameLabel} required value={form.name} onChange={set("name")} error={errors.name ?? server.name} maxLength={200} /> : null}
        <TextField
          label={editing ? `${personLabel} mới (để trống = giữ ${cur.data ? (person(cur.data)?.label ?? "người hiện tại") : "người hiện tại"})` : personLabel}
          required={!editing}
          value={form.rep}
          onChange={set("rep")}
          error={errors.rep ?? server[`${pk}.full_name`]}
          maxLength={120}
        />
        <TextField label={`Điện thoại ${personLabel.toLowerCase()}`} required={!editing} value={form.phone} onChange={set("phone")} error={errors.phone ?? server[`${pk}.phone`]} maxLength={20} hint="Lưu ở kho định danh, hiển thị che." />
        <NumberField label={kind === "household" ? "Diện tích canh tác" : "Diện tích"} unit="ha" value={form.area_ha} onChange={set("area_ha")} rule={AREA_RULE} error={errors.area_ha ?? server.area_ha} onBlurError={(e) => setErrors((x) => ({ ...x, area_ha: e }))} />
        {kind === "cooperative" ? <TextField label="Mã số thuế" value={form.tax_code} onChange={set("tax_code")} error={errors.tax_code ?? server.tax_code} maxLength={14} /> : null}
        {kind && kind !== "household" ? <TextField label="Địa chỉ trụ sở" value={form.address} onChange={set("address")} error={errors.address ?? server.address} maxLength={300} /> : null}
        {kind === "household" && !parentRef ? (
          <SelectField label="Thuộc HTX/Tổ hợp tác" value={form.parent_ref} onChange={set("parent_ref")} error={server.parent_ref} placeholder="Không thuộc HTX/THT" options={[{ value: "", label: "Không thuộc HTX/THT" }, ...(parents.data ?? []).map((p) => ({ value: p.ref, label: `${p.kind_label}: ${p.display_name}` }))]} />
        ) : null}
        {kind === "household" && parentRef ? <TextField label="Thuộc HTX/Tổ hợp tác" value={parent.data?.display_name ?? ""} onChange={() => undefined} disabled /> : null}
        {kind === "cooperative" ? (
          <SelectField
            label="Liên kết HTX đã công bố (tùy chọn)"
            value={form.htx_ref}
            onChange={set("htx_ref")}
            error={server.htx_ref}
            placeholder="Không liên kết"
            options={[{ value: "", label: "Không liên kết" }, ...(candidates.data?.items ?? []).filter((x) => !x.linked_ref || x.linked_ref === editRef || x.htx_ref === form.htx_ref).map((x) => ({ value: x.htx_ref, label: x.name }))]}
          />
        ) : null}
      </div>
    </DetailPage>
  );
}

/** Trang Thêm/Sửa theo tham số đường dẫn (dùng ở app/kn/du-lieu/…/[id]/sua, …/[id]/them-nong-ho). */
export function ProducerEditRoute({ kind }: { kind: KnProducerKind }) {
  const { id } = useParams<{ id: string }>();
  const cfg = PRODUCER_KINDS[kind];
  return <ProducerFormScreen kind={kind} editRef={id} back={{ href: `${cfg.base}/${id}`, label: cfg.label }} />;
}

export function HouseholdAddRoute({ parentKind }: { parentKind: KnProducerKind }) {
  const { id } = useParams<{ id: string }>();
  const cfg = PRODUCER_KINDS[parentKind];
  return <ProducerFormScreen kind="household" parentRef={id} back={{ href: `${cfg.base}/${id}`, label: cfg.label }} />;
}

const ALL_KINDS: KnProducerKind[] = ["cooperative", "household", "coop_group"];

/** TT-08 (#102): trang Thêm chung có ô "Loại" (bắt buộc), mặc định theo danh sách đang xem. */
export function ProducerCreateRoute({ kind }: { kind: KnProducerKind }) {
  const cfg = PRODUCER_KINDS[kind];
  return <ProducerFormScreen kind={kind} kinds={ALL_KINDS} back={{ href: cfg.base, label: cfg.list }} />;
}

/** Trang chi tiết theo loại (Nông hộ, Tổ hợp tác — #102). */
export function ProducerDetailRoute({ kind }: { kind: KnProducerKind }) {
  return <ProducerDetailScreen kind={kind} />;
}