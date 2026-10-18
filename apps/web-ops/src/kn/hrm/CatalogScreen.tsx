"use client";

// FN-19 — Danh mục nhân sự (BRD HCTH v3.0, UR-ADM-08; W11.21, dom/hrm). Bên trái: 4 danh mục Admin cấu hình (Chức
// vụ, Ngạch/CDNN, Trình độ chuyên môn, Chuyên ngành), bảng hệ số theo bậc, tham số nâng bậc/quy đổi thành tích và các
// danh sách khai báo sẵn; bên phải: lưới mục của danh mục đang chọn, bảng hệ số của ngạch đang chọn (Hình 39, 40).
// Quyền lấy từ máy chủ (can_manage): Admin thêm/sửa/ngừng; Cấp Tỉnh – HCTH chỉ xem. Không xóa (BR-01) — chỉ ngừng sử
// dụng. Mục "Mẫu" là dữ liệu cài sẵn chưa được Phòng HCTH xác nhận (Q-070).
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import {
  DataTable,
  PageHeader,
  SearchBox,
  StatusPill,
  type Column,
} from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import {
  createHrmApi,
  dmy,
  EQUIV_GROUPS,
  EQUIV_LABEL,
  FIXED_KINDS,
  GRADE_TYPE_LABEL,
  KIND_LABEL,
  MAIN_KINDS,
  normDecimal,
  normGradeCode,
  RULE_LABEL,
  vnDecimal,
  type HrmAchievementConversion,
  type HrmApi,
  type HrmAttrs,
  type HrmItem,
  type HrmKind,
  type HrmPromotionRules,
  type HrmRuleKey,
  type HrmScale,
} from "./api";

type View =
  | { t: "kind"; kind: HrmKind }
  | { t: "scales" }
  | { t: "rule"; key: HrmRuleKey };
const PAGE = 20;

/** Cấp quản lý của tài khoản (pf/accounts MgmtLevels, W11.07) — gắn tùy chọn với chức vụ. */
const MGMT_LEVELS: [string, string][] = [
  ["chuyen_vien", "Chuyên viên - Cán bộ"],
  ["phu_trach", "Phụ trách"],
  ["pho_truong_phong", "Phó trưởng phòng"],
  ["truong_phong", "Trưởng phòng"],
  ["lanh_dao_don_vi", "Lãnh đạo đơn vị"],
  ["pho_giam_doc", "Phó Giám đốc"],
  ["giam_doc", "Giám đốc"],
];

function vnToday(): string {
  return new Date(Date.now() + 7 * 3_600_000).toISOString().slice(0, 10);
}

export function useHrmApi(): HrmApi {
  const { api } = useAuth();
  return useMemo(() => createHrmApi(api), [api]);
}

function PlaceholderPill({ on }: { on: boolean }) {
  return on ? (
    <StatusPill tone="warn">
      <span title="Dữ liệu mẫu cài khi triển khai — chờ Phòng HCTH xác nhận (Q-070)">
        Mẫu
      </span>
    </StatusPill>
  ) : null;
}

export function HrmCatalogScreen() {
  const ha = useHrmApi();
  const [view, setView] = useState<View>({ t: "kind", kind: "grade" });
  const [nonce, setNonce] = useState(0);
  const bump = () => setNonce((n) => n + 1);
  const status = useLoad(() => ha.status(), [nonce]);
  const act = useAction();
  const navBtn = (
    label: string,
    active: boolean,
    onClick: () => void,
    count?: number,
  ) => (
    <button
      key={label}
      type="button"
      className={active ? "btn btn-p btn-sm" : "btn btn-o btn-sm"}
      style={{ justifyContent: "space-between", width: "100%" }}
      aria-current={active ? "true" : undefined}
      onClick={onClick}
    >
      <span>{label}</span>
      {count !== undefined ? <span className="mute small">{count}</span> : null}
    </button>
  );
  return (
    <>
      <PageHeader title="Danh mục nhân sự">
        Chức vụ, ngạch/chức danh nghề nghiệp (bảng hệ số theo bậc có ngày hiệu
        lực), trình độ chuyên môn, chuyên ngành và các danh sách dùng chung của
        hồ sơ nhân sự. Danh mục đang dùng không xóa, chỉ ngừng sử dụng.
      </PageHeader>
      <LoadState s={status}>
        {(st) => (
          <>
            {act.view}
            {st.placeholders > 0 ? (
              <div className="alert alert-info" role="note">
                <span>
                  {st.placeholders} mục, bảng hệ số hoặc tham số là{" "}
                  <b>dữ liệu mẫu</b> (nhãn “Mẫu”) — chờ Phòng Hành chính – Tổng
                  hợp gửi số liệu thật (Q-070). Sửa một mục sẽ bỏ nhãn mẫu.
                </span>
              </div>
            ) : null}
            {st.can_manage && Object.values(st.counts).every((n) => n === 0) ? (
              <div className="alert alert-info" role="status">
                <span>Danh mục còn trống.</span>{" "}
                <button
                  type="button"
                  className="btn btn-p btn-sm"
                  disabled={act.busy}
                  onClick={() =>
                    void act
                      .run(
                        () => ha.installDefaults(),
                        (r) =>
                          `Đã cài ${r.items} mục, ${r.scales} bảng hệ số, ${r.rules} tham số mặc định.`,
                      )
                      .then(bump)
                  }
                >
                  Cài dữ liệu mặc định
                </button>
              </div>
            ) : null}
            <div
              style={{
                display: "grid",
                gridTemplateColumns: "minmax(200px, 240px) minmax(0, 1fr)",
                gap: 16,
                alignItems: "start",
              }}
            >
              <nav
                aria-label="Danh mục nhân sự"
                className="card"
                style={{
                  display: "flex",
                  flexDirection: "column",
                  gap: 6,
                  padding: 12,
                }}
              >
                <div className="mute small">Master data</div>
                {MAIN_KINDS.map((k) =>
                  navBtn(
                    KIND_LABEL[k],
                    view.t === "kind" && view.kind === k,
                    () => setView({ t: "kind", kind: k }),
                    st.counts[k] ?? 0,
                  ),
                )}
                <div className="mute small" style={{ marginTop: 8 }}>
                  Ngạch, bậc
                </div>
                {navBtn(
                  "Bảng hệ số theo bậc",
                  view.t === "scales",
                  () => setView({ t: "scales" }),
                  st.scales,
                )}
                {(["promotion", "achievement_conversion"] as HrmRuleKey[]).map(
                  (k) =>
                    navBtn(
                      RULE_LABEL[k],
                      view.t === "rule" && view.key === k,
                      () => setView({ t: "rule", key: k }),
                    ),
                )}
                <div className="mute small" style={{ marginTop: 8 }}>
                  Danh sách khai báo sẵn
                </div>
                {FIXED_KINDS.map((k) =>
                  navBtn(
                    KIND_LABEL[k],
                    view.t === "kind" && view.kind === k,
                    () => setView({ t: "kind", kind: k }),
                    st.counts[k] ?? 0,
                  ),
                )}
              </nav>
              <section aria-live="polite">
                {view.t === "kind" ? (
                  <KindPanel
                    key={view.kind}
                    ha={ha}
                    kind={view.kind}
                    canManage={st.can_manage}
                    nonce={nonce}
                    onChanged={bump}
                  />
                ) : null}
                {view.t === "scales" ? (
                  <ScalesPanel
                    ha={ha}
                    canManage={st.can_manage}
                    nonce={nonce}
                    onChanged={bump}
                  />
                ) : null}
                {view.t === "rule" ? (
                  <RulePanel
                    key={view.key}
                    ha={ha}
                    rkey={view.key}
                    canManage={st.can_manage}
                    nonce={nonce}
                    onChanged={bump}
                  />
                ) : null}
              </section>
            </div>
          </>
        )}
      </LoadState>
    </>
  );
}

