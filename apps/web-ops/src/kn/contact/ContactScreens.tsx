"use client";

// FN-13 Contact (BRD HCTH v3.0; W11.23, pf/contact `cap/directory`) — menu "Dùng chung > Contact" cho MỌI vai. Danh sách
// thẻ Cán bộ / thẻ Đơn vị, tìm kiếm, chi tiết. Cột theo bộ trường máy chủ trả (field_set): Cấp TW, Cấp Xã chỉ Họ tên, Năm
// sinh, Số điện thoại — không có nút Thêm/Sửa/Xuất, không xem chi tiết (AC-01); Cấp Tỉnh, Admin thêm Chức danh, Đơn vị công
// tác, Cấp, Email. Cấp Tỉnh – HCTH, Admin thêm/sửa/ngừng hiển thị liên hệ ngoài tỉnh và thẻ Đơn vị (B4). Dữ liệu cá nhân
// chỉ giữ trong bộ nhớ của màn.
import Link from "next/link";
import { useParams } from "next/navigation";
import { useMemo, useState, type ReactNode } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { Tabs } from "@/components/Tabs";
import { BackLink, LabelValueBlock } from "@/components/patterns/detail";
import { DataTable, FilterBar, FilterChip, PageHeader, SearchBox, StatusPill, type Column } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { dialUri } from "@/lib/dial";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";
import {
  createContactApi,
  FIELD_LABEL,
  isFull,
  LEVEL_LABEL,
  type ContactAccess,
  type ContactApi,
  type ContactLevel,
  type ContactPerson,
  type ContactPersonDetail,
  type ContactPersonWrite,
  type ContactUnit,
  type ContactUnitWrite,
  type PersonFilter,
} from "./api";

const PAGE = 20;
export const CONTACT_ROUTE = "/kn/dung-chung/contact";

export function useContactApi(): ContactApi {
  const { api } = useAuth();
  return useMemo(() => createContactApi(api), [api]);
}

function Phone({ value }: { value: string }) {
  return value ? <a href={dialUri("tel", value)}>{value}</a> : <span className="mute">—</span>;
}

function StaffSourceNote({ src }: { src: string }) {
  if (src !== "disabled") return null;
  return (
    <div className="alert alert-info" role="note">
      <span>
        Cán bộ thuộc tỉnh đồng bộ từ hồ sơ nhân sự — hồ sơ nhân sự chưa bật cho dữ liệu thật (Q-005), danh bạ hiện chỉ có liên hệ
        ngoài tỉnh.
      </span>
    </div>
  );
}

const LEVEL_OPTIONS = [
  { value: "", label: "Tất cả" },
  ...Object.entries(LEVEL_LABEL).map(([value, label]) => ({ value, label })),
];

// --------------------------------------------------------------------------------------------------------------------
// Danh sách (FN-13 B1–B4).
// --------------------------------------------------------------------------------------------------------------------

export function ContactListScreen() {
  const ca = useContactApi();
  const access = useLoad(() => ca.access(), []);
  const [tab, setTab] = useState("persons");
  return (
    <>
      <PageHeader title="Contact">Danh bạ liên hệ cán bộ và đơn vị khuyến nông các cấp — cột hiển thị theo vai trò.</PageHeader>
      <LoadState s={access}>
        {(a) => (
          <Tabs
            label="Danh bạ"
            tabs={[
              { id: "persons", label: "Cán bộ" },
              { id: "units", label: "Đơn vị" },
            ]}
            value={tab}
            onChange={setTab}
          >
            {tab === "persons" ? <PersonsTab ca={ca} a={a} /> : <UnitsTab ca={ca} a={a} />}
          </Tabs>
        )}
      </LoadState>
    </>
  );
}

