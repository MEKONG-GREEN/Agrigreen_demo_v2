"use client";

// Phần dùng chung của màn CT/DA và hộ mô hình CNTS (W11.47): biểu điểm trình diễn, chi tiết điểm kèm hộ mô hình, biểu hộ,
// hộp lý do đóng/rút. Chủ hộ nhập họ tên/SĐT — máy chủ ghi vào kho định danh, danh sách chỉ hiện nhãn che bớt.
import { useState } from "react";
import { unwrap } from "@/api/client";
import type { CatalogItem } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { StatusPill } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import {
  DIARY_FORMS,
  SCALE_UNITS,
  dmy,
  parseLatLng,
  scaleError,
  type Candidate,
  type CntsModelApi,
  type Household,
  type HouseholdSave,
  type Site,
  type SiteDetail,
  type SiteSave,
} from "./api";

const pointText = (p: { lat: number; lng: number } | null | undefined) => (p ? `${p.lat}, ${p.lng}` : "");

/** Danh mục loại mô hình CNTS. */
export function useModelTypes() {
  const { api } = useAuth();
  return useLoad(
    api ? async () => (await unwrap(api.GET("/v1/masterdata/catalogs/{catalog}", { params: { path: { catalog: "cnts_model_type" }, query: {} } }))).items as CatalogItem[] : null,
    [api],
  );
}

