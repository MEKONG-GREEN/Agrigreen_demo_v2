"use client";

// Cấp Xã › Dữ liệu (FN-XA-03.03..03.06) trên API P7 /v1/kn/*: HTX & nông hộ (thêm, vô hiệu hóa thay xóa, SĐT che),
// máy móc theo HTX (khai báo thay toàn bộ, tổng xã = Σ HTX; năm SX ở hồ sơ KN riêng #119), thửa ruộng (danh sách; vẽ ở field-app), vật tư (ghi nhận,
// tổng hợp theo xã). Cấp Tỉnh/Quốc gia chỉ xem (useCycleReadOnly + máy chủ 403 *.read_only).
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useState } from "react";
import { fieldErrorMap } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { NumberField, SelectField, TextField, parseNumberInput, validateForm } from "@/components/form";
import { ActionBar, BackLink, DetailLayout, DetailPage, LabelValueBlock, LogPanel, RelatedTabs } from "@/components/patterns/detail";
import { DataTable, FilterBar, FilterChip, PageHeader, RowActions, SearchBox, StatusPill, type Column } from "@/components/patterns/list";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";
import type { KnCooperative } from "../api";
import { LoadState, num, useAction, useCommuneNames, useCycleReadOnly, useLoad } from "../common";
import { PiiHint } from "@/kn/pii";
import { vnDate } from "@/kn/schedule/week";
import { normalizeNumberInput } from "@/lib/exactDecimal";

/** Ghi chú phần còn ở nơi khác (nạp Excel, vẽ bản đồ ngoại tuyến…). */
export function GapNotice({ what }: { what: string }) {
  return (
    <div className="alert alert-info" role="note" style={{ marginBottom: 12 }}>
      <Icon name="info" />
      <span>{what}</span>
    </div>
  );
}

/** Bộ lọc Xã/phường theo các xã phụ trách của phiên KN (dùng chung các màn Dữ liệu xã). */
export function useCommuneFilter() {
  const { kn } = useAuth();
  const communes = kn.context?.communes ?? [];
  const [commune, setCommune] = useState("");
  const chip = <FilterChip label="Xã/phường" value={commune} onChange={setCommune} options={[{ value: "", label: "Tất cả xã phụ trách" }, ...communes.map((c) => ({ value: c.code, label: c.name }))]} />;
  return { commune, chip, communes };
}

/** FN-XA-03.03 Danh sách HTX (Hình 16a). */
export function CoopsScreen() {
  const { collectApi } = useAuth();
  const readOnly = useCycleReadOnly();
  const f = useCommuneFilter();
  const [status, setStatus] = useState<"active" | "inactive" | "all">("active");
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const s = useLoad(() => collectApi.cooperatives({ commune: f.commune || undefined, status, q: q || undefined }), [f.commune, status, q]);
  const name = useCommuneNames(s.data?.items.map((c) => c.commune_code) ?? []);
  const columns: Column<KnCooperative>[] = [
    { key: "name", header: "Tên HTX", cell: (c) => c.name },
    { key: "commune", header: "Xã/phường", cell: (c) => name(c.commune_code) },
    { key: "rep", header: "Người đại diện", cell: (c) => c.representative?.label ?? "—" },
    { key: "phone", header: "SĐT", cell: (c) => c.representative?.masked_phone ?? "—" },
    { key: "area", header: "Diện tích (ha)", cell: (c) => num(c.area_ha), className: "num" },
    { key: "hh", header: "Nông hộ", cell: (c) => c.household_count, className: "num" },
    { key: "st", header: "Trạng thái", cell: (c) => <StatusPill tone={c.active ? "ok" : "neutral"}>{c.active ? "Hoạt động" : "Ngừng hoạt động"}</StatusPill> },
    { key: "act", header: "Thao tác", cell: (c) => <RowActions label={`HTX ${c.name}`} viewHref={`/kn/du-lieu/htx/${c.ref}`} /> },
  ];
  return (
    <>
      <PageHeader
        title="Danh sách HTX"
        actions={
          readOnly ? null : (
            <Link className="btn btn-p" href="/kn/du-lieu/htx/them-moi">
              Thêm HTX
            </Link>
          )
        }
      />
      <FilterBar onReset={() => (setStatus("active"), setQ(""), setPage(1))}>
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
        <SearchBox value={q} onChange={(v) => (setQ(v), setPage(1))} placeholder="Tìm theo tên, mã HTX" />
      </FilterBar>
      <LoadState s={s}>
        {(d) => <DataTable caption="Danh sách HTX" columns={columns} rows={d.items.slice((page - 1) * 10, page * 10)} rowKey={(c) => c.ref} page={page} pageSize={10} total={d.items.length} onPage={setPage} empty="Xã phụ trách chưa có HTX." />}
      </LoadState>
    </>
  );
}

