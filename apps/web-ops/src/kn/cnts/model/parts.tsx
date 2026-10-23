"use client";

// Phần dùng chung của màn CT/DA và hộ mô hình CNTS (W11.47): biểu điểm trình diễn, chi tiết điểm kèm hộ mô hình, biểu hộ,
// hộp lý do đóng/rút. Chủ hộ nhập họ tên/SĐT — máy chủ ghi vào kho định danh, danh sách chỉ hiện nhãn che bớt.
// BF-SWEEP-CNTS (#101): ô quy mô dùng NumberField (số dương, ≤ 2 số lẻ), trường bắt buộc báo lỗi theo trường khi bấm Lưu và
// không gọi API khi còn lỗi; bảng hộ có STT; lý do đóng/rút nhập trong pop-up ngắn. Nhật ký BM-08/BM-09 theo hộ — TODO(#109).
import { useState } from "react";
import { fieldErrorMap } from "@/api/problem";
import { unwrap } from "@/api/client";
import type { CatalogItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { NumberField, SelectField, TextField } from "@/components/form";
import { DataTable, StatusPill } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { ReasonDialog } from "../ReasonDialog";
import {
  DIARY_FORMS,
  SCALE_RULE,
  SCALE_UNITS,
  decimalValue,
  dmy,
  parseLatLng,
  scaleErrors,
  type Candidate,
  type CntsModelApi,
  type Household,
  type HouseholdSave,
  type Site,
  type SiteDetail,
  type SiteSave,
} from "./api";

const pointText = (p: { lat: number; lng: number } | null | undefined) => (p ? `${p.lat}, ${p.lng}` : "");
const POINT_ERROR = "Tọa độ dạng “vĩ độ, kinh độ” trong Việt Nam.";
const opts = (xs: readonly string[]) => xs.map((x) => ({ value: x, label: x }));

/** Danh mục loại mô hình CNTS. */
export function useModelTypes() {
  const { api } = useAuth();
  return useLoad(
    api ? async () => (await unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: "cnts_model_type" }, query: {} } }))).items as CatalogItem[] : null,
    [api],
  );
}

/** Pop-up lý do ngắn (đóng/mở lại dự án, rút hộ): lý do bắt buộc 5–1000 ký tự như máy chủ. */
export function ReasonBox({ label, onConfirm, onCancel, busy }: { label: string; onConfirm: (reason: string) => void; onCancel: () => void; busy: boolean }) {
  return <ReasonDialog title={label} confirmLabel={label} min={5} max={1000} busy={busy} onConfirm={onConfirm} onClose={onCancel} />;
}

type SiteF = { commune_code: string; name: string; model_type_code: string; scale: string; scale_unit: string; officer_ref: string; diary_form: string; note: string; point: string };

