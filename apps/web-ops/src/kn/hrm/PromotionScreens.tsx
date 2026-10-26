"use client";

// FN-09 Danh sách nâng bậc NL01–NL04 (BRD HCTH v3.0; W11.25, dom/hrm) theo bố cục prototype HCTH: khối chỉ số (tổng số
// đến kỳ, thường xuyên, vượt khung, trước hạn / chỉ tiêu năm), trạng thái xử lý, số người đến kỳ theo tháng, cần chú ý;
// bộ lọc (thời gian 1/3/6 tháng, tháng, loại, điều kiện, trạng thái, đơn vị); bảng người đến kỳ có ô chọn (đưa vào danh
// sách — người chưa đủ bắt buộc lý do); "Lập danh sách trước hạn" (NLTH từ thành tích, kiểm chỉ tiêu); "Xuất danh sách đã
// chọn" (hệ thống tự chia Mẫu số 1/2/4, xem trước, tải xuống); "File đã xuất". Chi tiết một kỳ: kỳ nâng bậc, căn cứ điều
// kiện, lịch sử, xếp loại năm (nhập dữ liệu chuyển đổi). Quyền do máy chủ quyết định.
import Link from "next/link";
import { useParams } from "next/navigation";
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { BackLink } from "@/components/patterns/detail";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import {
  COND_LABEL,
  createPromotionApi,
  FORM_LABEL,
  fromTo,
  KIND_FORM,
  KIND_LABEL,
  RATINGS,
  STATUS_LABEL,
  vnNum,
  type NLTHPool,
  type PromoCase,
  type PromoCond,
  type PromoKind,
  type PromoStatus,
  type PromotionApi,
} from "./promotion-api";
import { vnDate } from "./staff-api";

export const PROMO_BASE = "/kn/nhan-su/nang-bac";

function usePromotionApi(): PromotionApi {
  const { api } = useAuth();
  return useMemo(() => createPromotionApi(api), [api]);
}

const caseHref = (key: string) => `${PROMO_BASE}/${encodeURIComponent(key)}`;

export function CondPill({ c, why }: { c: PromoCond; why?: string[] }) {
  const tone = c === "ok" ? "ok" : c === "review" ? "warn" : "bad";
  return (
    <StatusPill tone={tone}>
      {COND_LABEL[c]}
      {why && why.length ? `: ${why.join(", ")}` : ""}
    </StatusPill>
  );
}

export function PromoStatusPill({ s }: { s: PromoStatus }) {
  const tone =
    s === "decided"
      ? "ok"
      : s === "pending"
        ? "bad"
        : s === "exported"
          ? "info"
          : "warn";
  return <StatusPill tone={tone}>{STATUS_LABEL[s]}</StatusPill>;
}

