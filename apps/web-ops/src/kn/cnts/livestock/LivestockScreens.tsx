"use client";

// FN-10 — Danh sách hộ chăn nuôi (BRD CNTS v4.0, CR-CNTS-001; CNTS-V4-HOUSEHOLD-LIST #251, ADR-0056). Menu "Số liệu cấp
// xã › Danh sách hộ chăn nuôi". Danh sách theo xã (Cấp Xã chỉ xã phụ trách — AC-06), thêm hộ phải check-in tại nhà hộ
// (H1–H3, AC-05), sửa (H4), đổi vị trí = check-in lại có lý do, hộ "Chờ bổ sung vị trí" check-in → Đang dùng (BR-08), vô
// hiệu hóa chỉ cán bộ tổng hợp Cấp Tỉnh (BR-04), lịch sử người sửa, thời điểm, cũ → mới (HCN-09). Trang riêng cho Thêm /
// Xem / Sửa (UI-KIT R-A2); pop-up chỉ cho lý do ngắn.
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { NumberField, SelectField, TextField } from "@/components/form";
import { DetailPage, LabelValueBlock, LogPanel, type LogEntry } from "@/components/patterns/detail";
import { DataTable, FilterBar, FilterChip, PageHeader, StatusPill, type Column } from "@/components/patterns/list";
import { LoadState, ReadOnlyNotice, dt, useAction, useLoad } from "@/kn/cycle/common";
import { CommuneField } from "../catalog/CommuneField";
import { ReasonDialog } from "../ReasonDialog";
import {
  EVENT_LABEL,
  FIELD_LABEL,
  LIST_ROUTE,
  PAGE_SIZE,
  STATUS_LABEL,
  STATUS_OPTIONS,
  STATUS_TONE,
  TYPE_LABEL,
  TYPE_OPTIONS,
  WARNING_LABEL,
  animalsText,
  captureCheckin,
  createLivestockApi,
  emptyForm,
  formOf,
  payload,
  validateHousehold,
  type Checkin,
  type Household,
  type HouseholdDetail,
  type HouseholdForm,
  type LivestockType,
  type Status,
} from "./api";

function useLivestock() {
  const { api, kn } = useAuth();
  const lapi = useMemo(() => createLivestockApi(api), [api]);
  const ctx = kn.context;
  const role = ctx?.role;
  const readOnly = !!ctx?.read_only;
  const communes = (ctx?.communes ?? []).map((c) => ({ value: c.code, label: c.name ? `${c.name} (${c.code})` : c.code }));
  // Ma trận quyền FN-10: Cấp Xã CRU xã mình; Cấp Tỉnh chỉ cán bộ tổng hợp CRU + vô hiệu hóa; Admin đủ quyền.
  const canCreate = !readOnly && (role === "admin" || role === "kn_xa" || (role === "kn_tinh" && ctx?.province_group === "tong_hop"));
  return { api, lapi, role, readOnly, communes, canCreate };
}

function fieldErrors(e: ApiProblem | null): Record<string, string> {
  const m: Record<string, string> = {};
  for (const x of e?.problem.errors ?? []) if (x.field) m[x.field] = x.reason ?? "";
  return m;
}

function today(): string {
  return new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
}

/** Danh mục vật nuôi đang dùng (FN-06). */
function useSpecies() {
  const { api } = useAuth();
  return useLoad(
    () =>
      unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: "cnts_species" }, query: { at: today() } } })).then((p) =>
        p.items.map((i) => ({ value: i.code, label: i.name })),
      ),
    [],
  );
}