export function SiteForm({
  mapi,
  projectRef,
  site,
  officers,
  communes,
  onDone,
}: {
  mapi: CntsModelApi;
  projectRef: string;
  site?: Site;
  officers: Candidate[];
  communes: string[];
  onDone: (ref?: string) => void;
}) {
  const models = useModelTypes();
  const [f, setF] = useState<SiteF>({
    commune_code: site?.commune_code ?? communes[0] ?? "",
    name: site?.name ?? "",
    model_type_code: site?.model_type_code ?? "",
    scale: site?.scale ?? "",
    scale_unit: site?.scale_unit ?? "",
    officer_ref: site?.officer_ref ?? "",
    diary_form: site?.diary_form ?? "",
    note: site?.note ?? "",
    point: pointText(site?.location),
  });
  const [errs, setErrs] = useState<Partial<Record<keyof SiteF, string>>>({});
  const act = useAction();
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  const set = (k: keyof SiteF) => (v: string) => setF((x) => ({ ...x, [k]: v }));
  const err = (k: keyof SiteF) => errs[k] ?? server[k];
  const communeOpts = [...new Set([...communes, ...(site ? [site.commune_code] : [])])].sort();
  const save = async () => {
    const pt = parseLatLng(f.point);
    const e: Partial<Record<keyof SiteF, string>> = { ...scaleErrors(f.scale, f.scale_unit) };
    if (!f.commune_code.trim()) e.commune_code = "Bắt buộc nhập.";
    if (!f.name.trim()) e.name = "Bắt buộc nhập.";
    if (!f.model_type_code) e.model_type_code = "Bắt buộc nhập.";
    if (pt === "invalid") e.point = POINT_ERROR;
    setErrs(e);
    if (Object.keys(e).length) return;
    const body: SiteSave = {
      ...(site ? { base_version: site.version } : { project_ref: projectRef }),
      commune_code: f.commune_code.trim(),
      name: f.name.trim(),
      model_type_code: f.model_type_code,
      scale: decimalValue(f.scale, SCALE_RULE),
      scale_unit: (f.scale_unit || null) as SiteSave["scale_unit"],
      officer_ref: f.officer_ref || null,
      diary_form: (f.diary_form || null) as SiteSave["diary_form"],
      note: f.note.trim() || null,
      location: pt && pt !== "invalid" ? { ...pt, source: "map" } : null,
      remove_location: !pt && !!site?.location,
    };
    const r = await act.run(() => (site ? mapi.updateSite(site.ref, body) : mapi.createSite(body)));
    if (r) onDone(r.ref);
  };
  return (
    <section className="card sec" aria-label={site ? "Sửa điểm trình diễn" : "Thêm điểm trình diễn"}>
      <h3>{site ? "Sửa điểm trình diễn" : "Thêm điểm trình diễn"}</h3>
      {act.view}
      <div className="form-grid">
        {communeOpts.length ? (
          <SelectField label="Xã/phường" required value={f.commune_code} onChange={set("commune_code")} error={err("commune_code")} options={opts(communeOpts)} />
        ) : (
          <TextField label="Mã xã/phường" required value={f.commune_code} onChange={set("commune_code")} error={err("commune_code")} maxLength={10} />
        )}
        <TextField label="Tên điểm" required value={f.name} onChange={set("name")} error={err("name")} maxLength={300} placeholder="Không ghi họ tên, số điện thoại" />
        <SelectField
          label="Loại mô hình"
          required
          value={f.model_type_code}
          onChange={set("model_type_code")}
          error={err("model_type_code")}
          placeholder="— Chọn —"
          options={(models.data ?? []).map((m) => ({ value: m.code, label: m.name }))}
        />
        <NumberField label="Quy mô" rule={SCALE_RULE} value={f.scale} onChange={set("scale")} error={err("scale")} onBlurError={(m) => setErrs((x) => ({ ...x, scale: m }))} />
        <SelectField label="Đơn vị" value={f.scale_unit} onChange={set("scale_unit")} error={err("scale_unit")} placeholder="—" options={opts(SCALE_UNITS)} />
        <SelectField
          label="Cán bộ theo dõi"
          value={f.officer_ref}
          onChange={set("officer_ref")}
          placeholder="— Chưa phân công —"
          options={officers.map((o) => ({ value: o.ref, label: o.display ?? o.ref }))}
        />
        <SelectField label="Mẫu nhật ký" value={f.diary_form} onChange={set("diary_form")} placeholder="—" options={opts(DIARY_FORMS)} />
        <TextField label="Tọa độ (vĩ độ, kinh độ)" value={f.point} onChange={set("point")} error={err("point")} placeholder="10.38, 105.43" />
        <TextField label="Ghi chú" value={f.note} onChange={set("note")} maxLength={1000} />
      </div>
      <div className="row">
        <button className="btn btn-primary" disabled={act.busy} onClick={() => void save()}>
          Lưu
        </button>
        <button className="btn" onClick={() => onDone()}>
          Hủy bỏ
        </button>
      </div>
    </section>
  );
}

type HouseholdF = {
  owner_name: string;
  owner_phone: string;
  scale: string;
  scale_unit: string;
  keeper_kind: string;
  keeper_ref: string;
  joined_on: string;
  note: string;
  point: string;
};