/** Tóm tắt thuộc tính riêng của mục theo loại. */
function attrsText(
  kind: HrmKind,
  a: HrmAttrs,
  names: Map<string, string>,
): string {
  switch (kind) {
    case "position":
      return [
        a.leader ? "Lãnh đạo, quản lý" : "",
        a.allowance ? `PCCV ${vnDecimal(a.allowance)}` : "",
      ]
        .filter(Boolean)
        .join(" · ");
    case "grade":
      return [
        a.equiv_group ? EQUIV_LABEL[a.equiv_group] : "",
        a.grade_type ? GRADE_TYPE_LABEL[a.grade_type] : "",
        a.salary_class ?? "",
      ]
        .filter(Boolean)
        .join(" · ");
    case "education_level":
      return a.rank ? `Thứ hạng ${a.rank}` : "";
    case "major_group":
      return a.level
        ? `Trình độ: ${names.get(`education_level:${a.level}`) ?? a.level}`
        : "";
    case "major":
      return Object.entries(a.groups ?? {})
        .map(
          ([lv, g]) =>
            `${names.get(`education_level:${lv}`) ?? lv}: ${names.get(`major_group:${g}`) ?? g}`,
        )
        .join(" · ");
    case "ethnicity":
      return a.minority ? "Dân tộc thiểu số" : "";
    case "religion":
      return a.no_religion ? "Không tôn giáo" : "";
    default:
      return "";
  }
}