/** FN-XA-03.03 Chi tiết HTX (Hình 16b): Thông tin chung, Nông hộ, Máy móc, Thửa; Thêm nông hộ, Vô hiệu hóa. */
export function CoopDetailScreen() {
  const { id } = useParams<{ id: string }>();
  const { collectApi } = useAuth();
  const readOnly = useCycleReadOnly();
  const act = useAction();
  const s = useLoad(async () => {
    const [c, hh, mm, pl] = await Promise.all([collectApi.cooperative(id), collectApi.households(id), collectApi.coopMachines(id).catch(() => null), collectApi.plots({ cooperative_ref: id }).catch(() => null)]);
    return { c, hh: hh.items, mm, pl: pl?.items ?? [] };
  }, [id, act.note]);
  const [off, setOff] = useState(false);
  const [reason, setReason] = useState("");
  const [reasonErr, setReasonErr] = useState<string | undefined>();
  useBreadcrumbTail(s.data ? [{ label: s.data.c.name }] : []);
  const name = useCommuneNames(s.data ? [s.data.c.commune_code] : []);
  return (
    <>
      <BackLink href="/kn/du-lieu/htx">Danh sách HTX</BackLink>
      <LoadState s={s}>
        {({ c, hh: households, mm, pl }) => (
          <>
            <PageHeader title={c.name} badge={<StatusPill tone={c.active ? "ok" : "neutral"}>{c.active ? "Hoạt động" : "Ngừng hoạt động"}</StatusPill>} />
            {readOnly || !c.active ? null : (
              <ActionBar>
                <Link className="btn btn-p btn-sm" href={`/kn/du-lieu/htx/${id}/them-nong-ho`}>
                  Thêm nông hộ
                </Link>
                <button type="button" className="btn btn-o btn-sm" onClick={() => setOff(true)}>
                  Vô hiệu hóa
                </button>
              </ActionBar>
            )}
            {act.view}
            <DetailLayout
              main={
                <>
                  <LabelValueBlock title="Thông tin chung" items={[{ label: "Mã HTX", value: c.code }, { label: "Người đại diện", value: c.representative ? `${c.representative.label} · ${c.representative.masked_phone ?? ""}` : "—" }, { label: "Mã số thuế", value: c.tax_code }, { label: "Lý do ngừng", value: c.status_reason }]} />
                  <LabelValueBlock title="Địa bàn – sản xuất" items={[{ label: "Xã/phường", value: name(c.commune_code) }, { label: "Diện tích (ha)", value: num(c.area_ha) }, { label: "Trụ sở", value: c.head_office }]} />
                  <RelatedTabs
                    tabs={[
                      {
                        id: "hh",
                        label: `Nông hộ (${households.length})`,
                        content: (
                          <ul className="list-plain">
                            {households.map((h) => (
                              <li key={h.ref}>
                                {h.holder.label} · {h.holder.masked_phone ?? "—"} · {num(h.cultivated_area_ha)} ha {h.active ? "" : "(ngừng)"}
                              </li>
                            ))}
                          </ul>
                        ),
                      },
                      { id: "mm", label: `Máy móc (${mm?.total ?? 0})`, content: <ul className="list-plain">{(mm?.lines ?? []).map((l) => <li key={l.machine_type}>{l.machine_type_name}: {l.quantity}</li>)}</ul> },
                      { id: "pl", label: `Thửa ruộng (${pl.length})`, content: <ul className="list-plain">{pl.map((p) => <li key={p.ref}>{p.lot_name ?? p.code} · {num(p.area_ha, 2)} ha</li>)}</ul> },
                    ]}
                  />
                </>
              }
              log={<LogPanel entries={[{ event: "Cập nhật hồ sơ", actor_display: "—", at: c.updated_at }]} />}
            />
            {off ? (
              <Dialog
                title="Vô hiệu hóa HTX"
                onClose={() => setOff(false)}
                actions={
                  <button
                    type="button"
                    className="btn btn-p"
                    disabled={act.busy}
                    onClick={async () => {
                      // Nhập lý do ngắn là việc còn được làm trong pop-up (ui-kit.md). Máy chủ cũng kiểm ≥ 20 ký tự.
                      const err = reason.trim() === "" ? "Bắt buộc nhập." : reason.trim().length < 20 ? "Tối thiểu 20 ký tự." : undefined;
                      setReasonErr(err);
                      if (err) return;
                      if (await act.run(() => collectApi.deactivate(id, reason.trim()), (r) => `Đã vô hiệu hóa (không xóa). Liên kết: ${r.warnings.households} nông hộ, ${r.warnings.machines ?? 0} máy.`)) setOff(false);
                    }}
                  >
                    Vô hiệu hóa
                  </button>
                }
              >
                <p className="small">HTX có nông hộ, máy, thửa liên kết vẫn giữ dữ liệu — vô hiệu hóa thay xóa.</p>
                <TextField label="Lý do (≥ 20 ký tự)" required multiline value={reason} onChange={setReason} error={reasonErr} hint={<PiiHint text={reason} />} />
              </Dialog>
            ) : null}
          </>
        )}
      </LoadState>
    </>
  );
}