/** Lý do + nút xác nhận (đóng dự án/điểm, rút hộ). */
export function ReasonBox({ label, onConfirm, onCancel, busy }: { label: string; onConfirm: (reason: string) => void; onCancel: () => void; busy: boolean }) {
  const [reason, setReason] = useState("");
  return (
    <div className="row">
      <input aria-label="Lý do" placeholder="Lý do (bắt buộc)" value={reason} onChange={(e) => setReason(e.target.value)} maxLength={1000} />
      <button className="btn btn-primary" disabled={busy || reason.trim().length < 5} onClick={() => onConfirm(reason.trim())}>
        {label}
      </button>
      <button className="btn" onClick={onCancel}>
        Hủy bỏ
      </button>
    </div>
  );
}

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
  const [f, setF] = useState({
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
  const act = useAction();
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });
  const pt = parseLatLng(f.point);
  const errs = [
    !f.commune_code.trim() && "Nhập mã xã",
    !f.name.trim() && "Nhập tên điểm",
    !f.model_type_code && "Chọn loại mô hình",
    scaleError(f.scale, f.scale_unit),
    pt === "invalid" && "Tọa độ dạng “vĩ độ, kinh độ” trong Việt Nam",
  ].filter(Boolean) as string[];
  const body: SiteSave = {
    ...(site ? { base_version: site.version } : { project_ref: projectRef }),
    commune_code: f.commune_code.trim(),
    name: f.name.trim(),
    model_type_code: f.model_type_code,
    scale: f.scale || null,
    scale_unit: (f.scale_unit || null) as SiteSave["scale_unit"],
    officer_ref: f.officer_ref || null,
    diary_form: (f.diary_form || null) as SiteSave["diary_form"],
    note: f.note.trim() || null,
    location: pt && pt !== "invalid" ? { ...pt, source: "map" } : null,
    remove_location: !pt && !!site?.location,
  };
  return (
    <section className="card sec" aria-label={site ? "Sửa điểm trình diễn" : "Thêm điểm trình diễn"}>
      <h3>{site ? "Sửa điểm trình diễn" : "Thêm điểm trình diễn"}</h3>
      {act.view}
      <div className="form-grid">
        <label>
          Mã xã
          <input value={f.commune_code} onChange={set("commune_code")} list="cnts-model-communes" />
          <datalist id="cnts-model-communes">
            {communes.map((c) => (
              <option key={c} value={c} />
            ))}
          </datalist>
        </label>
        <label>
          Tên điểm
          <input value={f.name} onChange={set("name")} maxLength={300} placeholder="Không ghi họ tên, số điện thoại" />
        </label>
        <label>
          Loại mô hình
          <select value={f.model_type_code} onChange={set("model_type_code")}>
            <option value="">— Chọn —</option>
            {(models.data ?? []).map((m) => (
              <option key={m.code} value={m.code}>
                {m.name}
              </option>
            ))}
          </select>
        </label>
        <label>
          Quy mô
          <input inputMode="decimal" value={f.scale} onChange={set("scale")} />
        </label>
        <label>
          Đơn vị
          <select value={f.scale_unit} onChange={set("scale_unit")}>
            <option value="">—</option>
            {SCALE_UNITS.map((u) => (
              <option key={u}>{u}</option>
            ))}
          </select>
        </label>
        <label>
          Cán bộ theo dõi
          <select value={f.officer_ref} onChange={set("officer_ref")}>
            <option value="">— Chưa phân công —</option>
            {officers.map((o) => (
              <option key={o.ref} value={o.ref}>
                {o.display ?? o.ref}
              </option>
            ))}
          </select>
        </label>
        <label>
          Mẫu nhật ký
          <select value={f.diary_form} onChange={set("diary_form")}>
            <option value="">—</option>
            {DIARY_FORMS.map((d) => (
              <option key={d}>{d}</option>
            ))}
          </select>
        </label>
        <label>
          Tọa độ (vĩ độ, kinh độ)
          <input value={f.point} onChange={set("point")} placeholder="10.38, 105.43" />
        </label>
        <label>
          Ghi chú
          <input value={f.note} onChange={set("note")} maxLength={1000} />
        </label>
      </div>
      {errs.length ? <p className="mute">{errs.join(" · ")}</p> : null}
      <div className="row">
        <button
          className="btn btn-primary"
          disabled={act.busy || errs.length > 0}
          onClick={async () => {
            const r = await act.run(() => (site ? mapi.updateSite(site.ref, body) : mapi.createSite(body)));
            if (r) onDone(r.ref);
          }}
        >
          Lưu
        </button>
        <button className="btn" onClick={() => onDone()}>
          Hủy bỏ
        </button>
      </div>
    </section>
  );
}

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
  const [f, setF] = useState({
    owner_name: "",
    owner_phone: "",
    scale: household?.scale ?? "",
    scale_unit: household?.scale_unit ?? "",
    keeper_kind: household?.keeper_kind ?? "officer",
    keeper_ref: household?.keeper_ref ?? site.officer_ref ?? "",
    self_entry: household?.self_entry ?? false,
    joined_on: household?.joined_on ?? "",
    note: household?.note ?? "",
    point: pointText(household?.location),
  });
  const act = useAction();
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });
  const pt = parseLatLng(f.point);
  const errs = [
    !household && !f.owner_name.trim() && "Nhập họ tên chủ hộ",
    scaleError(f.scale, f.scale_unit),
    f.keeper_kind === "officer" && !f.keeper_ref && "Chọn cán bộ ghi nhật ký",
    pt === "invalid" && "Tọa độ dạng “vĩ độ, kinh độ” trong Việt Nam",
  ].filter(Boolean) as string[];
  const body: HouseholdSave = {
    ...(household ? { base_version: household.version } : { site_ref: site.ref, owner_name: f.owner_name.trim(), owner_phone: f.owner_phone.trim() || null }),
    scale: f.scale || null,
    scale_unit: (f.scale_unit || null) as HouseholdSave["scale_unit"],
    keeper_kind: f.keeper_kind as HouseholdSave["keeper_kind"],
    keeper_ref: f.keeper_kind === "officer" ? f.keeper_ref || null : null,
    self_entry: f.self_entry,
    joined_on: f.joined_on || null,
    note: f.note.trim() || null,
    location: pt && pt !== "invalid" ? { ...pt, source: "map" } : null,
    remove_location: !pt && !!household?.location,
  };
  const keepers = officers.some((o) => o.ref === site.officer_ref) || !site.officer_ref ? officers : [...officers, { ref: site.officer_ref, display: site.officer_display, commune_codes: [] }];
  return (
    <section className="card sec" aria-label={household ? "Sửa hộ mô hình" : "Thêm hộ mô hình"}>
      <h3>{household ? `Sửa hộ ${household.owner_label ?? ""}` : "Thêm hộ mô hình"}</h3>
      {act.view}
      <div className="form-grid">
        {household ? null : (
          <>
            <label>
              Họ tên chủ hộ
              <input value={f.owner_name} onChange={set("owner_name")} maxLength={200} />
            </label>
            <label>
              Số điện thoại
              <input inputMode="tel" value={f.owner_phone} onChange={set("owner_phone")} maxLength={20} />
            </label>
          </>
        )}
        <label>
          Quy mô
          <input inputMode="decimal" value={f.scale} onChange={set("scale")} />
        </label>
        <label>
          Đơn vị
          <select value={f.scale_unit} onChange={set("scale_unit")}>
            <option value="">—</option>
            {SCALE_UNITS.map((u) => (
              <option key={u}>{u}</option>
            ))}
          </select>
        </label>
        <label>
          Người ghi nhật ký
          <select value={f.keeper_kind} onChange={set("keeper_kind")}>
            <option value="officer">Cán bộ</option>
            <option value="self">Hộ tự ghi</option>
          </select>
        </label>
        {f.keeper_kind === "officer" ? (
          <label>
            Cán bộ ghi nhật ký
            <select value={f.keeper_ref} onChange={set("keeper_ref")}>
              <option value="">— Chọn —</option>
              {keepers.map((o) => (
                <option key={o.ref} value={o.ref}>
                  {o.display ?? o.ref}
                </option>
              ))}
            </select>
          </label>
        ) : null}
        <label>
          Ngày tham gia
          <input type="date" value={f.joined_on} onChange={set("joined_on")} />
        </label>
        <label>
          Tọa độ (vĩ độ, kinh độ)
          <input value={f.point} onChange={set("point")} placeholder="10.38, 105.43" />
        </label>
        <label>
          Ghi chú
          <input value={f.note} onChange={set("note")} maxLength={1000} placeholder="Không ghi số điện thoại, CCCD" />
        </label>
        <label className="check">
          <input type="checkbox" checked={f.self_entry} onChange={(e) => setF({ ...f, self_entry: e.target.checked })} /> Hộ tự nhập (đề nghị cấp quyền nông hộ)
        </label>
      </div>
      <p className="mute">Họ tên, số điện thoại chủ hộ lưu ở kho định danh; danh sách chỉ hiện nhãn che bớt.</p>
      {errs.length ? <p className="mute">{errs.join(" · ")}</p> : null}
      <div className="row">
        <button
          className="btn btn-primary"
          disabled={act.busy || errs.length > 0}
          onClick={async () => {
            const r = await act.run(() => (household ? mapi.updateHousehold(household.ref, body) : mapi.createHousehold(body)));
            if (r) onDone(r.ref);
          }}
        >
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
      <table className="table">
        <thead>
          <tr>
            <th>Chủ hộ</th>
            <th>Điện thoại</th>
            <th>Quy mô</th>
            <th>Người ghi nhật ký</th>
            <th>Tham gia</th>
            <th>Trạng thái</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {d.households.length ? (
            d.households.map((h) => (
              <tr key={h.ref}>
                <td>{h.owner_label ?? "—"}</td>
                <td>{h.owner_phone_masked ?? "—"}</td>
                <td>{h.scale ? `${h.scale} ${h.scale_unit ?? ""}` : "—"}</td>
                <td>{h.keeper_kind === "self" ? "Hộ tự ghi" : (h.keeper_display ?? "Cán bộ")}</td>
                <td>{dmy(h.joined_on)}</td>
                <td>{h.status === "withdrawn" ? <StatusPill tone="neutral">Đã rút</StatusPill> : <StatusPill tone="ok">Đang tham gia</StatusPill>}</td>
                <td>
                  {h.can_edit && h.status === "active" ? (
                    <div className="row">
                      <button className="btn btn-sm" onClick={() => setMode({ kind: "edit", h })}>
                        Sửa
                      </button>
                      <button className="btn btn-sm" onClick={() => setMode({ kind: "withdraw", h })}>
                        Rút hộ
                      </button>
                    </div>
                  ) : null}
                </td>
              </tr>
            ))
          ) : (
            <tr>
              <td colSpan={7} className="empty">
                Chưa có hộ mô hình.
              </td>
            </tr>
          )}
        </tbody>
      </table>
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