function KindPanel({
  ha,
  kind,
  canManage,
  nonce,
  onChanged,
}: {
  ha: HrmApi;
  kind: HrmKind;
  canManage: boolean;
  nonce: number;
  onChanged: () => void;
}) {
  const [all, setAll] = useState(false);
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const [edit, setEdit] = useState<HrmItem | "new" | null>(null);
  const [hist, setHist] = useState<HrmItem | null>(null);
  const [grade, setGrade] = useState<string | null>(null);
  const list = useLoad(() => ha.list(kind, all), [kind, all, nonce]);
  const needsRefs = kind === "major" || kind === "major_group";
  const levels = useLoad(needsRefs ? () => ha.list("education_level") : null, [
    kind,
    nonce,
  ]);
  const groups = useLoad(
    kind === "major" ? () => ha.list("major_group") : null,
    [kind, nonce],
  );
  const names = useMemo(() => {
    const m = new Map<string, string>();
    for (const x of [
      ...(levels.data?.items ?? []),
      ...(groups.data?.items ?? []),
    ])
      m.set(`${x.kind}:${x.code}`, x.name);
    return m;
  }, [levels.data, groups.data]);
  const act = useAction();
  const toggle = (it: HrmItem) =>
    void act
      .run(
        () =>
          ha.revise(kind, it.code, {
            name: it.name,
            sort_order: it.sort_order,
            active: !it.active,
            attrs: it.attrs,
          }),
        it.active ? `Đã ngừng sử dụng ${it.code}.` : `Đã dùng lại ${it.code}.`,
      )
      .then((r) => r && onChanged());
  const cols: Column<HrmItem>[] = [
    {
      key: "code",
      header: kind === "grade" ? "Mã ngạch" : "Mã",
      cell: (r) => <code>{r.code}</code>,
    },
    {
      key: "name",
      header: "Tên",
      cell: (r) =>
        kind === "grade" ? (
          <button
            type="button"
            className="link-all"
            style={{
              margin: 0,
              padding: 0,
              border: 0,
              background: "none",
              cursor: "pointer",
            }}
            onClick={() => setGrade(r.code)}
            title="Xem bảng hệ số"
          >
            {r.name}
          </button>
        ) : (
          r.name
        ),
    },
    {
      key: "attrs",
      header: "Thuộc tính",
      cell: (r) => (
        <span className="small">{attrsText(kind, r.attrs, names) || "—"}</span>
      ),
    },
    {
      key: "status",
      header: "Trạng thái",
      cell: (r) => (
        <>
          <StatusPill tone={r.active ? "ok" : "neutral"}>
            {r.active ? "Đang dùng" : "Ngừng sử dụng"}
          </StatusPill>{" "}
          <PlaceholderPill on={r.placeholder} />
        </>
      ),
    },
    { key: "version", header: "Phiên bản", cell: (r) => r.version },
    {
      key: "act",
      header: "",
      cell: (r) => (
        <div className="row">
          <button
            type="button"
            className="btn btn-o btn-sm"
            onClick={() => setHist(r)}
          >
            Lịch sử
          </button>
          {canManage ? (
            <>
              <button
                type="button"
                className="btn btn-o btn-sm"
                onClick={() => setEdit(r)}
                aria-label={`Sửa ${r.code}`}
              >
                Sửa
              </button>
              <button
                type="button"
                className={r.active ? "btn btn-d btn-sm" : "btn btn-o btn-sm"}
                disabled={act.busy}
                onClick={() => toggle(r)}
              >
                {r.active ? "Ngừng sử dụng" : "Dùng lại"}
              </button>
            </>
          ) : null}
        </div>
      ),
    },
  ];
  return (
    <LoadState s={list}>
      {(d) => {
        const needle = q.trim().toLowerCase();
        const rows = d.items.filter(
          (x) =>
            !needle ||
            x.code.toLowerCase().includes(needle) ||
            x.name.toLowerCase().includes(needle),
        );
        return (
          <>
            <div className="row" style={{ marginBottom: 12, flexWrap: "wrap" }}>
              <h2 style={{ margin: 0, fontSize: 17 }}>{KIND_LABEL[kind]}</h2>
              <SearchBox
                value={q}
                onChange={(v) => (setQ(v), setPage(1))}
                placeholder="Tìm mã, tên"
              />
              <label className="row small">
                <input
                  type="checkbox"
                  checked={all}
                  onChange={(e) => (setAll(e.target.checked), setPage(1))}
                />{" "}
                Hiện cả mục đã ngừng
              </label>
              {canManage ? (
                <button
                  type="button"
                  className="btn btn-p btn-sm"
                  onClick={() => setEdit("new")}
                >
                  Thêm
                </button>
              ) : null}
            </div>
            {act.view}
            <DataTable
              caption={KIND_LABEL[kind]}
              columns={cols}
              rows={rows.slice((page - 1) * PAGE, page * PAGE)}
              rowKey={(r) => r.code}
              page={page}
              pageSize={PAGE}
              total={rows.length}
              onPage={setPage}
              empty="Chưa có mục nào."
            />
            {kind === "grade" && grade ? (
              <GradeScaleCard
                ha={ha}
                grade={grade}
                item={d.items.find((x) => x.code === grade)}
                nonce={0}
              />
            ) : null}
            {edit ? (
              <ItemDialog
                ha={ha}
                kind={kind}
                item={edit === "new" ? null : edit}
                levels={levels.data?.items ?? []}
                groups={groups.data?.items ?? []}
                nextOrder={
                  (d.items.reduce((m, x) => Math.max(m, x.sort_order), 0) ||
                    0) + 10
                }
                onClose={() => setEdit(null)}
                onSaved={() => (setEdit(null), onChanged())}
              />
            ) : null}
            {hist ? (
              <HistoryDialog
                ha={ha}
                kind={kind}
                item={hist}
                names={names}
                onClose={() => setHist(null)}
              />
            ) : null}
          </>
        );
      }}
    </LoadState>
  );
}

