"use client";

import Link from "next/link";
import { useParams, usePathname, useRouter } from "next/navigation";
import { useState } from "react";
import { fieldErrorMap } from "@/api/problem";
import { NumberField, SelectField, TextField } from "@/components/form";
import { BackLink, DetailPage, LabelValueBlock, LogPanel, type LogEntry } from "@/components/patterns/detail";
import { DataTable, StatusPill } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { EQUIV_GROUPS, EQUIV_LABEL, GRADE_TYPE_LABEL, KIND_LABEL, type HrmAttrs, type HrmItem, type HrmKind } from "./api";
import { attrsText, HrmCatalogScreen, useHrmApi } from "./CatalogScreen";
import { catalogBase, catalogItemPath, isHrmKind, validateCatalogItem, type CatalogValues } from "./catalog-form";

const MGMT_LEVELS = [
  ["chuyen_vien", "Chuyên viên - Cán bộ"], ["phu_trach", "Phụ trách"],
  ["pho_truong_phong", "Phó trưởng phòng"], ["truong_phong", "Trưởng phòng"],
  ["lanh_dao_don_vi", "Lãnh đạo đơn vị"], ["pho_giam_doc", "Phó Giám đốc"], ["giam_doc", "Giám đốc"],
] as const;
const MGMT_OPTIONS = MGMT_LEVELS.map(([value, label]) => ({ value, label }));

function namesFor(levels: HrmItem[], groups: HrmItem[]) {
  return new Map([...levels, ...groups].map((item) => [`${item.kind}:${item.code}`, item.name]));
}

/** API chỉ trả thời điểm và phiên bản, không trả tên người thực hiện; không suy đoán từ vai đăng nhập. */
export function catalogHistory(kind: HrmKind, versions: HrmItem[], names: Map<string, string>): LogEntry[] {
  return [...versions].sort((a, b) => b.version - a.version).map((item) => ({
    event: `Phiên bản ${item.version}${item.placeholder ? " · Mẫu" : ""}`,
    actor_display: "API chưa cung cấp người thực hiện",
    at: item.recorded_at,
    changes: [
      { field: "Tên", old: null, new: item.name },
      { field: "Thứ tự", old: null, new: String(item.sort_order) },
      { field: "Thuộc tính", old: null, new: attrsText(kind, item.attrs, names) || "—" },
      { field: "Trạng thái", old: null, new: item.active ? "Đang dùng" : "Ngừng sử dụng" },
    ],
  }));
}

function Missing({ base }: { base: string }) {
  return <><BackLink href={base}>Danh sách</BackLink><p role="alert">Không tìm thấy mục danh mục.</p></>;
}

export function HrmCatalogKindScreen() {
  const { kind } = useParams<{ kind: string }>();
  const base = catalogBase(usePathname());
  return isHrmKind(kind) ? <HrmCatalogScreen key={kind} initialKind={kind} /> : <Missing base={base} />;
}

export function HrmCatalogItemScreen() {
  const { kind, code } = useParams<{ kind: string; code: string }>();
  const base = catalogBase(usePathname());
  return isHrmKind(kind) ? <CatalogItem key={`${kind}/${code}`} kind={kind} code={code} base={base} /> : <Missing base={base} />;
}

function CatalogItem({ kind, code, base }: { kind: HrmKind; code: string; base: string }) {
  const ha = useHrmApi();
  const status = useLoad(() => ha.status(), []);
  const history = useLoad(() => ha.history(kind, code), [kind, code]);
  const refs = useLoad(async () => {
    if (kind !== "major" && kind !== "major_group") return new Map<string, string>();
    const levels = await ha.list("education_level", true);
    const groups = kind === "major" ? await ha.list("major_group", true) : { items: [] };
    return namesFor(levels.items, groups.items);
  }, [kind]);
  return <LoadState s={status}>{(st) => <LoadState s={history}>{({ versions }) => {
    const item = [...versions].sort((a, b) => b.version - a.version)[0];
    return !item ? <Missing base={`${base}/${kind}`} /> : <LoadState s={refs}>{(names) => <DetailPage
      back={{ href: `${base}/${kind}`, label: "Danh sách" }}
      title={`${item.name} · ${item.code}`}
      badge={<StatusPill tone={item.active ? "ok" : "neutral"}>{item.active ? "Đang dùng" : "Ngừng sử dụng"}</StatusPill>}
      actions={st.can_manage ? <Link className="btn btn-p btn-sm" href={`${catalogItemPath(base, kind, code)}/sua`}>Sửa</Link> : undefined}
      log={<LogPanel entries={catalogHistory(kind, versions, names)} />}
    >
      <LabelValueBlock title={KIND_LABEL[kind]} items={[
        { label: "Mã", value: item.code }, { label: "Tên", value: item.name },
        { label: "Thứ tự", value: item.sort_order }, { label: "Phiên bản", value: item.version },
        { label: "Thuộc tính", value: attrsText(kind, item.attrs, names) },
        { label: "Dữ liệu mẫu", value: item.placeholder ? "Mẫu — chờ Phòng HCTH xác nhận (Q-070)" : "Không" },
      ]} />
      <DataTable caption="Lịch sử phiên bản" rows={[...versions].sort((a, b) => b.version - a.version)} rowKey={(row) => String(row.version)} columns={[
        { key: "version", header: "Phiên bản", cell: (row) => row.version },
        { key: "name", header: "Tên", cell: (row) => row.name },
        { key: "attrs", header: "Thuộc tính", cell: (row) => attrsText(kind, row.attrs, names) || "—" },
        { key: "active", header: "Trạng thái", cell: (row) => row.active ? "Đang dùng" : "Ngừng sử dụng" },
      ]} />
    </DetailPage>}</LoadState>;
  }}</LoadState>}</LoadState>;
}