export function LivestockListScreen() {
  const { lapi, communes, canCreate, role } = useLivestock();
  const [commune, setCommune] = useState("");
  const [status, setStatus] = useState<Status | "">("");
  const [type, setType] = useState<LivestockType | "">("");
  const [page, setPage] = useState(1);
  const s = useLoad(() => lapi.list({ commune, status, livestock_type: type, page }), [commune, status, type, page]);
  const reset = (f: () => void) => {
    f();
    setPage(1);
  };
  const columns: Column<Household>[] = [
    { key: "code", header: "Mã hộ", cell: (h) => <Link href={`${LIST_ROUTE}/${h.id}`}>{h.code}</Link> },
    { key: "owner", header: "Chủ hộ / trang trại", cell: (h) => h.owner_label ?? "—" },
    { key: "phone", header: "Số điện thoại", cell: (h) => h.owner_masked_phone ?? "—" },
    { key: "commune", header: "Xã; ấp, khu phố", cell: (h) => [h.commune_name ?? h.commune_code, h.hamlet].filter(Boolean).join("; ") },
    { key: "animals", header: "Vật nuôi, số con", cell: (h) => animalsText(h.animals) },
    { key: "type", header: "Loại chăn nuôi", cell: (h) => TYPE_LABEL[h.livestock_type] },
    { key: "location", header: "Vị trí", cell: (h) => (h.location ? `Đã check-in (±${h.location.accuracy_m} m)` : "Chưa có") },
    { key: "status", header: "Trạng thái", cell: (h) => <StatusPill tone={STATUS_TONE[h.status]}>{STATUS_LABEL[h.status]}</StatusPill> },
  ];
  return (
    <>
      <PageHeader
        title="Danh sách hộ chăn nuôi"
        actions={
          canCreate ? (
            <Link className="btn btn-p btn-sm" href={`${LIST_ROUTE}/them-moi`}>
              Thêm hộ
            </Link>
          ) : null
        }
      />
      {!canCreate ? <ReadOnlyNotice>Bạn chỉ xem danh sách hộ chăn nuôi (thêm, sửa: cán bộ xã phụ trách, cán bộ tổng hợp Cấp Tỉnh).</ReadOnlyNotice> : null}
      <FilterBar onReset={() => reset(() => (setCommune(""), setStatus(""), setType("")))}>
        {role === "kn_xa" && communes.length > 1 ? (
          <FilterChip label="Xã" value={commune} options={communes} onChange={(v) => reset(() => setCommune(v))} />
        ) : null}
        <FilterChip label="Trạng thái" value={status} options={STATUS_OPTIONS} onChange={(v) => reset(() => setStatus(v as Status | ""))} />
        <FilterChip label="Loại chăn nuôi" value={type} options={TYPE_OPTIONS} onChange={(v) => reset(() => setType(v as LivestockType | ""))} />
      </FilterBar>
      <LoadState s={s}>
        {(d) => (
          <DataTable
            caption="Danh sách hộ chăn nuôi"
            columns={columns}
            rows={d.items}
            rowKey={(h) => h.id}
            page={page}
            pageSize={PAGE_SIZE}
            total={d.total}
            onPage={setPage}
            empty="Chưa có hộ chăn nuôi phù hợp bộ lọc."
          />
        )}
      </LoadState>
    </>
  );
}

/** Trường thông tin hộ HCN-02…HCN-06. */
function HouseholdFields({
  f,
  set,
  errs,
  mode,
  communes,
  role,
  maskedPhone,
}: {
  f: HouseholdForm;
  set: (p: Partial<HouseholdForm>) => void;
  errs: Record<string, string>;
  mode: "create" | "edit";
  communes: { value: string; label: string }[];
  role: string | undefined;
  maskedPhone?: string | null;
}) {
  const species = useSpecies();
  const options = species.data ?? [];
  const setAnimal = (i: number, p: Partial<HouseholdForm["animals"][number]>) => set({ animals: f.animals.map((a, j) => (j === i ? { ...a, ...p } : a)) });
  return (
    <>
      <TextField label="Họ tên chủ hộ / tên trang trại" required={mode === "create"} maxLength={200} value={f.owner_name} onChange={(v) => set({ owner_name: v })} error={errs.owner_name} />
      <TextField
        label="Số điện thoại"
        maxLength={15}
        value={f.owner_phone}
        onChange={(v) => set({ owner_phone: v })}
        error={errs.owner_phone}
        placeholder={mode === "edit" && maskedPhone ? `Hiện tại ${maskedPhone} — để trống nếu không đổi` : undefined}
      />
      {role === "kn_xa" ? (
        <SelectField label="Xã" required value={f.commune_code} onChange={(v) => set({ commune_code: v })} error={errs.commune_code} options={communes} />
      ) : (
        <CommuneField label="Xã" required value={f.commune_code} at={today()} onChange={(v) => set({ commune_code: v })} error={errs.commune_code} />
      )}
      <TextField label="Ấp, khu phố" maxLength={200} value={f.hamlet} onChange={(v) => set({ hamlet: v })} error={errs.hamlet} />
      <SelectField
        label="Loại chăn nuôi"
        required
        value={f.livestock_type}
        onChange={(v) => set({ livestock_type: v as LivestockType })}
        error={errs.livestock_type}
        options={TYPE_OPTIONS}
      />
      <fieldset className="fieldset">
        <legend>Vật nuôi, số con *</legend>
        {errs.animals ? <p className="field-error">{errs.animals}</p> : null}
        {f.animals.map((a, i) => (
          <div key={i} className="row" style={{ gap: 8, alignItems: "flex-end" }}>
            <SelectField
              label={`Vật nuôi ${i + 1}`}
              required
              value={a.species_code}
              onChange={(v) => setAnimal(i, { species_code: v })}
              error={errs[`animals[${i}].species_code`]}
              placeholder={species.loading ? "Đang tải…" : "Chọn vật nuôi"}
              options={options}
            />
            <NumberField
              label="Số con"
              required
              unit="con"
              rule={{ integer: true }}
              value={a.head_count}
              onChange={(v) => setAnimal(i, { head_count: v })}
              error={errs[`animals[${i}].head_count`]}
            />
            {f.animals.length > 1 ? (
              <button type="button" className="btn btn-sm" onClick={() => set({ animals: f.animals.filter((_, j) => j !== i) })}>
                Bỏ dòng
              </button>
            ) : null}
          </div>
        ))}
        {f.animals.length < 30 ? (
          <button type="button" className="btn btn-sm" onClick={() => set({ animals: [...f.animals, { species_code: "", head_count: "" }] })}>
            Thêm vật nuôi
          </button>
        ) : null}
      </fieldset>
    </>
  );
}

