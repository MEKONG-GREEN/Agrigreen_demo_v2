"use client";

// FN-19 — Danh mục nhân sự (BRD HCTH v3.0, UR-ADM-08; W11.21, dom/hrm). Bên trái: 4 danh mục Admin cấu hình (Chức
// vụ, Ngạch/CDNN, Trình độ chuyên môn, Chuyên ngành), bảng hệ số theo bậc, tham số nâng bậc/quy đổi thành tích và các
// danh sách khai báo sẵn; bên phải: lưới mục của danh mục đang chọn, bảng hệ số của ngạch đang chọn (Hình 39, 40).
// Quyền lấy từ máy chủ (can_manage): Admin thêm/sửa/ngừng; Cấp Tỉnh – HCTH chỉ xem. Không xóa (BR-01) — chỉ ngừng sử
// dụng. Mục "Mẫu" là dữ liệu cài sẵn chưa được Phòng HCTH xác nhận (Q-070).
import { useMemo, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
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
import { catalogBase, catalogItemPath } from "./catalog-form";
import { HrmScalesList } from "./ScaleScreens";
import { scaleListPath, scalePath, scaleToday, validScaleDay } from "./scale-form";
import {
  createHrmApi,
  dmy,
  EQUIV_LABEL,
  FIXED_KINDS,
  GRADE_TYPE_LABEL,
  KIND_LABEL,
  MAIN_KINDS,
  normDecimal,
  RULE_LABEL,
  vnDecimal,
  type HrmAchievementConversion,
  type HrmApi,
  type HrmAttrs,
  type HrmItem,
  type HrmKind,
  type HrmPromotionRules,
  type HrmRuleKey,
} from "./api";

type View =
  | { t: "kind"; kind: HrmKind }
  | { t: "scales" }
  | { t: "rule"; key: HrmRuleKey };
const PAGE = 20;

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

export function HrmCatalogScreen(props: { initialKind?: HrmKind; initialScales?: boolean; initialScaleDay?: string }) {
  const { kn } = useAuth();
  return <CatalogList key={`${kn?.context?.context_id ?? "unknown"}/${props.initialKind ?? "grade"}/${!!props.initialScales}/${props.initialScaleDay ?? ""}`} {...props} />;
}

export function HrmCatalogScalesScreen() {
  const day = useSearchParams().get("at") ?? scaleToday();
  return validScaleDay(day) ? <HrmCatalogScreen initialScales initialScaleDay={day} /> : <p role="alert">Ngày hiệu lực không hợp lệ.</p>;
}

function CatalogList({ initialKind = "grade", initialScales = false, initialScaleDay }: { initialKind?: HrmKind; initialScales?: boolean; initialScaleDay?: string }) {
  const ha = useHrmApi();
  const base = catalogBase(usePathname());
  const router = useRouter();
  const [view, setView] = useState<View>(initialScales ? { t: "scales" } : { t: "kind", kind: initialKind });
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
                  () => { setView({ t: "scales" }); router.push(scaleListPath(base, initialScaleDay ?? scaleToday())); },
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
                    base={base}
                    ha={ha}
                    kind={view.kind}
                    canManage={st.can_manage}
                    nonce={nonce}
                    onChanged={bump}
                  />
                ) : null}
                {view.t === "scales" ? (
                  <HrmScalesList
                    ha={ha}
                    base={base}
                    canManage={st.can_manage}
                    nonce={nonce}
                    initialDay={initialScaleDay}
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
export function attrsText(
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
  base,
  ha,
  kind,
  canManage,
  nonce,
  onChanged,
}: {
  base: string;
  ha: HrmApi;
  kind: HrmKind;
  canManage: boolean;
  nonce: number;
  onChanged: () => void;
}) {
  const [all, setAll] = useState(false);
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
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
      cell: (r) => <Link href={catalogItemPath(base, kind, r.code)}><code>{r.code}</code></Link>,
    },
    {
      key: "name",
      header: "Tên",
      cell: (r) =>
        kind === "grade" ? (
          <Link
            href={scalePath(base, r.code, scaleToday())}
            title="Xem bảng hệ số"
          >
            {r.name}
          </Link>
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
          <Link
            className="btn btn-o btn-sm"
            href={catalogItemPath(base, kind, r.code)}
          >
            Lịch sử
          </Link>
          {canManage ? (
            <>
              <Link
                className="btn btn-o btn-sm"
                href={`${catalogItemPath(base, kind, r.code)}/sua`}
                aria-label={`Sửa ${r.code}`}
              >
                Sửa
              </Link>
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
                <Link
                  className="btn btn-p btn-sm"
                  href={`${base}/${kind}/them-moi`}
                >
                  Thêm
                </Link>
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
          </>
        );
      }}
    </LoadState>
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