type CoopForm = { name: string; commune_code: string; area_ha: string; rep: string; phone: string };

/**
 * FN-XA-03.03 Thêm HTX trên trang riêng (R-A2, thay pop-up cũ). Bắt buộc theo TT-08: Xã/phường, Tên, Người đại diện, Điện thoại.
 * TODO(#102): trường "Loại" (HTX / Nông hộ / Tổ hợp tác) chưa có trong form và API — gói #102 bổ sung.
 */
export function CoopCreateScreen() {
  const { collectApi } = useAuth();
  const router = useRouter();
  const readOnly = useCycleReadOnly();
  const { communes } = useCommuneFilter();
  const act = useAction();
  const [form, setForm] = useState<CoopForm>(() => ({ name: "", commune_code: communes.length === 1 ? communes[0]!.code : "", area_ha: "", rep: "", phone: "" }));
  const [errors, setErrors] = useState<Partial<Record<keyof CoopForm, string>>>({});
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  const set = (k: keyof CoopForm) => (v: string) => setForm((x) => ({ ...x, [k]: v }));
  const save = async () => {
    const v = validateForm(form, {
      name: { kind: "text", required: true, maxLength: 200 },
      commune_code: { kind: "text", required: true },
      area_ha: { kind: "number", maxDecimals: 4 },
      rep: { kind: "text", required: true, maxLength: 120 },
      phone: { kind: "text", required: true, maxLength: 20 },
    });
    setErrors(v.errors);
    if (!v.valid) return;
    const b = v.values;
    const r = await act.run(() =>
      collectApi.createCooperative({
        name: b.name.trim(),
        commune_code: b.commune_code,
        ...(b.area_ha ? { area_ha: b.area_ha } : {}),
        representative: { full_name: b.rep.trim(), phone: b.phone.trim() },
      }),
    );
    if (r) router.push(`/kn/du-lieu/htx/${r.ref}`);
  };
  return (
    <DetailPage
      back={{ href: "/kn/du-lieu/htx", label: "Danh sách HTX" }}
      title="Thêm HTX"
      actions={
        <>
          <Link className="btn btn-o" href="/kn/du-lieu/htx">
            Hủy
          </Link>
          <button type="button" className="btn btn-p" disabled={readOnly || act.busy} onClick={() => void save()}>
            Lưu
          </button>
        </>
      }
    >
      {act.view}
      <div className="grid2">
        <TextField label="Tên HTX" required value={form.name} onChange={set("name")} error={errors.name ?? server.name} maxLength={200} />
        <SelectField label="Xã/phường" required value={form.commune_code} onChange={set("commune_code")} error={errors.commune_code ?? server.commune_code} options={communes.map((c) => ({ value: c.code, label: c.name }))} />
        <NumberField label="Diện tích" unit="ha" value={form.area_ha} onChange={set("area_ha")} rule={{ maxDecimals: 4 }} error={errors.area_ha ?? server.area_ha} onBlurError={(e) => setErrors((x) => ({ ...x, area_ha: e }))} />
        <TextField label="Người đại diện" required value={form.rep} onChange={set("rep")} error={errors.rep ?? server["representative.full_name"]} maxLength={120} />
        <TextField label="Điện thoại người đại diện" required value={form.phone} onChange={set("phone")} error={errors.phone ?? server["representative.phone"]} maxLength={20} hint="Lưu ở kho định danh, hiển thị che." />
      </div>
    </DetailPage>
  );
}