function PersonsTab({ ca, a }: { ca: ContactApi; a: ContactAccess }) {
  const full = a.field_set === "full";
  const [f, setF] = useState<PersonFilter>({});
  const [page, setPage] = useState(1);
  const [nonce, setNonce] = useState(0);
  const [creating, setCreating] = useState(false);
  const act = useAction();
  const list = useLoad(() => ca.persons({ ...f, offset: (page - 1) * PAGE, limit: PAGE }), [JSON.stringify(f), page, nonce]);
  const set = (patch: PersonFilter) => {
    setF((x) => ({ ...x, ...patch }));
    setPage(1);
  };
  const cell = (key: string, r: ContactPerson): ReactNode => {
    switch (key) {
      case "full_name":
        if (isFull(r) && r.erased) return <span className="mute">(đã xóa định danh)</span>;
        return a.can_detail ? <Link href={`${CONTACT_ROUTE}/${r.id}`}>{r.full_name}</Link> : r.full_name;
      case "birth_year":
        return r.birth_year || "—";
      case "phone":
        return <Phone value={r.phone} />;
    }
    if (!isFull(r)) return null;
    switch (key) {
      case "title":
        return r.title || "—";
      case "unit":
        return (
          <>
            {r.unit || "—"}
            {r.hidden ? (
              <>
                {" "}
                <StatusPill tone="warn">Ngừng hiển thị</StatusPill>
              </>
            ) : null}
          </>
        );
      case "level":
        return r.level_label;
      case "email":
        return r.email ? <a href={`mailto:${r.email}`}>{r.email}</a> : "—";
    }
    return null;
  };
  // Cột theo bộ trường máy chủ trả (không tự suy từ vai ở trình duyệt).
  const fields = list.data?.fields ?? a.fields;
  const cols: Column<ContactPerson>[] = fields.map((k) => ({ key: k, header: FIELD_LABEL[k] ?? k, cell: (r) => cell(k, r) }));
  return (
    <>
      {act.view}
      <FilterBar onReset={() => set({ q: undefined, level: undefined, unit: undefined, title: undefined, include_hidden: undefined })}>
        <SearchBox
          value={f.q ?? ""}
          onChange={(q) => set({ q })}
          placeholder={full ? "Họ tên, số điện thoại, đơn vị, email" : "Họ tên hoặc số điện thoại"}
        />
        {full ? (
          <>
            <FilterChip
              label="Cấp"
              value={f.level ?? ""}
              options={LEVEL_OPTIONS}
              onChange={(v) => set({ level: (v || undefined) as ContactLevel | undefined })}
            />
            <label className="small">
              Đơn vị{" "}
              <input aria-label="Lọc theo đơn vị" value={f.unit ?? ""} onChange={(e) => set({ unit: e.target.value || undefined })} />
            </label>
            <label className="small">
              Chức danh{" "}
              <input aria-label="Lọc theo chức danh" value={f.title ?? ""} onChange={(e) => set({ title: e.target.value || undefined })} />
            </label>
          </>
        ) : null}
        {a.can_manage ? (
          <label className="small">
            <input
              type="checkbox"
              checked={!!f.include_hidden}
              onChange={(e) => set({ include_hidden: e.target.checked || undefined })}
            />{" "}
            Gồm liên hệ ngừng hiển thị
          </label>
        ) : null}
        {a.can_manage ? (
          <button type="button" className="btn btn-p btn-sm" onClick={() => setCreating(true)}>
            Thêm liên hệ ngoài tỉnh
          </button>
        ) : null}
      </FilterBar>
      <StaffSourceNote src={a.staff_source} />
      <LoadState s={list}>
        {(l) => (
          <>
            <p className="mute small">{l.total} liên hệ</p>
            <DataTable
              caption="Danh bạ cán bộ"
              columns={cols}
              rows={l.items as ContactPerson[]}
              rowKey={(r) => r.id}
              page={page}
              pageSize={PAGE}
              total={l.total}
              onPage={setPage}
            />
          </>
        )}
      </LoadState>
      {creating ? (
        <PersonDialog
          title="Thêm liên hệ ngoài tỉnh"
          onClose={() => setCreating(false)}
          onSave={async (body) => {
            const r = await act.run(() => ca.createPerson(body), (d) => `Đã thêm ${d.full_name}.`);
            if (r) {
              setCreating(false);
              setNonce((n) => n + 1);
            }
          }}
          busy={act.busy}
        />
      ) : null}
    </>
  );
}