export function HouseholdForm({
  mapi,
  site,
  household,
  officers,
  onDone,
}: {
  mapi: CntsModelApi;
  site: Site;
  household?: Household;
  officers: Candidate[];
  onDone: (ref?: string) => void;
}) {
  const [f, setF] = useState<HouseholdF>({
    owner_name: "",
    owner_phone: "",
    scale: household?.scale ?? "",
    scale_unit: household?.scale_unit ?? "",
    keeper_kind: household?.keeper_kind ?? "officer",
    keeper_ref: household?.keeper_ref ?? site.officer_ref ?? "",
    joined_on: household?.joined_on ?? "",
    note: household?.note ?? "",
    point: pointText(household?.location),
  });
  const [selfEntry, setSelfEntry] = useState(household?.self_entry ?? false);
  const [errs, setErrs] = useState<Partial<Record<keyof HouseholdF, string>>>({});
  const act = useAction();
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  const set = (k: keyof HouseholdF) => (v: string) => setF((x) => ({ ...x, [k]: v }));
  const err = (k: keyof HouseholdF) => errs[k] ?? server[k];
  const save = async () => {
    const pt = parseLatLng(f.point);
    const e: Partial<Record<keyof HouseholdF, string>> = { ...scaleErrors(f.scale, f.scale_unit) };
    if (!household && !f.owner_name.trim()) e.owner_name = "Bắt buộc nhập.";
    if (f.keeper_kind === "officer" && !f.keeper_ref) e.keeper_ref = "Bắt buộc nhập.";
    if (pt === "invalid") e.point = POINT_ERROR;
    setErrs(e);
    if (Object.keys(e).length) return;
    const body: HouseholdSave = {
      ...(household ? { base_version: household.version } : { site_ref: site.ref, owner_name: f.owner_name.trim(), owner_phone: f.owner_phone.trim() || null }),
      scale: decimalValue(f.scale, SCALE_RULE),
      scale_unit: (f.scale_unit || null) as HouseholdSave["scale_unit"],
      keeper_kind: f.keeper_kind as HouseholdSave["keeper_kind"],
      keeper_ref: f.keeper_kind === "officer" ? f.keeper_ref || null : null,
      self_entry: selfEntry,
      joined_on: f.joined_on || null,
      note: f.note.trim() || null,
      location: pt && pt !== "invalid" ? { ...pt, source: "map" } : null,
      remove_location: !pt && !!household?.location,
    };
    const r = await act.run(() => (household ? mapi.updateHousehold(household.ref, body) : mapi.createHousehold(body)));
    if (r) onDone(r.ref);
  };
  const keepers = officers.some((o) => o.ref === site.officer_ref) || !site.officer_ref ? officers : [...officers, { ref: site.officer_ref, display: site.officer_display, commune_codes: [] }];
  return (
    <section className="card sec" aria-label={household ? "Sửa hộ mô hình" : "Thêm hộ mô hình"}>
      <h3>{household ? `Sửa hộ ${household.owner_label ?? ""}` : "Thêm hộ mô hình"}</h3>
      {act.view}
      <div className="form-grid">
        {household ? null : (
          <>
            <TextField label="Họ tên chủ hộ" required value={f.owner_name} onChange={set("owner_name")} error={err("owner_name")} maxLength={200} />
            <TextField label="Số điện thoại" value={f.owner_phone} onChange={set("owner_phone")} error={err("owner_phone")} maxLength={20} />
          </>
        )}
        <NumberField label="Quy mô" rule={SCALE_RULE} value={f.scale} onChange={set("scale")} error={err("scale")} onBlurError={(m) => setErrs((x) => ({ ...x, scale: m }))} />
        <SelectField label="Đơn vị" value={f.scale_unit} onChange={set("scale_unit")} error={err("scale_unit")} placeholder="—" options={opts(SCALE_UNITS)} />
        <SelectField
          label="Người ghi nhật ký"
          required
          value={f.keeper_kind}
          onChange={set("keeper_kind")}
          options={[
            { value: "officer", label: "Cán bộ" },
            { value: "self", label: "Hộ tự ghi" },
          ]}
        />
        {f.keeper_kind === "officer" ? (
          <SelectField
            label="Cán bộ ghi nhật ký"
            required
            value={f.keeper_ref}
            onChange={set("keeper_ref")}
            error={err("keeper_ref")}
            placeholder="— Chọn —"
            options={keepers.map((o) => ({ value: o.ref, label: o.display ?? o.ref }))}
          />
        ) : null}
        <div className="field">
          <label>
            Ngày tham gia
            <input className="input" type="date" value={f.joined_on} onChange={(e) => set("joined_on")(e.target.value)} />
          </label>
        </div>
        <TextField label="Tọa độ (vĩ độ, kinh độ)" value={f.point} onChange={set("point")} error={err("point")} placeholder="10.38, 105.43" />
        <TextField label="Ghi chú" value={f.note} onChange={set("note")} maxLength={1000} placeholder="Không ghi số điện thoại, CCCD" />
        <label className="check">
          <input type="checkbox" checked={selfEntry} onChange={(e) => setSelfEntry(e.target.checked)} /> Hộ tự nhập (đề nghị cấp quyền nông hộ)
        </label>
      </div>
      <p className="mute">Họ tên, số điện thoại chủ hộ lưu ở kho định danh; danh sách chỉ hiện nhãn che bớt.</p>
      <div className="row">
        <button className="btn btn-primary" disabled={act.busy} onClick={() => void save()}>
          Lưu
        </button>
        <button className="btn" onClick={() => onDone()}>
          Hủy bỏ
        </button>
      </div>
    </section>
  );
}