type HouseholdForm = { full_name: string; phone: string; area: string };

/** FN-XA-03.03 Thêm nông hộ vào HTX trên trang riêng (R-A2, thay pop-up cũ). */
export function HouseholdCreateScreen() {
  const { id } = useParams<{ id: string }>();
  const { collectApi } = useAuth();
  const router = useRouter();
  const readOnly = useCycleReadOnly();
  const act = useAction();
  const coop = useLoad(() => collectApi.cooperative(id), [id]);
  const [form, setForm] = useState<HouseholdForm>({ full_name: "", phone: "", area: "" });
  const [errors, setErrors] = useState<Partial<Record<keyof HouseholdForm, string>>>({});
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  const back = `/kn/du-lieu/htx/${id}`;
  const set = (k: keyof HouseholdForm) => (v: string) => setForm((x) => ({ ...x, [k]: v }));
  const save = async () => {
    const v = validateForm(form, {
      full_name: { kind: "text", required: true, maxLength: 120 },
      phone: { kind: "text", required: true, maxLength: 20 },
      area: { kind: "number", maxDecimals: 4 },
    });
    setErrors(v.errors);
    if (!v.valid) return;
    const b = v.values;
    const r = await act.run(() => collectApi.addHousehold(id, { holder: { full_name: b.full_name.trim(), phone: b.phone.trim() }, ...(b.area ? { cultivated_area_ha: b.area } : {}) }));
    if (r) router.push(back);
  };
  const coopName = coop.data?.name ?? "HTX";
  return (
    <DetailPage
      back={{ href: back, label: coopName }}
      title="Thêm nông hộ"
      actions={
        <>
          <Link className="btn btn-o" href={back}>
            Hủy
          </Link>
          <button type="button" className="btn btn-p" disabled={readOnly || act.busy || coop.data?.active === false} onClick={() => void save()}>
            Lưu
          </button>
        </>
      }
    >
      {act.view}
      <div className="grid2">
        <TextField label="Chủ hộ" required value={form.full_name} onChange={set("full_name")} error={errors.full_name ?? server.holder} maxLength={120} />
        <TextField label="Số điện thoại" required value={form.phone} onChange={set("phone")} error={errors.phone ?? server.holder} maxLength={20} />
        <NumberField label="Diện tích canh tác" unit="ha" value={form.area} onChange={set("area")} rule={{ maxDecimals: 4 }} error={errors.area ?? server.cultivated_area_ha} onBlurError={(e) => setErrors((x) => ({ ...x, area: e }))} />
      </div>
    </DetailPage>
  );
}

type MachineLine = { machine_type: string; quantity: string; year_made: string; note: string };
type MachineField = "type" | "quantity" | "year";