export function HrmCatalogItemFormScreen({ edit = false }: { edit?: boolean }) {
  const { kind, code } = useParams<{ kind: string; code?: string }>();
  const base = catalogBase(usePathname());
  return isHrmKind(kind) ? <CatalogFormAccess key={`${kind}/${code ?? "new"}/${edit}`} kind={kind} code={code} edit={edit} base={base} /> : <Missing base={base} />;
}

function CatalogFormAccess({ kind, code, edit, base }: { kind: HrmKind; code?: string; edit: boolean; base: string }) {
  const ha = useHrmApi();
  const status = useLoad(() => ha.status(), []);
  const allowed = status.data?.can_manage === true;
  // Quyền chưa rõ, chỉ xem hoặc status lỗi: không tải dữ liệu cho form ghi.
  const data = useLoad(allowed ? async () => {
    const list = await ha.list(kind, true);
    const history = edit && code ? await ha.history(kind, code) : { versions: [] };
    const levels = kind === "major" || kind === "major_group" ? await ha.list("education_level", true) : { items: [] };
    const groups = kind === "major" ? await ha.list("major_group", true) : { items: [] };
    return { list: list.items, versions: history.versions, levels: levels.items, groups: groups.items };
  } : null, [allowed, kind, code, edit]);
  const router = useRouter();
  return <LoadState s={status}>{() => !allowed ? <>
    <BackLink href={`${base}/${kind}`}>Danh sách</BackLink><p role="alert">Bạn không có quyền cập nhật danh mục nhân sự.</p>
  </> : <LoadState s={data}>{(d) => {
    const item = edit ? [...d.versions].sort((a, b) => b.version - a.version)[0] : null;
    if (edit && !item) return <Missing base={`${base}/${kind}`} />;
    return <ItemEditor kind={kind} item={item ?? null} levels={d.levels} groups={d.groups}
      versions={d.versions} nextOrder={Math.min(100000, d.list.reduce((max, x) => Math.max(max, x.sort_order), 0) + 10)}
      base={base} onSaved={(savedCode) => router.push(catalogItemPath(base, kind, savedCode))} />;
  }}</LoadState>}</LoadState>;
}