/** Chi tiết điểm: thông tin, hộ mô hình (thêm/sửa/rút), lịch sử. */
export function SitePanel({
  mapi,
  siteRef,
  officers,
  communeName,
  onChanged,
}: {
  mapi: CntsModelApi;
  siteRef: string;
  officers: Candidate[];
  communeName: (c: string) => string;
  onChanged: () => void;
}) {
  const s = useLoad(() => mapi.site(siteRef), [mapi, siteRef]);
  return <LoadState s={s}>{(d) => <SiteBody mapi={mapi} d={d} officers={officers} communeName={communeName} reload={() => (s.reload(), onChanged())} />}</LoadState>;
}

function SiteBody({ mapi, d, officers, communeName, reload }: { mapi: CntsModelApi; d: SiteDetail; officers: Candidate[]; communeName: (c: string) => string; reload: () => void }) {
  const [mode, setMode] = useState<{ kind: "add" } | { kind: "edit"; h: Household } | { kind: "withdraw"; h: Household } | null>(null);
  const act = useAction();
  return (
    <section className="card sec" aria-label="Chi tiết điểm trình diễn">
      <h2>
        {d.name} · {communeName(d.commune_code)} {d.status === "closed" ? <StatusPill tone="neutral">Đã đóng</StatusPill> : null}
      </h2>
      <p className="mute">
        {d.project_code} · {d.model_type_name ?? d.model_type_code}
        {d.scale ? ` · ${d.scale} ${d.scale_unit ?? ""}` : ""} · Cán bộ theo dõi: {d.officer_display ?? d.officer_ref ?? "chưa phân công"}
        {d.diary_form ? ` · Nhật ký ${d.diary_form}` : ""}
        {d.location ? ` · Tọa độ ${d.location.lat}, ${d.location.lng}` : ""}
      </p>
      {act.view}
      <DataTable
        caption="Hộ mô hình của điểm"
        rows={d.households}
        rowKey={(h) => h.ref}
        empty="Chưa có hộ mô hình."
        columns={[
          { key: "owner", header: "Chủ hộ", cell: (h) => h.owner_label ?? "—" },
          { key: "phone", header: "Điện thoại", cell: (h) => h.owner_phone_masked ?? "—" },
          { key: "scale", header: "Quy mô", cell: (h) => (h.scale ? `${h.scale} ${h.scale_unit ?? ""}` : "—") },
          { key: "keeper", header: "Người ghi nhật ký", cell: (h) => (h.keeper_kind === "self" ? "Hộ tự ghi" : (h.keeper_display ?? "Cán bộ")) },
          { key: "joined", header: "Tham gia", cell: (h) => dmy(h.joined_on) },
          { key: "st", header: "Trạng thái", cell: (h) => (h.status === "withdrawn" ? <StatusPill tone="neutral">Đã rút</StatusPill> : <StatusPill tone="ok">Đang tham gia</StatusPill>) },
          {
            key: "act",
            header: "",
            cell: (h) =>
              h.can_edit && h.status === "active" ? (
                <div className="row">
                  <button className="btn btn-sm" onClick={() => setMode({ kind: "edit", h })}>
                    Sửa
                  </button>
                  <button className="btn btn-sm" onClick={() => setMode({ kind: "withdraw", h })}>
                    Rút hộ
                  </button>
                </div>
              ) : null,
          },
        ]}
      />
      {d.can_add_households && !mode ? (
        <button className="btn btn-primary" onClick={() => setMode({ kind: "add" })}>
          Thêm hộ mô hình
        </button>
      ) : null}
      {mode?.kind === "add" || mode?.kind === "edit" ? (
        <HouseholdForm
          key={mode.kind === "edit" ? mode.h.ref : "add"}
          mapi={mapi}
          site={d}
          household={mode.kind === "edit" ? mode.h : undefined}
          officers={officers}
          onDone={(ref) => {
            setMode(null);
            if (ref) reload();
          }}
        />
      ) : null}
      {mode?.kind === "withdraw" ? (
        <ReasonBox
          label={`Rút hộ ${mode.h.owner_label ?? ""}`}
          busy={act.busy}
          onCancel={() => setMode(null)}
          onConfirm={async (reason) => {
            if (await act.run(() => mapi.householdStatus(mode.h.ref, true, mode.h.version, reason), "Đã rút hộ khỏi mô hình")) {
              setMode(null);
              reload();
            }
          }}
        />
      ) : null}
    </section>
  );
}