/** Năm hiện tại theo lịch Việt Nam — cùng mốc máy chủ #119 dùng cho "Năm SX ≤ năm hiện tại". */
export const vnYear = (ms = Date.now()) => Number(vnDate(ms).slice(0, 4));
/** TT-10 (R-A1): Số lượng là số nguyên 1 – 1.000.000 như pf/mechanization (#113 BF-SRV-GUARD). */
export const MACHINE_QTY_RULE = { required: true, integer: true, positive: true, max: 1_000_000 } as const;
/** Năm nhỏ nhất máy chủ nhận (giả định #119, chờ khách). */
export const MACHINE_YEAR_MIN = 1900;

/** TT-10: Năm SX bắt buộc, số nguyên, 1900 ≤ năm ≤ năm hiện tại (giờ VN). Trả lỗi tiếng Việt hoặc undefined. */
export function machineYearError(raw: string, maxYear = vnYear()): string | undefined {
  const p = parseNumberInput(raw, { required: true, integer: true, max: maxYear });
  if (!p.ok) return p.error;
  return Number(p.value) < MACHINE_YEAR_MIN ? `Không được nhỏ hơn ${MACHINE_YEAR_MIN}.` : undefined;
}

/** Kiểm cả bảng khai báo trước khi Lưu: lỗi theo `${dòng}.${trường}`; trùng loại máy báo ở dòng sau (máy chủ cũng chặn). */
export function validateMachineLines(rows: MachineLine[], maxYear = vnYear()): Record<string, string> {
  const errs: Record<string, string> = {};
  const seen = new Set<string>();
  rows.forEach((l, i) => {
    if (!l.machine_type) errs[`${i}.type`] = "Bắt buộc nhập.";
    else if (seen.has(l.machine_type)) errs[`${i}.type`] = "Trùng loại máy — sửa dòng đã có.";
    seen.add(l.machine_type);
    const q = parseNumberInput(l.quantity, MACHINE_QTY_RULE);
    if (!q.ok) errs[`${i}.quantity`] = q.error;
    const y = machineYearError(l.year_made, maxYear);
    if (y) errs[`${i}.year`] = y;
  });
  return errs;
}

/**
 * Lỗi theo trường máy chủ trả → ô của dòng: pf/mechanization `lines[i].quantity` (#113), hồ sơ KN
 * `lines.<loại máy>.year_made` | `lines.<loại máy>.machine_type` (#119).
 */
export function machineServerErrors(fields: Record<string, string>, rows: { machine_type: string }[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [f, reason] of Object.entries(fields)) {
    const byIdx = /^lines\[(\d+)\]\.(quantity|machine_type)$/.exec(f);
    if (byIdx) {
      out[`${byIdx[1]}.${byIdx[2] === "quantity" ? "quantity" : "type"}`] = reason;
      continue;
    }
    const byType = /^lines\.(.+)\.(year_made|machine_type)$/.exec(f);
    if (!byType) continue;
    const i = rows.findIndex((r) => r.machine_type === byType[1]);
    if (i >= 0) out[`${i}.${byType[2] === "year_made" ? "year" : "type"}`] = reason;
  }
  return out;
}

/** Ô nhập số trong bảng (số lượng, năm) — lỗi tiếng Việt ngay dưới ô (UI-KIT R-A1). */
function CellInput({ label, value, error, disabled, onChange, onBlur }: { label: string; value: string; error?: string; disabled: boolean; onChange: (v: string) => void; onBlur: () => void }) {
  return (
    <>
      <input className="input" aria-label={label} aria-required aria-invalid={error ? true : undefined} disabled={disabled} inputMode="numeric" value={value} onChange={(e) => onChange(e.target.value)} onBlur={onBlur} />
      {error ? (
        <span className="err" role="alert">
          {error}
        </span>
      ) : null}
    </>
  );
}

/**
 * FN-XA-03.04 Máy móc HTX: trái danh sách HTX + tổng xã theo loại; phải bảng khai báo của HTX đã chọn.
 * TT-10 (#103): HTX bắt buộc chọn trước khi khai báo; mỗi dòng bắt buộc Loại máy, Số lượng > 0, Năm SX (1900 – năm hiện
 * tại). Số lượng lưu ở pf/mechanization (/v1/kn/machine-counts), năm ở hồ sơ KN riêng (/v1/kn/machine-profiles, #119):
 * lưu số lượng trước rồi năm trong cùng một lượt thao tác (bấm lại sau lỗi dùng lại khóa chống gửi trùng).
 */