function ItemEditor({ kind, item, levels, groups, versions, nextOrder, base, onSaved }: {
  kind: HrmKind; item: HrmItem | null; levels: HrmItem[]; groups: HrmItem[]; versions: HrmItem[];
  nextOrder: number; base: string; onSaved: (code: string) => void;
}) {
  const ha = useHrmApi();
  const [v, setV] = useState<CatalogValues>({ code: item?.code ?? "", name: item?.name ?? "", order: String(item?.sort_order ?? nextOrder), allowance: item?.attrs.allowance ?? "", rank: String(item?.attrs.rank ?? "") });
  const [attrs, setAttrs] = useState<HrmAttrs>(item?.attrs ?? (kind === "grade" ? { equiv_group: "CV", grade_type: "vien_chuc" } : {}));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const act = useAction();
  const set = (patch: Partial<HrmAttrs>) => setAttrs((current) => ({ ...current, ...patch }));
  const val = (key: keyof CatalogValues, value: string) => setV((current) => ({ ...current, [key]: value }));
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  const err = (key: string) => errors[key] || server[key === "order" ? "sort_order" : key] || server[`attrs.${key}`];
  const names = namesFor(levels, groups);
  const save = async () => {
    const result = validateCatalogItem(kind, v, attrs);
    setErrors(result.errors);
    if (!result.valid) return;
    const { code, ...body } = result.body;
    const saved = await act.run(() => item ? ha.revise(kind, item.code, { ...body, active: item.active }) : ha.create(kind, { ...body, code, active: true }));
    if (saved) onSaved(saved.code);
  };
  return <DetailPage back={{ href: `${base}/${kind}`, label: "Danh sách" }}
    title={`${item ? "Sửa" : "Thêm"} ${KIND_LABEL[kind].toLowerCase()}${item ? ` · ${item.code}` : ""}`}
    log={<LogPanel entries={catalogHistory(kind, versions, names)} empty="Chưa có phiên bản. Lịch sử xuất hiện sau khi lưu." />}
    actions={<>
      <Link href={`${base}/${kind}`} className="btn btn-o btn-sm">Hủy</Link>
      <button type="submit" form="hrm-catalog-item" className="btn btn-p btn-sm" disabled={act.busy}>Lưu</button>
    </>}
  >
    {act.view}
    <form id="hrm-catalog-item" noValidate onSubmit={(e) => { e.preventDefault(); if (!act.busy) void save(); }}>
      <div className="grid2">
        <TextField label="Mã" required value={v.code} onChange={(value) => val("code", value)} disabled={!!item} error={err("code")}
          hint={kind === "grade" ? "Tối đa 40 ký tự. Khoảng trắng, xuống dòng được tự loại bỏ." : "Tối đa 40 ký tự; mã được viết thường và bỏ khoảng trắng."} />
        <TextField label="Tên" required maxLength={200} value={v.name} onChange={(value) => val("name", value)} error={err("name")} />
        <NumberField label="Thứ tự" required value={v.order} onChange={(value) => val("order", value)} rule={{ integer: true, max: 100000, maxInputLength: 32 }} onBlurError={(error) => setErrors((old) => ({ ...old, order: error ?? "" }))} error={err("order")} hint="Số nguyên từ 0 đến 100000." />
        {kind === "position" ? <>
          <label className="row"><input type="checkbox" checked={!!attrs.leader} onChange={(e) => { set({ leader: e.target.checked }); if (!e.target.checked) val("allowance", ""); }} />Chức vụ lãnh đạo, quản lý</label>
          <NumberField label="Hệ số phụ cấp chức vụ" value={v.allowance} disabled={!attrs.leader} onChange={(value) => val("allowance", value)} rule={{ positive: true, max: "10", maxDecimals: 2, maxInputLength: 32 }} onBlurError={(error) => setErrors((old) => ({ ...old, allowance: error ?? "" }))} error={err("allowance")} hint="Để trống hoặc nhập lớn hơn 0, không quá 10; tối đa 2 chữ số thập phân." />
          <SelectField label="Cấp quản lý của tài khoản" value={attrs.mgmt_level ?? ""} onChange={(mgmt_level) => set({ mgmt_level })} options={MGMT_OPTIONS} error={err("mgmt_level")} />
        </> : null}
        {kind === "grade" ? <>
          <SelectField label="Nhóm tương đương (NS06)" required value={attrs.equiv_group ?? ""} onChange={(equiv_group) => set({ equiv_group: equiv_group as HrmAttrs["equiv_group"] })} options={EQUIV_GROUPS.map((value) => ({ value, label: EQUIV_LABEL[value] ?? value }))} error={err("equiv_group")} />
          <SelectField label="Loại" required value={attrs.grade_type ?? ""} onChange={(grade_type) => set({ grade_type: grade_type as HrmAttrs["grade_type"] })} options={Object.entries(GRADE_TYPE_LABEL).map(([value, label]) => ({ value, label }))} error={err("grade_type")} />
          <TextField label="Loại bảng lương" maxLength={20} placeholder="vd A1, A2.1, B" value={attrs.salary_class ?? ""} onChange={(salary_class) => set({ salary_class })} error={err("salary_class")} />
        </> : null}
        {kind === "education_level" ? <NumberField label="Thứ hạng (cao hơn = lớn hơn)" required value={v.rank} onChange={(value) => val("rank", value)} rule={{ integer: true, positive: true, max: 100, maxInputLength: 32 }} onBlurError={(error) => setErrors((old) => ({ ...old, rank: error ?? "" }))} error={err("rank")} hint="Số nguyên từ 1 đến 100." /> : null}
        {kind === "major_group" ? <SelectField label="Trình độ" required value={attrs.level ?? ""} onChange={(level) => set({ level })} options={levels.filter((level) => level.active || level.code === attrs.level).map((level) => ({ value: level.code, label: level.name }))} error={err("level")} /> : null}
        {kind === "ethnicity" ? <label className="row"><input type="checkbox" checked={!!attrs.minority} onChange={(e) => set({ minority: e.target.checked })} />Dân tộc thiểu số</label> : null}
        {kind === "religion" ? <label className="row"><input type="checkbox" checked={!!attrs.no_religion} onChange={(e) => set({ no_religion: e.target.checked })} />Mục “Không” (không tôn giáo)</label> : null}
      </div>
      {kind === "major" ? <fieldset className="card" style={{ marginTop: 12, padding: 12 }}>
        <legend>Nhóm chuyên ngành theo trình độ (NS05)</legend>
        <div className="grid2">{levels.filter((level) => level.active || attrs.groups?.[level.code]).map((level) => {
          const options = groups.filter((group) => group.attrs.level === level.code && (group.active || group.code === attrs.groups?.[level.code]));
          return <SelectField key={level.code} label={level.name} value={attrs.groups?.[level.code] ?? ""} disabled={!options.length}
            onChange={(value) => set({ groups: { ...(attrs.groups ?? {}), [level.code]: value } })}
            options={options.map((group) => ({ value: group.code, label: group.name }))} error={err(`groups.${level.code}`)} />;
        })}</div>
      </fieldset> : null}
    </form>
  </DetailPage>;
}