function PersonDialog({
  title,
  initial,
  onClose,
  onSave,
  busy,
}: {
  title: string;
  initial?: ContactPersonDetail;
  onClose: () => void;
  onSave: (b: ContactPersonWrite) => void;
  busy: boolean;
}) {
  const [b, setB] = useState<ContactPersonWrite>(() =>
    initial
      ? {
          full_name: initial.full_name,
          phone: initial.phone,
          birth_year: initial.birth_year,
          email: initial.email,
          level: initial.level,
          organization: initial.organization,
          title: initial.title,
        }
      : { level: "tw" },
  );
  const txt = (k: keyof ContactPersonWrite, label: string, required = false, type = "text") => (
    <label className="field">
      <span>
        {label}
        {required ? " *" : ""}
      </span>
      <input
        type={type}
        aria-label={label}
        value={(b[k] as string | undefined) ?? ""}
        required={required}
        onChange={(e) => setB((x) => ({ ...x, [k]: e.target.value }))}
      />
    </label>
  );
  return (
    <Dialog
      title={title}
      onClose={onClose}
      actions={
        <>
          <button type="button" className="btn btn-o btn-sm" onClick={onClose}>
            Hủy
          </button>
          <button type="button" className="btn btn-p btn-sm" disabled={busy} onClick={() => onSave(b)}>
            Lưu
          </button>
        </>
      }
    >
      <div className="form-grid">
        {txt("full_name", "Họ tên", true)}
        {txt("phone", "Số điện thoại", true, "tel")}
        {txt("birth_year", "Năm sinh")}
        {txt("email", "Email", false, "email")}
        <label className="field">
          <span>Cấp *</span>
          <select aria-label="Cấp" value={b.level ?? "tw"} onChange={(e) => setB((x) => ({ ...x, level: e.target.value as ContactLevel }))}>
            {Object.entries(LEVEL_LABEL).map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </select>
        </label>
        {txt("organization", "Cơ quan")}
        {txt("title", "Chức danh")}
      </div>
      <p className="mute small">Họ tên, số điện thoại, email, năm sinh lưu ở kho định danh (mục đích danh bạ liên hệ).</p>
    </Dialog>
  );
}

function UnitsTab({ ca, a }: { ca: ContactApi; a: ContactAccess }) {
  const full = a.field_set === "full";
  const [q, setQ] = useState("");
  const [level, setLevel] = useState("");
  const [hidden, setHidden] = useState(false);
  const [nonce, setNonce] = useState(0);
  const [editing, setEditing] = useState<ContactUnit | "new" | null>(null);
  const act = useAction();
  const list = useLoad(() => ca.units(q || undefined, (level || undefined) as ContactLevel | undefined, hidden || undefined), [
    q,
    level,
    hidden,
    nonce,
  ]);
  const cols: Column<ContactUnit>[] = [
    {
      key: "name",
      header: "Tên đơn vị",
      cell: (u) => (
        <>
          {u.name}
          {u.hidden ? (
            <>
              {" "}
              <StatusPill tone="warn">Ngừng hiển thị</StatusPill>
            </>
          ) : null}
        </>
      ),
    },
    { key: "level", header: "Cấp", cell: (u) => u.level_label },
    { key: "phone", header: "Số điện thoại", cell: (u) => <Phone value={u.phone} /> },
    ...(full
      ? [
          { key: "email", header: "Email", cell: (u: ContactUnit) => u.email || "—" },
          { key: "address", header: "Địa chỉ", cell: (u: ContactUnit) => u.address || "—" },
        ]
      : []),
    ...(a.can_manage
      ? [
          {
            key: "act",
            header: "",
            cell: (u: ContactUnit) => (
              <button type="button" className="btn btn-o btn-sm" onClick={() => setEditing(u)}>
                Sửa
              </button>
            ),
          },
        ]
      : []),
  ];
  return (
    <>
      {act.view}
      <FilterBar
        onReset={() => {
          setQ("");
          setLevel("");
          setHidden(false);
        }}
      >
        <SearchBox value={q} onChange={setQ} placeholder="Tên đơn vị hoặc số điện thoại" />
        <FilterChip label="Cấp" value={level} options={LEVEL_OPTIONS} onChange={setLevel} />
        {a.can_manage ? (
          <>
            <label className="small">
              <input type="checkbox" checked={hidden} onChange={(e) => setHidden(e.target.checked)} /> Gồm đơn vị ngừng hiển thị
            </label>
            <button type="button" className="btn btn-p btn-sm" onClick={() => setEditing("new")}>
              Thêm đơn vị
            </button>
          </>
        ) : null}
      </FilterBar>
      <LoadState s={list}>
        {(l) => (
          <DataTable
            caption="Danh bạ đơn vị"
            columns={cols}
            rows={l.items}
            rowKey={(u) => u.id}
            page={1}
            pageSize={Math.max(l.items.length, 1)}
            total={l.items.length}
            onPage={() => undefined}
          />
        )}
      </LoadState>
      {editing ? (
        <UnitDialog
          initial={editing === "new" ? undefined : editing}
          busy={act.busy}
          onClose={() => setEditing(null)}
          onSave={async (b) => {
            const r = await act.run(
              () => (editing === "new" ? ca.createUnit(b) : ca.reviseUnit(editing.id, b)),
              (u) => `Đã lưu ${u.name}.`,
            );
            if (r) {
              setEditing(null);
              setNonce((n) => n + 1);
            }
          }}
        />
      ) : null}
    </>
  );
}

function UnitDialog({
  initial,
  onClose,
  onSave,
  busy,
}: {
  initial?: ContactUnit;
  onClose: () => void;
  onSave: (b: ContactUnitWrite) => void;
  busy: boolean;
}) {
  const [b, setB] = useState<ContactUnitWrite>(() =>
    initial
      ? { name: initial.name, level: initial.level, phone: initial.phone, email: initial.email, address: initial.address, hidden: initial.hidden }
      : { name: "", level: "tw" },
  );
  const txt = (k: "name" | "phone" | "email" | "address", label: string) => (
    <label className="field">
      <span>{label}</span>
      <input aria-label={label} value={b[k] ?? ""} onChange={(e) => setB((x) => ({ ...x, [k]: e.target.value }))} />
    </label>
  );
  return (
    <Dialog
      title={initial ? "Sửa đơn vị" : "Thêm đơn vị"}
      onClose={onClose}
      actions={
        <>
          <button type="button" className="btn btn-o btn-sm" onClick={onClose}>
            Hủy
          </button>
          <button type="button" className="btn btn-p btn-sm" disabled={busy} onClick={() => onSave(b)}>
            Lưu
          </button>
        </>
      }
    >
      <div className="form-grid">
        {txt("name", "Tên đơn vị")}
        <label className="field">
          <span>Cấp</span>
          <select aria-label="Cấp đơn vị" value={b.level} onChange={(e) => setB((x) => ({ ...x, level: e.target.value as ContactLevel }))}>
            {Object.entries(LEVEL_LABEL).map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </select>
        </label>
        {txt("phone", "Số máy cơ quan")}
        {txt("email", "Email cơ quan")}
        {txt("address", "Địa chỉ")}
        {initial ? (
          <label className="small">
            <input type="checkbox" checked={!!b.hidden} onChange={(e) => setB((x) => ({ ...x, hidden: e.target.checked }))} /> Ngừng
            hiển thị
          </label>
        ) : null}
      </div>
      <p className="mute small">Thẻ Đơn vị là liên hệ công vụ của cơ quan — số di động của một người nhập ở thẻ Cán bộ.</p>
    </Dialog>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// Chi tiết (Cấp Tỉnh, Admin — BR-03: Cấp TW, Cấp Xã không xem chi tiết).
// --------------------------------------------------------------------------------------------------------------------

const CHANGE_LABEL: Record<string, string> = { created: "Thêm", updated: "Sửa", hidden: "Ngừng hiển thị", shown: "Hiện lại" };

export function ContactDetailScreen() {
  const { id } = useParams<{ id: string }>();
  const ca = useContactApi();
  const [nonce, setNonce] = useState(0);
  const [editing, setEditing] = useState(false);
  const act = useAction();
  const d = useLoad(() => ca.person(id), [id, nonce]);
  useBreadcrumbTail(d.data ? [{ label: d.data.full_name || "Liên hệ" }] : []);
  const save = async (body: ContactPersonWrite) => {
    const r = await act.run(() => ca.revisePerson(id, body), "Đã lưu.");
    if (r) {
      setEditing(false);
      setNonce((n) => n + 1);
    }
  };
  return (
    <>
      <BackLink href={CONTACT_ROUTE}>Danh bạ</BackLink>
      <LoadState s={d}>
        {(p) => (
          <>
            <PageHeader
              title={p.full_name || "(đã xóa định danh)"}
              badge={p.hidden ? <StatusPill tone="warn">Ngừng hiển thị</StatusPill> : null}
              actions={
                p.can_edit ? (
                  <>
                    <button type="button" className="btn btn-o btn-sm" onClick={() => setEditing(true)}>
                      Sửa
                    </button>
                    <button type="button" className="btn btn-o btn-sm" disabled={act.busy} onClick={() => void save({ hidden: !p.hidden })}>
                      {p.hidden ? "Hiện lại" : "Ngừng hiển thị"}
                    </button>
                  </>
                ) : null
              }
            >
              {p.source === "staff"
                ? "Cán bộ thuộc tỉnh — đồng bộ từ hồ sơ nhân sự (sửa tại hồ sơ, FN-07)."
                : "Liên hệ ngoài tỉnh — Cấp Tỉnh – HCTH, Admin cập nhật."}
            </PageHeader>
            {act.view}
            <LabelValueBlock
              title="Thông tin liên hệ"
              items={[
                { label: "Họ tên", value: p.full_name },
                { label: "Năm sinh", value: p.birth_year },
                { label: "Số điện thoại", value: p.phone ? <Phone value={p.phone} /> : "" },
                { label: "Email", value: p.email },
                { label: "Chức danh", value: p.title },
                { label: p.source === "staff" ? "Đơn vị công tác" : "Cơ quan", value: p.unit },
                { label: "Cấp", value: p.level_label },
              ]}
            />
            {p.source === "external" ? (
              <section className="lv-block">
                <h2>Lịch sử thay đổi</h2>
                <DataTable
                  caption="Lịch sử thay đổi"
                  columns={[
                    { key: "v", header: "Phiên bản", cell: (h) => h.version },
                    { key: "c", header: "Thao tác", cell: (h) => CHANGE_LABEL[h.change] ?? h.change },
                    {
                      key: "f",
                      header: "Trường đã đổi",
                      cell: (h) => h.changed_fields.map((x) => FIELD_LABEL[x] ?? x).join(", ") || "—",
                    },
                    { key: "t", header: "Thời điểm", cell: (h) => new Date(h.at).toLocaleString("vi-VN") },
                  ]}
                  rows={p.history}
                  rowKey={(h) => String(h.version)}
                  page={1}
                  pageSize={Math.max(p.history.length, 1)}
                  total={p.history.length}
                  onPage={() => undefined}
                />
              </section>
            ) : null}
            {editing ? <PersonDialog title="Sửa liên hệ" initial={p} busy={act.busy} onClose={() => setEditing(false)} onSave={(b) => void save(b)} /> : null}
          </>
        )}
      </LoadState>
    </>
  );
}
