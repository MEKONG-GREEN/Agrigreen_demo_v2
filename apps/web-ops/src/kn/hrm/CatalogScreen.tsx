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
import { rulePath } from "./rule-form";
import { scaleListPath, scalePath, scaleToday, validScaleDay } from "./scale-form";
import {
  createHrmApi,
  EQUIV_LABEL,
  FIXED_KINDS,
  GRADE_TYPE_LABEL,
  KIND_LABEL,
  MAIN_KINDS,
  RULE_LABEL,
  vnDecimal,
  type HrmApi,
  type HrmAttrs,
  type HrmItem,
  type HrmKind,
  type HrmRuleKey,
} from "./api";

type View =
  | { t: "kind"; kind: HrmKind }
  | { t: "scales" };
const PAGE = 20;

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
                {(["promotion", "achievement_conversion"] as HrmRuleKey[]).map((key) => <Link key={key} className="btn btn-o" href={rulePath(base,key,initialScaleDay ?? scaleToday())}>{RULE_LABEL[key]}</Link>)}
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
