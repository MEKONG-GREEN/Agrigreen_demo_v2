"use client";

// Cấp quyền cán bộ xã, nông hộ (W11.47 — BRD CNTS v3.7 UR-TINH-09, FN-14; prototype A "xa-users"): lọc quyền đã cấp, cấp
// quyền (tài khoản Cấp Xã có sẵn, vai trong module, địa bàn xã/cơ sở/điểm/hộ, biểu mẫu, hiệu lực), sửa, thu hồi kèm người
// nhận việc chưa nộp, nhật ký thay đổi. Cấp Xã xem quyền của mình.
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, useAction, useCommuneNames, useLoad } from "@/kn/cycle/common";
import {
  FORM_CODES,
  GRANT_STATUS_LABEL,
  GRANT_TONE,
  ROLE_LABEL,
  createCntsModelApi,
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
          <table className="table">
            <thead>
              <tr>
                <th>Tài khoản</th>
                <th>Vai</th>
                <th>Địa bàn</th>
                <th>Biểu mẫu</th>
                <th>Hiệu lực</th>
                <th>Trạng thái</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {d.items.length ? (
                d.items.map((g) => (
                  <tr key={g.ref}>
                    <td>
                      <button className="link" onClick={() => setOpen(open === g.ref ? null : g.ref)}>
                        {g.grantee_display ?? g.grantee_ref}
                      </button>
                    </td>
                    <td>{ROLE_LABEL[g.role]}</td>
                    <td>
                      {g.commune_codes.map(communeName).join(", ")}
                      {g.facility_refs.length ? ` · ${g.facility_refs.length} cơ sở` : ""}
                      {g.site_refs.length ? ` · ${g.site_refs.length} điểm` : ""}
                      {g.household_refs.length ? ` · ${g.household_refs.length} hộ` : ""}
                    </td>
                    <td>{g.forms.join(", ") || "—"}</td>
                    <td>
                      {dmy(g.valid_from)} – {g.valid_to ? dmy(g.valid_to) : "không thời hạn"}
                    </td>
                    <td>
                      <StatusPill tone={GRANT_TONE[g.status]}>{GRANT_STATUS_LABEL[g.status]}</StatusPill>
                    </td>
                    <td>
                      {g.can_edit ? (
                        <div className="row">
                          <button className="btn btn-sm" onClick={() => setMode({ kind: "edit", g })}>
                            Sửa
                          </button>
                          <button className="btn btn-sm" onClick={() => setMode({ kind: "revoke", g })}>
                            Thu hồi
                          </button>
                        </div>
                      ) : null}
                    </td>
                  </tr>
                ))
              ) : (
                <tr>
                  <td colSpan={7} className="empty">
                    Chưa có quyền nào.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
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
    valid_from: grant?.valid_from ?? new Date().toISOString().slice(0, 10),
    valid_to: grant?.valid_to ?? null,
  });
  const act = useAction();
  const errs = grantErrors(g);
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
        <label>
          Tài khoản
          {grant ? (
            <input value={grant.grantee_display ?? grant.grantee_ref} disabled />
          ) : (
            <select value={g.grantee_ref ?? ""} onChange={(e) => setG({ ...g, grantee_ref: e.target.value, commune_codes: [] })}>
              <option value="">— Chọn tài khoản Cấp Xã —</option>
              {candidates.map((c) => (
                <option key={c.ref} value={c.ref}>
                  {c.display ?? c.ref}
                </option>
              ))}
            </select>
          )}
        </label>
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
        <label>
          Đến
          <input type="date" value={g.valid_to ?? ""} onChange={(e) => setG({ ...g, valid_to: e.target.value || null })} />
        </label>
      </div>
      <fieldset>
        <legend>Xã phụ trách</legend>
        {communes.length ? (
          communes.map((c) => (
            <label key={c} className="check">
              <input type="checkbox" checked={inCommunes(c)} onChange={() => setG({ ...g, commune_codes: toggle(g.commune_codes ?? [], c) })} /> {communeName(c)}
            </label>
          ))
        ) : (
          <p className="mute">Chọn tài khoản để hiện các xã tài khoản phụ trách.</p>
        )}
      </fieldset>
      {role === "theo_doi_mo_hinh" ? (
        <fieldset>
          <legend>Điểm trình diễn được theo dõi</legend>
          {(sites.data ?? []).filter((s) => inCommunes(s.commune_code)).map((s) => (
            <label key={s.ref} className="check">
              <input type="checkbox" checked={(g.site_refs ?? []).includes(s.ref)} onChange={() => setG({ ...g, site_refs: toggle(g.site_refs ?? [], s.ref) })} /> {s.project_code} · {s.name}
            </label>
          ))}
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
      {Object.keys(errs).length ? <p className="mute">{Object.values(errs).join(" · ")}</p> : null}
      <div className="row">
        <button
          className="btn btn-primary"
          disabled={act.busy || Object.keys(errs).length > 0}
          onClick={async () => {
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
  const [reason, setReason] = useState("");
  const [successor, setSuccessor] = useState("");
  const act = useAction();
  return (
    <section className="card sec" aria-label="Thu hồi quyền">
      <h2>Thu hồi quyền {ROLE_LABEL[g.role]} của {g.grantee_display ?? g.grantee_ref}</h2>
      {act.view}
      <div className="form-grid">
        <label>
          Lý do
          <input value={reason} onChange={(e) => setReason(e.target.value)} maxLength={1000} />
        </label>
        <label>
          Người nhận việc chưa nộp
          <select value={successor} onChange={(e) => setSuccessor(e.target.value)}>
            <option value="">— Không chuyển —</option>
            {candidates
              .filter((c) => c.ref !== g.grantee_ref)
              .map((c) => (
                <option key={c.ref} value={c.ref}>
                  {c.display ?? c.ref}
                </option>
              ))}
          </select>
        </label>
      </div>
      <div className="row">
        <button
          className="btn btn-primary"
          disabled={act.busy || reason.trim().length < 5}
          onClick={async () => {
            if (await act.run(() => mapi.revokeGrant(g.ref, { base_version: g.version, reason: reason.trim(), successor_ref: successor || null }))) onDone();
          }}
        >
          Thu hồi
        </button>
        <button className="btn" onClick={onCancel}>
          Hủy bỏ
        </button>
      </div>
    </section>
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