/** Khối check-in tại nhà hộ (HCN-07): chỉ lấy GPS của thiết bị, không nhập tay (BR-03). */
function CheckinBox({ value, onChange, error }: { value: Checkin | null; onChange: (c: Checkin | null) => void; error?: string }) {
  const [busy, setBusy] = useState(false);
  const [gpsError, setGpsError] = useState<string | null>(null);
  const take = () => {
    setBusy(true);
    setGpsError(null);
    captureCheckin()
      .then(onChange, (e: Error) => (setGpsError(e.message), onChange(null)))
      .finally(() => setBusy(false));
  };
  return (
    <div className="card" style={{ padding: 12, marginTop: 12 }}>
      <strong>Vị trí tại nhà hộ *</strong>
      <p className="muted">Bấm Check-in khi đang ở nhà hộ; hệ thống ghi tọa độ GPS, độ chính xác, thời điểm (không nhập tay).</p>
      {value ? (
        <p>
          Đã check-in: {value.lat.toFixed(6)}, {value.lon.toFixed(6)} — độ chính xác ±{value.accuracy_m} m, lúc {dt(value.captured_at)}
        </p>
      ) : null}
      {gpsError || error ? <p className="field-error">{gpsError ?? error}</p> : null}
      <button type="button" className="btn btn-sm" disabled={busy} onClick={take}>
        {busy ? "Đang lấy GPS…" : value ? "Check-in lại" : "Check-in"}
      </button>
    </div>
  );
}

function DuplicateNotice({ problem, onConfirm, busy }: { problem: ApiProblem | null; onConfirm: () => void; busy: boolean }) {
  if (problem?.problem.code !== "extension.cnts_livestock_household_duplicate") return null;
  const reason = problem.problem.errors?.find((x) => x.field === "confirm_distinct")?.reason;
  return (
    <div className="alert alert-warn" role="alert">
      <span>Trùng họ tên chủ hộ, ấp, xã với hộ đang dùng{reason ? ` (${reason})` : ""}. Nếu đây là hộ khác, xác nhận để lưu.</span>
      <button type="button" className="btn btn-sm" disabled={busy} onClick={onConfirm}>
        Xác nhận là hộ khác và lưu
      </button>
    </div>
  );
}

export function LivestockCreateScreen() {
  const { lapi, communes, canCreate, role } = useLivestock();
  const router = useRouter();
  const act = useAction();
  const [f, setF] = useState<HouseholdForm>(() => emptyForm(communes.length === 1 ? communes[0]!.value : ""));
  const [checkin, setCheckin] = useState<Checkin | null>(null);
  const [errs, setErrs] = useState<Record<string, string>>({});
  const set = (p: Partial<HouseholdForm>) => setF((x) => ({ ...x, ...p }));
  const save = async (confirm: boolean) => {
    const e = validateHousehold(f, "create");
    if (!checkin) e.checkin = "Chưa check-in tại nhà hộ — không lưu được (AC-05).";
    setErrs(e);
    if (Object.keys(e).length > 0 || !checkin) return;
    const r = await act.run(() => lapi.create(payload(f, { checkin, confirm_distinct: confirm })));
    if (r) router.push(`${LIST_ROUTE}/${r.id}${r.warnings.length ? `?canh-bao=${encodeURIComponent(r.warnings.join(","))}` : ""}`);
  };
  const serverErrs = { ...fieldErrors(act.problem), ...errs };
  return (
    <DetailPage back={{ href: LIST_ROUTE, label: "Danh sách hộ chăn nuôi" }} title="Thêm hộ chăn nuôi">
      {!canCreate ? <ReadOnlyNotice>Bạn không có quyền thêm hộ.</ReadOnlyNotice> : null}
      {act.view}
      <DuplicateNotice problem={act.problem} busy={act.busy} onConfirm={() => void save(true)} />
      <HouseholdFields f={f} set={set} errs={serverErrs} mode="create" communes={communes} role={role} />
      <CheckinBox value={checkin} onChange={setCheckin} error={serverErrs.checkin} />
      <div className="row" style={{ gap: 8, marginTop: 16 }}>
        <button type="button" className="btn btn-p" disabled={act.busy || !canCreate} onClick={() => void save(false)}>
          Lưu
        </button>
        <Link className="btn" href={LIST_ROUTE}>
          Hủy
        </Link>
      </div>
    </DetailPage>
  );
}

