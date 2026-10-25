"use client";

// Cấp quyền cán bộ xã, nông hộ (W11.47 — BRD CNTS v3.7 UR-TINH-09, FN-14; prototype A "xa-users"): lọc quyền đã cấp, cấp
// quyền (tài khoản Cấp Xã có sẵn, vai trong module, địa bàn xã/cơ sở/điểm/hộ, biểu mẫu, hiệu lực), sửa, thu hồi kèm người
// nhận việc chưa nộp, nhật ký thay đổi. Cấp Xã xem quyền của mình.
// BF-SWEEP-CNTS (#101): bảng có STT; trường bắt buộc báo lỗi theo trường khi bấm Lưu; thu hồi nhập lý do trong pop-up ngắn.
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { SelectField } from "@/components/form";
import { DataTable, PageHeader, StatusPill } from "@/components/patterns/list";
import { ReasonDialog } from "../ReasonDialog";
import { LoadState, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import {
  FORM_CODES,
  GRANT_STATUS_LABEL,
  GRANT_TONE,
  ROLE_LABEL,
  createCntsModelApi,
  defaultGrantValidFrom,
  dmy,
  grantErrors,
  toggle,
  type Candidate,
  type CntsModelApi,
  type Grant,
  type GrantRole,
  type GrantSave,
} from "./api";

export function GrantsScreen() {
  const { api, kn } = useAuth();
  const mapi = useMemo(() => (api ? createCntsModelApi(api) : null), [api]);
  const role = kn.context?.role;
  const mine = role === "kn_xa";
  const [status, setStatus] = useState<"" | Grant["status"]>("active");
  const list = useLoad(mapi ? () => mapi.grants({ ...(mine ? { mine: true } : {}), ...(status ? { status } : {}) }) : null, [mapi, mine, status]);
  const candidates = useLoad(mapi && !mine ? () => mapi.candidates() : null, [mapi, mine]);
  const communeName = useCommuneNames([...(list.data?.items ?? []).flatMap((g) => g.commune_codes), ...(candidates.data ?? []).flatMap((c) => c.commune_codes)]);
  const [mode, setMode] = useState<{ kind: "new" } | { kind: "edit"; g: Grant } | { kind: "revoke"; g: Grant } | null>(null);
  const [open, setOpen] = useState<string | null>(null);
  if (!mapi) return null;
  const done = () => (setMode(null), list.reload());
  return (
    <div className="page">
      <PageHeader
        title={mine ? "Quyền của tôi trong module CNTS" : "Cấp quyền cán bộ xã, nông hộ"}
        actions={
          list.data?.can_grant && !mode ? (
            <button className="btn btn-primary" onClick={() => setMode({ kind: "new" })}>
              Cấp quyền
            </button>
          ) : null
        }
      >
        <p className="mute">Chỉ cấp vai Cấp Xã, Nông hộ trong module CNTS; địa bàn ngoài tỉnh bị chặn; mọi thay đổi quyền được ghi nhật ký. Tài khoản do Admin tạo.</p>
      </PageHeader>
      {mode?.kind === "new" || mode?.kind === "edit" ? (
        <GrantForm key={mode.kind === "edit" ? mode.g.ref : "new"} mapi={mapi} grant={mode.kind === "edit" ? mode.g : undefined} candidates={candidates.data ?? []} communeName={communeName} onDone={done} onCancel={() => setMode(null)} />
      ) : null}
      {mode?.kind === "revoke" ? <RevokeForm mapi={mapi} g={mode.g} candidates={candidates.data ?? []} onDone={done} onCancel={() => setMode(null)} /> : null}
      <div className="row">
        <label>
          Trạng thái{" "}
          <select value={status} onChange={(e) => setStatus(e.target.value as typeof status)}>
            <option value="">Tất cả</option>
            {Object.entries(GRANT_STATUS_LABEL).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </label>
      </div>
      <LoadState s={list}>
        {(d) => (
          <DataTable
            caption="Quyền đã cấp"
            rows={d.items}
            rowKey={(g) => g.ref}
            empty="Chưa có quyền nào."
            columns={[
              {
                key: "who",
                header: "Tài khoản",
                cell: (g) => (
                  <button className="link" aria-expanded={open === g.ref} onClick={() => setOpen(open === g.ref ? null : g.ref)}>
                    {g.grantee_display ?? g.grantee_ref}
                  </button>
                ),
              },
              { key: "role", header: "Vai", cell: (g) => ROLE_LABEL[g.role] },
              {
                key: "area",
                header: "Địa bàn",
                cell: (g) =>
                  `${g.commune_codes.map(communeName).join(", ")}${g.facility_refs.length ? ` · ${g.facility_refs.length} cơ sở` : ""}${g.site_refs.length ? ` · ${g.site_refs.length} điểm` : ""}${g.household_refs.length ? ` · ${g.household_refs.length} hộ` : ""}`,
              },
              { key: "forms", header: "Biểu mẫu", cell: (g) => g.forms.join(", ") || "—" },
              { key: "valid", header: "Hiệu lực", cell: (g) => `${dmy(g.valid_from)} – ${g.valid_to ? dmy(g.valid_to) : "không thời hạn"}` },
              { key: "st", header: "Trạng thái", cell: (g) => <StatusPill tone={GRANT_TONE[g.status]}>{GRANT_STATUS_LABEL[g.status]}</StatusPill> },
              {
                key: "act",
                header: "",
                cell: (g) =>
                  g.can_edit ? (
                    <div className="row">
                      <button className="btn btn-sm" onClick={() => setMode({ kind: "edit", g })}>
                        Sửa
                      </button>
                      <button className="btn btn-sm" onClick={() => setMode({ kind: "revoke", g })}>
                        Thu hồi
                      </button>
                    </div>
                  ) : null,
              },
            ]}
          />
        )}
      </LoadState>
      {open ? <GrantLog key={open} mapi={mapi} grantRef={open} /> : null}
    </div>
  );
}

function GrantForm({
  mapi,
  grant,
  candidates,
  communeName,
  onDone,
  onCancel,
}: {
  mapi: CntsModelApi;
  grant?: Grant;
  candidates: Candidate[];
  communeName: (c: string) => string;
  onDone: () => void;
  onCancel: () => void;
}) {
  const sites = useLoad(() => mapi.sites({ status: "active" }), [mapi]);
  const households = useLoad(() => mapi.households({ status: "active" }), [mapi]);
  const [g, setG] = useState<GrantSave>({
    grantee_ref: grant?.grantee_ref ?? "",
    role: grant?.role ?? "theo_doi_mo_hinh",
    commune_codes: grant?.commune_codes ?? [],
    facility_refs: grant?.facility_refs ?? [],
    site_refs: grant?.site_refs ?? [],
    household_refs: grant?.household_refs ?? [],
    forms: grant?.forms ?? [],
    valid_from: grant?.valid_from ?? defaultGrantValidFrom(),
    valid_to: grant?.valid_to ?? null,
  });
  const act = useAction();
  const [errs, setErrs] = useState<Record<string, string>>({});
  const errOf = (k: string) =>
    errs[k] ? (
      <span className="err" role="alert">
        {errs[k]}
      </span>
    ) : null;
  const who = candidates.find((c) => c.ref === g.grantee_ref);
  const communes = [...new Set([...(who?.commune_codes ?? []), ...(g.commune_codes ?? [])])].sort();
  const inCommunes = (c: string) => (g.commune_codes ?? []).includes(c);
  const role = g.role as GrantRole;
  const body: GrantSave = grant ? { ...g, base_version: grant.version, grantee_ref: undefined, role: undefined } : g;
  return (
    <section className="card sec" aria-label={grant ? "Sửa quyền" : "Cấp quyền"}>
      <h2>{grant ? `Sửa quyền của ${grant.grantee_display ?? grant.grantee_ref}` : "Cấp quyền"}</h2>
      {act.view}
      <div className="form-grid">
        {grant ? (
          <label>
            Tài khoản
            <input value={grant.grantee_display ?? grant.grantee_ref} disabled />
          </label>
        ) : (
          <SelectField
            label="Tài khoản"
            required
            value={g.grantee_ref ?? ""}
            onChange={(v) => setG({ ...g, grantee_ref: v, commune_codes: [] })}
            error={errs.grantee_ref}
            placeholder="— Chọn tài khoản Cấp Xã —"
            options={candidates.map((c) => ({ value: c.ref, label: c.display ?? c.ref }))}
          />
        )}
        <label>
          Vai trong module
          <select value={role} disabled={!!grant} onChange={(e) => setG({ ...g, role: e.target.value as GrantRole, facility_refs: [], site_refs: [], household_refs: [] })}>
            {Object.entries(ROLE_LABEL).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </label>
        <label>
          Hiệu lực từ
          <input type="date" value={g.valid_from ?? ""} onChange={(e) => setG({ ...g, valid_from: e.target.value })} />
        </label>
        <div>
          <label>
            Đến
            <input type="date" value={g.valid_to ?? ""} onChange={(e) => setG({ ...g, valid_to: e.target.value || null })} />
          </label>
          {errOf("valid_to")}
        </div>
      </div>
      <fieldset>
        <legend>
          Xã phụ trách<span className="req" aria-hidden="true"> *</span>
        </legend>
        {communes.length ? (
          communes.map((c) => (
            <label key={c} className="check">
              <input type="checkbox" checked={inCommunes(c)} onChange={() => setG({ ...g, commune_codes: toggle(g.commune_codes ?? [], c) })} /> {communeName(c)}
            </label>
          ))
        ) : (
          <p className="mute">Chọn tài khoản để hiện các xã tài khoản phụ trách.</p>
        )}
        {errOf("commune_codes")}
      </fieldset>
      {role === "theo_doi_mo_hinh" ? (
        <fieldset>
          <legend>Điểm trình diễn được theo dõi</legend>
          {(sites.data ?? []).filter((s) => inCommunes(s.commune_code)).map((s) => (
            <label key={s.ref} className="check">
              <input type="checkbox" checked={(g.site_refs ?? []).includes(s.ref)} onChange={() => setG({ ...g, site_refs: toggle(g.site_refs ?? [], s.ref) })} /> {s.project_code} · {s.name}
            </label>
          ))}
          {errOf("site_refs")}
        </fieldset>
      ) : null}
      {role === "theo_doi_mo_hinh" || role === "nong_ho" ? (
        <fieldset>
          <legend>Hộ mô hình</legend>
          {(households.data ?? []).filter((h) => inCommunes(h.commune_code)).map((h) => (
            <label key={h.ref} className="check">
              <input type="checkbox" checked={(g.household_refs ?? []).includes(h.ref)} onChange={() => setG({ ...g, household_refs: toggle(g.household_refs ?? [], h.ref) })} /> {h.owner_label ?? h.ref} · {h.site_name}
            </label>
          ))}
          {errOf("household_refs")}
        </fieldset>
      ) : null}
      <fieldset>
        <legend>Biểu mẫu được giao</legend>
        {FORM_CODES.map((f) => (
          <label key={f} className="check">
            <input type="checkbox" checked={(g.forms ?? []).includes(f)} onChange={() => setG({ ...g, forms: toggle(g.forms ?? [], f) })} /> {f}
          </label>
        ))}
      </fieldset>
      {errs.role ? errOf("role") : null}
      <div className="row">
        <button
          className="btn btn-primary"
          disabled={act.busy}
          onClick={async () => {
            const e = grantErrors(g);
            setErrs(e);
            if (Object.keys(e).length) return;
            if (await act.run(() => (grant ? mapi.updateGrant(grant.ref, body) : mapi.createGrant(body)))) onDone();
          }}
        >
          Lưu
        </button>
        <button className="btn" onClick={onCancel}>
          Hủy bỏ
        </button>
      </div>
    </section>
  );
}

function RevokeForm({ mapi, g, candidates, onDone, onCancel }: { mapi: CntsModelApi; g: Grant; candidates: Candidate[]; onDone: () => void; onCancel: () => void }) {
  const [successor, setSuccessor] = useState("");
  const act = useAction();
  return (
    <ReasonDialog
      title={`Thu hồi quyền ${ROLE_LABEL[g.role]} của ${g.grantee_display ?? g.grantee_ref}`}
      confirmLabel="Thu hồi"
      min={5}
      max={1000}
      busy={act.busy}
      onClose={onCancel}
      onConfirm={async (reason) => {
        if (await act.run(() => mapi.revokeGrant(g.ref, { base_version: g.version, reason, successor_ref: successor || null }))) onDone();
      }}
    >
      {act.view}
      <SelectField
        label="Người nhận việc chưa nộp"
        value={successor}
        onChange={setSuccessor}
        placeholder="— Không chuyển —"
        options={candidates.filter((c) => c.ref !== g.grantee_ref).map((c) => ({ value: c.ref, label: c.display ?? c.ref }))}
      />
    </ReasonDialog>
  );
}

function GrantLog({ mapi, grantRef }: { mapi: CntsModelApi; grantRef: string }) {
  const s = useLoad(() => mapi.grant(grantRef), [mapi, grantRef]);
  return (
    <LoadState s={s}>
      {(g) => (
        <section className="card sec" aria-label="Nhật ký thay đổi quyền">
          <h3>Nhật ký thay đổi quyền</h3>
          <ul>
            {g.versions.map((v) => (
              <li key={v.version}>
                v{v.version} · {v.event} · {new Date(v.updated_at).toLocaleString("vi-VN")} · {v.actor_display ?? v.updated_by}
                {v.reason ? ` · ${v.reason}` : ""}
                {v.successor_ref ? ` · chuyển cho ${g.successor_display ?? v.successor_ref}` : ""}
              </li>
            ))}
          </ul>
        </section>
      )}
    </LoadState>
  );
}