function ItemDialog({
  ha,
  kind,
  item,
  levels,
  groups,
  nextOrder,
  onClose,
  onSaved,
}: {
  ha: HrmApi;
  kind: HrmKind;
  item: HrmItem | null;
  levels: HrmItem[];
  groups: HrmItem[];
  nextOrder: number;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [code, setCode] = useState(item?.code ?? "");
  const [name, setName] = useState(item?.name ?? "");
  const [order, setOrder] = useState(String(item?.sort_order ?? nextOrder));
  const [a, setA] = useState<HrmAttrs>(
    item?.attrs ??
      (kind === "grade" ? { equiv_group: "CV", grade_type: "vien_chuc" } : {}),
  );
  const set = (p: Partial<HrmAttrs>) => setA((x) => ({ ...x, ...p }));
  const act = useAction();
  const save = () => {
    const attrs: HrmAttrs = { ...a };
    if (attrs.allowance !== undefined) {
      const v = normDecimal(attrs.allowance);
      if (v) attrs.allowance = v;
      else delete attrs.allowance;
    }
    if (!attrs.mgmt_level) delete attrs.mgmt_level;
    if (!attrs.salary_class) delete attrs.salary_class;
    if (attrs.groups)
      attrs.groups = Object.fromEntries(
        Object.entries(attrs.groups).filter(([, g]) => g),
      );
    const body = {
      name: name.trim(),
      sort_order: Number(order) || 0,
      attrs,
      active: item?.active ?? true,
    };
    void act
      .run(() =>
        item
          ? ha.revise(kind, item.code, body)
          : ha.create(kind, {
              ...body,
              code: kind === "grade" ? normGradeCode(code) : code.trim(),
            }),
      )
      .then((r) => r && onSaved());
  };
  return (
    <Dialog
      title={`${item ? "Sửa" : "Thêm"} ${KIND_LABEL[kind].toLowerCase()}`}
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={act.busy || !name.trim() || (!item && !code.trim())}
          onClick={save}
        >
          Lưu
        </button>
      }
    >
      {act.view}
      <div className="grid2">
        <div className="field">
          <label htmlFor="hr-code" className="req">
            Mã
          </label>
          <input
            id="hr-code"
            className="input"
            value={code}
            disabled={!!item}
            maxLength={60}
            onChange={(e) => setCode(e.target.value)}
          />
          {kind === "grade" && !item ? (
            <span className="mute small">
              Khoảng trắng, xuống dòng được tự loại bỏ.
            </span>
          ) : null}
        </div>
        <div className="field">
          <label htmlFor="hr-name" className="req">
            Tên
          </label>
          <input
            id="hr-name"
            className="input"
            value={name}
            maxLength={200}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div className="field">
          <label htmlFor="hr-order">Thứ tự</label>
          <input
            id="hr-order"
            className="input"
            inputMode="numeric"
            value={order}
            onChange={(e) => setOrder(e.target.value.replace(/\D/g, ""))}
          />
        </div>
        {kind === "position" ? (
          <>
            <div className="field">
              <span className="lbl">Lãnh đạo, quản lý</span>
              <label className="row">
                <input
                  type="checkbox"
                  checked={!!a.leader}
                  onChange={(e) =>
                    set({
                      leader: e.target.checked,
                      ...(e.target.checked ? {} : { allowance: undefined }),
                    })
                  }
                />
                Chức vụ lãnh đạo, quản lý
              </label>
            </div>
            <div className="field">
              <label htmlFor="hr-pccv">Hệ số phụ cấp chức vụ</label>
              <input
                id="hr-pccv"
                className="input"
                inputMode="decimal"
                disabled={!a.leader}
                placeholder="vd 0,30"
                value={a.allowance ? vnDecimal(a.allowance) : ""}
                onChange={(e) => set({ allowance: e.target.value })}
              />
            </div>
            <div className="field">
              <label htmlFor="hr-mgmt">Cấp quản lý của tài khoản</label>
              <select
                id="hr-mgmt"
                className="select"
                value={a.mgmt_level ?? ""}
                onChange={(e) => set({ mgmt_level: e.target.value })}
              >
                <option value="">—</option>
                {MGMT_LEVELS.map(([c, l]) => (
                  <option key={c} value={c}>
                    {l}
                  </option>
                ))}
              </select>
            </div>
          </>
        ) : null}
        {kind === "grade" ? (
          <>
            <div className="field">
              <label htmlFor="hr-equiv" className="req">
                Nhóm tương đương (NS06)
              </label>
              <select
                id="hr-equiv"
                className="select"
                value={a.equiv_group ?? ""}
                onChange={(e) =>
                  set({
                    equiv_group: e.target.value as HrmAttrs["equiv_group"],
                  })
                }
              >
                {EQUIV_GROUPS.map((g) => (
                  <option key={g} value={g}>
                    {EQUIV_LABEL[g]}
                  </option>
                ))}
              </select>
            </div>
            <div className="field">
              <label htmlFor="hr-gtype" className="req">
                Loại
              </label>
              <select
                id="hr-gtype"
                className="select"
                value={a.grade_type ?? ""}
                onChange={(e) =>
                  set({ grade_type: e.target.value as HrmAttrs["grade_type"] })
                }
              >
                {Object.entries(GRADE_TYPE_LABEL).map(([c, l]) => (
                  <option key={c} value={c}>
                    {l}
                  </option>
                ))}
              </select>
            </div>
            <div className="field">
              <label htmlFor="hr-class">Loại bảng lương</label>
              <input
                id="hr-class"
                className="input"
                maxLength={20}
                placeholder="vd A1, A2.1, B"
                value={a.salary_class ?? ""}
                onChange={(e) => set({ salary_class: e.target.value })}
              />
            </div>
          </>
        ) : null}
        {kind === "education_level" ? (
          <div className="field">
            <label htmlFor="hr-rank" className="req">
              Thứ hạng (cao hơn = lớn hơn)
            </label>
            <input
              id="hr-rank"
              className="input"
              inputMode="numeric"
              value={a.rank ?? ""}
              onChange={(e) =>
                set({
                  rank: Number(e.target.value.replace(/\D/g, "")) || undefined,
                })
              }
            />
          </div>
        ) : null}
        {kind === "major_group" ? (
          <div className="field">
            <label htmlFor="hr-level" className="req">
              Trình độ
            </label>
            <select
              id="hr-level"
              className="select"
              value={a.level ?? ""}
              onChange={(e) => set({ level: e.target.value })}
            >
              <option value="">—</option>
              {levels.map((l) => (
                <option key={l.code} value={l.code}>
                  {l.name}
                </option>
              ))}
            </select>
          </div>
        ) : null}
        {kind === "ethnicity" ? (
          <label className="row">
            <input
              type="checkbox"
              checked={!!a.minority}
              onChange={(e) => set({ minority: e.target.checked })}
            />{" "}
            Dân tộc thiểu số
          </label>
        ) : null}
        {kind === "religion" ? (
          <label className="row">
            <input
              type="checkbox"
              checked={!!a.no_religion}
              onChange={(e) => set({ no_religion: e.target.checked })}
            />{" "}
            Mục “Không” (không tôn giáo)
          </label>
        ) : null}
      </div>
      {kind === "major" ? (
        <fieldset className="card" style={{ marginTop: 12, padding: 12 }}>
          <legend className="small">
            Nhóm chuyên ngành theo trình độ (NS05)
          </legend>
          <div className="grid2">
            {levels.map((l) => {
              const opts = groups.filter((g) => g.attrs.level === l.code);
              return (
                <div className="field" key={l.code}>
                  <label htmlFor={`hr-g-${l.code}`}>{l.name}</label>
                  <select
                    id={`hr-g-${l.code}`}
                    className="select"
                    value={a.groups?.[l.code] ?? ""}
                    disabled={!opts.length}
                    onChange={(e) =>
                      set({
                        groups: {
                          ...(a.groups ?? {}),
                          [l.code]: e.target.value,
                        },
                      })
                    }
                  >
                    <option value="">—</option>
                    {opts.map((g) => (
                      <option key={g.code} value={g.code}>
                        {g.name}
                      </option>
                    ))}
                  </select>
                </div>
              );
            })}
          </div>
        </fieldset>
      ) : null}
    </Dialog>
  );
}

