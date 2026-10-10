"use client";

// Admin KN v7.7 (BRD §4.5): Tổng quan hệ thống (Hình 3c), Người dùng & phân quyền FN-AD-04.04 (Hình 25a/b), Menu các cấp
// FN-AD-04.07 (Hình 28), Cơ cấu tổ chức FN-AD-04.02 (Hình 23), Phòng ban FN-AD-04.03, ĐVHC FN-AD-04.01, Danh mục FN-AD-04.05.
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { AlertList, SectionTitle, type DashboardAlert } from "@/components/patterns/dashboard";
import { ActionBar, BackLink, DetailLayout, LabelValueBlock, LogPanel, RelatedTabs } from "@/components/patterns/detail";
import { DataTable, FilterBar, FilterChip, PageHeader, SearchBox, StatusPill, type Column } from "@/components/patterns/list";
import { KN_ROLE_LABEL, type KnRole } from "@/kn/contract";
import { listForms } from "@/kn/forms/api";
import { Greeting } from "@/kn/ui";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";
import type { AccountsUser, KnAssignment, KnAssignmentInput, KnDepartment, KnOrgTreeNode } from "../api";
import { LoadState, dt, useAction, useLoad } from "../common";

const AG_REF_FALLBACK = "";
// W11.06: 5 mã phòng ban seed (docs/kn/phong-ban-seed.md); mã tạm phong_ban_5/6 đã thay.
const DEPT_OPTIONS_ALL = ["trong_trot", "chan_nuoi_thuy_san", "ke_hoach_tai_chinh", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"];
const AG_COMMUNE_OPTIONS = [
  { code: "91301", name: "Mỹ Hòa Hưng" },
  { code: "91302", name: "Bình Đức" },
  { code: "91303", name: "Long Xuyên" },
  { code: "91304", name: "Vĩnh Tế" },
  { code: "91305", name: "An Cư" },
  { code: "91306", name: "Núi Sam" },
  { code: "91307", name: "Châu Phong" },
];
const fullName = (u: AccountsUser) => `${u.last_name} ${u.first_name}`.trim();

/** Hình 3c Tổng quan hệ thống. */
export function AdminOverviewScreen() {
  const { knAdminApi, adminApi, cycleApi } = useAuth();
  const s = useLoad(async () => {
    const [users, depts, forms, dash] = await Promise.all([
      knAdminApi.users(),
      knAdminApi.departments(),
      listForms(adminApi, { department: "trong_trot" }).catch(() => ({ items: [] as unknown[] })),
      cycleApi.dashboard({ province: "91" }).catch(() => null),
    ]);
    const asg = await Promise.all(users.items.map((u) => knAdminApi.assignments(u.id).then((a) => ({ u, a: a.items }))));
    return { users, depts, forms: forms.items as { open?: { status: string } | null }[], dash, asg };
  }, []);
  return (
    <>
      <Greeting />
      <LoadState s={s}>
        {(d) => {
          const active = d.users.items.filter((u) => u.enabled).length;
          const locked = d.users.items.length - active;
          const drafts = d.forms.filter((f) => f.open).length;
          const sub = (d.dash?.cards as { submitted?: { communes_submitted: number; communes_total: number } } | undefined)?.submitted;
          const noCommune = d.asg.filter((x) => x.a.some((a) => a.role === "kn_xa" && !a.communes.length));
          const noOfficer = ((d.dash?.unsubmitted ?? []) as { commune_name: string; no_officer: boolean }[]).filter((u) => u.no_officer);
          const alerts: DashboardAlert[] = [
            ...(noCommune.length ? [{ id: "nc", level: "attention" as const, title: `${noCommune.length} tài khoản Cấp Xã chưa gán xã/phường`, description: noCommune.map((x) => fullName(x.u)).join(", "), at: new Date().toISOString(), href: "/kn/quan-tri/nguoi-dung" }] : []),
            ...(noOfficer.length ? [{ id: "no", level: "attention" as const, title: `${noOfficer.length} xã chưa có cán bộ phụ trách Phòng Trồng trọt`, description: noOfficer.map((x) => x.commune_name).join(", "), at: new Date().toISOString(), href: "/kn/quan-tri/co-cau-to-chuc" }] : []),
            ...(drafts ? [{ id: "dr", level: "info" as const, title: `${drafts} mẫu báo cáo đang ở bản nháp`, description: "Phòng Trồng trọt › Mẫu báo cáo", at: new Date().toISOString(), href: "/kn/phong-ban/trong_trot/mau-bao-cao" }] : []),
          ];
          const stats = [
            { label: "Tài khoản hoạt động", value: String(active), foot: `${locked} tài khoản đang khóa` },
            { label: "Phòng ban đang áp dụng", value: `${d.depts.items.filter((x) => x.status === "active").length} / ${d.depts.items.length}`, foot: d.depts.items.filter((x) => x.status === "active").map((x) => x.name).join(", ") },
            { label: "Mẫu báo cáo", value: String(d.forms.length), foot: `${drafts} bản nháp chưa phát hành` },
            { label: `Báo cáo đã nộp ${d.dash?.period?.label ?? ""}`, value: sub ? `${sub.communes_submitted} / ${sub.communes_total}` : "—", foot: sub ? `${sub.communes_total - sub.communes_submitted} xã chưa nộp` : "Chưa có kỳ" },
          ];
          return (
            <>
              <div className="hot-grid">
                {stats.map((x) => (
                  <div className="card hot" key={x.label} style={{ minHeight: 0, paddingBottom: 14 }}>
                    <div className="lb">{x.label}</div>
                    <div className="vl">{x.value}</div>
                    <div className="dl neutral">{x.foot}</div>
                  </div>
                ))}
              </div>
              <SectionTitle>Phòng ban</SectionTitle>
              <div className="dept-grid">
                {d.depts.items.slice(0, 3).map((x) =>
                  x.status === "active" ? (
                    <div className="card dept-card is-on" key={x.code}>
                      <h3>
                        <span className="row" style={{ gap: 8 }}>
                          <Icon name="sprout" /> {x.name}
                        </span>
                        <span className="pill pill-ok">{x.status_label}</span>
                      </h3>
                      <dl>
                        <dt>Tài khoản</dt>
                        <dd>{x.account_count ?? "—"}</dd>
                        <dt>Mẫu báo cáo</dt>
                        <dd>{d.forms.length}</dd>
                        <dt>Xã chưa nộp</dt>
                        <dd>{sub ? sub.communes_total - sub.communes_submitted : "—"}</dd>
                      </dl>
                      <p style={{ marginTop: 10 }}>
                        <Link className="link-all" href={`/kn/phong-ban/${x.code}/theo-doi-nop`}>
                          Mở menu phòng ban →
                        </Link>
                      </p>
                    </div>
                  ) : (
                    <div className="card dept-card is-off" key={x.code}>
                      <h3>
                        <span className="row" style={{ gap: 8 }}>
                          <Icon name="building" /> {x.name}
                        </span>
                        <span className="pill">{x.status_label}</span>
                      </h3>
                      <p className="small">Module chưa mở. Admin bật “Đang áp dụng” ở Quản trị hệ thống › Phòng ban.</p>
                    </div>
                  ),
                )}
              </div>
              <AlertList title="Cảnh báo hệ thống" alerts={alerts} />
            </>
          );
        }}
      </LoadState>
    </>
  );
}

interface AsgDraft {
  role: Exclude<KnRole, "admin">;
  departments: string[];
  mgmt: string;
  communes: string[];
}
const emptyDraft = (): AsgDraft => ({ role: "kn_xa", departments: ["trong_trot"], mgmt: "chuyen_vien", communes: [] });

function AssignmentEditor({ value, onChange, orgRefs }: { value: AsgDraft; onChange: (v: AsgDraft) => void; orgRefs: Record<string, string> }) {
  void orgRefs;
  return (
    <div className="stack" role="group" aria-label="Vai trò và phạm vi">
      <label className="field">
        <span>Vai trò*</span>
        <select className="input" aria-label="Vai trò" value={value.role} onChange={(e) => onChange({ ...value, role: e.target.value as AsgDraft["role"] })}>
          <option value="kn_xa">Cấp Xã</option>
          <option value="kn_tinh">Cấp Tỉnh</option>
          <option value="kn_tw">Cấp Quốc gia</option>
        </select>
      </label>
      <p className="small mute">Nút: {value.role === "kn_tw" ? "Trung tâm Khuyến nông Quốc gia" : "TTKN tỉnh An Giang"}</p>
      <fieldset style={{ border: 0 }}>
        <legend className="small">Phòng ban*</legend>
        {DEPT_OPTIONS_ALL.map((c) => (
          <label key={c} className="row" style={{ gap: 4 }}>
            <input type="checkbox" checked={value.departments.includes(c)} onChange={(e) => onChange({ ...value, departments: e.target.checked ? [...value.departments, c] : value.departments.filter((x) => x !== c) })} /> {c}
          </label>
        ))}
      </fieldset>
      <label className="field">
        <span>Cấp quản lý (tạm, Q-05)</span>
        <select className="input" aria-label="Cấp quản lý" value={value.mgmt} onChange={(e) => onChange({ ...value, mgmt: e.target.value })}>
          <option value="chuyen_vien">Chuyên viên</option>
          <option value="pho_phong">Phó phòng</option>
          <option value="truong_phong">Trưởng phòng</option>
        </select>
      </label>
      {value.role === "kn_xa" ? (
        <fieldset style={{ border: 0 }}>
          <legend className="small">Xã/phường phụ trách</legend>
          {AG_COMMUNE_OPTIONS.map((c) => (
            <label key={c.code} className="row" style={{ gap: 4 }}>
              <input type="checkbox" aria-label={`Xã ${c.name}`} checked={value.communes.includes(c.code)} onChange={(e) => onChange({ ...value, communes: e.target.checked ? [...value.communes, c.code] : value.communes.filter((x) => x !== c.code) })} /> {c.name}
            </label>
          ))}
        </fieldset>
      ) : null}
    </div>
  );
}

function toInput(d: AsgDraft, refs: { national: string; province: string }): KnAssignmentInput {
  return { role: d.role, org_ref: d.role === "kn_tw" ? refs.national : refs.province, department_codes: d.departments, mgmt_level: d.mgmt, ...(d.role === "kn_xa" ? { commune_codes: d.communes } : {}) };
}

function useOrgRefs() {
  const { knAdminApi } = useAuth();
  const tree = useLoad(() => knAdminApi.orgTree(), []);
  const national = tree.data?.items[0]?.ref ?? AG_REF_FALLBACK;
  const province = tree.data?.items[0]?.children[0]?.ref ?? AG_REF_FALLBACK;
  return { national: national ?? "", province: province ?? "" };
}

/** FN-AD-04.04 Danh sách tài khoản (Hình 25a) + Tạo tài khoản. */
export function UsersScreen() {
  const { knAdminApi } = useAuth();
  const router = useRouter();
  const [q, setQ] = useState("");
  const [status, setStatus] = useState("");
  const [role, setRole] = useState("");
  const [page, setPage] = useState(1);
  const [creating, setCreating] = useState(false);
  const act = useAction();
  const refs = useOrgRefs();
  const s = useLoad(async () => {
    const users = await knAdminApi.users();
    const asg = await Promise.all(users.items.map((u) => knAdminApi.assignments(u.id).then((a) => [u.id, a.items] as const)));
    return { users: users.items, asg: Object.fromEntries(asg) as Record<string, KnAssignment[]> };
  }, [act.note]);
  const [form, setForm] = useState({ full_name: "", email: "", phone: "" });
  const [drafts, setDrafts] = useState<AsgDraft[]>([emptyDraft()]);
  return (
    <>
      <PageHeader
        title="Người dùng"
        actions={
          <button type="button" className="btn btn-p" disabled={!refs.province} title={!refs.province ? "Cần khai báo cơ cấu tổ chức và phòng ban trước" : undefined} onClick={() => setCreating(true)}>
            <Icon name="user" size={15} /> Tạo tài khoản
          </button>
        }
      />
      {act.view}
      <FilterBar onReset={() => (setQ(""), setStatus(""), setRole(""))}>
        <FilterChip label="Vai trò" value={role} onChange={setRole} options={[{ value: "", label: "Tất cả" }, ...Object.entries(KN_ROLE_LABEL).map(([value, label]) => ({ value, label }))]} />
        <FilterChip
          label="Trạng thái"
          value={status}
          onChange={setStatus}
          options={[
            { value: "", label: "Tất cả" },
            { value: "on", label: "Hoạt động" },
            { value: "off", label: "Đang khóa" },
          ]}
        />
        <SearchBox value={q} onChange={setQ} placeholder="Tìm họ tên, email" />
      </FilterBar>
      <LoadState s={s}>
        {(d) => {
          const rows = d.users.filter(
            (u) =>
              (!q || `${fullName(u)} ${u.email}`.toLowerCase().includes(q.toLowerCase())) &&
              (!status || (status === "on") === u.enabled) &&
              (!role || (d.asg[u.id] ?? []).some((a) => a.role === role)),
          );
          const columns: Column<AccountsUser>[] = [
            { key: "name", header: "Họ tên", cell: (u) => <Link href={`/kn/quan-tri/nguoi-dung/${u.id}`}>{fullName(u)}</Link> },
            { key: "email", header: "Email", cell: (u) => u.email },
            { key: "roles", header: "Vai trò", cell: (u) => (d.asg[u.id] ?? []).map((a) => KN_ROLE_LABEL[a.role]).join(", ") || "—" },
            { key: "depts", header: "Phòng ban", cell: (u) => [...new Set((d.asg[u.id] ?? []).flatMap((a) => a.departments.map((x) => x.name.replace(/^Phòng /, ""))))].join(", ") || "Tất cả" },
            { key: "status", header: "Trạng thái", cell: (u) => <StatusPill tone={u.enabled ? "ok" : "bad"}>{u.enabled ? "Hoạt động" : "Đang khóa"}</StatusPill> },
          ];
          return <DataTable caption="Danh sách tài khoản" columns={columns} rows={rows.slice((page - 1) * 20, page * 20)} rowKey={(u) => u.id} page={page} pageSize={20} total={rows.length} onPage={setPage} />;
        }}
      </LoadState>
      {creating ? (
        <Dialog
          title="Tạo tài khoản"
          onClose={() => setCreating(false)}
          actions={
            <button
              type="button"
              className="btn btn-p"
              disabled={act.busy}
              onClick={async () => {
                const r = await act.run(
                  () => knAdminApi.createKnUser({ ...form, ...(form.phone ? {} : { phone: undefined }), assignments: drafts.map((x) => toInput(x, refs)) }),
                  (x) => `Đã tạo tài khoản ${form.full_name} (${x.assignments.length} vai trò), đã gửi liên kết kích hoạt.${x.warnings.length ? ` Cảnh báo: ${x.warnings.map((w) => w.message).join("; ")}` : ""}`,
                );
                if (r) {
                  setCreating(false);
                  router.push(`/kn/quan-tri/nguoi-dung/${r.user.id}`);
                }
              }}
            >
              Tạo tài khoản
            </button>
          }
        >
          {act.problem ? act.view : null}
          <label className="field">
            <span>Họ tên*</span>
            <input className="input" value={form.full_name} onChange={(e) => setForm({ ...form, full_name: e.target.value })} />
          </label>
          <label className="field">
            <span>Email*</span>
            <input className="input" type="email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} />
          </label>
          <label className="field">
            <span>Số điện thoại (tên đăng nhập)</span>
            <input className="input" value={form.phone} onChange={(e) => setForm({ ...form, phone: e.target.value })} />
          </label>
          {drafts.map((d, i) => (
            <fieldset key={i} className="card sec" style={{ marginTop: 8 }}>
              <legend>Vai trò {i + 1}</legend>
              <AssignmentEditor value={d} orgRefs={{}} onChange={(v) => setDrafts(drafts.map((x, k) => (k === i ? v : x)))} />
              {drafts.length > 1 ? (
                <button type="button" className="btn btn-o btn-sm" onClick={() => setDrafts(drafts.filter((_, k) => k !== i))}>
                  Bỏ vai trò này
                </button>
              ) : null}
            </fieldset>
          ))}
          <button type="button" className="btn btn-o btn-sm" onClick={() => setDrafts([...drafts, { ...emptyDraft(), role: "kn_tinh" }])}>
            + Thêm vai trò
          </button>
          <p className="small mute">Tài khoản mới nhận liên kết kích hoạt; lần đầu đăng nhập bắt buộc đặt mật khẩu (không mật khẩu tạm).</p>
        </Dialog>
      ) : null}
    </>
  );
}

const LOG_EVENT: Record<string, string> = {
  "account.created": "Tạo tài khoản",
  "account.locked": "Khóa tài khoản",
  "account.unlocked": "Mở khóa tài khoản",
  "account.activation_sent": "Gửi lại liên kết kích hoạt / mật khẩu",
  "account.profile_updated": "Sửa thông tin tài khoản",
  "account.assignment_changed": "Đổi đơn vị/vai",
  "assignment.granted": "Thêm vai trò",
  "assignment.changed": "Sửa vai trò",
  "assignment.revoked": "Xóa vai trò",
};

/** FN-AD-04.04 Chi tiết tài khoản có Log (Hình 25b). */
export function UserDetailScreen() {
  const { id } = useParams<{ id: string }>();
  const { knAdminApi } = useAuth();
  const act = useAction();
  const refs = useOrgRefs();
  const s = useLoad(async () => {
    const [user, asg, log] = await Promise.all([knAdminApi.user(id), knAdminApi.assignments(id), knAdminApi.log(id)]);
    return { user, asg: asg.items, log: log.items };
  }, [id, act.note]);
  const [edit, setEdit] = useState<{ full_name: string; email: string; phone: string } | null>(null);
  const [asgDlg, setAsgDlg] = useState<{ aid: string | null; draft: AsgDraft } | null>(null);
  const [reasonDlg, setReasonDlg] = useState<{ kind: "lock" | "unlock" | "revoke"; aid?: string } | null>(null);
  const [reason, setReason] = useState("");
  useBreadcrumbTail(s.data ? [{ label: fullName(s.data.user) }] : []);
  return (
    <>
      <BackLink href="/kn/quan-tri/nguoi-dung">Danh sách tài khoản</BackLink>
      <LoadState s={s}>
        {({ user, asg, log }) => {
          const f = edit ?? { full_name: fullName(user), email: user.email, phone: user.phone ?? "" };
          return (
            <>
              <PageHeader title={fullName(user)} />
              <ActionBar>
                <button type="button" className="btn btn-p btn-sm" disabled={!edit || act.busy} onClick={() => void act.run(() => knAdminApi.profile(id, edit!), (r) => `Đã lưu thay đổi (${r.changed_fields.join(", ")}).`).then(() => setEdit(null))}>
                  <Icon name="check" size={15} /> Lưu thay đổi
                </button>
                <button type="button" className="btn btn-o btn-sm" disabled={!edit} onClick={() => setEdit(null)}>
                  Hủy
                </button>
                <button type="button" className="btn btn-o btn-sm" onClick={() => void act.run(() => knAdminApi.resend(id), "Đã gửi lại liên kết đặt mật khẩu.")}>
                  <Icon name="send" size={15} /> Gửi lại mật khẩu
                </button>
                <button type="button" className="btn btn-d btn-sm" onClick={() => (setReason(""), setReasonDlg({ kind: user.enabled ? "lock" : "unlock" }))}>
                  <Icon name={user.enabled ? "lock" : "unlock"} size={15} /> {user.enabled ? "Khóa tài khoản" : "Mở khóa"}
                </button>
              </ActionBar>
              {act.view}
              <DetailLayout
                main={
                  <>
                    <section className="lv-block">
                      <h2>Thông tin tài khoản</h2>
                      <div className="grid2">
                        <label className="field">
                          <span>Họ tên*</span>
                          <input className="input" value={f.full_name} onChange={(e) => setEdit({ ...f, full_name: e.target.value })} />
                        </label>
                        <div className="field">
                          <span>Trạng thái</span>
                          <StatusPill tone={user.enabled ? "ok" : "bad"}>{user.enabled ? "Hoạt động" : "Đang khóa"}</StatusPill>
                        </div>
                        <label className="field">
                          <span>Email*</span>
                          <input className="input" value={f.email} onChange={(e) => setEdit({ ...f, email: e.target.value })} />
                        </label>
                        <label className="field">
                          <span>Số điện thoại</span>
                          <input className="input" value={f.phone} onChange={(e) => setEdit({ ...f, phone: e.target.value })} />
                        </label>
                      </div>
                    </section>
                    <div className="row" style={{ justifyContent: "space-between", marginTop: 14 }}>
                      <h2>Vai trò và phạm vi</h2>
                      <button type="button" className="btn btn-o btn-sm" onClick={() => setAsgDlg({ aid: null, draft: emptyDraft() })}>
                        + Thêm vai trò
                      </button>
                    </div>
                    <table className="dt" aria-label="Vai trò và phạm vi">
                      <thead>
                        <tr>
                          <th scope="col">Vai trò</th>
                          <th scope="col">Nút</th>
                          <th scope="col">Phòng ban</th>
                          <th scope="col">Cấp quản lý</th>
                          <th scope="col">Xã/phường phụ trách</th>
                          <th scope="col">Thao tác</th>
                        </tr>
                      </thead>
                      <tbody>
                        {asg.map((a) => (
                          <tr key={a.assignment_id}>
                            <td>
                              <b>{KN_ROLE_LABEL[a.role]}</b>
                            </td>
                            <td>{a.org_name}</td>
                            <td>{a.role === "admin" ? "Tất cả" : a.departments.map((x) => x.name.replace(/^Phòng /, "")).join(", ")}</td>
                            <td>{a.mgmt_level ?? "—"}</td>
                            <td>{a.role === "kn_xa" ? a.communes.map((c) => c.name).join(", ") || <StatusPill tone="warn">Chưa gán xã</StatusPill> : "—"}</td>
                            <td>
                              {a.role === "admin" ? null : (
                                <span className="row-acts">
                                  <button type="button" aria-label={`Sửa vai trò ${KN_ROLE_LABEL[a.role]}`} onClick={() => setAsgDlg({ aid: a.assignment_id, draft: { role: a.role as AsgDraft["role"], departments: a.departments.map((x) => x.code), mgmt: a.mgmt_level_code ?? "chuyen_vien", communes: a.communes.map((c) => c.code) } })}>
                                    <Icon name="edit" size={16} />
                                  </button>
                                  <button type="button" className="del" aria-label={`Xóa vai trò ${KN_ROLE_LABEL[a.role]}`} onClick={() => (setReason(""), setReasonDlg({ kind: "revoke", aid: a.assignment_id }))}>
                                    <Icon name="trash" size={16} />
                                  </button>
                                </span>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                    <p className="small mute">Một tài khoản có thể có nhiều vai trò, phòng ban; mỗi xã phụ trách là một tab khi nhập báo cáo.</p>
                    <RelatedTabs
                      tabs={[
                        {
                          id: "sys",
                          label: "Thông tin hệ thống",
                          content: <LabelValueBlock items={[{ label: "Tạo bởi", value: "admin.mekong" }, { label: "Thời điểm tạo", value: dt(user.created_at) }, { label: "Tên đăng nhập", value: user.username }, { label: "Kích hoạt", value: user.required_actions.length ? "Chưa kích hoạt" : "Đã kích hoạt" }]} />,
                        },
                        { id: "sess", label: "Phiên đang hoạt động", content: <p className="mute">Đọc từ Keycloak Admin REST (sessions) — bản giả lập chưa có (P1, mục còn mở).</p> },
                        { id: "hist", label: "Lịch sử đăng nhập", content: <p className="mute">Đọc từ Keycloak events — bản giả lập chưa có (P1, mục còn mở).</p> },
                      ]}
                    />
                  </>
                }
                log={<LogPanel title="Log chỉnh sửa" entries={log.map((l) => ({ event: LOG_EVENT[l.event] ?? l.event, actor_display: l.actor_display ?? "—", at: l.at, changes: l.changes, reason: l.reason }))} />}
              />
              {asgDlg ? (
                <Dialog
                  title={asgDlg.aid ? "Sửa vai trò" : "Thêm vai trò"}
                  onClose={() => setAsgDlg(null)}
                  actions={
                    <button
                      type="button"
                      className="btn btn-p"
                      onClick={async () => {
                        const input = toInput(asgDlg.draft, refs);
                        const r = await act.run(() => (asgDlg.aid ? knAdminApi.changeAssignment(id, asgDlg.aid, input) : knAdminApi.grant(id, input)), asgDlg.aid ? "Đã sửa vai trò." : "Đã thêm vai trò.");
                        if (r) setAsgDlg(null);
                      }}
                    >
                      Lưu
                    </button>
                  }
                >
                  <AssignmentEditor value={asgDlg.draft} orgRefs={{}} onChange={(v) => setAsgDlg({ ...asgDlg, draft: v })} />
                </Dialog>
              ) : null}
              {reasonDlg ? (
                <Dialog
                  title={reasonDlg.kind === "lock" ? "Khóa tài khoản" : reasonDlg.kind === "unlock" ? "Mở khóa tài khoản" : "Xóa vai trò"}
                  onClose={() => setReasonDlg(null)}
                  actions={
                    <button
                      type="button"
                      className="btn btn-p"
                      onClick={async () => {
                        const r = await act.run(
                          () => (reasonDlg.kind === "revoke" ? knAdminApi.revoke(id, reasonDlg.aid!, reason) : reasonDlg.kind === "lock" ? knAdminApi.lock(id, reason) : knAdminApi.unlock(id, reason)),
                          reasonDlg.kind === "lock" ? "Đã khóa tài khoản — mọi phiên bị thu hồi." : reasonDlg.kind === "unlock" ? "Đã mở khóa tài khoản." : "Đã xóa vai trò (dữ liệu đã nhập giữ nguyên).",
                        );
                        if (r !== null) setReasonDlg(null);
                      }}
                    >
                      Xác nhận
                    </button>
                  }
                >
                  <label className="field">
                    <span>Lý do* ({reasonDlg.kind === "revoke" ? "≥ 10" : "≥ 20"} ký tự)</span>
                    <textarea className="textarea" value={reason} onChange={(e) => setReason(e.target.value)} />
                  </label>
                </Dialog>
              ) : null}
            </>
          );
        }}
      </LoadState>
    </>
  );
}

/** FN-AD-04.07 Menu các cấp (hỗ trợ người dùng) — Hình 28. */
export function MenuLevelsScreen() {
  const { knAdminApi, enterSupport } = useAuth();
  const router = useRouter();
  const act = useAction();
  const [role, setRole] = useState<"kn_xa" | "kn_tinh" | "kn_tw">("kn_xa");
  const [target, setTarget] = useState("");
  const [reason, setReason] = useState("");
  const refs = useOrgRefs();
  const s = useLoad(async () => {
    const users = await knAdminApi.users();
    const asg = (await Promise.all(users.items.map((u) => knAdminApi.assignments(u.id).then((a) => a.items.map((x) => ({ u, a: x })))))).flat();
    return { asg, log: (await knAdminApi.supportLog()).items };
  }, []);
  const options = useMemo(() => (s.data?.asg ?? []).filter((x) => x.a.role === role && x.a.departments.some((d) => d.code === "trong_trot")), [s.data, role]);
  const [page, setPage] = useState(1);
  return (
    <>
      <PageHeader title="Menu các cấp (hỗ trợ người dùng)" />
      {act.view}
      <section className="card sec" aria-label="Chọn vai trò cần hỗ trợ">
        <h2>Chọn vai trò cần hỗ trợ</h2>
        <div className="grid4" style={{ gridTemplateColumns: "1fr 1fr 1fr" }}>
          <label className="field">
            <span>Xem như vai trò*</span>
            <select className="input" value={role} onChange={(e) => (setRole(e.target.value as typeof role), setTarget(""))}>
              <option value="kn_xa">Cấp Xã</option>
              <option value="kn_tinh">Cấp Tỉnh</option>
              <option value="kn_tw">Cấp Quốc gia</option>
            </select>
          </label>
          <label className="field">
            <span>Tài khoản / xã*</span>
            <select className="input" value={target} onChange={(e) => setTarget(e.target.value)}>
              <option value="">— Theo vai trò (không gắn tài khoản) —</option>
              {options.map((x) => (
                <option key={x.a.assignment_id} value={x.a.assignment_id}>
                  {fullName(x.u)}
                  {x.a.communes.length ? ` · ${x.a.communes.map((c) => c.name).join(", ")}` : ""}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span>Lý do hỗ trợ*</span>
            <input className="input" value={reason} onChange={(e) => setReason(e.target.value)} />
          </label>
        </div>
        <button
          type="button"
          className="btn btn-p"
          disabled={act.busy}
          onClick={async () => {
            const picked = options.find((x) => x.a.assignment_id === target);
            const ctx = await act.run(() =>
              knAdminApi.startSupport(
                picked
                  ? { target_assignment_id: picked.a.assignment_id, department_code: "trong_trot", reason }
                  : { role, org_ref: role === "kn_tw" ? refs.national : refs.province, department_code: "trong_trot", reason, ...(role === "kn_xa" ? { commune_codes: ["91301"] } : {}) },
              ),
            );
            if (ctx) {
              await enterSupport(ctx as never, picked ? `${fullName(picked.u)}${picked.a.communes.length ? ` · ${picked.a.communes.map((c) => c.name).join(", ")}` : ""}` : KN_ROLE_LABEL[role], reason);
              router.push(ctx.default_route);
            }
          }}
        >
          <Icon name="arrowLeft" size={15} /> Vào menu {KN_ROLE_LABEL[role]}
        </button>
      </section>
      <div className="alert alert-warn" role="note" style={{ margin: "12px 0" }}>
        <Icon name="support" />
        <span>Khi đang hỗ trợ, mọi màn hình có dải cảnh báo vàng “Đang hỗ trợ thay …”; mọi thao tác trên dữ liệu nghiệp vụ được ghi nhật ký riêng của Admin.</span>
      </div>
      <SectionTitle>Nhật ký thao tác Admin</SectionTitle>
      <LoadState s={s}>
        {(d) => {
          const rows = d.log.filter((l) => l.phase === "completed");
          const columns: Column<(typeof rows)[number]>[] = [
            { key: "at", header: "Thời điểm", cell: (l) => dt(l.at) },
            { key: "admin", header: "Admin", cell: (l) => l.actor_display ?? "—" },
            { key: "for", header: "Thay cho", cell: (l) => l.on_behalf_of_display ?? KN_ROLE_LABEL[l.acted_role] },
            { key: "fn", header: "Chức năng", cell: (l) => l.function },
            { key: "st", header: "Kết quả", cell: (l) => (l.status_code ?? 0) < 400 ? "Thành công" : `Lỗi ${l.status_code}` },
            { key: "reason", header: "Lý do", cell: (l) => l.reason },
          ];
          return <DataTable caption="Nhật ký hỗ trợ" columns={columns} rows={rows.slice((page - 1) * 20, page * 20)} rowKey={(l) => l.id} page={page} pageSize={20} total={rows.length} onPage={setPage} empty="Chưa có thao tác hỗ trợ." />;
        }}
      </LoadState>
    </>
  );
}

/** FN-AD-04.02 Cơ cấu tổ chức 3 cấp (Hình 23): cây TTKNQG › TTKN tỉnh › Khu vực › Xã, gán Khu vực, bảng Ảnh hưởng. */
export function OrgStructureScreen() {
  const { knAdminApi } = useAuth();
  const act = useAction();
  const tree = useLoad(() => knAdminApi.orgTree(), [act.note]);
  const [sel, setSel] = useState<KnOrgTreeNode | null>(null);
  const impact = useLoad(sel?.ref ? () => knAdminApi.impact(sel.ref!) : null, [sel?.ref]);
  const [region, setRegion] = useState("");
  const [reason, setReason] = useState("");
  const render = (n: KnOrgTreeNode, depth: number) => (
    <li key={`${n.kind}-${n.code}`} style={{ marginLeft: depth * 14 }}>
      {n.ref ? (
        <button type="button" className={`link-all${sel?.ref === n.ref ? " hl" : ""}`} onClick={() => (setSel(n), setRegion(n.region?.code ?? ""))}>
          {n.name}
        </button>
      ) : (
        <span className="small mute">{n.name} (khu vực, {n.commune_count} xã)</span>
      )}
      {n.children.length ? <ul className="list-plain">{n.children.map((c) => render(c, depth + 1))}</ul> : null}
    </li>
  );
  return (
    <>
      <PageHeader title="Cơ cấu tổ chức 3 cấp" />
      {act.view}
      <DetailLayout
        main={
          <LoadState s={tree}>
            {(t) => (
              <nav aria-label="Cây tổ chức">
                <ul className="list-plain tree">{t.items.map((n) => render(n, 0))}</ul>
              </nav>
            )}
          </LoadState>
        }
        log={
          <aside className="card sec" aria-label="Nút đã chọn">
            {sel ? (
              <>
                <h2>{sel.name}</h2>
                <LabelValueBlock items={[{ label: "Mã", value: sel.code }, { label: "Cấp", value: String(sel.level) }, { label: "Mã ĐVHC", value: sel.admin_unit_code ?? "—" }, { label: "Khu vực", value: sel.region?.name ?? "—" }]} />
                {sel.kind === "extension_team" ? (
                  <>
                    <label className="field">
                      <span>Khu vực</span>
                      <select className="input" value={region} onChange={(e) => setRegion(e.target.value)}>
                        <option value="">— Bỏ gán —</option>
                        <option value="LX">Khu vực Long Xuyên</option>
                        <option value="CD">Khu vực Châu Đốc</option>
                      </select>
                    </label>
                    <label className="field">
                      <span>Lý do (đổi/bỏ cần ≥ 10 ký tự)</span>
                      <input className="input" value={reason} onChange={(e) => setReason(e.target.value)} />
                    </label>
                    <button type="button" className="btn btn-p btn-sm" onClick={() => void act.run(() => knAdminApi.assignRegion(sel.ref!, region || null, reason), "Đã gán khu vực.")}>
                      Lưu khu vực
                    </button>
                  </>
                ) : (
                  <p className="small mute">Không di chuyển Nút cấp 1, cấp 2.</p>
                )}
                <h3 style={{ marginTop: 12 }}>Ảnh hưởng</h3>
                <LoadState s={impact}>
                  {(im) => (
                    <table className="dt">
                      <tbody>
                        {(im.rows as { label: string; count: number | null }[]).map((r) => (
                          <tr key={r.label}>
                            <td>{r.label}</td>
                            <td>{r.count ?? "chưa nối nguồn"}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </LoadState>
              </>
            ) : (
              <p className="mute">Chọn một Nút trong cây.</p>
            )}
          </aside>
        }
      />
    </>
  );
}

/** FN-AD-04.03 Danh mục phòng ban. */
export function DepartmentsScreen() {
  const { knAdminApi } = useAuth();
  const act = useAction();
  const s = useLoad(() => knAdminApi.departments(), [act.note]);
  const [sel, setSel] = useState<KnDepartment | null>(null);
  const log = useLoad(sel ? () => knAdminApi.departmentLog(sel.id) : null, [sel?.id, act.note]);
  const [page, setPage] = useState(1);
  return (
    <>
      <PageHeader title="Phòng ban" />
      {act.view}
      <DetailLayout
        main={
          <LoadState s={s}>
            {(d) => {
              const columns: Column<KnDepartment>[] = [
                { key: "code", header: "Mã", cell: (x) => x.code },
                { key: "name", header: "Tên", cell: (x) => <button type="button" className="link-all" onClick={() => setSel(x)}>{x.name}</button> },
                { key: "st", header: "Trạng thái", cell: (x) => <StatusPill tone={x.status === "active" ? "ok" : x.status === "suspended" ? "bad" : "neutral"}>{x.status_label}</StatusPill> },
                { key: "order", header: "Thứ tự", cell: (x) => x.sort_order },
                { key: "acc", header: "Số tài khoản", cell: (x) => x.account_count ?? "—" },
                {
                  key: "act",
                  header: "Đổi trạng thái",
                  cell: (x) => (
                    <select
                      className="input"
                      aria-label={`Trạng thái ${x.name}`}
                      value={x.status}
                      onChange={(e) => {
                        const st = e.target.value as KnDepartment["status"];
                        const r = st === "suspended" ? window.prompt("Lý do ngưng hoạt động (≥ 10 ký tự):") ?? "" : undefined;
                        void act.run(() => knAdminApi.departmentStatus(x.id, st, r), (res) => `Đã đổi “${x.name}” → ${res.department.status_label}.${res.warnings.length ? ` ${res.warnings.join(" ")}` : ""}`);
                      }}
                    >
                      <option value="active">Đang áp dụng</option>
                      <option value="not_open">Chưa mở</option>
                      <option value="suspended">Ngưng hoạt động</option>
                    </select>
                  ),
                },
              ];
              return <DataTable caption="Phòng ban của TTKN tỉnh An Giang" columns={columns} rows={d.items.slice((page - 1) * 20, page * 20)} rowKey={(x) => x.id} page={page} pageSize={20} total={d.items.length} onPage={setPage} />;
            }}
          </LoadState>
        }
        log={<LogPanel entries={(log.data?.items ?? []).map((l) => ({ event: l.event === "department.status_changed" ? "Đổi trạng thái" : l.event === "department.created" ? "Khai báo phòng ban" : l.event, actor_display: l.actor_display ?? "—", at: l.at, changes: l.changes, reason: l.reason }))} empty={sel ? "Chưa có thay đổi nào." : "Chọn một phòng ban để xem Log."} />}
      />
    </>
  );
}

/** FN-AD-04.01 Đơn vị hành chính (Tỉnh → Xã/Phường/Đặc khu, sắp xếp 2025). */
export function AdminUnitsScreen() {
  const { api } = useAuth();
  const [parent, setParent] = useState("91");
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const provinces = useLoad(() => unwrap(api.GET("/v1/masterdata/admin-units", { params: { query: {} } })), []);
  const s = useLoad(() => unwrap(api.GET("/v1/masterdata/admin-units", { params: { query: { parent } } })), [parent]);
  type U = NonNullable<typeof s.data>["items"][number];
  const columns: Column<U>[] = [
    { key: "code", header: "Mã", cell: (u) => u.code },
    { key: "name", header: "Tên", cell: (u) => u.name },
    { key: "level", header: "Cấp", cell: (u) => (u.level === "province" ? "Tỉnh" : u.level === "commune" ? "Xã/phường" : "Huyện (trước 2025)") },
    { key: "from", header: "Hiệu lực từ", cell: (u) => u.valid_from },
    { key: "st", header: "Trạng thái", cell: (u) => (u.valid_to ? <StatusPill>Hết hiệu lực</StatusPill> : <StatusPill tone="ok">Còn hiệu lực</StatusPill>) },
  ];
  return (
    <>
      <PageHeader title="Đơn vị hành chính" />
      <p className="mute small">Mã duy nhất, không tái sử dụng; không xóa — chỉ “Hết hiệu lực” (nạp/sắp xếp ĐVHC qua Admin API masterdata).</p>
      <FilterBar onReset={() => (setParent("91"), setQ(""))}>
        <FilterChip label="Tỉnh" value={parent} defaultValue="91" onChange={(v) => (setParent(v), setPage(1))} options={(provinces.data?.items ?? []).map((p) => ({ value: p.code, label: p.name }))} />
        <SearchBox value={q} onChange={setQ} placeholder="Tìm theo tên, mã" />
      </FilterBar>
      <LoadState s={s}>
        {(d) => {
          const rows = d.items.filter((u) => !q || `${u.code} ${u.name}`.toLowerCase().includes(q.toLowerCase()));
          return <DataTable caption="Đơn vị hành chính" columns={columns} rows={rows.slice((page - 1) * 20, page * 20)} rowKey={(u) => u.code} page={page} pageSize={20} total={rows.length} onPage={setPage} />;
        }}
      </LoadState>
    </>
  );
}

const CATALOG_TABS = [
  { id: "rice_variety", label: "Giống lúa" },
  { id: "material", label: "Vật tư" },
  { id: "crop", label: "Cây trồng" },
  { id: "pest", label: "Sâu bệnh dịch hại" },
  { id: "machine_type", label: "Loại máy móc" },
  { id: "production_model", label: "Mô hình sản xuất" },
] as const;

/** FN-AD-04.05 Danh mục nông nghiệp (Phòng ban › Phòng Trồng trọt). Thêm/sửa/vô hiệu hóa: màn danh mục chung. */
export function CatalogScreen() {
  const { adminApi } = useAuth();
  const [tab, setTab] = useState<(typeof CATALOG_TABS)[number]["id"]>("rice_variety");
  const [page, setPage] = useState(1);
  const s = useLoad(() => unwrap(adminApi.GET("/v1/masterdata/catalogs/{catalog}/items", { params: { path: { catalog: tab }, query: { include_inactive: true, limit: 200 } } })), [tab]);
  type I = NonNullable<typeof s.data>["items"][number];
  const columns: Column<I>[] = [
    { key: "code", header: "Mã", cell: (i) => i.code },
    { key: "name", header: "Tên", cell: (i) => (i.active ? i.name : <span className="mute">{i.name}</span>) },
    { key: "group", header: "Nhóm / ĐVT", cell: (i) => i.attrs.group ?? i.attrs.parent_code ?? i.attrs.unit ?? "—" },
    { key: "forms", header: "Biểu đang dùng", cell: (i) => i.forms_in_use ?? "—" },
    { key: "st", header: "Trạng thái", cell: (i) => (i.active ? <StatusPill tone="ok">Đang dùng</StatusPill> : <StatusPill>Ngừng dùng</StatusPill>) },
  ];
  return (
    <>
      <PageHeader
        title="Danh mục nông nghiệp"
        actions={
          <Link className="btn btn-o" href="/quan-tri/danh-muc">
            Thêm / sửa / vô hiệu hóa
          </Link>
        }
      />
      <div className="dtabs" role="tablist" aria-label="Danh mục">
        {CATALOG_TABS.map((t) => (
          <button key={t.id} type="button" role="tab" aria-selected={t.id === tab} onClick={() => (setTab(t.id), setPage(1))}>
            {t.label}
          </button>
        ))}
      </div>
      <LoadState s={s}>{(d) => <DataTable caption="Mục danh mục" columns={columns} rows={d.items.slice((page - 1) * 20, page * 20)} rowKey={(i) => i.code} page={page} pageSize={20} total={d.items.length} onPage={setPage} empty="Danh mục chưa có mục (chờ khách Q-01)." />}</LoadState>
    </>
  );
}