export function MachinesScreen() {
  const { collectApi } = useAuth();
  const readOnly = useCycleReadOnly();
  const f = useCommuneFilter();
  const act = useAction();
  const coops = useLoad(() => collectApi.cooperatives({ commune: f.commune || undefined }), [f.commune]);
  const overview = useLoad(() => collectApi.machines(f.commune || undefined), [f.commune, act.note]);
  const [ref, setRef] = useState("");
  const cur = useLoad(
    ref
      ? async () => {
          const [m, p] = await Promise.all([collectApi.coopMachines(ref), collectApi.machineProfile(ref)]);
          return { ...m, years: new Map(p.items.map((x) => [x.machine_type, x.year_made])) };
        }
      : null,
    [ref, act.note],
  );
  const [lines, setLines] = useState<MachineLine[] | null>(null);
  const rows = lines ?? (cur.data?.lines ?? []).map((l) => ({ machine_type: l.machine_type, quantity: String(l.quantity), year_made: String(cur.data?.years.get(l.machine_type) ?? ""), note: l.note ?? "" }));
  const name = useCommuneNames(overview.data?.commune_totals.map((t) => t.commune_code) ?? []);
  const TYPES = [
    ["may_cay", "Máy cày"],
    ["may_cay_xoi", "Máy xới"],
    ["may_gieo_sa", "Máy gieo sạ"],
    ["may_cay_lua", "Máy cấy"],
    ["drone", "Thiết bị bay không người lái"],
    ["may_gat", "Máy gặt đập liên hợp"],
    ["may_say", "Máy sấy"],
  ];
  const [errs, setErrs] = useState<Record<string, string>>({});
  const server = act.problem ? machineServerErrors(fieldErrorMap(act.problem), rows) : {};
  const err = (i: number, k: MachineField) => errs[`${i}.${k}`] ?? server[`${i}.${k}`];
  const setErr = (i: number, k: MachineField, e: string | undefined) =>
    setErrs((x) => {
      const next = { ...x };
      if (e) next[`${i}.${k}`] = e;
      else delete next[`${i}.${k}`];
      return next;
    });
  const setRow = (i: number, patch: Partial<MachineLine>) => setLines(rows.map((x, k) => (k === i ? { ...x, ...patch } : x)));
  const saveMachines = async () => {
    const e = validateMachineLines(rows);
    setErrs(e);
    if (Object.keys(e).length) return;
    const counts = rows.map((l) => ({ machine_type: l.machine_type, quantity: Number(normalizeNumberInput(l.quantity)), note: l.note.trim() || null }));
    const years = rows.map((l) => ({ machine_type: l.machine_type, year_made: Number(normalizeNumberInput(l.year_made)) }));
    const r = await act.run(
      async (idem) => {
        const saved = await collectApi.putMachines(ref, counts);
        // Sau await: bọc bằng idem của CÙNG lượt để bấm lại sau mất phản hồi giữ khóa cho cả hai bước (#129).
        await idem(() => collectApi.putMachineProfile(ref, years));
        return saved;
      },
      (r) => `Đã lưu khai báo máy (${r.total} máy).`,
    );
    if (r) setLines(null);
  };
  const machineCols: Column<MachineLine & { i: number }>[] = [
    {
      key: "type",
      header: "Loại máy",
      cell: (l) => (
        <>
          <select className="select" aria-label={`Loại máy dòng ${l.i + 1}`} aria-required aria-invalid={err(l.i, "type") ? true : undefined} disabled={readOnly} value={l.machine_type} onChange={(e) => (setRow(l.i, { machine_type: e.target.value }), setErr(l.i, "type", undefined))}>
            {TYPES.map(([v, t]) => (
              <option key={v} value={v}>
                {t}
              </option>
            ))}
          </select>
          {err(l.i, "type") ? (
            <span className="err" role="alert">
              {err(l.i, "type")}
            </span>
          ) : null}
        </>
      ),
    },
    {
      key: "qty",
      header: "Số lượng",
      cell: (l) => {
        const blur = () => {
          const p = parseNumberInput(l.quantity, MACHINE_QTY_RULE);
          setErr(l.i, "quantity", p.ok ? undefined : p.error);
        };
        return <CellInput label={`Số lượng dòng ${l.i + 1}`} value={l.quantity} error={err(l.i, "quantity")} disabled={readOnly} onChange={(v) => setRow(l.i, { quantity: v })} onBlur={blur} />;
      },
    },
    {
      key: "year",
      header: "Năm SX",
      cell: (l) => <CellInput label={`Năm sản xuất dòng ${l.i + 1}`} value={l.year_made} error={err(l.i, "year")} disabled={readOnly} onChange={(v) => setRow(l.i, { year_made: v })} onBlur={() => setErr(l.i, "year", machineYearError(l.year_made))} />,
    },
    { key: "note", header: "Ghi chú", cell: (l) => <input className="input" aria-label={`Ghi chú dòng ${l.i + 1}`} disabled={readOnly} value={l.note} onChange={(e) => setRow(l.i, { note: e.target.value })} /> },
    {
      key: "del",
      header: "",
      cell: (l) =>
        readOnly ? null : (
          <button type="button" className="btn btn-o btn-sm" aria-label={`Xóa dòng ${l.i + 1}`} onClick={() => (setLines(rows.filter((_, k) => k !== l.i)), setErrs({}))}>
            Xóa
          </button>
        ),
    },
  ];
  // Dòng mới mặc định loại máy chưa khai báo (tránh trùng loại ngay khi thêm).
  const nextType = TYPES.find(([v]) => !rows.some((r) => r.machine_type === v))?.[0] ?? "may_cay";
  return (
    <>
      <PageHeader title="Máy móc HTX" />
      <FilterBar>
        {f.chip}
        <FilterChip label="HTX" value={ref} onChange={(v) => (setRef(v), setLines(null), setErrs({}))} options={[{ value: "", label: "Chọn HTX" }, ...(coops.data?.items ?? []).map((c) => ({ value: c.ref, label: c.name }))]} />
      </FilterBar>
      {act.view}
      <DetailLayout
        main={
          ref ? (
            <LoadState s={cur}>
              {(m) => (
                <>
                  <h2>{m.name}</h2>
                  <DataTable caption="Khai báo máy" columns={machineCols} rows={rows.map((l, i) => ({ ...l, i }))} rowKey={(l) => String(l.i)} empty="HTX chưa khai báo máy." />
                  {readOnly ? null : (
                    <div className="row" style={{ gap: 8, marginTop: 8 }}>
                      <button type="button" className="btn btn-o btn-sm" onClick={() => setLines([...rows, { machine_type: nextType, quantity: "1", year_made: "", note: "" }])}>
                        + Thêm dòng
                      </button>
                      <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void saveMachines()}>
                        Lưu
                      </button>
                    </div>
                  )}
                </>
              )}
            </LoadState>
          ) : (
            <p className="mute">Chọn một HTX để khai báo máy (bắt buộc).</p>
          )
        }
        log={
          <aside className="card sec" aria-label="Tổng xã theo loại máy">
            <h2>Tổng xã theo loại máy</h2>
            <ul className="list-plain">
              {(overview.data?.commune_totals ?? []).map((t) => (
                <li key={`${t.commune_code}-${t.machine_type}`}>
                  {name(t.commune_code)} · {t.machine_type_name}: <b>{t.quantity}</b>
                </li>
              ))}
            </ul>
          </aside>
        }
      />
    </>
  );
}

// FN-XA-03.05 Vẽ thửa ruộng (danh sách, thêm, sửa — TT-11 #104): xem PlotScreens.tsx.

/** FN-XA-03.06 Vật tư — bảng nhập nhiều dòng (BF-TT-MAT #105) ở MaterialsScreen.tsx. */
export { MaterialsScreen } from "./MaterialsScreen";