function logOf(d: HouseholdDetail): LogEntry[] {
  return d.history.map((h) => ({
    event: EVENT_LABEL[h.event] ?? h.event,
    actor_display: h.actor_name ?? "—",
    at: h.at,
    changes: h.changes.map((c) => ({ field: FIELD_LABEL[c.field] ?? c.field, old: c.old, new: c.new })),
    reason: h.reason,
  }));
}

export function LivestockDetailScreen() {
  const { lapi } = useLivestock();
  const ref = String(useParams<{ id: string }>().id ?? "");
  const s = useLoad(() => lapi.get(ref), [ref]);
  const act = useAction();
  const [dialog, setDialog] = useState<"relocate" | "deactivate" | null>(null);
  const [fix, setFix] = useState<Checkin | null>(null);
  const [gpsError, setGpsError] = useState<string | null>(null);
  const [warnings, setWarnings] = useState<string[]>(() =>
    typeof window === "undefined" ? [] : (new URLSearchParams(window.location.search).get("canh-bao") ?? "").split(",").filter(Boolean),
  );
  const doCheckin = async (d: HouseholdDetail, reason?: string) => {
    setGpsError(null);
    let c = fix;
    if (!c) {
      try {
        c = await captureCheckin();
      } catch (e) {
        setGpsError((e as Error).message);
        return;
      }
    }
    const r = await act.run(() => lapi.checkin(d.id, d.version, c!, reason), "Đã check-in tại nhà hộ.");
    if (r) {
      setFix(null);
      setDialog(null);
      setWarnings(r.warnings);
      s.reload();
    }
  };
  return (
    <LoadState s={s}>
      {(d) => (
        <DetailPage
          back={{ href: LIST_ROUTE, label: "Danh sách hộ chăn nuôi" }}
          title={`Hộ ${d.code}`}
          badge={<StatusPill tone={STATUS_TONE[d.status]}>{STATUS_LABEL[d.status]}</StatusPill>}
          actions={
            <>
              {d.can_edit ? (
                <Link className="btn btn-sm" href={`${LIST_ROUTE}/${d.id}/sua`}>
                  Sửa
                </Link>
              ) : null}
              {d.can_edit && d.status === "pending_location" ? (
                <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void doCheckin(d)}>
                  Check-in tại nhà hộ
                </button>
              ) : null}
              {d.can_edit && d.status === "active" ? (
                <button type="button" className="btn btn-sm" disabled={act.busy} onClick={() => setDialog("relocate")}>
                  Đổi vị trí
                </button>
              ) : null}
              {d.can_deactivate ? (
                <button type="button" className="btn btn-sm btn-danger" disabled={act.busy} onClick={() => setDialog("deactivate")}>
                  Vô hiệu hóa
                </button>
              ) : null}
            </>
          }
          log={<LogPanel title="Lịch sử thay đổi" entries={logOf(d)} />}
        >
          {act.view}
          {gpsError ? <div className="alert alert-warn">{gpsError}</div> : null}
          {warnings.map((w) => (
            <div key={w} className="alert alert-warn" role="status">
              {WARNING_LABEL[w] ?? w}
            </div>
          ))}
          <LabelValueBlock
            title="Thông tin hộ"
            items={[
              { label: "Mã hộ", value: d.code },
              { label: "Họ tên chủ hộ / tên trang trại", value: d.owner_name ?? d.owner_label ?? "—" },
              { label: "Số điện thoại", value: d.owner_masked_phone ?? "—" },
              { label: "Xã", value: d.commune_name ? `${d.commune_name} (${d.commune_code})` : d.commune_code },
              { label: "Ấp, khu phố", value: d.hamlet ?? "—" },
              { label: "Vật nuôi, số con", value: animalsText(d.animals) },
              { label: "Loại chăn nuôi", value: TYPE_LABEL[d.livestock_type] },
              {
                label: "Vị trí",
                value: d.location
                  ? `${d.location.lat.toFixed(6)}, ${d.location.lon.toFixed(6)} (±${d.location.accuracy_m} m, ${dt(d.location.captured_at)})${d.location.mocked ? " — vị trí giả lập" : ""}`
                  : "Chưa có — check-in tại nhà hộ",
              },
              { label: "Trạng thái", value: STATUS_LABEL[d.status] + (d.status === "deactivated" && d.reason ? ` — ${d.reason}` : "") },
              { label: "Cập nhật", value: `${dt(d.updated_at)}${d.updated_by_name ? ` — ${d.updated_by_name}` : ""}` },
            ]}
          />
          {dialog === "relocate" ? (
            <ReasonDialog
              title="Đổi vị trí hộ — check-in lại tại nhà hộ"
              label="Lý do đổi vị trí"
              confirmLabel="Check-in"
              min={5}
              max={1000}
              busy={act.busy}
              onClose={() => setDialog(null)}
              onConfirm={(reason) => void doCheckin(d, reason)}
            >
              <p>Hệ thống lấy GPS của thiết bị ngay tại nhà hộ (không nhập tay).</p>
            </ReasonDialog>
          ) : null}
          {dialog === "deactivate" ? (
            <ReasonDialog
              title={`Vô hiệu hóa hộ ${d.code}`}
              label="Lý do vô hiệu hóa"
              confirmLabel="Vô hiệu hóa"
              min={5}
              max={1000}
              busy={act.busy}
              onClose={() => setDialog(null)}
              onConfirm={async (reason) => {
                if (await act.run(() => lapi.deactivate(d.id, d.version, reason), "Đã vô hiệu hóa hộ.")) {
                  setDialog(null);
                  s.reload();
                }
              }}
            >
              <p>Hộ vô hiệu hóa không còn trong danh sách chọn của báo cáo dịch bệnh, không kích hoạt lại được.</p>
            </ReasonDialog>
          ) : null}
        </DetailPage>
      )}
    </LoadState>
  );
}