function HistoryDialog({
  ha,
  kind,
  item,
  names,
  onClose,
}: {
  ha: HrmApi;
  kind: HrmKind;
  item: HrmItem;
  names: Map<string, string>;
  onClose: () => void;
}) {
  const h = useLoad(() => ha.history(kind, item.code), [kind, item.code]);
  return (
    <Dialog title={`Lịch sử ${item.code}`} onClose={onClose}>
      <LoadState s={h}>
        {(d) => (
          <table className="dt">
            <thead>
              <tr>
                <th scope="col">Phiên bản</th>
                <th scope="col">Thời điểm</th>
                <th scope="col">Tên</th>
                <th scope="col">Thuộc tính</th>
                <th scope="col">Trạng thái</th>
              </tr>
            </thead>
            <tbody>
              {d.versions.map((v) => (
                <tr key={v.version}>
                  <td>{v.version}</td>
                  <td>{new Date(v.recorded_at).toLocaleString("vi-VN")}</td>
                  <td>{v.name}</td>
                  <td className="small">
                    {attrsText(kind, v.attrs, names) || "—"}
                  </td>
                  <td>
                    {v.active ? "Đang dùng" : "Ngừng sử dụng"}{" "}
                    <PlaceholderPill on={v.placeholder} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </LoadState>
    </Dialog>
  );
}

function CoefTable({ s }: { s: HrmScale }) {
  return (
    <div className="tbl-wrap">
      <table className="dt">
        <caption className="sr-only">Hệ số theo bậc</caption>
        <thead>
          <tr>
            <th scope="col">Bậc</th>
            {s.coefficients.map((_, i) => (
              <th key={i} scope="col">
                {i + 1}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          <tr>
            <th scope="row">Hệ số</th>
            {s.coefficients.map((c, i) => (
              <td key={i}>{vnDecimal(c)}</td>
            ))}
          </tr>
        </tbody>
      </table>
    </div>
  );
}

/** Bảng hệ số của ngạch đang chọn (Hình 40): bảng hiệu lực hôm nay + các phiên bản theo ngày hiệu lực. */
function GradeScaleCard({
  ha,
  grade,
  item,
  nonce,
}: {
  ha: HrmApi;
  grade: string;
  item?: HrmItem;
  nonce: number;
}) {
  const h = useLoad(() => ha.scaleHistory(grade), [grade, nonce]);
  const today = vnToday();
  return (
    <div className="card" style={{ marginTop: 16, padding: 16 }}>
      <h3 style={{ marginTop: 0 }}>
        Bảng hệ số — {item?.name ?? grade} ({grade})
      </h3>
      <LoadState s={h}>
        {(d) => {
          if (!d.versions.length)
            return <p className="mute">Chưa có bảng hệ số.</p>;
          const eff = d.versions.filter((v) => v.valid_from <= today);
          const cur = eff[eff.length - 1];
          return (
            <>
              {cur ? (
                <>
                  <p className="small">
                    Hiệu lực từ {dmy(cur.valid_from)} · {cur.steps} bậc · giữ
                    bậc {cur.hold_months} tháng{" "}
                    <PlaceholderPill on={cur.placeholder} />
                  </p>
                  <CoefTable s={cur} />
                </>
              ) : null}
              <p className="mute small" style={{ marginTop: 8 }}>
                Các phiên bản:{" "}
                {d.versions
                  .map((v) => `${dmy(v.valid_from)} (v${v.version})`)
                  .join(" · ")}
                . Đổi hệ số không làm đổi quyết định nâng bậc đã ban hành.
              </p>
            </>
          );
        }}
      </LoadState>
    </div>
  );
}

function ScalesPanel({
  ha,
  canManage,
  nonce,
  onChanged,
}: {
  ha: HrmApi;
  canManage: boolean;
  nonce: number;
  onChanged: () => void;
}) {
  const [at, setAt] = useState(vnToday());
  const [edit, setEdit] = useState<{
    grade: HrmItem;
    scale: HrmScale | null;
  } | null>(null);
  const [open, setOpen] = useState<string | null>(null);
  const list = useLoad(() => ha.scales(at), [at, nonce]);
  return (
    <>
      <div className="row" style={{ marginBottom: 12 }}>
        <h2 style={{ margin: 0, fontSize: 17 }}>Bảng hệ số theo bậc</h2>
        <label htmlFor="hr-at">Hiệu lực tại ngày</label>
        <input
          id="hr-at"
          type="date"
          className="input"
          style={{ width: 170 }}
          value={at}
          onChange={(e) => e.target.value && setAt(e.target.value)}
        />
      </div>
      <LoadState s={list}>
        {(d) => (
          <div className="card dt-card">
            <div className="tbl-wrap">
              <table className="dt">
                <caption className="sr-only">Bảng hệ số các ngạch</caption>
                <thead>
                  <tr>
                    <th scope="col">Mã ngạch</th>
                    <th scope="col">Tên ngạch/CDNN</th>
                    <th scope="col">Nhóm</th>
                    <th scope="col">Số bậc</th>
                    <th scope="col">Hệ số bậc 1 → cuối</th>
                    <th scope="col">Giữ bậc</th>
                    <th scope="col">Hiệu lực từ</th>
                    <th scope="col" />
                  </tr>
                </thead>
                <tbody>
                  {d.grades.map(({ grade, scale }) => (
                    <tr key={grade.code}>
                      <td>
                        <code>{grade.code}</code>
                      </td>
                      <td>{grade.name}</td>
                      <td>{grade.attrs.equiv_group ?? "—"}</td>
                      <td>{scale?.steps ?? "—"}</td>
                      <td>
                        {scale
                          ? `${vnDecimal(scale.coefficients[0])} → ${vnDecimal(scale.coefficients[scale.coefficients.length - 1])}`
                          : "Chưa có"}{" "}
                        <PlaceholderPill on={!!scale?.placeholder} />
                      </td>
                      <td>{scale ? `${scale.hold_months} tháng` : "—"}</td>
                      <td>{scale ? dmy(scale.valid_from) : "—"}</td>
                      <td>
                        <div className="row">
                          <button
                            type="button"
                            className="btn btn-o btn-sm"
                            onClick={() =>
                              setOpen(open === grade.code ? null : grade.code)
                            }
                          >
                            {open === grade.code ? "Ẩn" : "Xem"}
                          </button>
                          {canManage ? (
                            <button
                              type="button"
                              className="btn btn-p btn-sm"
                              onClick={() => setEdit({ grade, scale })}
                              aria-label={`Bảng hệ số mới ${grade.code}`}
                            >
                              Bảng mới
                            </button>
                          ) : null}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}
      </LoadState>
      {open ? (
        <GradeScaleCard
          ha={ha}
          grade={open}
          item={list.data?.grades.find((g) => g.grade.code === open)?.grade}
          nonce={nonce}
        />
      ) : null}
      {edit ? (
        <ScaleDialog
          ha={ha}
          grade={edit.grade}
          base={edit.scale}
          onClose={() => setEdit(null)}
          onSaved={() => (setEdit(null), onChanged())}
        />
      ) : null}
    </>
  );
}

function ScaleDialog({
  ha,
  grade,
  base,
  onClose,
  onSaved,
}: {
  ha: HrmApi;
  grade: HrmItem;
  base: HrmScale | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [from, setFrom] = useState(vnToday());
  const [coefs, setCoefs] = useState<string[]>(
    base ? base.coefficients.map(vnDecimal) : [""],
  );
  const [hold, setHold] = useState(String(base?.hold_months ?? 36));
  const [note, setNote] = useState("");
  const act = useAction();
  const save = () =>
    void act
      .run(() =>
        ha.addScale(grade.code, {
          valid_from: from,
          coefficients: coefs.map(normDecimal),
          hold_months: Number(hold) || 0,
          ...(note.trim() ? { note: note.trim() } : {}),
        }),
      )
      .then((r) => r && onSaved());
  return (
    <Dialog
      title={`Bảng hệ số mới — ${grade.name} (${grade.code})`}
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={act.busy || coefs.some((c) => !c.trim())}
          onClick={save}
        >
          Lưu
        </button>
      }
    >
      {act.view}
      <p className="mute small">
        Bảng mới áp dụng từ ngày hiệu lực; bảng cũ giữ nguyên cho ngày trước đó
        và cho quyết định đã ban hành.
      </p>
      <div className="grid2">
        <div className="field">
          <label htmlFor="sc-from" className="req">
            Ngày hiệu lực
          </label>
          <input
            id="sc-from"
            type="date"
            className="input"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
          />
        </div>
        <div className="field">
          <label htmlFor="sc-hold" className="req">
            Thời gian giữ bậc (tháng)
          </label>
          <input
            id="sc-hold"
            className="input"
            inputMode="numeric"
            value={hold}
            onChange={(e) => setHold(e.target.value.replace(/\D/g, ""))}
          />
        </div>
      </div>
      <div className="field" style={{ marginTop: 12 }}>
        <span className="lbl">
          Hệ số từng bậc (tăng dần, tối đa 2 chữ số thập phân)
        </span>
        <div className="row" style={{ flexWrap: "wrap" }}>
          {coefs.map((c, i) => (
            <input
              key={i}
              aria-label={`Hệ số bậc ${i + 1}`}
              className="input"
              style={{ width: 80 }}
              inputMode="decimal"
              value={c}
              onChange={(e) =>
                setCoefs((xs) =>
                  xs.map((x, j) => (j === i ? e.target.value : x)),
                )
              }
            />
          ))}
          <button
            type="button"
            className="btn btn-o btn-sm"
            disabled={coefs.length >= 20}
            onClick={() => setCoefs((xs) => [...xs, ""])}
          >
            + Bậc
          </button>
          <button
            type="button"
            className="btn btn-o btn-sm"
            disabled={coefs.length <= 1}
            onClick={() => setCoefs((xs) => xs.slice(0, -1))}
          >
            − Bậc
          </button>
        </div>
      </div>
      <div className="field" style={{ marginTop: 12 }}>
        <label htmlFor="sc-note">Ghi chú (căn cứ)</label>
        <input
          id="sc-note"
          className="input"
          maxLength={500}
          value={note}
          onChange={(e) => setNote(e.target.value)}
        />
      </div>
    </Dialog>
  );
}

const PROMOTION_FIELDS: {
  k: keyof HrmPromotionRules;
  label: string;
  pct?: boolean;
}[] = [
  { k: "min_rating", label: "Xếp loại tối thiểu mỗi năm (NLTX)" },
  { k: "rating_years", label: "Số năm xếp loại xét" },
  { k: "achievement_years", label: "Số năm thành tích xét (NLTH)" },
  { k: "nlth_quota_pct", label: "Tỷ lệ chỉ tiêu NLTH (%)", pct: true },
  {
    k: "nlth_retirement_months",
    label: "NLTH nghỉ hưu: số tháng nâng trước tối đa",
  },
  {
    k: "nlth_retirement_within_months",
    label: "NLTH nghỉ hưu: trong vòng (tháng)",
  },
  { k: "tnvk_start_pct", label: "% TNVK khởi điểm", pct: true },
  { k: "tnvk_step_pct", label: "% TNVK tăng mỗi chu kỳ", pct: true },
  { k: "tnvk_cycle_months", label: "Chu kỳ TNVK (tháng)" },
  { k: "tnvk_first_months", label: "Giữ bậc cuối trước TNVK lần đầu (tháng)" },
  { k: "retirement_age_male_months", label: "Tuổi nghỉ hưu nam (tháng tuổi)" },
  { k: "retirement_age_female_months", label: "Tuổi nghỉ hưu nữ (tháng tuổi)" },
];

/** Dòng mã → số tháng (kéo dài theo kỷ luật; quy đổi thành tích). */
type CodeMonths = { code: string; months: string }[];

function RulePanel({
  ha,
  rkey,
  canManage,
  nonce,
  onChanged,
}: {
  ha: HrmApi;
  rkey: HrmRuleKey;
  canManage: boolean;
  nonce: number;
  onChanged: () => void;
}) {
  const [at, setAt] = useState(vnToday());
  const [edit, setEdit] = useState(false);
  const v = useLoad(() => ha.rule(rkey, at), [rkey, at, nonce]);
  const refKind: HrmKind =
    rkey === "promotion" ? "discipline_form" : "award_form";
  const refs = useLoad(() => ha.list(refKind), [refKind, nonce]);
  const refName = (c: string) =>
    refs.data?.items.find((x) => x.code === c)?.name ?? c;
  return (
    <>
      <div className="row" style={{ marginBottom: 12 }}>
        <h2 style={{ margin: 0, fontSize: 17 }}>{RULE_LABEL[rkey]}</h2>
        <label htmlFor="ru-at">Hiệu lực tại ngày</label>
        <input
          id="ru-at"
          type="date"
          className="input"
          style={{ width: 170 }}
          value={at}
          onChange={(e) => e.target.value && setAt(e.target.value)}
        />
        {canManage ? (
          <button
            type="button"
            className="btn btn-p btn-sm"
            onClick={() => setEdit(true)}
          >
            Phiên bản mới
          </button>
        ) : null}
      </div>
      <LoadState s={v}>
        {(d) => (
          <>
            {d.current ? (
              <div className="card" style={{ padding: 16 }}>
                <p className="small" style={{ marginTop: 0 }}>
                  Phiên bản {d.current.version}, hiệu lực từ{" "}
                  {dmy(d.current.valid_from)}{" "}
                  <PlaceholderPill on={d.current.placeholder} />
                  {d.current.note ? (
                    <span className="mute"> — {d.current.note}</span>
                  ) : null}
                </p>
                {rkey === "promotion" ? (
                  <table className="dt">
                    <tbody>
                      {PROMOTION_FIELDS.map((f) => {
                        const p = d.current!
                          .payload as unknown as HrmPromotionRules;
                        const raw = p[f.k];
                        return (
                          <tr key={f.k}>
                            <th scope="row">{f.label}</th>
                            <td>
                              {f.pct ? vnDecimal(String(raw)) : String(raw)}
                            </td>
                          </tr>
                        );
                      })}
                      <tr>
                        <th scope="row">Mốc nhắc đến kỳ (tháng tới)</th>
                        <td>
                          {(
                            (d.current.payload as unknown as HrmPromotionRules)
                              .remind_months ?? []
                          ).join(", ")}
                        </td>
                      </tr>
                      <tr>
                        <th scope="row">Kéo dài giữ bậc theo kỷ luật</th>
                        <td>
                          {Object.entries(
                            (d.current.payload as unknown as HrmPromotionRules)
                              .discipline_extension_months ?? {},
                          )
                            .map(([c, m]) => `${refName(c)}: ${m} tháng`)
                            .join(" · ") || "—"}
                        </td>
                      </tr>
                    </tbody>
                  </table>
                ) : (
                  <table className="dt">
                    <thead>
                      <tr>
                        <th scope="col">Hình thức khen thưởng</th>
                        <th scope="col">Số tháng nâng bậc trước hạn</th>
                      </tr>
                    </thead>
                    <tbody>
                      {(
                        d.current.payload as unknown as HrmAchievementConversion
                      ).rules.map((r) => (
                        <tr key={r.award_code}>
                          <td>{refName(r.award_code)}</td>
                          <td>{r.months}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )}
              </div>
            ) : (
              <p className="mute">Chưa có phiên bản hiệu lực tại ngày này.</p>
            )}
            <p className="mute small" style={{ marginTop: 8 }}>
              Các phiên bản:{" "}
              {d.versions
                .map(
                  (x) =>
                    `${dmy(x.valid_from)} (v${x.version}${x.placeholder ? ", mẫu" : ""})`,
                )
                .join(" · ") || "—"}
            </p>
            {edit ? (
              <RuleDialog
                ha={ha}
                rkey={rkey}
                base={
                  d.current?.payload as unknown as
                    HrmPromotionRules | HrmAchievementConversion | undefined
                }
                refs={refs.data?.items ?? []}
                onClose={() => setEdit(false)}
                onSaved={() => (setEdit(false), onChanged())}
              />
            ) : null}
          </>
        )}
      </LoadState>
    </>
  );
}

function RuleDialog({
  ha,
  rkey,
  base,
  refs,
  onClose,
  onSaved,
}: {
  ha: HrmApi;
  rkey: HrmRuleKey;
  base?: HrmPromotionRules | HrmAchievementConversion;
  refs: HrmItem[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [from, setFrom] = useState(vnToday());
  const [note, setNote] = useState("");
  const promo =
    rkey === "promotion" ? (base as HrmPromotionRules | undefined) : undefined;
  const [fields, setFields] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      PROMOTION_FIELDS.map((f) => [
        f.k,
        promo
          ? f.pct
            ? vnDecimal(String(promo[f.k]))
            : String(promo[f.k])
          : "",
      ]),
    ),
  );
  const [remind, setRemind] = useState(
    promo?.remind_months?.join(", ") ?? "1, 3, 6",
  );
  const [rows, setRows] = useState<CodeMonths>(() =>
    rkey === "promotion"
      ? Object.entries(promo?.discipline_extension_months ?? {}).map(
          ([code, m]) => ({ code, months: String(m) }),
        )
      : ((base as HrmAchievementConversion | undefined)?.rules ?? []).map(
          (r) => ({ code: r.award_code, months: String(r.months) }),
        ),
  );
  const act = useAction();
  const payload = (): HrmPromotionRules | HrmAchievementConversion => {
    const pairs = rows.filter((r) => r.code);
    if (rkey === "achievement_conversion")
      return {
        rules: pairs.map((r) => ({
          award_code: r.code,
          months: Number(r.months) || 0,
        })),
      };
    const p: Record<string, unknown> = {};
    for (const f of PROMOTION_FIELDS) {
      const raw = fields[f.k] ?? "";
      p[f.k] =
        f.k === "min_rating"
          ? raw.trim()
          : f.pct
            ? normDecimal(raw)
            : Number(raw) || 0;
    }
    p.remind_months = remind
      .split(/[,;\s]+/)
      .filter(Boolean)
      .map(Number);
    p.discipline_extension_months = Object.fromEntries(
      pairs.map((r) => [r.code, Number(r.months) || 0]),
    );
    return p as unknown as HrmPromotionRules;
  };
  const save = () =>
    void act
      .run(() =>
        ha.addRule(rkey, {
          valid_from: from,
          payload: payload(),
          ...(note.trim() ? { note: note.trim() } : {}),
        }),
      )
      .then((r) => r && onSaved());
  return (
    <Dialog
      title={`${RULE_LABEL[rkey]} — phiên bản mới`}
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={act.busy}
          onClick={save}
        >
          Lưu
        </button>
      }
    >
      {act.view}
      <div className="grid2">
        <div className="field">
          <label htmlFor="ru-from" className="req">
            Ngày hiệu lực
          </label>
          <input
            id="ru-from"
            type="date"
            className="input"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
          />
        </div>
        <div className="field">
          <label htmlFor="ru-note">Ghi chú (căn cứ)</label>
          <input
            id="ru-note"
            className="input"
            maxLength={500}
            value={note}
            onChange={(e) => setNote(e.target.value)}
          />
        </div>
        {rkey === "promotion" ? (
          <>
            {PROMOTION_FIELDS.map((f) => (
              <div className="field" key={f.k}>
                <label htmlFor={`ru-${f.k}`}>{f.label}</label>
                {f.k === "min_rating" ? (
                  <select
                    id={`ru-${f.k}`}
                    className="select"
                    value={fields[f.k] ?? ""}
                    onChange={(e) =>
                      setFields((x) => ({ ...x, [f.k]: e.target.value }))
                    }
                  >
                    {["HTXSNV", "HTTNV", "HTNV", "KHTNV"].map((r) => (
                      <option key={r}>{r}</option>
                    ))}
                  </select>
                ) : (
                  <input
                    id={`ru-${f.k}`}
                    className="input"
                    inputMode={f.pct ? "decimal" : "numeric"}
                    value={fields[f.k] ?? ""}
                    onChange={(e) =>
                      setFields((x) => ({ ...x, [f.k]: e.target.value }))
                    }
                  />
                )}
              </div>
            ))}
            <div className="field">
              <label htmlFor="ru-remind">Mốc nhắc (tháng tới, tăng dần)</label>
              <input
                id="ru-remind"
                className="input"
                value={remind}
                onChange={(e) => setRemind(e.target.value)}
              />
            </div>
          </>
        ) : null}
      </div>
      <fieldset className="card" style={{ marginTop: 12, padding: 12 }}>
        <legend className="small">
          {rkey === "promotion"
            ? "Kéo dài thời gian giữ bậc theo hình thức kỷ luật"
            : "Bảng quy đổi thành tích"}
        </legend>
        {rows.map((r, i) => (
          <div className="row" key={i} style={{ marginBottom: 6 }}>
            <select
              aria-label={`Mục ${i + 1}`}
              className="select"
              value={r.code}
              onChange={(e) =>
                setRows((xs) =>
                  xs.map((x, j) =>
                    j === i ? { ...x, code: e.target.value } : x,
                  ),
                )
              }
            >
              <option value="">—</option>
              {refs.map((x) => (
                <option key={x.code} value={x.code}>
                  {x.name}
                </option>
              ))}
            </select>
            <input
              aria-label={`Số tháng ${i + 1}`}
              className="input"
              style={{ width: 90 }}
              inputMode="numeric"
              value={r.months}
              onChange={(e) =>
                setRows((xs) =>
                  xs.map((x, j) =>
                    j === i
                      ? { ...x, months: e.target.value.replace(/\D/g, "") }
                      : x,
                  ),
                )
              }
            />
            <span className="small">tháng</span>
            <button
              type="button"
              className="btn btn-o btn-sm"
              onClick={() => setRows((xs) => xs.filter((_, j) => j !== i))}
            >
              Bỏ
            </button>
          </div>
        ))}
        <button
          type="button"
          className="btn btn-o btn-sm"
          onClick={() => setRows((xs) => [...xs, { code: "", months: "" }])}
        >
          + Thêm dòng
        </button>
      </fieldset>
    </Dialog>
  );
}
