"use client";

// FN-07, FN-08, FN-20 — Hồ sơ nhân sự (BRD HCTH v3.0; W11.22, dom/hrm). Danh sách (lọc kết hợp, ngày chốt as-of, xuất
// CSV có ghi nhật ký), chi tiết theo thẻ (Thông tin chung, Công tác, Trình độ - Bồi dưỡng, Khen thưởng, Ngạch - Bậc,
// Thông tin làm việc, Thống kê BC Sở, Lịch sử sửa, Nhật ký xem/xuất) và ma trận phân quyền hồ sơ (Admin). Quyền lấy từ
// máy chủ (/v1/hrm/access, can của từng hồ sơ). Dữ liệu cá nhân chỉ giữ trong bộ nhớ của màn — không lưu trình duyệt.
import Link from "next/link";
import { useParams } from "next/navigation";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { Tabs } from "@/components/Tabs";
import { BackLink, LabelValueBlock } from "@/components/patterns/detail";
import {
  DataTable,
  FilterBar,
  FilterChip,
  PageHeader,
  SearchBox,
  StatusPill,
  type Column,
} from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";
import { vnDecimal } from "./api";
import {
  createStaffApi,
  EVENT_LABEL,
  GROUP_LABEL,
  PERM_LABEL,
  SCOPE_LABEL,
  SENSITIVE_LABEL,
  STAFF_TYPE_LABEL,
  STATUS_LABEL,
  TAB_EVENTS,
  vnDate,
  type HrmEventKind,
  type HrmPermEntry,
  type HrmPermMatrix,
  type HrmScope,
  type HrmStaffDetail,
  type HrmStaffEvent,
  type HrmStaffItem,
  type HrmStaffType,
  type StaffApi,
  type StaffFilter,
} from "./staff-api";

const PAGE = 20;
const DETAIL = "/kn/nhan-su/ho-so";

export function useStaffApi(): StaffApi {
  const { api } = useAuth();
  return useMemo(() => createStaffApi(api), [api]);
}

function vnToday(): string {
  return new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
}

function StatusTag({ s }: { s: string }) {
  return (
    <StatusPill
      tone={s === "active" ? "ok" : s === "left" ? "warn" : "neutral"}
    >
      {STATUS_LABEL[s] ?? s}
    </StatusPill>
  );
}

function gradeText(c: { grade: string; step: number; steps: number }): string {
  if (!c.grade) return "—";
  return c.step
    ? `${c.grade} · bậc ${c.step}${c.steps ? `/${c.steps}` : ""}`
    : c.grade;
}