export function LivestockEditScreen() {
  const { lapi } = useLivestock();
  const ref = String(useParams<{ id: string }>().id ?? "");
  const s = useLoad(() => lapi.get(ref), [ref]);
  return <LoadState s={s}>{(d) => <EditForm d={d} />}</LoadState>;
}

function EditForm({ d }: { d: HouseholdDetail }) {
  const { lapi, communes, role } = useLivestock();
  const router = useRouter();
  const act = useAction();
  const [f, setF] = useState<HouseholdForm>(() => formOf(d));
  const [errs, setErrs] = useState<Record<string, string>>({});
  const set = (p: Partial<HouseholdForm>) => setF((x) => ({ ...x, ...p }));
  const detail = `${LIST_ROUTE}/${d.id}`;
  const save = async (confirm: boolean) => {
    const e = validateHousehold(f, "edit");
    setErrs(e);
    if (Object.keys(e).length > 0) return;
    const r = await act.run(() => lapi.update(d.id, payload(f, { base_version: d.version, confirm_distinct: confirm })));
    if (r) router.push(detail);
  };
  return (
    <DetailPage back={{ href: detail, label: `Hộ ${d.code}` }} title={`Sửa hộ ${d.code}`}>
      {!d.can_edit ? <ReadOnlyNotice>Bạn không có quyền sửa hộ này.</ReadOnlyNotice> : null}
      {act.view}
      <DuplicateNotice problem={act.problem} busy={act.busy} onConfirm={() => void save(true)} />
      <p className="muted">Đổi vị trí: dùng nút “Đổi vị trí” ở trang chi tiết để check-in lại tại nhà hộ.</p>
      <HouseholdFields f={f} set={set} errs={{ ...fieldErrors(act.problem), ...errs }} mode="edit" communes={communes} role={role} maskedPhone={d.owner_masked_phone} />
      <div className="row" style={{ gap: 8, marginTop: 16 }}>
        <button type="button" className="btn btn-p" disabled={act.busy || !d.can_edit} onClick={() => void save(false)}>
          Lưu
        </button>
        <Link className="btn" href={detail}>
          Hủy
        </Link>
      </div>
    </DetailPage>
  );
}