function Bars({
  title,
  rows,
  onPick,
  active,
}: {
  title: string;
  rows: { key: string; label: string; n: number }[];
  onPick: (k: string) => void;
  active: string;
}) {
  const max = Math.max(1, ...rows.map((r) => r.n));
  return (
    <div className="card">
      <h2 className="h3">{title}</h2>
      {rows.length === 0 ? (
        <p className="mute small">
          Không có người đến kỳ trong khung thời gian.
        </p>
      ) : (
        <ul className="plain">
          {rows.map((r) => (
            <li key={r.key}>
              <button
                type="button"
                className="btn-link"
                aria-pressed={active === r.key}
                onClick={() => onPick(active === r.key ? "all" : r.key)}
              >
                {r.label}
              </button>{" "}
              <span
                className="bar"
                style={{
                  display: "inline-block",
                  width: `${(r.n / max) * 55}%`,
                  minWidth: 4,
                  height: 8,
                  background: "var(--accent, #2f7d4f)",
                }}
              />{" "}
              <strong>{r.n}</strong>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

type Filter = {
  window: number;
  month: string;
  kind: "all" | PromoKind;
  cond: "all" | PromoCond;
  status: "all" | PromoStatus;
  unit: string;
  q: string;
};
const DEF: Filter = {
  window: 3,
  month: "all",
  kind: "all",
  cond: "all",
  status: "all",
  unit: "all",
  q: "",
};

// --------------------------------------------------------------------------------------------------------------------
// FN-09 B1, B2 — màn danh sách.
// --------------------------------------------------------------------------------------------------------------------

export function HrmPromotionScreen() {
  const pa = usePromotionApi();
  const [f, setF] = useState<Filter>(DEF);
  const [nonce, setNonce] = useState(0);
  const [reasonFor, setReasonFor] = useState<PromoCase | null>(null);
  const [dlg, setDlg] = useState<"" | "export" | "nlth" | "files">("");
  const act = useAction();
  const list = useLoad(() => pa.list(f.window), [f.window, nonce]);
  const reload = () => setNonce((n) => n + 1);
  const set = (p: Partial<Filter>) => setF((x) => ({ ...x, ...p }));
  const reset = { month: "all", kind: "all", cond: "all", status: "all" } as const;

  const toggle = (r: PromoCase) => {
    if (r.status === "decided" || r.status === "exported") return;
    if (r.kind === "nlth") return;
    if (r.status === "listed") {
      void act.run(
        () => pa.exclude([r.key]),
        "Đã bỏ khỏi danh sách.",
      ).then(reload);
      return;
    }
    if (r.cond !== "ok") {
      setReasonFor(r);
      return;
    }
    void act.run(
      () => pa.include([r.key]),
      "Đã đưa vào danh sách.",
    ).then(reload);
  };

  return (
    <>
      <PageHeader
        title="Danh sách nâng bậc"
        actions={
          <div className="row">
            <button
              type="button"
              className="btn btn-p btn-sm"
              onClick={() => setDlg("nlth")}
            >
              Lập danh sách trước hạn
            </button>
            <button
              type="button"
              className="btn btn-o btn-sm"
              onClick={() => setDlg("files")}
            >
              File đã xuất
            </button>
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={
                !list.data?.items.some(
                  (r) => r.status === "listed" || r.status === "exported",
                )
              }
              onClick={() => setDlg("export")}
            >
              Xuất danh sách đã chọn (
              {list.data?.items.filter((r) => r.status === "listed").length ??
                0}
              )
            </button>
          </div>
        }
      >
        Hệ thống tự tính ngày đến kỳ nâng bậc thường xuyên (NLTX), thâm niên
        vượt khung (TNVK) từ ngạch, bậc và mốc xếp bậc; nâng bậc trước hạn
        (NLTH) do Phòng HCTH lập từ thành tích. Danh sách xuất theo Mẫu số 1, 2,
        4 để trình ký ngoài hệ thống; quyết định lưu ở “Quyết định nâng bậc”.
      </PageHeader>
      {act.view}
      <LoadState s={list}>
        {(l) => {
          const base = l.items;
          const months = [...new Set(base.map((r) => r.due.slice(0, 7)))].sort();
          const units = [
            ...new Set(base.map((r) => r.unit_name || r.unit)),
          ].sort();
          const rows = base.filter(
            (r) =>
              (f.kind === "all" || r.kind === f.kind) &&
              (f.cond === "all" || r.cond === f.cond) &&
              (f.status === "all" || r.status === f.status) &&
              (f.unit === "all" || (r.unit_name || r.unit) === f.unit) &&
              (f.month === "all" || r.due.startsWith(f.month)) &&
              (!f.q ||
                `${r.name} ${r.code}`
                  .toLowerCase()
                  .includes(f.q.toLowerCase())),
          );
          const s = l.summary;
          const q = s.quota;
          const today = l.as_of;
          const attn = [
            ...base
              .filter((r) => r.cond !== "ok" && r.status !== "decided")
              .map((r) => ({
                r,
                t: `${r.name || r.code}: ${COND_LABEL[r.cond].toLowerCase()} (${r.why.join(", ")})`,
              })),
            ...base
              .filter(
                (r) =>
                  r.due < today && r.status === "pending" && r.cond === "ok",
              )
              .map((r) => ({
                r,
                t: `${r.name || r.code}: quá hạn ${vnDate(r.due)}, chưa xử lý`,
              })),
          ];
          const Kpi = ({
            label,
            v,
            tone,
            on,
            pick,
            sub,
          }: {
            label: string;
            v: string | number;
            tone: string;
            on: boolean;
            pick: Partial<Filter>;
            sub?: string;
          }) => (
            <button
              type="button"
              className={`kpi ${tone}`}
              aria-pressed={on}
              style={{ textAlign: "left", border: on ? "3px solid #fff" : 0 }}
              onClick={() => set({ ...reset, ...pick })}
            >
              <div className="lb">{label}</div>
              <div className="vl">{v}</div>
              {sub ? <div className="ft">{sub}</div> : null}
            </button>
          );
          const allOff =
            f.kind === "all" &&
            f.cond === "all" &&
            f.status === "all" &&
            f.month === "all";
          return (
            <>
              <div className="card filters" role="search">
                <label className="small">
                  Thời gian{" "}
                  <select
                    aria-label="Thời gian"
                    value={f.window}
                    onChange={(e) => set({ window: Number(e.target.value) })}
                  >
                    <option value={1}>1 tháng tới</option>
                    <option value={3}>3 tháng tới</option>
                    <option value={6}>6 tháng tới</option>
                    <option value={12}>12 tháng tới (NL01)</option>
                  </select>
                </label>
                <label className="small">
                  Tháng đến kỳ{" "}
                  <select
                    aria-label="Tháng đến kỳ"
                    value={f.month}
                    onChange={(e) => set({ month: e.target.value })}
                  >
                    <option value="all">Tất cả</option>
                    {months.map((m) => (
                      <option key={m} value={m}>
                        {m.slice(5)}/{m.slice(0, 4)}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="small">
                  Loại{" "}
                  <select
                    aria-label="Loại nâng bậc"
                    value={f.kind}
                    onChange={(e) =>
                      set({ kind: e.target.value as Filter["kind"] })
                    }
                  >
                    <option value="all">Tất cả</option>
                    {(["nltx", "tnvk", "nlth"] as PromoKind[]).map((k) => (
                      <option key={k} value={k}>
                        {KIND_LABEL[k]}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="small">
                  Điều kiện{" "}
                  <select
                    aria-label="Điều kiện"
                    value={f.cond}
                    onChange={(e) =>
                      set({ cond: e.target.value as Filter["cond"] })
                    }
                  >
                    <option value="all">Tất cả</option>
                    {(["ok", "fail", "review"] as PromoCond[]).map((c) => (
                      <option key={c} value={c}>
                        {COND_LABEL[c]}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="small">
                  Trạng thái{" "}
                  <select
                    aria-label="Trạng thái xử lý"
                    value={f.status}
                    onChange={(e) =>
                      set({ status: e.target.value as Filter["status"] })
                    }
                  >
                    <option value="all">Tất cả</option>
                    {(Object.keys(STATUS_LABEL) as PromoStatus[]).map((s0) => (
                      <option key={s0} value={s0}>
                        {STATUS_LABEL[s0]}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="small">
                  Đơn vị{" "}
                  <select
                    aria-label="Đơn vị"
                    value={f.unit}
                    onChange={(e) => set({ unit: e.target.value })}
                  >
                    <option value="all">Tất cả</option>
                    {units.map((u) => (
                      <option key={u} value={u}>
                        {u}
                      </option>
                    ))}
                  </select>
                </label>
                <input
                  type="search"
                  aria-label="Tìm theo họ tên, mã hồ sơ"
                  placeholder="Họ tên, mã hồ sơ"
                  value={f.q}
                  onChange={(e) => set({ q: e.target.value })}
                />
              </div>
              <div className="grid4" style={{ marginBottom: 16 }}>
                <Kpi
                  label="Tổng số đến kỳ"
                  v={s.total}
                  tone="green"
                  on={allOff}
                  pick={{}}
                  sub={`Đến ${vnDate(today)} + ${l.window} tháng`}
                />
                <Kpi
                  label="Thường xuyên"
                  v={s.nltx}
                  tone="blue"
                  on={f.kind === "nltx"}
                  pick={{ kind: "nltx" }}
                />
                <Kpi
                  label="Thâm niên vượt khung"
                  v={s.tnvk}
                  tone="orange"
                  on={f.kind === "tnvk"}
                  pick={{ kind: "tnvk" }}
                />
                <Kpi
                  label="Nâng bậc trước hạn / chỉ tiêu năm"
                  v={`${q.used + q.pending} / ${q.total}`}
                  tone="violet"
                  on={f.kind === "nlth"}
                  pick={{ kind: "nlth" }}
                  sub={`Năm ${q.year}: đã có quyết định ${q.used}, đang lập ${q.pending}`}
                />
              </div>
              <div className="grid2" style={{ gridTemplateColumns: "repeat(3, minmax(0, 1fr))", marginBottom: 16 }}>
                <Bars
                  title="Trạng thái xử lý"
                  rows={(Object.keys(STATUS_LABEL) as PromoStatus[]).map((k) => ({
                    key: k,
                    label: STATUS_LABEL[k],
                    n: s.by_status[k] ?? 0,
                  }))}
                  active={f.status}
                  onPick={(k) =>
                    set({ ...reset, status: k as Filter["status"] })
                  }
                />
                <Bars
                  title="Số người đến kỳ theo tháng"
                  rows={Object.keys(s.by_month)
                    .sort()
                    .map((m) => ({
                      key: m,
                      label: `${m.slice(5)}/${m.slice(0, 4)}`,
                      n: s.by_month[m] ?? 0,
                    }))}
                  active={f.month}
                  onPick={(k) => set({ ...reset, month: k })}
                />
                <div className="card">
                  <h2 className="h3">Cần chú ý</h2>
                  {attn.length === 0 ? (
                    <p className="mute small">
                      Không có trường hợp cần chú ý.
                    </p>
                  ) : (
                    <ul className="plain">
                      {attn.slice(0, 5).map((a, i) => (
                        <li key={i}>
                          <Link href={caseHref(a.r.key)}>{a.t}</Link>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              </div>
              {l.missing.length ? (
                <div className="alert alert-warn" role="note">
                  <span>
                    {l.missing.length} viên chức chưa có ngạch, bậc, bảng hệ
                    số hoặc mốc xếp bậc nên chưa tính được ngày đến kỳ:{" "}
                    {l.missing
                      .slice(0, 4)
                      .map((m) => m.name || m.code)
                      .join(", ")}
                    {l.missing.length > 4 ? "…" : ""}.
                  </span>
                </div>
              ) : null}
              <details className="card">
                <summary>
                  Quy tắc xác định danh sách{" "}
                  <StatusPill tone="warn">Chờ Phòng HCTH xác nhận</StatusPill>
                </summary>
                <ul>
                  <li>
                    <b>Mốc xếp bậc:</b> ngày xếp bậc hiện tại trong hồ sơ (thẻ
                    Ngạch – Bậc; quyết định lưu ở sổ quyết định cập nhật mốc
                    này).
                  </li>
                  <li>
                    <b>Thường xuyên:</b> bậc hiện tại nhỏ hơn số bậc của ngạch;
                    ngày đến kỳ = mốc xếp bậc + thời gian giữ bậc của ngạch (+
                    kéo dài do kỷ luật). Điều kiện: xếp loại{" "}
                    {l.rules.rating_years} năm gần nhất từ {l.rules.min_rating}{" "}
                    trở lên (thiếu xếp loại: “Cần xem xét”), không đang thi
                    hành kỷ luật (cờ nhập tay).
                  </li>
                  <li>
                    <b>Vượt khung:</b> đang ở bậc cuối; lần đầu sau{" "}
                    {l.rules.tnvk_first_months} tháng giữ bậc cuối (
                    {l.rules.tnvk_start_pct}%), các lần sau mỗi{" "}
                    {l.rules.tnvk_cycle_months} tháng (+
                    {l.rules.tnvk_step_pct}%).
                  </li>
                  <li>
                    <b>Trước hạn:</b> Phòng HCTH lập từ thành tích{" "}
                    {l.rules.achievement_years} năm gần nhất theo bảng quy đổi;
                    ngày nâng = ngày đến kỳ thường xuyên − số tháng quy đổi.
                    Chỉ tiêu = {vnNum(l.rules.nlth_quota_pct)}% số viên chức
                    đang công tác.
                  </li>
                  <li>
                    <b>Bị chặn khi xuất Mẫu số 2:</b> vượt chỉ tiêu, thành tích
                    đã dùng, ngày nâng ngoài năm chỉ tiêu, đang ở bậc cuối.
                  </li>
                </ul>
              </details>
              <div className="card dt-card">
                <div className="tbl-wrap">
                  <table className="dt">
                    <caption className="sr-only">Người đến kỳ nâng bậc</caption>
                    <thead>
                      <tr>
                        <th>Chọn</th>
                        <th>Họ và tên</th>
                        <th>Đơn vị</th>
                        <th>Loại nâng bậc</th>
                        <th>Hiện tại → sau nâng</th>
                        <th>Ngày đến kỳ</th>
                        <th>Điều kiện</th>
                        <th>Trạng thái xử lý</th>
                      </tr>
                    </thead>
                    <tbody>
                      {rows.length === 0 ? (
                        <tr>
                          <td colSpan={8} className="mute">
                            Không có người đến kỳ phù hợp bộ lọc.
                          </td>
                        </tr>
                      ) : null}
                      {rows.map((r) => {
                        const [a, b] = fromTo(r);
                        return (
                          <tr key={r.key}>
                            <td>
                              <input
                                type="checkbox"
                                aria-label={`Chọn ${r.name || r.code}`}
                                checked={r.status !== "pending"}
                                disabled={
                                  act.busy ||
                                  r.kind === "nlth" ||
                                  r.status === "decided" ||
                                  r.status === "exported"
                                }
                                onChange={() => toggle(r)}
                              />
                            </td>
                            <td>
                              <Link href={caseHref(r.key)}>
                                {r.name || r.code}
                              </Link>
                            </td>
                            <td>{r.unit_name || r.unit}</td>
                            <td>{KIND_LABEL[r.kind]}</td>
                            <td>
                              {a} → {b}
                            </td>
                            <td>{vnDate(r.due)}</td>
                            <td>
                              <CondPill c={r.cond} why={r.why} />
                              {r.reason ? (
                                <small className="mute"> · có lý do</small>
                              ) : null}
                              {r.planned_year ? (
                                <StatusPill tone="info">
                                  Dự kiến năm {r.planned_year}
                                </StatusPill>
                              ) : null}
                            </td>
                            <td>
                              <PromoStatusPill s={r.status} />
                              {r.note ? (
                                <div className="mute small">{r.note}</div>
                              ) : null}
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
                <p className="mute small" style={{ padding: "8px 14px" }}>
                  Ngày xét {vnDate(today)}.
                  {l.last_scan
                    ? ` Hệ thống tính mốc lần gần nhất ngày ${vnDate(l.last_scan.day)}.`
                    : ""}{" "}
                  Chọn người rồi “Xuất danh sách” để chia file Mẫu số 1, 2, 4
                  (không có luồng trình ký, phê duyệt trên hệ thống).
                </p>
              </div>
              {dlg === "export" ? (
                <ExportDialog
                  pa={pa}
                  rows={base.filter(
                    (r) => r.status === "listed" || r.status === "exported",
                  )}
                  onClose={() => setDlg("")}
                  onDone={() => {
                    setDlg("");
                    reload();
                  }}
                />
              ) : null}
            </>
          );
        }}
      </LoadState>
      {reasonFor ? (
        <ReasonDialog
          c={reasonFor}
          onClose={() => setReasonFor(null)}
          onOk={(reason) => {
            const k = reasonFor.key;
            setReasonFor(null);
            void act.run(
              () => pa.include([k], reason),
              "Đã thêm vào danh sách kèm lý do.",
            ).then(reload);
          }}
        />
      ) : null}
      {dlg === "nlth" ? (
        <NLTHDialog
          pa={pa}
          onClose={() => setDlg("")}
          onDone={() => {
            setDlg("");
            set({ ...reset, kind: "nlth", window: 6 });
            reload();
          }}
        />
      ) : null}
      {dlg === "files" ? (
        <FilesDialog pa={pa} onClose={() => setDlg("")} />
      ) : null}
    </>
  );
}

function ReasonDialog({
  c,
  onClose,
  onOk,
}: {
  c: PromoCase;
  onClose: () => void;
  onOk: (reason: string) => void;
}) {
  const [reason, setReason] = useState("");
  return (
    <Dialog
      title={`Thêm vào danh sách · ${c.name || c.code}`}
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={!reason.trim()}
          onClick={() => onOk(reason.trim())}
        >
          Thêm vào danh sách
        </button>
      }
    >
      <div className="alert alert-warn" role="note">
        <span>
          Người này <b>{COND_LABEL[c.cond].toLowerCase()}</b> ({c.why.join(", ")}
          ). Thêm vào danh sách phải có lý do.
        </span>
      </div>
      <label className="small" style={{ display: "block" }}>
        Lý do thêm vào danh sách
        <textarea
          aria-label="Lý do thêm vào danh sách"
          rows={3}
          style={{ width: "100%" }}
          value={reason}
          onChange={(e) => setReason(e.target.value)}
        />
      </label>
    </Dialog>
  );
}

function vnMonthNext(): string {
  const d = new Date(Date.now() + 7 * 3_600_000);
  d.setUTCMonth(d.getUTCMonth() + 1);
  return d.toISOString().slice(0, 7);
}

function ExportDialog({
  pa,
  rows,
  onClose,
  onDone,
}: {
  pa: PromotionApi;
  rows: PromoCase[];
  onClose: () => void;
  onDone: () => void;
}) {
  const [month, setMonth] = useState(vnMonthNext());
  const [signer, setSigner] = useState<"Giám đốc" | "Phó Giám đốc">("Giám đốc");
  const [pick, setPick] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(rows.filter((r) => r.status === "listed").map((r) => [r.key, true])),
  );
  const [pct, setPct] = useState<Record<string, string>>({});
  const [open, setOpen] = useState("");
  const act = useAction();
  const sel = rows.filter((r) => pick[r.key]);
  const groups = (["nltx", "nlth", "tnvk"] as PromoKind[])
    .map((k) => ({ k, rows: sel.filter((r) => r.kind === k) }))
    .filter((g) => g.rows.length);
  return (
    <Dialog
      title="Xuất danh sách nâng bậc"
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={act.busy || !sel.length || !month}
          onClick={() =>
            void act
              .run(async () => {
                const r = await pa.exportLists({
                  keys: sel.map((x) => x.key),
                  review_month: month,
                  signer_title: signer,
                  tnvk_pct: pct,
                });
                for (const x of r.items) {
                  const f = await pa.file(x.id);
                  saveBlob(f.blob, f.name);
                }
                return r;
              }, (r) => `Đã xuất ${r.items.length} file. Trạng thái người được chọn: “Đã xuất danh sách”.`)
              .then((r) => r && onDone())
          }
        >
          Xuất và tải xuống tất cả
        </button>
      }
    >
      {act.view}
      <p>
        Không cần chọn mẫu: hệ thống chia {sel.length} người đã chọn thành từng
        file đúng mẫu của Sở. Người lập là hồ sơ gắn tài khoản của bạn.
      </p>
      <div className="row">
        <label className="small">
          Tháng xét{" "}
          <input
            type="month"
            aria-label="Tháng xét"
            value={month}
            onChange={(e) => setMonth(e.target.value)}
          />
        </label>
        <label className="small">
          Người ký{" "}
          <select
            aria-label="Người ký"
            value={signer}
            onChange={(e) => setSigner(e.target.value as typeof signer)}
          >
            <option>Giám đốc</option>
            <option>Phó Giám đốc</option>
          </select>
        </label>
      </div>
      <ul className="plain">
        {rows.map((r) => (
          <li key={r.key}>
            <label>
              <input
                type="checkbox"
                checked={!!pick[r.key]}
                onChange={(e) => setPick((p) => ({ ...p, [r.key]: e.target.checked }))}
              />{" "}
              {r.name || r.code} · {KIND_LABEL[r.kind]} · {vnDate(r.due)}
              {r.status === "exported" ? " (đã xuất — xuất lại)" : ""}
            </label>
          </li>
        ))}
      </ul>
      {groups.map((g) => {
        const name = `MauSo${g.k === "nltx" ? 1 : g.k === "nlth" ? 2 : 4}_T${month.slice(5)}_${month.slice(0, 4)}.xlsx`;
        const shown = open === g.k || g.k === "tnvk";
        return (
          <div key={g.k} className="card" style={{ marginTop: 8 }}>
            <div className="row" style={{ justifyContent: "space-between" }}>
              <div>
                <b>{KIND_FORM[g.k]}</b>{" "}
                <span className="mute small">
                  {g.rows.length} người · {name}
                </span>
              </div>
              {g.k !== "tnvk" ? (
                <button
                  type="button"
                  className="btn btn-o btn-sm"
                  onClick={() => setOpen(open === g.k ? "" : g.k)}
                >
                  {open === g.k ? "Ẩn xem trước" : "Xem trước"}
                </button>
              ) : null}
            </div>
            {g.k === "tnvk" ? (
              <p className="mute small">
                Nhập “% TNVK đang hưởng” (không lưu trong hồ sơ; để trống hoặc 0
                nếu hưởng lần đầu — dùng % khởi điểm).
              </p>
            ) : null}
            {g.k === "nlth" && g.rows.some((r) => r.blocked.length) ? (
              <div className="alert alert-warn" role="note">
                <span>
                  Không xuất được Mẫu số 2:{" "}
                  {g.rows
                    .filter((r) => r.blocked.length)
                    .map((r) => `${r.name || r.code} (${r.blocked.join(", ")})`)
                    .join("; ")}
                </span>
              </div>
            ) : null}
            {shown ? (
              <div className="tbl-wrap">
                <table className="dt">
                  <caption className="sr-only">Xem trước {KIND_FORM[g.k]}</caption>
                  <thead>
                    <tr>
                      <th>STT</th>
                      <th>Họ và tên - đơn vị</th>
                      <th>Hiện giữ</th>
                      <th>Sau nâng</th>
                      <th>{g.k === "tnvk" ? "% TNVK đang hưởng" : "Hệ số chênh lệch"}</th>
                      <th>{g.k === "nlth" ? "Thành tích · số tháng" : "Thời gian tính lần sau"}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {g.rows.map((r, i) => {
                      const [a, b] = fromTo(r);
                      return (
                        <tr key={r.key}>
                          <td>{i + 1}</td>
                          <td>
                            {r.name || r.code} · {r.unit_name || r.unit}
                          </td>
                          <td>{a}</td>
                          <td>{b}</td>
                          <td>
                            {g.k === "tnvk" ? (
                              <input
                                type="number"
                                min={0}
                                max={100}
                                style={{ width: 90 }}
                                aria-label={`% TNVK đang hưởng của ${r.name || r.code}`}
                                value={pct[r.key] ?? r.pct_from ?? ""}
                                onChange={(e) => setPct((p) => ({ ...p, [r.key]: e.target.value }))}
                              />
                            ) : (
                              vnNum(r.diff)
                            )}
                          </td>
                          <td>
                            {g.k === "nlth"
                              ? `${r.award?.label ?? ""} · ${r.early_months} tháng`
                              : vnDate(r.next_due ?? "")}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            ) : null}
          </div>
        );
      })}
    </Dialog>
  );
}

function NLTHDialog({
  pa,
  onClose,
  onDone,
}: {
  pa: PromotionApi;
  onClose: () => void;
  onDone: () => void;
}) {
  const pool = useLoad(() => pa.pool(3), []);
  const [sel, setSel] = useState<Record<string, string>>({}); // staff → award
  const [reason, setReason] = useState("");
  const act = useAction();
  return (
    <Dialog
      title="Lập danh sách nâng bậc trước hạn (NLTH)"
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={act.busy || !Object.keys(sel).length}
          onClick={() =>
            void act
              .run(
                () =>
                  pa.propose(
                    Object.entries(sel).map(([staff_id, award_event_id]) => ({
                      staff_id,
                      award_event_id,
                    })),
                    reason.trim() || undefined,
                  ),
                (r) => `Đã đưa ${r.keys.length} người vào danh sách nâng bậc trước hạn.`,
              )
              .then((r) => r && onDone())
          }
        >
          Đưa vào danh sách
        </button>
      }
    >
      {act.view}
      <LoadState s={pool}>
        {(p: NLTHPool) => {
          const chosen = p.rows.flatMap((r) =>
            r.options.filter((o) => sel[r.staff_id] === o.award.event_id),
          );
          const nowYear = chosen.filter((o) => !o.planned_year).length;
          const needReason = chosen.some((o) => o.cond !== "ok");
          return (
            <>
              <div className="alert alert-info" role="note">
                <span>
                  Chỉ tiêu NLTH năm {p.quota.year}: đã có quyết định{" "}
                  <strong>{p.quota.used}</strong>, đang trong danh sách{" "}
                  <strong>{p.quota.pending}</strong>, đang chọn thêm{" "}
                  <strong>{nowYear}</strong>, tối đa{" "}
                  <strong>{p.quota.total}</strong>.
                </span>
              </div>
              <p className="small">
                Danh sách gồm viên chức đang công tác,{" "}
                <b>không gồm {p.excluded} người đang trong danh sách đến kỳ</b>{" "}
                (nâng bậc đúng hạn). Ngày nâng ở năm sau được lập dạng dự kiến.
              </p>
              <div className="tbl-wrap">
                <table className="dt">
                  <caption className="sr-only">Chọn người nâng bậc trước hạn</caption>
                  <thead>
                    <tr>
                      <th>Chọn</th>
                      <th>Họ và tên</th>
                      <th>Đơn vị</th>
                      <th>Bậc</th>
                      <th>Đến kỳ thường xuyên</th>
                      <th>Thành tích</th>
                      <th>Số tháng nâng trước</th>
                      <th>Ngày nâng trước hạn</th>
                      <th>Điều kiện</th>
                    </tr>
                  </thead>
                  <tbody>
                    {p.rows.map((r) => {
                      const cur =
                        r.options.find((o) => o.award.event_id === sel[r.staff_id]) ??
                        r.options[0];
                      return (
                        <tr key={r.staff_id}>
                          <td>
                            <input
                              type="checkbox"
                              aria-label={`Chọn ${r.name || r.code}`}
                              disabled={!cur}
                              checked={!!sel[r.staff_id]}
                              onChange={(e) =>
                                setSel((s) => {
                                  const n = { ...s };
                                  if (e.target.checked && cur) n[r.staff_id] = cur.award.event_id;
                                  else delete n[r.staff_id];
                                  return n;
                                })
                              }
                            />
                          </td>
                          <td>{r.name || r.code}</td>
                          <td>{r.unit_name}</td>
                          <td>{r.steps ? `${r.step}/${r.steps}` : "—"}</td>
                          <td>{r.normal_due ? vnDate(r.normal_due) : "—"}</td>
                          <td>
                            {r.block ? (
                              <span className="mute">{r.block}</span>
                            ) : r.options.length > 1 ? (
                              <select
                                aria-label={`Thành tích của ${r.name || r.code}`}
                                value={cur?.award.event_id}
                                onChange={(e) =>
                                  setSel((s) =>
                                    s[r.staff_id] ? { ...s, [r.staff_id]: e.target.value } : s,
                                  )
                                }
                              >
                                {r.options.map((o) => (
                                  <option key={o.award.event_id} value={o.award.event_id}>
                                    {o.award.label}
                                  </option>
                                ))}
                              </select>
                            ) : (
                              cur?.award.label
                            )}
                          </td>
                          <td>{cur ? cur.early_months : "—"}</td>
                          <td>{cur ? vnDate(cur.due) : "—"}</td>
                          <td>
                            {cur ? <CondPill c={cur.cond} why={cur.why} /> : "—"}
                            {cur?.planned_year ? (
                              <StatusPill tone="info">Dự kiến năm {cur.planned_year}</StatusPill>
                            ) : null}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
              {needReason ? (
                <label className="small" style={{ display: "block" }}>
                  Lý do đề xuất người chưa đủ điều kiện
                  <textarea
                    aria-label="Lý do đề xuất người chưa đủ điều kiện"
                    rows={3}
                    style={{ width: "100%" }}
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                  />
                </label>
              ) : null}
            </>
          );
        }}
      </LoadState>
    </Dialog>
  );
}

function FilesDialog({ pa, onClose }: { pa: PromotionApi; onClose: () => void }) {
  const xs = useLoad(() => pa.exports(), []);
  const act = useAction();
  return (
    <Dialog title="File đã xuất" onClose={onClose}>
      {act.view}
      <LoadState s={xs}>
        {(l) =>
          l.items.length === 0 ? (
            <p>Chưa có file nào được xuất. Chọn người và bấm “Xuất danh sách đã chọn”.</p>
          ) : (
            <div className="tbl-wrap">
              <table className="dt">
                <caption className="sr-only">File đã xuất</caption>
                <thead>
                  <tr>
                    <th>Tên file</th>
                    <th>Mẫu</th>
                    <th>Số người</th>
                    <th>Thời điểm xuất</th>
                    <th>Quyết định</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {l.items.map((x) => (
                    <tr key={x.id}>
                      <td>{x.file_name}</td>
                      <td>{FORM_LABEL[x.form]}</td>
                      <td>{x.count}</td>
                      <td>{new Date(x.recorded_at).toLocaleString("vi-VN")}</td>
                      <td>{x.decision_no || <span className="mute">Chưa có</span>}</td>
                      <td>
                        <button
                          type="button"
                          className="btn btn-o btn-sm"
                          disabled={act.busy}
                          onClick={() =>
                            void act.run(
                              async () => {
                                const f = await pa.file(x.id);
                                saveBlob(f.blob, f.name);
                                return f;
                              },
                              (f) =>
                                f.drift
                                  ? "Đã tải lại — nội dung khác bản gốc (định danh đã sửa sau lần xuất)."
                                  : "Đã tải lại tệp đúng bản gốc.",
                            )
                          }
                        >
                          Tải lại
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        }
      </LoadState>
    </Dialog>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// FN-09 B3 — chi tiết một kỳ.
// --------------------------------------------------------------------------------------------------------------------

export function HrmPromotionCaseScreen() {
  const params = useParams<{ key: string }>();
  const key = decodeURIComponent(params.key);
  const pa = usePromotionApi();
  const [nonce, setNonce] = useState(0);
  const [reasonOpen, setReasonOpen] = useState(false);
  const [rating, setRating] = useState({ year: new Date().getFullYear() - 1, rating: "HTNV", reason: "" });
  const act = useAction();
  const d = useLoad(() => pa.detail(key), [key, nonce]);
  const reload = () => setNonce((n) => n + 1);
  return (
    <>
      <BackLink href={PROMO_BASE}>Danh sách nâng bậc</BackLink>
      {act.view}
      <LoadState s={d}>
        {(c) => {
          const [a, b] = fromTo(c);
          const label = KIND_LABEL[c.kind].toLowerCase();
          return (
            <>
              <PageHeader
                title={`${c.name || c.code} · ${label}`}
                badge={<PromoStatusPill s={c.status} />}
                actions={
                  <div className="row">
                    {c.kind !== "nlth" && c.status === "pending" ? (
                      <button
                        type="button"
                        className="btn btn-p btn-sm"
                        disabled={act.busy}
                        onClick={() =>
                          c.cond === "ok"
                            ? void act.run(() => pa.include([c.key]), "Đã đưa vào danh sách.").then(reload)
                            : setReasonOpen(true)
                        }
                      >
                        {c.cond === "ok" ? "Chọn để xuất danh sách" : "Thêm vào danh sách (nhập lý do)"}
                      </button>
                    ) : null}
                    {c.kind !== "nlth" && c.status === "listed" ? (
                      <button
                        type="button"
                        className="btn btn-o btn-sm"
                        disabled={act.busy}
                        onClick={() => void act.run(() => pa.exclude([c.key]), "Đã bỏ khỏi danh sách.").then(reload)}
                      >
                        Bỏ khỏi danh sách xuất
                      </button>
                    ) : null}
                    {c.kind === "nlth" && c.status === "listed" ? (
                      <button
                        type="button"
                        className="btn btn-o btn-sm"
                        disabled={act.busy}
                        onClick={() =>
                          void act.run(
                            () => pa.withdraw([c.key]),
                            "Đã gỡ khỏi danh sách trước hạn (thành tích vẫn giữ trong hồ sơ).",
                          ).then(reload)
                        }
                      >
                        Gỡ khỏi danh sách trước hạn
                      </button>
                    ) : null}
                    <Link className="btn btn-o btn-sm" href={`/kn/nhan-su/ho-so/${c.staff_id}`}>
                      Mở hồ sơ
                    </Link>
                  </div>
                }
              >
                {c.code} · {c.unit_name || c.unit}
              </PageHeader>
              <div className="card">
                <h2 className="h3">Kỳ nâng bậc</h2>
                <dl className="kv">
                  <dt>Ngạch</dt>
                  <dd>{c.grade_name || c.grade}</dd>
                  <dt>{c.kind === "nlth" ? "Ngày nâng trước hạn" : "Ngày đến kỳ"}</dt>
                  <dd>{vnDate(c.due)}</dd>
                  <dt>Hiện tại</dt>
                  <dd>{a}</dd>
                  <dt>Sau nâng</dt>
                  <dd>{b}</dd>
                  <dt>Điều kiện</dt>
                  <dd>
                    <CondPill c={c.cond} why={c.why} />
                  </dd>
                  {c.reason ? (
                    <>
                      <dt>Lý do thêm vào danh sách</dt>
                      <dd>{c.reason}</dd>
                    </>
                  ) : null}
                  {c.kind === "nltx" ? (
                    <>
                      <dt>Số tháng được hưởng bậc mới (gợi ý)</dt>
                      <dd>{vnNum(c.months_enjoyed)}</dd>
                    </>
                  ) : null}
                  {c.kind === "nlth" ? (
                    <>
                      <dt>Số tháng nâng trước thời hạn</dt>
                      <dd>{c.early_months}</dd>
                    </>
                  ) : null}
                  <dt>Xuất theo</dt>
                  <dd>{KIND_FORM[c.kind]}</dd>
                  {c.note ? (
                    <>
                      <dt>Ghi chú</dt>
                      <dd>{c.note}</dd>
                    </>
                  ) : null}
                  {c.decision_no ? (
                    <>
                      <dt>Quyết định</dt>
                      <dd>
                        <Link href={`/kn/nhan-su/quyet-dinh-nang-bac/${c.decision_id}`}>{c.decision_no}</Link>
                      </dd>
                    </>
                  ) : null}
                </dl>
              </div>
              <div className="card dt-card">
                <div className="tbl-wrap">
                  <table className="dt">
                    <caption>Căn cứ điều kiện</caption>
                    <thead>
                      <tr>
                        <th>Tiêu chí</th>
                        <th>Giá trị</th>
                        <th>Kết quả</th>
                      </tr>
                    </thead>
                    <tbody>
                      {c.basis.map((x, i) => (
                        <tr key={i}>
                          <td>{x.label}</td>
                          <td>{x.value}</td>
                          <td>
                            <CondPill c={x.result} why={x.note ? [x.note] : undefined} />
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
              <div className="grid2">
                <div className="card">
                  <h2 className="h3">Xếp loại năm</h2>
                  {c.ratings.length === 0 ? (
                    <p className="mute small">Chưa có xếp loại (FN-18 hoặc dữ liệu chuyển đổi ban đầu).</p>
                  ) : (
                    <ul className="plain">
                      {c.ratings.map((r) => (
                        <li key={r.id}>
                          {r.year}: <strong>{r.rating}</strong>{" "}
                          <span className="mute small">
                            ({r.source === "conversion" ? "chuyển đổi" : r.source === "evaluation" ? "đánh giá" : "nhập tay"})
                          </span>
                        </li>
                      ))}
                    </ul>
                  )}
                  <div className="row">
                    <input
                      type="number"
                      aria-label="Năm xếp loại"
                      style={{ width: 90 }}
                      value={rating.year}
                      onChange={(e) => setRating((r) => ({ ...r, year: Number(e.target.value) }))}
                    />
                    <select
                      aria-label="Xếp loại"
                      value={rating.rating}
                      onChange={(e) => setRating((r) => ({ ...r, rating: e.target.value }))}
                    >
                      {RATINGS.map((x) => (
                        <option key={x}>{x}</option>
                      ))}
                    </select>
                    <input
                      aria-label="Lý do sửa (khi năm đã có xếp loại)"
                      placeholder="Lý do sửa (nếu đã có)"
                      value={rating.reason}
                      onChange={(e) => setRating((r) => ({ ...r, reason: e.target.value }))}
                    />
                    <button
                      type="button"
                      className="btn btn-o btn-sm"
                      disabled={act.busy}
                      onClick={() =>
                        void act
                          .run(
                            () =>
                              pa.setRating(c.staff_id, {
                                year: rating.year,
                                rating: rating.rating,
                                source: "conversion",
                                ...(rating.reason ? { reason: rating.reason } : {}),
                              }),
                            "Đã ghi xếp loại năm.",
                          )
                          .then(reload)
                      }
                    >
                      Ghi xếp loại
                    </button>
                  </div>
                </div>
                <div className="card">
                  <h2 className="h3">Lịch sử</h2>
                  <ul className="plain">
                    <li>Hệ thống tính ngày đến kỳ và kiểm tra điều kiện</li>
                    {c.history.map((h) => (
                      <li key={h.id}>
                        {new Date(h.recorded_at).toLocaleString("vi-VN")} · {HIST_LABEL[h.action] ?? h.action}
                        {h.reason ? ` — ${h.reason}` : ""}
                      </li>
                    ))}
                  </ul>
                </div>
              </div>
              {reasonOpen ? (
                <ReasonDialog
                  c={c}
                  onClose={() => setReasonOpen(false)}
                  onOk={(r) => {
                    setReasonOpen(false);
                    void act.run(() => pa.include([c.key], r), "Đã thêm vào danh sách kèm lý do.").then(reload);
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

const HIST_LABEL: Record<string, string> = {
  include: "Đưa vào danh sách",
  exclude: "Bỏ khỏi danh sách",
  propose: "Đề xuất nâng bậc trước hạn",
  withdraw: "Gỡ khỏi danh sách trước hạn",
  export: "Đã xuất danh sách",
  decide: "Đã có quyết định",
  undecide: "Quyết định bị hủy",
  return: "Không có tên trong quyết định — trở lại bảng theo dõi",
};