function Disabled() {
  return (
    <div className="alert alert-info" role="note">
      <span>
        Hồ sơ nhân sự chưa bật cho đơn vị (chờ đánh giá tác động xử lý dữ liệu
        cá nhân — Q-005). Danh mục nhân sự vẫn dùng được.
      </span>
    </div>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// Danh sách (FN-07 B1, B2, B5; FN-08 lọc trạng thái).
// --------------------------------------------------------------------------------------------------------------------

/** mode = "unit": "Nhân sự của phòng" (lãnh đạo — máy chủ đã giới hạn phạm vi đơn vị mình). */
export function HrmStaffListScreen({
  mode = "all",
}: {
  mode?: "all" | "unit";
}) {
  const sa = useStaffApi();
  const access = useLoad(() => sa.access(), []);
  const [f, setF] = useState<StaffFilter>({});
  const [page, setPage] = useState(1);
  const [nonce, setNonce] = useState(0);
  const [creating, setCreating] = useState(false);
  const act = useAction();
  const enabled =
    access.data?.enabled === true &&
    access.data.perms["profile.view"] !== "none";
  const list = useLoad(
    enabled
      ? () => sa.list({ ...f, offset: (page - 1) * PAGE, limit: PAGE })
      : null,
    [enabled, JSON.stringify(f), page, nonce],
  );
  const units = useLoad(enabled ? () => sa.units() : null, [enabled]);
  const unitName = (code: string) =>
    units.data?.items.find((u) => u.code === code)?.name ?? code;
  const set = (patch: StaffFilter) => {
    setF((x) => ({ ...x, ...patch }));
    setPage(1);
  };
  const cols: Column<HrmStaffItem>[] = [
    {
      key: "code",
      header: "Mã hồ sơ",
      cell: (r) => <Link href={`${DETAIL}/${r.id}`}>{r.code}</Link>,
    },
    {
      key: "name",
      header: "Họ tên",
      cell: (r) => r.label || <span className="mute">(ẩn)</span>,
    },
    {
      key: "type",
      header: "Loại nhân sự",
      cell: (r) => STAFF_TYPE_LABEL[r.type],
    },
    {
      key: "unit",
      header: "Đơn vị",
      cell: (r) =>
        r.current.unit
          ? unitName(r.current.unit)
          : r.current.mgmt_unit
            ? `Xã ${r.current.commune} · ${unitName(r.current.mgmt_unit)}`
            : "—",
    },
    { key: "pos", header: "Chức vụ", cell: (r) => r.current.position || "—" },
    { key: "grade", header: "Ngạch - Bậc", cell: (r) => gradeText(r.current) },
    {
      key: "coef",
      header: "Hệ số",
      className: "num",
      cell: (r) => vnDecimal(r.current.coefficient),
    },
    {
      key: "status",
      header: "Trạng thái",
      cell: (r) => <StatusTag s={r.current.status} />,
    },
  ];
  return (
    <>
      <PageHeader
        title={mode === "unit" ? "Nhân sự của phòng" : "Hồ sơ nhân sự"}
        actions={
          access.data?.enabled ? (
            <>
              {access.data.perms.export !== "none" ? (
                <button
                  type="button"
                  className="btn btn-o btn-sm"
                  disabled={act.busy}
                  onClick={() =>
                    void act.run(
                      () => sa.exportCsv(f),
                      (r) => (
                        saveBlob(r.blob, r.name),
                        "Đã xuất danh sách (thao tác được ghi nhật ký)."
                      ),
                    )
                  }
                >
                  Xuất CSV
                </button>
              ) : null}
              {mode === "all" &&
              access.data.perms["profile.edit"] !== "none" ? (
                <button
                  type="button"
                  className="btn btn-p btn-sm"
                  onClick={() => setCreating(true)}
                >
                  Thêm hồ sơ
                </button>
              ) : null}
            </>
          ) : null
        }
      >
        {mode === "unit"
          ? "Nhân sự thuộc đơn vị của bạn và cán bộ khuyến nông cấp xã do đơn vị quản lý."
          : "Hồ sơ tối giản của viên chức, hợp đồng lao động và cán bộ khuyến nông cấp xã. Số liệu tính tại ngày chốt."}
      </PageHeader>
      <LoadState s={access}>
        {(a) =>
          !a.enabled ? (
            <Disabled />
          ) : a.perms["profile.view"] === "none" ? (
            <div className="alert alert-info" role="note">
              <span>Bạn không có quyền xem hồ sơ nhân sự.</span>
            </div>
          ) : (
            <>
              {act.view}
              <FilterBar
                onReset={() =>
                  set({
                    at: undefined,
                    type: undefined,
                    status: undefined,
                    unit: undefined,
                    q: undefined,
                  })
                }
              >
                <SearchBox
                  value={f.q ?? ""}
                  onChange={(q) => set({ q })}
                  placeholder="Mã hồ sơ hoặc số điện thoại"
                />
                <FilterChip
                  label="Loại"
                  value={f.type ?? ""}
                  options={[
                    { value: "", label: "Tất cả" },
                    ...Object.entries(STAFF_TYPE_LABEL).map(
                      ([value, label]) => ({ value, label }),
                    ),
                  ]}
                  onChange={(v) =>
                    set({ type: (v || undefined) as HrmStaffType | undefined })
                  }
                />
                <FilterChip
                  label="Trạng thái"
                  value={f.status ?? ""}
                  options={[
                    { value: "", label: "Tất cả" },
                    ...Object.entries(STATUS_LABEL).map(([value, label]) => ({
                      value,
                      label,
                    })),
                  ]}
                  onChange={(v) =>
                    set({ status: (v || undefined) as StaffFilter["status"] })
                  }
                />
                <FilterChip
                  label="Đơn vị"
                  value={f.unit ?? ""}
                  options={[
                    { value: "", label: "Tất cả" },
                    ...(units.data?.items ?? []).map((u) => ({
                      value: u.code,
                      label: u.name,
                    })),
                  ]}
                  onChange={(v) => set({ unit: v || undefined })}
                />
                <label className="small">
                  Ngày chốt{" "}
                  <input
                    type="date"
                    aria-label="Ngày chốt"
                    value={f.at ?? ""}
                    max="2200-12-31"
                    onChange={(e) => set({ at: e.target.value || undefined })}
                  />
                </label>
              </FilterBar>
              <LoadState s={list}>
                {(l) => (
                  <>
                    <p className="mute small">
                      Ngày chốt {vnDate(l.day)} · phạm vi:{" "}
                      {SCOPE_LABEL[l.scope]} · {l.total} hồ sơ
                    </p>
                    <DataTable
                      caption="Hồ sơ nhân sự"
                      columns={cols}
                      rows={l.items}
                      rowKey={(r) => r.id}
                      page={page}
                      pageSize={PAGE}
                      total={l.total}
                      onPage={setPage}
                    />
                  </>
                )}
              </LoadState>
            </>
          )
        }
      </LoadState>
      {creating ? (
        <CreateDialog
          sa={sa}
          units={units.data?.items ?? []}
          canSensitive={access.data?.perms["sensitive.view"] !== "none"}
          onClose={() => setCreating(false)}
          onDone={(code) => {
            setCreating(false);
            act.setNote(`Đã tạo hồ sơ ${code}.`);
            setNonce((n) => n + 1);
          }}
        />
      ) : null}
    </>
  );
}

function Field({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <label className="field">
      <span>{label}</span>
      {children}
    </label>
  );
}

function CreateDialog({
  sa,
  units,
  canSensitive,
  onClose,
  onDone,
}: {
  sa: StaffApi;
  units: { code: string; name: string; active: boolean }[];
  canSensitive: boolean;
  onClose: () => void;
  onDone: (code: string) => void;
}) {
  const act = useAction();
  const [v, setV] = useState({
    type: "vien_chuc" as HrmStaffType,
    full_name: "",
    gender: "nu",
    birth_date: "",
    phone: "",
    email: "",
    address: "",
    unit: "",
    commune: "",
    mgmt_unit: "",
    position: "vien_chuc",
    appointed_on: "",
    grade: "",
    step: "",
    start: vnToday(),
    ethnicity: "",
    religion: "",
    hometown: "",
  });
  const up = (k: keyof typeof v) => (e: { target: { value: string } }) =>
    setV((x) => ({ ...x, [k]: e.target.value }));
  const xa = v.type === "can_bo_xa";
  const submit = () => {
    const from = v.start || vnToday();
    const events: Parameters<StaffApi["create"]>[0]["events"] = [
      { kind: "work_start", valid_from: from, payload: {} },
      {
        kind: "unit",
        valid_from: from,
        payload: xa
          ? { commune: v.commune, mgmt_unit: v.mgmt_unit }
          : { unit: v.unit },
      },
      {
        kind: "position",
        valid_from: from,
        payload: v.appointed_on
          ? { position: v.position, appointed_on: v.appointed_on }
          : { position: v.position },
      },
    ];
    if (v.grade)
      events.push({
        kind: "grade",
        valid_from: from,
        payload: { grade: v.grade, step: v.step || "1" },
      });
    const sensitive =
      !xa && canSensitive
        ? Object.fromEntries(
            Object.entries({
              ethnicity: v.ethnicity,
              religion: v.religion,
              hometown: v.hometown,
            }).filter(([, x]) => x),
          )
        : {};
    void act
      .run(() =>
        sa.create({
          type: v.type,
          profile: {
            full_name: v.full_name,
            gender: v.gender as "nam" | "nu",
            birth_date: v.birth_date,
            phone: v.phone,
            email: v.email || undefined,
            address: v.address || undefined,
          },
          sensitive: Object.keys(sensitive).length ? sensitive : undefined,
          events,
        }),
      )
      .then((r) => r && onDone(r.code));
  };
  return (
    <Dialog
      title="Thêm hồ sơ nhân sự"
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={act.busy}
          onClick={submit}
        >
          Lưu
        </button>
      }
    >
      {act.view}
      <div className="grid2">
        <Field label="Loại nhân sự">
          <select value={v.type} onChange={up("type")}>
            {Object.entries(STAFF_TYPE_LABEL).map(([k, l]) => (
              <option key={k} value={k}>
                {l}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Họ và tên">
          <input
            value={v.full_name}
            onChange={up("full_name")}
            maxLength={100}
            autoComplete="off"
          />
        </Field>
        <Field label="Giới tính">
          <select value={v.gender} onChange={up("gender")}>
            <option value="nu">Nữ</option>
            <option value="nam">Nam</option>
          </select>
        </Field>
        <Field label="Ngày sinh (YYYY-MM-DD, YYYY-MM hoặc YYYY)">
          <input
            value={v.birth_date}
            onChange={up("birth_date")}
            placeholder="1988-05-01"
            autoComplete="off"
          />
        </Field>
        <Field label="Số điện thoại">
          <input
            value={v.phone}
            onChange={up("phone")}
            inputMode="tel"
            autoComplete="off"
          />
        </Field>
        <Field label="Email">
          <input
            value={v.email}
            onChange={up("email")}
            type="email"
            autoComplete="off"
          />
        </Field>
        <Field label="Địa chỉ">
          <input
            value={v.address}
            onChange={up("address")}
            autoComplete="off"
          />
        </Field>
        <Field label="Ngày vào làm">
          <input type="date" value={v.start} onChange={up("start")} />
        </Field>
        {xa ? (
          <>
            <Field label="Mã xã (ĐVHC)">
              <input
                value={v.commune}
                onChange={up("commune")}
                inputMode="numeric"
                maxLength={5}
              />
            </Field>
            <Field label="Đơn vị quản lý">
              <select value={v.mgmt_unit} onChange={up("mgmt_unit")}>
                <option value="">— chọn —</option>
                {units
                  .filter((u) => u.active)
                  .map((u) => (
                    <option key={u.code} value={u.code}>
                      {u.name}
                    </option>
                  ))}
              </select>
            </Field>
          </>
        ) : (
          <Field label="Đơn vị công tác">
            <select value={v.unit} onChange={up("unit")}>
              <option value="">— chọn —</option>
              {units
                .filter((u) => u.active)
                .map((u) => (
                  <option key={u.code} value={u.code}>
                    {u.name}
                  </option>
                ))}
            </select>
          </Field>
        )}
        <Field label="Chức vụ (mã danh mục)">
          <input value={v.position} onChange={up("position")} />
        </Field>
        <Field label="Ngày QĐ bổ nhiệm (chức vụ lãnh đạo)">
          <input
            type="date"
            value={v.appointed_on}
            onChange={up("appointed_on")}
          />
        </Field>
        <Field label="Ngạch/CDNN (mã)">
          <input value={v.grade} onChange={up("grade")} placeholder="01.003" />
        </Field>
        <Field label="Bậc">
          <input
            value={v.step}
            onChange={up("step")}
            inputMode="numeric"
            maxLength={2}
          />
        </Field>
        {!xa && canSensitive ? (
          <>
            <Field label="Dân tộc (mã)">
              <input
                value={v.ethnicity}
                onChange={up("ethnicity")}
                placeholder="kinh"
              />
            </Field>
            <Field label="Tôn giáo (mã)">
              <input
                value={v.religion}
                onChange={up("religion")}
                placeholder="khong"
              />
            </Field>
            <Field label="Quê quán">
              <input value={v.hometown} onChange={up("hometown")} />
            </Field>
          </>
        ) : null}
      </div>
    </Dialog>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// Chi tiết (FN-07 B3, B4, B6, B7; FN-08).
// --------------------------------------------------------------------------------------------------------------------

const PAYLOAD_FIELDS: Record<HrmEventKind, [string, string][]> = {
  work_start: [["decision_no", "Số quyết định"]],
  work_end: [
    ["reason", "Lý do nghỉ (mã)"],
    ["decision_no", "Số quyết định"],
  ],
  unit: [
    ["unit", "Đơn vị công tác"],
    ["commune", "Xã (cán bộ xã)"],
    ["mgmt_unit", "Đơn vị quản lý (cán bộ xã)"],
  ],
  position: [
    ["position", "Chức vụ (mã)"],
    ["title", "Tên chức vụ ghi rõ"],
    ["allowance", "Hệ số PCCV"],
    ["appointed_on", "Ngày QĐ bổ nhiệm"],
    ["decision_no", "Số quyết định"],
  ],
  secondment: [
    ["to_org", "Đơn vị được tăng cường"],
    ["decision_no", "Số quyết định"],
  ],
  grade: [
    ["grade", "Ngạch/CDNN (mã)"],
    ["step", "Bậc"],
    ["step_since", "Ngày xếp bậc"],
    ["reserved_coeff", "Hệ số bảo lưu"],
    ["decision_no", "Số quyết định"],
  ],
  step: [
    ["step", "Bậc"],
    ["step_since", "Ngày xếp bậc"],
    ["reserved_coeff", "Hệ số bảo lưu"],
    ["decision_no", "Số quyết định"],
  ],
  degree: [
    ["level", "Trình độ (mã)"],
    ["major", "Chuyên ngành (mã)"],
    ["form", "Hình thức (chinh_quy | tai_chuc)"],
    ["institution", "Cơ sở đào tạo"],
  ],
  training: [
    [
      "type",
      "Loại (political_theory, state_mgmt, foreign_language, it_level, leadership, extension_cdnn)",
    ],
    ["code", "Mã"],
    ["level", "Trình độ ngoại ngữ"],
    ["cert", "Loại văn bằng (degree | certificate)"],
  ],
  award: [
    ["form", "Hình thức (mã)"],
    ["title", "Danh hiệu"],
    ["level", "Cấp quyết định"],
    ["decision_no", "Số quyết định"],
  ],
  discipline: [
    ["form", "Hình thức (mã)"],
    ["decision_no", "Số quyết định"],
  ],
};

function payloadText(e: HrmStaffEvent): string {
  return Object.entries(e.payload)
    .map(
      ([k, v]) =>
        `${PAYLOAD_FIELDS[e.kind as HrmEventKind]?.find((x) => x[0] === k)?.[1] ?? k}: ${v}`,
    )
    .join(" · ");
}

function EventDialog({
  kinds,
  base,
  onClose,
  onSave,
}: {
  kinds: HrmEventKind[];
  base?: HrmStaffEvent;
  onClose: () => void;
  onSave: (body: {
    kind: HrmEventKind;
    valid_from: string;
    valid_to?: string;
    payload: Record<string, string>;
    reason?: string;
  }) => Promise<unknown>;
}) {
  const act = useAction();
  const [kind, setKind] = useState<HrmEventKind>(
    (base?.kind as HrmEventKind) ?? kinds[0]!,
  );
  const [from, setFrom] = useState(base?.valid_from ?? vnToday());
  const [to, setTo] = useState(base?.valid_to ?? "");
  const [payload, setPayload] = useState<Record<string, string>>(
    base?.payload ?? {},
  );
  const [reason, setReason] = useState("");
  return (
    <Dialog
      title={base ? `Sửa dòng ${EVENT_LABEL[kind]}` : "Thêm dòng quá trình"}
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={act.busy}
          onClick={() =>
            void act
              .run(() =>
                onSave({
                  kind,
                  valid_from: from,
                  valid_to: to || undefined,
                  payload: Object.fromEntries(
                    Object.entries(payload).filter(([, v]) => v),
                  ),
                  reason: base ? reason : undefined,
                }),
              )
              .then((r) => r && onClose())
          }
        >
          Lưu
        </button>
      }
    >
      {act.view}
      <div className="grid2">
        <Field label="Loại">
          <select
            value={kind}
            disabled={!!base}
            onChange={(e) => setKind(e.target.value as HrmEventKind)}
          >
            {kinds.map((k) => (
              <option key={k} value={k}>
                {EVENT_LABEL[k]}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Có hiệu lực từ (YYYY-MM-DD, YYYY-MM, YYYY)">
          <input value={from} onChange={(e) => setFrom(e.target.value)} />
        </Field>
        {kind === "secondment" || kind === "discipline" ? (
          <Field label="Đến ngày">
            <input
              type="date"
              value={to}
              onChange={(e) => setTo(e.target.value)}
            />
          </Field>
        ) : null}
        {PAYLOAD_FIELDS[kind].map(([k, l]) => (
          <Field key={k} label={l}>
            <input
              value={payload[k] ?? ""}
              onChange={(e) =>
                setPayload((p) => ({ ...p, [k]: e.target.value }))
              }
            />
          </Field>
        ))}
        {base ? (
          <Field label="Lý do sửa (10–500 ký tự)">
            <textarea
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={2}
            />
          </Field>
        ) : null}
      </div>
    </Dialog>
  );
}

function EventsTab({
  d,
  kinds,
  sa,
  onChanged,
}: {
  d: HrmStaffDetail;
  kinds: HrmEventKind[];
  sa: StaffApi;
  onChanged: () => void;
}) {
  const rows = d.events
    .filter((e) => e.effective && kinds.includes(e.kind as HrmEventKind))
    .sort((a, b) => b.valid_from.localeCompare(a.valid_from));
  const [dlg, setDlg] = useState<
    | { t: "add" }
    | { t: "edit"; e: HrmStaffEvent }
    | { t: "void"; e: HrmStaffEvent }
    | null
  >(null);
  const [reason, setReason] = useState("");
  const act = useAction();
  const edit = d.can["profile.edit"] === true;
  return (
    <section>
      {act.view}
      {edit ? (
        <div className="row" style={{ marginBottom: 8 }}>
          <button
            type="button"
            className="btn btn-o btn-sm"
            onClick={() => setDlg({ t: "add" })}
          >
            Thêm dòng
          </button>
        </div>
      ) : null}
      <div className="tbl-wrap">
        <table className="dt">
          <caption className="sr-only">Quá trình</caption>
          <thead>
            <tr>
              <th scope="col">Loại</th>
              <th scope="col">Từ</th>
              <th scope="col">Đến</th>
              <th scope="col">Nội dung</th>
              {edit ? <th scope="col">Thao tác</th> : null}
            </tr>
          </thead>
          <tbody>
            {rows.length ? (
              rows.map((e) => (
                <tr key={e.id}>
                  <td>{EVENT_LABEL[e.kind as HrmEventKind] ?? e.kind}</td>
                  <td>{vnDate(e.valid_from)}</td>
                  <td>{vnDate(e.valid_to)}</td>
                  <td>{payloadText(e) || "—"}</td>
                  {edit ? (
                    <td>
                      <button
                        type="button"
                        className="btn btn-o btn-sm"
                        onClick={() => setDlg({ t: "edit", e })}
                      >
                        Sửa
                      </button>{" "}
                      <button
                        type="button"
                        className="btn btn-o btn-sm"
                        onClick={() => setDlg({ t: "void", e })}
                      >
                        Hủy
                      </button>
                    </td>
                  ) : null}
                </tr>
              ))
            ) : (
              <tr>
                <td colSpan={edit ? 5 : 4} className="empty">
                  Chưa có dòng nào.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {dlg?.t === "add" ? (
        <EventDialog
          kinds={kinds}
          onClose={() => setDlg(null)}
          onSave={(b) => sa.addEvent(d.id, b).then(onChanged)}
        />
      ) : dlg?.t === "edit" ? (
        <EventDialog
          kinds={kinds}
          base={dlg.e}
          onClose={() => setDlg(null)}
          onSave={(b) => sa.replaceEvent(d.id, dlg.e.id, b).then(onChanged)}
        />
      ) : dlg?.t === "void" ? (
        <Dialog
          title={`Hủy dòng ${EVENT_LABEL[dlg.e.kind as HrmEventKind]}`}
          onClose={() => setDlg(null)}
          actions={
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={act.busy}
              onClick={() =>
                void act
                  .run(() => sa.voidEvent(d.id, dlg.e.id, reason))
                  .then((r) => {
                    if (r) {
                      setDlg(null);
                      setReason("");
                      onChanged();
                    }
                  })
              }
            >
              Hủy dòng
            </button>
          }
        >
          {act.view}
          <Field label="Lý do (10–500 ký tự)">
            <textarea
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={3}
            />
          </Field>
        </Dialog>
      ) : null}
    </section>
  );
}

function ProfileDialog({
  d,
  sa,
  onClose,
  onDone,
}: {
  d: HrmStaffDetail;
  sa: StaffApi;
  onClose: () => void;
  onDone: () => void;
}) {
  const act = useAction();
  const [v, setV] = useState({ ...d.profile });
  const up = (k: keyof typeof v) => (e: { target: { value: string } }) =>
    setV((x) => ({ ...x, [k]: e.target.value }));
  const changed = Object.fromEntries(
    Object.entries(v).filter(
      ([k, x]) =>
        k !== "birth_precision" && x !== d.profile[k as keyof typeof v],
    ),
  );
  return (
    <Dialog
      title="Sửa thông tin chung"
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={act.busy}
          onClick={() =>
            void act
              .run(() => sa.updateProfile(d.id, changed))
              .then((r) => r && onDone())
          }
        >
          Lưu
        </button>
      }
    >
      {act.view}
      <div className="grid2">
        {(
          [
            ["full_name", "Họ và tên"],
            ["phone", "Số điện thoại"],
            ["email", "Email"],
            ["address", "Địa chỉ"],
            ["birth_date", "Ngày sinh"],
            ["note", "Ghi chú"],
          ] as [keyof typeof v, string][]
        ).map(([k, l]) => (
          <Field key={k} label={l}>
            <input value={v[k]} onChange={up(k)} autoComplete="off" />
          </Field>
        ))}
        <Field label="Giới tính">
          <select value={v.gender} onChange={up("gender")}>
            <option value="nu">Nữ</option>
            <option value="nam">Nam</option>
          </select>
        </Field>
      </div>
    </Dialog>
  );
}

function SensitiveTab({ d, sa }: { d: HrmStaffDetail; sa: StaffApi }) {
  const [shown, setShown] = useState(false);
  const [edit, setEdit] = useState<Record<string, string> | null>(null);
  const [nonce, setNonce] = useState(0);
  const s = useLoad(shown ? () => sa.sensitive(d.id) : null, [shown, nonce]);
  const act = useAction();
  if (!shown)
    return (
      <div className="alert alert-info" role="note">
        <span>
          Thẻ chứa dữ liệu cá nhân nhạy cảm (dân tộc, tôn giáo, Đảng, quê
          quán…). Mỗi lần xem được ghi nhật ký.
        </span>{" "}
        <button
          type="button"
          className="btn btn-o btn-sm"
          onClick={() => setShown(true)}
        >
          Xem thẻ
        </button>
      </div>
    );
  return (
    <LoadState s={s}>
      {(x) => (
        <>
          {act.view}
          <LabelValueBlock
            items={Object.entries(SENSITIVE_LABEL).map(([k, label]) => ({
              label,
              value: k.endsWith("_date")
                ? vnDate(x.fields[k as keyof typeof x.fields])
                : x.fields[k as keyof typeof x.fields],
            }))}
          />
          {d.can["profile.edit"] ? (
            <button
              type="button"
              className="btn btn-o btn-sm"
              onClick={() =>
                setEdit({ ...(x.fields as Record<string, string>) })
              }
            >
              Sửa thẻ
            </button>
          ) : null}
          {edit ? (
            <Dialog
              title="Sửa thẻ Thống kê BC Sở"
              onClose={() => setEdit(null)}
              actions={
                <button
                  type="button"
                  className="btn btn-p btn-sm"
                  disabled={act.busy}
                  onClick={() =>
                    void act
                      .run(() =>
                        sa.updateSensitive(
                          d.id,
                          Object.fromEntries(
                            Object.entries(edit).filter(
                              ([k]) => k in SENSITIVE_LABEL,
                            ),
                          ),
                        ),
                      )
                      .then((r) => {
                        if (r) {
                          setEdit(null);
                          setNonce((n) => n + 1);
                        }
                      })
                  }
                >
                  Lưu
                </button>
              }
            >
              <div className="grid2">
                {Object.entries(SENSITIVE_LABEL).map(([k, l]) => (
                  <Field key={k} label={l}>
                    <input
                      value={edit[k] ?? ""}
                      onChange={(e) =>
                        setEdit((p) => ({ ...p!, [k]: e.target.value }))
                      }
                    />
                  </Field>
                ))}
              </div>
            </Dialog>
          ) : null}
        </>
      )}
    </LoadState>
  );
}

function AccessLogTab({ d, sa }: { d: HrmStaffDetail; sa: StaffApi }) {
  const s = useLoad(() => sa.accessLog(d.id), [d.id]);
  const label: Record<string, string> = {
    view: "Xem chi tiết",
    view_sensitive: "Xem thẻ Thống kê BC Sở",
    export: "Xuất danh sách",
  };
  return (
    <LoadState s={s}>
      {(x) => (
        <ol className="log-list">
          {x.items.length ? (
            x.items.map((a) => (
              <li key={a.id}>
                <b>{label[a.action] ?? a.action}</b> ·{" "}
                {new Date(a.recorded_at).toLocaleString("vi-VN")} · người xem{" "}
                {a.actor_ref.slice(0, 8)}… · phạm vi{" "}
                {SCOPE_LABEL[a.scope as HrmScope]}
                {a.action === "export" ? ` · ${a.item_count} hồ sơ` : ""}
              </li>
            ))
          ) : (
            <li className="mute">Chưa có lượt xem/xuất.</li>
          )}
        </ol>
      )}
    </LoadState>
  );
}

export function HrmStaffDetailScreen() {
  const { id } = useParams<{ id: string }>();
  const sa = useStaffApi();
  const [at, setAt] = useState("");
  const [nonce, setNonce] = useState(0);
  const [tab, setTab] = useState("chung");
  const [editing, setEditing] = useState(false);
  const s = useLoad(() => sa.get(id, at || undefined), [id, at, nonce]);
  useBreadcrumbTail(s.data ? [{ label: s.data.code }] : []);
  const reload = () => setNonce((n) => n + 1);
  return (
    <>
      <BackLink href={DETAIL}>Hồ sơ nhân sự</BackLink>
      <LoadState s={s}>
        {(d) => {
          const tabs = [
            { id: "chung", label: "Thông tin chung" },
            { id: "cong_tac", label: "Công tác" },
            { id: "trinh_do", label: "Trình độ - Bồi dưỡng" },
            { id: "khen_thuong", label: "Khen thưởng" },
            { id: "ngach_bac", label: "Ngạch - Bậc" },
            { id: "lam_viec", label: "Thông tin làm việc" },
            ...(d.can["sensitive.view"]
              ? [{ id: "tk", label: "Thống kê BC Sở" }]
              : []),
            { id: "lich_su", label: "Lịch sử sửa" },
            ...(d.can["audit.view"]
              ? [{ id: "nhat_ky", label: "Nhật ký xem/xuất" }]
              : []),
          ];
          const c = d.current;
          return (
            <>
              <PageHeader
                title={`${d.profile.full_name || "(đã xóa định danh)"} · ${d.code}`}
                badge={<StatusTag s={c.status} />}
              >
                {STAFF_TYPE_LABEL[d.type]} · số liệu tại ngày {vnDate(d.day)}
                {d.age !== null && d.age !== undefined
                  ? ` · ${d.age} tuổi`
                  : ""}
              </PageHeader>
              <div className="row" style={{ marginBottom: 8 }}>
                <label className="small">
                  Ngày chốt{" "}
                  <input
                    type="date"
                    aria-label="Ngày chốt hồ sơ"
                    value={at}
                    onChange={(e) => setAt(e.target.value)}
                  />
                </label>
              </div>
              <Tabs tabs={tabs} value={tab} onChange={setTab} label="Thẻ hồ sơ">
                {tab === "chung" ? (
                  <>
                    <LabelValueBlock
                      items={[
                        { label: "Mã hồ sơ", value: d.code },
                        { label: "Họ và tên", value: d.profile.full_name },
                        {
                          label: "Giới tính",
                          value:
                            d.profile.gender === "nam"
                              ? "Nam"
                              : d.profile.gender === "nu"
                                ? "Nữ"
                                : "",
                        },
                        {
                          label: "Ngày sinh",
                          value: vnDate(d.profile.birth_date),
                        },
                        { label: "Số điện thoại", value: d.profile.phone },
                        { label: "Email", value: d.profile.email },
                        { label: "Địa chỉ", value: d.profile.address },
                        {
                          label: "Đơn vị",
                          value:
                            c.unit ||
                            (c.mgmt_unit
                              ? `Xã ${c.commune} · đơn vị quản lý ${c.mgmt_unit}`
                              : ""),
                        },
                        {
                          label: "Chức vụ",
                          value: [c.position, c.title]
                            .filter(Boolean)
                            .join(" — "),
                        },
                        { label: "Ghi chú", value: d.profile.note },
                      ]}
                    />
                    {d.can["profile.edit"] ? (
                      <button
                        type="button"
                        className="btn btn-o btn-sm"
                        onClick={() => setEditing(true)}
                      >
                        Sửa
                      </button>
                    ) : null}
                  </>
                ) : tab === "ngach_bac" ? (
                  <>
                    <LabelValueBlock
                      items={[
                        { label: "Ngạch/CDNN", value: c.grade },
                        {
                          label: "Bậc",
                          value: c.step
                            ? `${c.step}${c.steps ? `/${c.steps}` : ""}`
                            : "",
                        },
                        { label: "Hệ số bậc", value: vnDecimal(c.coefficient) },
                        { label: "Ngày xếp bậc", value: vnDate(c.step_since) },
                        {
                          label: "Hệ số bảo lưu",
                          value: c.reserved_coeff
                            ? vnDecimal(c.reserved_coeff)
                            : "",
                        },
                      ]}
                    />
                    <EventsTab
                      d={d}
                      kinds={TAB_EVENTS.ngach_bac!}
                      sa={sa}
                      onChanged={reload}
                    />
                  </>
                ) : tab === "lam_viec" ? (
                  <>
                    <LabelValueBlock
                      items={[
                        {
                          label: "Trạng thái",
                          value: <StatusTag s={c.status} />,
                        },
                        { label: "Ngày vào làm", value: vnDate(c.started_on) },
                        { label: "Ngày nghỉ việc", value: vnDate(c.ended_on) },
                        { label: "Lý do nghỉ", value: c.leave_reason },
                      ]}
                    />
                    <EventsTab
                      d={d}
                      kinds={TAB_EVENTS.lam_viec!}
                      sa={sa}
                      onChanged={reload}
                    />
                  </>
                ) : tab === "tk" ? (
                  <SensitiveTab d={d} sa={sa} />
                ) : tab === "lich_su" ? (
                  <ol className="log-list">
                    {d.history.map((h) => (
                      <li key={`h${h.version}`}>
                        {new Date(h.recorded_at).toLocaleString("vi-VN")} ·{" "}
                        {h.change === "created"
                          ? "Tạo hồ sơ"
                          : `Sửa: ${h.changed_fields.join(", ")}`}
                      </li>
                    ))}
                    {d.events.map((e) => (
                      <li
                        key={e.id}
                        className={e.effective ? undefined : "mute"}
                      >
                        {new Date(e.recorded_at).toLocaleString("vi-VN")} ·{" "}
                        {e.void ? "Hủy" : e.supersedes ? "Sửa" : "Thêm"}{" "}
                        {EVENT_LABEL[e.kind as HrmEventKind]} từ{" "}
                        {vnDate(e.valid_from)}
                        {e.reason ? ` — lý do: ${e.reason}` : ""}
                        {!e.effective && !e.void ? " (đã bị thay)" : ""}
                      </li>
                    ))}
                  </ol>
                ) : tab === "nhat_ky" ? (
                  <AccessLogTab d={d} sa={sa} />
                ) : (
                  <EventsTab
                    d={d}
                    kinds={TAB_EVENTS[tab] ?? []}
                    sa={sa}
                    onChanged={reload}
                  />
                )}
              </Tabs>
              {editing ? (
                <ProfileDialog
                  d={d}
                  sa={sa}
                  onClose={() => setEditing(false)}
                  onDone={() => {
                    setEditing(false);
                    reload();
                  }}
                />
              ) : null}
            </>
          );
        }}
      </LoadState>
    </>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// Phân quyền hồ sơ (FN-20) — Admin.
// --------------------------------------------------------------------------------------------------------------------

export function HrmPermissionScreen() {
  const sa = useStaffApi();
  const [nonce, setNonce] = useState(0);
  const s = useLoad(() => sa.permissions(), [nonce]);
  const hist = useLoad(() => sa.permissionHistory(), [nonce]);
  const act = useAction();
  const [draft, setDraft] = useState<Record<string, HrmPermMatrix>>({});
  const [user, setUser] = useState({ grantee: "", note: "" });
  const save = (
    kind: "role" | "user",
    grantee: string,
    perms: HrmPermMatrix,
    note?: string,
  ) =>
    void act
      .run(
        () => sa.setPermission({ grantee_kind: kind, grantee, perms, note }),
        "Đã lưu phân quyền (ghi nhật ký đổi quyền).",
      )
      .then((r) => r && setNonce((n) => n + 1));
  return (
    <>
      <PageHeader title="Phân quyền hồ sơ">
        Ma trận nhóm quyền × phạm vi theo nhóm vai và theo người dùng (quyền
        theo người dùng ưu tiên). Quyền của Admin cố định; thẻ Thống kê BC Sở
        chỉ cấp cho nhóm Cấp Tỉnh – HCTH hoặc từng người.
      </PageHeader>
      {act.view}
      <LoadState s={s}>
        {(m) => (
          <>
            <div className="card tbl-wrap">
              <table className="dt">
                <caption className="sr-only">Ma trận phân quyền hồ sơ</caption>
                <thead>
                  <tr>
                    <th scope="col">Nhóm / người dùng</th>
                    {m.perms.map((p) => (
                      <th key={p} scope="col">
                        {PERM_LABEL[p] ?? p}
                      </th>
                    ))}
                    <th scope="col">Lưu</th>
                  </tr>
                </thead>
                <tbody>
                  {m.items.map((it: HrmPermEntry) => {
                    const key = `${it.grantee_kind}:${it.grantee}`;
                    const cur = draft[key] ?? it.perms;
                    return (
                      <tr key={key}>
                        <th scope="row">
                          {it.grantee_kind === "role"
                            ? (GROUP_LABEL[it.grantee] ?? it.grantee)
                            : `Người dùng ${it.grantee.slice(0, 8)}…`}
                          {it.version ? (
                            <span className="mute small"> · v{it.version}</span>
                          ) : null}
                        </th>
                        {m.perms.map((p) => (
                          <td key={p}>
                            <select
                              aria-label={`${it.grantee} — ${PERM_LABEL[p] ?? p}`}
                              disabled={it.locked}
                              value={(cur as Record<string, string>)[p] ?? ""}
                              onChange={(e) =>
                                setDraft((d) => ({
                                  ...d,
                                  [key]: {
                                    ...cur,
                                    [p]: e.target.value,
                                  } as HrmPermMatrix,
                                }))
                              }
                            >
                              {it.grantee_kind === "user" ? (
                                <option value="">(theo nhóm)</option>
                              ) : null}
                              {m.scopes.map((sc) => (
                                <option key={sc} value={sc}>
                                  {SCOPE_LABEL[sc]}
                                </option>
                              ))}
                            </select>
                          </td>
                        ))}
                        <td>
                          {it.locked ? (
                            <span className="mute small">Cố định</span>
                          ) : (
                            <button
                              type="button"
                              className="btn btn-o btn-sm"
                              disabled={act.busy || !draft[key]}
                              onClick={() =>
                                save(
                                  it.grantee_kind,
                                  it.grantee,
                                  Object.fromEntries(
                                    Object.entries(cur).filter(([, v]) => v),
                                  ) as HrmPermMatrix,
                                )
                              }
                            >
                              Lưu
                            </button>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            <section className="card" style={{ padding: 12, marginTop: 12 }}>
              <h2>Thêm quyền theo người dùng</h2>
              <div className="grid2">
                <Field label="Mã người dùng (UUID)">
                  <input
                    value={user.grantee}
                    onChange={(e) =>
                      setUser((u) => ({
                        ...u,
                        grantee: e.target.value.trim().toLowerCase(),
                      }))
                    }
                  />
                </Field>
                <Field label="Ghi chú">
                  <input
                    value={user.note}
                    onChange={(e) =>
                      setUser((u) => ({ ...u, note: e.target.value }))
                    }
                  />
                </Field>
              </div>
              <button
                type="button"
                className="btn btn-o btn-sm"
                disabled={act.busy || !user.grantee}
                onClick={() =>
                  save(
                    "user",
                    user.grantee,
                    { "profile.view": "self" },
                    user.note,
                  )
                }
              >
                Thêm (xem hồ sơ của bản thân)
              </button>
            </section>
          </>
        )}
      </LoadState>
      <LoadState s={hist}>
        {(h) => (
          <section
            className="card"
            style={{ padding: 12, marginTop: 12 }}
            aria-label="Nhật ký đổi quyền"
          >
            <h2>Nhật ký đổi quyền</h2>
            <ol className="log-list">
              {h.items.length ? (
                h.items.map((r) => (
                  <li key={`${r.grantee_kind}:${r.grantee}:${r.version}`}>
                    {r.recorded_at
                      ? new Date(r.recorded_at).toLocaleString("vi-VN")
                      : ""}{" "}
                    ·{" "}
                    {r.grantee_kind === "role"
                      ? (GROUP_LABEL[r.grantee] ?? r.grantee)
                      : r.grantee}{" "}
                    · v{r.version} ·{" "}
                    {Object.entries(r.perms)
                      .map(
                        ([p, v]) =>
                          `${PERM_LABEL[p] ?? p}: ${SCOPE_LABEL[v as HrmScope] ?? v}`,
                      )
                      .join(", ")}
                    {r.note ? ` — ${r.note}` : ""}
                  </li>
                ))
              ) : (
                <li className="mute">
                  Chưa đổi quyền — đang dùng ma trận mặc định.
                </li>
              )}
            </ol>
          </section>
        )}
      </LoadState>
    </>
  );
}
