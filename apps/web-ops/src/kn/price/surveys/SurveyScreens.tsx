"use client";

// Màn Điều tra giá thành sản xuất theo vụ (W11.78; BRD Báo giá nông sản v1.3, bố cục theo prototype KhuyenNong_TTTT):
//   - SurveyScreen (Cấp Tỉnh · Điều tra giá thành): danh sách đợt → chi tiết đợt với các thẻ Tiến độ (FN-TI-02.10 — xã,
//     chỉ tiêu, phiếu đã nhập, cán bộ điều tra), Nhập từ file (FN-TI-02.11 — 3 bước Tải tệp – Ánh xạ – Xem trước & ghi
//     nhận), Rà soát phiếu (FN-TI-02.12 — thẻ chỉ số, phiếu có cờ tô đỏ, xác minh, duyệt / loại, duyệt hàng loạt, chốt đợt),
//     Lần nhập (tải danh sách lỗi).
//   - CostReportScreen (Cấp TW, Cấp Tỉnh · Giá thành sản xuất — FN-TW-01.04): chỉ số tổng hợp từ đợt đã chốt, không dữ
//     liệu cá nhân.
// Bố cục co giãn (máy tính / điện thoại): hàng lọc, thẻ chỉ số tự xuống dòng; bảng cuộn ngang. Tiền là chuỗi decimal.
import { useMemo, useState, type ReactNode } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { formatDateVi } from "@/kn/grid/decimal";
import { saveBlob } from "@/lib/files";
import { createPricePointApi } from "../points/api";
import { createPriceItemApi } from "../items/api";
import { money, vnTime, vnToday } from "../tickets/api";
import { signedPct } from "../reports/api";
import {
  COST_KEYS,
  COST_LABEL,
  CROP_LABEL,
  CROPS,
  createSurveyApi,
  FLAG_LABEL,
  FORM_STATUS,
  ROUND_STATUS,
  SEASON_LABEL,
  type CostQuery,
  type Crop,
  type FormStatus,
  type ImportParts,
  type SurveyApi,
  type SurveyCostBlock,
  type SurveyForm,
  type SurveyFormList,
  type SurveyFormRow,
  type SurveyImportPreview,
  type SurveyRoundDetail,
  type SurveyRoundInput,
} from "./api";

export function useSurveyApi(): SurveyApi {
  const { api } = useAuth();
  return useMemo(() => createSurveyApi(api), [api]);
}

type Ref = { code: string; name: string };

const RED_BG = "var(--danger-50, #fff4f3)";

/** Bảng đơn giản (cuộn ngang trên màn hẹp); rowStyle tô dòng (phiếu có cờ). */
function Grid<T>({
  caption,
  head,
  rows,
  rowKey,
  cells,
  rowStyle,
  empty = "Không có dữ liệu phù hợp.",
}: {
  caption: string;
  head: string[];
  rows: T[];
  rowKey: (r: T) => string;
  cells: (r: T) => ReactNode[];
  rowStyle?: (r: T) => React.CSSProperties | undefined;
  empty?: ReactNode;
}) {
  return (
    <div className="card dt-card">
      <div className="tbl-wrap">
        <table className="dt">
          <caption className="sr-only">{caption}</caption>
          <thead>
            <tr>
              {head.map((h) => (
                <th key={h} scope="col">
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.length ? (
              rows.map((r) => (
                <tr key={rowKey(r)} style={rowStyle?.(r)}>
                  {cells(r).map((c, i) => (
                    <td key={i}>{c}</td>
                  ))}
                </tr>
              ))
            ) : (
              <tr>
                <td colSpan={head.length} className="empty">
                  {empty}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function Kpi({
  label,
  value,
  tone = "neutral",
  onClick,
}: {
  label: string;
  value: ReactNode;
  tone?: "neutral" | "ok" | "warn" | "bad" | "info";
  onClick?: () => void;
}) {
  const body = (
    <>
      <div className="muted" style={{ fontSize: 12 }}>
        {label}
      </div>
      <StatusPill tone={tone}>{value}</StatusPill>
    </>
  );
  return onClick ? (
    <button
      type="button"
      className="card"
      style={{ padding: "8px 12px", minWidth: 120, textAlign: "left" }}
      onClick={onClick}
    >
      {body}
    </button>
  ) : (
    <div className="card" style={{ padding: "8px 12px", minWidth: 120 }}>
      {body}
    </div>
  );
}

function Pick({
  id,
  label,
  value,
  onChange,
  options,
  all = "Tất cả",
}: {
  id: string;
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: Ref[];
  all?: string | null;
}) {
  return (
    <span className="row" style={{ gap: 6, alignItems: "center" }}>
      <label htmlFor={id}>{label}</label>
      <select
        id={id}
        className="input"
        style={{ width: 170, maxWidth: "100%" }}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        {all !== null ? <option value="">{all}</option> : null}
        {options.map((o) => (
          <option key={o.code} value={o.code}>
            {o.name}
          </option>
        ))}
      </select>
    </span>
  );
}

const CROP_REFS: Ref[] = CROPS.map((c) => ({ code: c, name: CROP_LABEL[c]! }));
const wrapRow = { flexWrap: "wrap" as const, gap: 8, marginBottom: 12 };

function roundPill(st: string) {
  const s = ROUND_STATUS[st] ?? { label: st, tone: "neutral" as const };
  return <StatusPill tone={s.tone}>{s.label}</StatusPill>;
}

function formPill(st: string) {
  const s = FORM_STATUS[st] ?? { label: st, tone: "neutral" as const };
  return <StatusPill tone={s.tone}>{s.label}</StatusPill>;
}

function download(act: ReturnType<typeof useAction>, f: () => Promise<{ blob: Blob; name: string }>) {
  return act.run(async () => {
    const x = await f();
    saveBlob(x.blob, x.name);
  });
}

// ---------------------------------------------------------------------------------------------------------------------
// FN-TI-02.10 — Đợt điều tra
// ---------------------------------------------------------------------------------------------------------------------

export function SurveyScreen() {
  const sa = useSurveyApi();
  const [open, setOpen] = useState<string | null>(null);
  const [create, setCreate] = useState(false);
  const list = useLoad(() => sa.rounds(), [open, create]);
  if (open) return <RoundScreen id={open} onBack={() => setOpen(null)} onOpen={setOpen} />;
  return (
    <>
      <PageHeader
        title="Điều tra giá thành sản xuất"
        actions={
          <button type="button" className="btn btn-primary" onClick={() => setCreate(true)}>
            + Tạo đợt
          </button>
        }
      >
        Cán bộ điều tra thu phiếu bằng Google Form / Excel; Cấp Tỉnh nhập tệp vào đợt, rà soát phiếu có cờ bất thường rồi chốt
        đợt. Họ tên, số điện thoại nông hộ chỉ Cấp Tỉnh xem.
      </PageHeader>
      <LoadState s={list}>
        {(d) => (
          <>
            <div className="row" style={wrapRow}>
              <Kpi label="Số đợt" value={d.items.length} />
              <Kpi label="Đang thu thập" tone="ok" value={d.items.filter((r) => r.status === "collecting").length} />
              <Kpi label="Đang rà soát" tone="warn" value={d.items.filter((r) => r.status === "reviewing").length} />
              <Kpi label="Đã chốt" tone="info" value={d.items.filter((r) => r.status === "closed").length} />
            </div>
            <Grid
              caption="Đợt điều tra giá thành"
              head={["Tên đợt", "Vụ · năm", "Thời gian thu thập", "Nhóm cây", "Số xã", "Phiếu đã nhập / chỉ tiêu", "Trạng thái", ""]}
              rows={d.items}
              rowKey={(r) => r.id}
              empty="Chưa có đợt điều tra."
              cells={(r) => [
                r.name,
                `${SEASON_LABEL[r.season] ?? r.season} ${r.year}`,
                `${formatDateVi(r.collect_from ?? "")} – ${formatDateVi(r.collect_to ?? "")}`,
                r.crops.map((c) => CROP_LABEL[c]).join(", "),
                r.communes,
                `${r.forms} / ${r.target}`,
                roundPill(r.status),
                <button key="o" type="button" className="btn" onClick={() => setOpen(r.id)}>
                  Mở
                </button>,
              ]}
            />
          </>
        )}
      </LoadState>
      {create ? (
        <RoundDialog
          onClose={() => setCreate(false)}
          onSaved={(id) => {
            setCreate(false);
            setOpen(id);
          }}
        />
      ) : null}
    </>
  );
}

type Tab = "progress" | "import" | "review" | "imports";

function RoundScreen({ id, onBack, onOpen }: { id: string; onBack: () => void; onOpen: (id: string) => void }) {
  const sa = useSurveyApi();
  const [nonce, setNonce] = useState(0);
  const d = useLoad(() => sa.round(id), [id, nonce]);
  const [tab, setTab] = useState<Tab>("progress");
  const [edit, setEdit] = useState(false);
  const [ask, setAsk] = useState<"extend" | "reopen" | null>(null);
  const act = useAction();
  const reload = () => setNonce((n) => n + 1);
  const run = (body: Parameters<SurveyApi["act"]>[1], ok: string) =>
    void act.run(() => sa.act(id, body), ok).then((r) => r && reload());
  return (
    <LoadState s={d}>
      {(rd) => {
        const st = rd.round.status;
        const live = st === "collecting" || st === "reviewing";
        return (
          <>
            <PageHeader
              title={rd.round.name}
              badge={roundPill(st)}
              actions={
                <>
                  <button type="button" className="btn" onClick={onBack}>
                    ← Danh sách đợt
                  </button>
                  {rd.can_edit ? (
                    <button type="button" className="btn" onClick={() => setEdit(true)}>
                      Sửa đợt
                    </button>
                  ) : null}
                  {rd.can_edit && st === "draft" ? (
                    <button type="button" className="btn btn-primary" disabled={act.busy} onClick={() => run({ op: "open", seq: rd.round.seq }, "Đã mở đợt: Đang thu thập")}>
                      Mở đợt
                    </button>
                  ) : null}
                  {rd.can_edit && st === "collecting" ? (
                    <button type="button" className="btn" disabled={act.busy} onClick={() => run({ op: "close_early", seq: rd.round.seq }, "Đợt chuyển Đang rà soát")}>
                      Đóng sớm
                    </button>
                  ) : null}
                  {rd.can_edit && live ? (
                    <button type="button" className="btn" onClick={() => setAsk("extend")}>
                      Gia hạn
                    </button>
                  ) : null}
                  {rd.can_reopen ? (
                    <button type="button" className="btn" onClick={() => setAsk("reopen")}>
                      Mở lại đợt
                    </button>
                  ) : null}
                  {rd.can_edit || st === "closed" ? (
                    <button
                      type="button"
                      className="btn"
                      disabled={act.busy}
                      onClick={() => void act.run(() => sa.clone(id), "Đã nhân bản đợt").then((r) => r && onOpen(r.round.id))}
                    >
                      Nhân bản
                    </button>
                  ) : null}
                </>
              }
            >
              {`${SEASON_LABEL[rd.round.season] ?? ""} ${rd.round.year} · thu thập ${formatDateVi(rd.round.collect_from ?? "")} – ${formatDateVi(
                rd.round.collect_to ?? "",
              )} · ${rd.round.crops.map((c) => CROP_LABEL[c]).join(", ")}`}
            </PageHeader>
            {act.view}
            {rd.warnings.map((w) => (
              <div key={w} className="alert alert-warn" role="note">
                {w}
              </div>
            ))}
            <div className="dtabs" role="tablist" aria-label="Đợt điều tra">
              {(
                [
                  { id: "progress", label: "Tiến độ" },
                  { id: "import", label: "Nhập từ file" },
                  { id: "review", label: "Rà soát phiếu" },
                  { id: "imports", label: "Lần nhập" },
                ] as const
              ).map((t) => (
                <button key={t.id} type="button" role="tab" aria-selected={tab === t.id} onClick={() => setTab(t.id)}>
                  {t.label}
                </button>
              ))}
            </div>
            {tab === "progress" ? <ProgressPanel d={rd} /> : null}
            {tab === "import" ? (
              live ? (
                <ImportPanel round={id} onDone={() => { reload(); setTab("review"); }} />
              ) : (
                <div className="alert alert-info">Chỉ nhập tệp khi đợt Đang thu thập hoặc Đang rà soát.</div>
              )
            ) : null}
            {tab === "review" ? <ReviewPanel round={id} onChanged={reload} /> : null}
            {tab === "imports" ? <ImportsPanel round={id} /> : null}
            {edit ? (
              <RoundDialog
                current={rd}
                onClose={() => setEdit(false)}
                onSaved={() => {
                  setEdit(false);
                  reload();
                }}
              />
            ) : null}
            {ask ? (
              <ReasonDialog
                title={ask === "extend" ? "Gia hạn thời gian thu thập" : "Mở lại đợt đã chốt"}
                withDate={ask === "extend"}
                onClose={() => setAsk(null)}
                onOk={(reason, to) => {
                  setAsk(null);
                  run(
                    ask === "extend" ? { op: "extend", reason, collect_to: to, seq: rd.round.seq } : { op: "reopen", reason, seq: rd.round.seq },
                    ask === "extend" ? "Đã gia hạn" : "Đã mở lại đợt: Đang rà soát",
                  );
                }}
              />
            ) : null}
          </>
        );
      }}
    </LoadState>
  );
}

function ProgressPanel({ d }: { d: SurveyRoundDetail }) {
  return (
    <Grid
      caption="Tiến độ theo xã"
      head={["Xã", ...d.round.crops.map((c) => `${CROP_LABEL[c]} (đã nhập / chỉ tiêu)`), "Cán bộ điều tra"]}
      rows={d.communes}
      rowKey={(c) => c.code}
      empty="Chưa chọn xã điều tra."
      cells={(c) => [
        c.name || c.code,
        ...d.round.crops.map((cr) => `${c.imported[cr]} / ${c.targets[cr]}`),
        c.officers.length ? (
          c.officers.map((o) => `${o.full_name || "—"}${o.phone ? ` · ${o.phone}` : ""} (${o.forms} phiếu)`).join("; ")
        ) : (
          <StatusPill tone="warn">Chưa giao cán bộ</StatusPill>
        ),
      ]}
    />
  );
}

function ReasonDialog({
  title,
  withDate,
  onClose,
  onOk,
}: {
  title: string;
  withDate?: boolean;
  onClose: () => void;
  onOk: (reason: string, date: string) => void;
}) {
  const [reason, setReason] = useState("");
  const [date, setDate] = useState(vnToday());
  return (
    <Dialog
      title={title}
      onClose={onClose}
      actions={
        <>
          <button type="button" className="btn" onClick={onClose}>
            Hủy
          </button>
          <button type="button" className="btn btn-primary" disabled={!reason.trim()} onClick={() => onOk(reason.trim(), date)}>
            Xác nhận
          </button>
        </>
      }
    >
      {withDate ? (
        <>
          <label htmlFor="rs-date">Thu thập đến ngày</label>
          <input id="rs-date" type="date" className="input" value={date} onChange={(e) => setDate(e.target.value)} />
        </>
      ) : null}
      <label htmlFor="rs-reason">Lý do (bắt buộc)</label>
      <textarea id="rs-reason" className="input" rows={3} value={reason} onChange={(e) => setReason(e.target.value)} />
    </Dialog>
  );
}

type OfficerDraft = { id?: string; commune: string; full_name: string; phone: string; unit: string };

/** Tạo / sửa đợt — 3 bước: Thông tin – Xã & chỉ tiêu – Cán bộ điều tra. */
function RoundDialog({
  current,
  onClose,
  onSaved,
}: {
  current?: SurveyRoundDetail;
  onClose: () => void;
  onSaved: (id: string) => void;
}) {
  const sa = useSurveyApi();
  const { api } = useAuth();
  const units = useLoad(() => createPricePointApi(api).adminUnits(), [api]);
  const [step, setStep] = useState(1);
  const r = current?.round;
  const [name, setName] = useState(r?.name ?? "");
  const [season, setSeason] = useState(r?.season ?? "he_thu");
  const [year, setYear] = useState(String(r?.year ?? Number(vnToday().slice(0, 4))));
  const [from, setFrom] = useState(r?.collect_from ?? vnToday());
  const [to, setTo] = useState(r?.collect_to ?? vnToday());
  const [crops, setCrops] = useState<string[]>(r?.crops ?? []);
  const [note, setNote] = useState(current?.note ?? "");
  const [communes, setCommunes] = useState<{ code: string; name: string; targets: Record<string, number> }[]>(
    current?.communes.map((c) => ({ code: c.code, name: c.name, targets: { ...c.targets } })) ?? [],
  );
  const [officers, setOfficers] = useState<OfficerDraft[]>(
    current?.communes.flatMap((c) => c.officers.map((o) => ({ id: o.id, commune: o.commune, full_name: o.full_name, phone: o.phone, unit: o.unit }))) ?? [],
  );
  const [confirm, setConfirm] = useState(false);
  const act = useAction();
  const body = (open: boolean): SurveyRoundInput => ({
    name,
    season: season as SurveyRoundInput["season"],
    year: Number(year),
    collect_from: from,
    collect_to: to,
    crops: crops as SurveyRoundInput["crops"],
    note,
    communes: communes.map((c) => ({ code: c.code, targets: c.targets })),
    officers: officers.map((o) => (o.id ? { id: o.id, commune: o.commune, unit: o.unit } : { commune: o.commune, full_name: o.full_name, phone: o.phone, unit: o.unit })),
    confirm,
    open,
    seq: r?.seq ?? 0,
  });
  const save = (open: boolean) =>
    void act
      .run(() => (current ? sa.update(current.round.id, body(false)) : sa.create(body(open))), current ? "Đã lưu đợt" : open ? "Đã mở đợt" : "Đã lưu nháp")
      .then((res) => res && onSaved(res.round.id));
  const taken = new Set(communes.map((c) => c.code));
  return (
    <Dialog
      title={current ? "Sửa đợt điều tra" : "Tạo đợt điều tra"}
      onClose={onClose}
      actions={
        <>
          <button type="button" className="btn" onClick={step > 1 ? () => setStep(step - 1) : onClose}>
            {step > 1 ? "Quay lại" : "Hủy"}
          </button>
          {step < 3 ? (
            <button type="button" className="btn btn-primary" onClick={() => setStep(step + 1)}>
              Tiếp
            </button>
          ) : (
            <>
              <button type="button" className="btn" disabled={act.busy} onClick={() => save(false)}>
                {current ? "Lưu" : "Lưu nháp"}
              </button>
              {!current ? (
                <button type="button" className="btn btn-primary" disabled={act.busy} onClick={() => save(true)}>
                  Tạo và mở đợt
                </button>
              ) : null}
            </>
          )}
        </>
      }
    >
      <div className="dtabs" role="tablist" aria-label="Bước">
        {["Thông tin", "Xã & chỉ tiêu", "Cán bộ điều tra"].map((t, i) => (
          <button key={t} type="button" role="tab" aria-selected={step === i + 1} onClick={() => setStep(i + 1)}>
            {`${i + 1}. ${t}`}
          </button>
        ))}
      </div>
      {act.view}
      {step === 1 ? (
        <div style={{ display: "grid", gap: 8 }}>
          <label htmlFor="rd-name">Tên đợt</label>
          <input id="rd-name" className="input" value={name} maxLength={120} onChange={(e) => setName(e.target.value)} />
          <div className="row" style={wrapRow}>
            <Pick id="rd-season" label="Vụ" value={season} all={null} onChange={(v) => setSeason(v as typeof season)} options={Object.entries(SEASON_LABEL).map(([code, n]) => ({ code, name: n }))} />
            <span className="row" style={{ gap: 6 }}>
              <label htmlFor="rd-year">Năm</label>
              <input id="rd-year" className="input" style={{ width: 90 }} value={year} onChange={(e) => setYear(e.target.value)} />
            </span>
          </div>
          <div className="row" style={wrapRow}>
            <label htmlFor="rd-from">Thu thập từ</label>
            <input id="rd-from" type="date" className="input" value={from} onChange={(e) => setFrom(e.target.value)} />
            <label htmlFor="rd-to">đến</label>
            <input id="rd-to" type="date" className="input" value={to} onChange={(e) => setTo(e.target.value)} />
          </div>
          <fieldset>
            <legend>Nhóm cây áp dụng</legend>
            {CROPS.map((c) => (
              <label key={c} className="row" style={{ gap: 6, display: "inline-flex", marginRight: 12 }}>
                <input
                  type="checkbox"
                  checked={crops.includes(c)}
                  onChange={(e) => setCrops(e.target.checked ? [...crops, c] : crops.filter((x) => x !== c))}
                />
                {CROP_LABEL[c]}
              </label>
            ))}
          </fieldset>
          <label htmlFor="rd-note">Ghi chú</label>
          <textarea id="rd-note" className="input" rows={2} maxLength={500} value={note} onChange={(e) => setNote(e.target.value)} />
        </div>
      ) : null}
      {step === 2 ? (
        <div style={{ display: "grid", gap: 8 }}>
          <LoadState s={units}>
            {(u) => (
              <Pick
                id="rd-add"
                label="Thêm xã (ĐVHC)"
                value=""
                all="— Chọn xã —"
                onChange={(code) => {
                  const x = u.admin_units.find((a) => a.code === code);
                  if (x) setCommunes([...communes, { code, name: x.name, targets: {} }]);
                }}
                options={u.admin_units.filter((a) => !taken.has(a.code)).map((a) => ({ code: a.code, name: a.name }))}
              />
            )}
          </LoadState>
          <Grid
            caption="Xã và chỉ tiêu"
            head={["Xã", ...crops.map((c) => `Chỉ tiêu ${CROP_LABEL[c]}`), ""]}
            rows={communes}
            rowKey={(c) => c.code}
            empty="Chưa chọn xã điều tra."
            cells={(c) => [
              c.name || c.code,
              ...crops.map((cr) => (
                <input
                  key={cr}
                  aria-label={`Chỉ tiêu ${CROP_LABEL[cr]} ${c.name}`}
                  className="input"
                  style={{ width: 70 }}
                  inputMode="numeric"
                  value={String(c.targets[cr] ?? 0)}
                  onChange={(e) =>
                    setCommunes(communes.map((x) => (x.code === c.code ? { ...x, targets: { ...x.targets, [cr]: Number(e.target.value.replace(/\D/g, "")) || 0 } } : x)))
                  }
                />
              )),
              <button key="rm" type="button" className="btn" onClick={() => setCommunes(communes.filter((x) => x.code !== c.code))}>
                Bỏ
              </button>,
            ]}
          />
        </div>
      ) : null}
      {step === 3 ? (
        <div style={{ display: "grid", gap: 8 }}>
          <p className="muted" style={{ margin: 0 }}>
            Khai báo cán bộ điều tra cho từng xã (một hoặc nhiều; không bắt buộc có tài khoản) để đối chiếu cột “Tên cán bộ điều tra”
            khi nhập tệp. Họ tên, số điện thoại lưu ở kho định danh.
          </p>
          {communes.map((c) => (
            <section key={c.code} className="card" style={{ padding: 8 }}>
              <strong>{c.name || c.code}</strong>
              {officers
                .map((o, i) => ({ o, i }))
                .filter(({ o }) => o.commune === c.code)
                .map(({ o, i }) => (
                  <div key={i} className="row" style={wrapRow}>
                    <input aria-label="Họ tên cán bộ điều tra" className="input" placeholder="Họ tên" disabled={!!o.id} value={o.full_name} onChange={(e) => setOfficers(officers.map((x, j) => (j === i ? { ...x, full_name: e.target.value } : x)))} />
                    <input aria-label="Số điện thoại cán bộ điều tra" className="input" placeholder="Số điện thoại" disabled={!!o.id} value={o.phone} onChange={(e) => setOfficers(officers.map((x, j) => (j === i ? { ...x, phone: e.target.value } : x)))} />
                    <input aria-label="Đơn vị" className="input" placeholder="Đơn vị" value={o.unit} onChange={(e) => setOfficers(officers.map((x, j) => (j === i ? { ...x, unit: e.target.value } : x)))} />
                    <button type="button" className="btn" onClick={() => setOfficers(officers.filter((_, j) => j !== i))}>
                      Bỏ
                    </button>
                  </div>
                ))}
              <button type="button" className="btn" onClick={() => setOfficers([...officers, { commune: c.code, full_name: "", phone: "", unit: "TTKN An Giang" }])}>
                + Thêm cán bộ
              </button>
            </section>
          ))}
          <label className="row" style={{ gap: 6 }}>
            <input type="checkbox" checked={confirm} onChange={(e) => setConfirm(e.target.checked)} />
            Vẫn lưu nếu trùng vụ, năm, nhóm cây với đợt khác
          </label>
        </div>
      ) : null}
    </Dialog>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// FN-TI-02.11 — Nhập từ file (3 bước)
// ---------------------------------------------------------------------------------------------------------------------

const LEVEL_TONE: Record<string, "ok" | "warn" | "bad"> = { ok: "ok", warning: "warn", error: "bad" };
const LEVEL_LABEL: Record<string, string> = { ok: "Hợp lệ", warning: "Cảnh báo", error: "Lỗi" };

function ImportPanel({ round, onDone }: { round: string; onDone: () => void }) {
  const sa = useSurveyApi();
  const { api } = useAuth();
  const [file, setFile] = useState<File | null>(null);
  const [pv, setPv] = useState<SurveyImportPreview | null>(null);
  const [mapping, setMapping] = useState<Record<string, number> | undefined>(undefined);
  const [names, setNames] = useState<{ item: Record<string, string>; commune: Record<string, string> }>({ item: {}, commune: {} });
  const [save, setSave] = useState(true);
  const [step, setStep] = useState(1);
  const act = useAction();
  const items = useLoad(() => createPriceItemApi(api).items({ state: "active" }), [api]);
  const units = useLoad(() => createPricePointApi(api).adminUnits(), [api]);
  const parts = (): ImportParts => ({ file: file!, mapping, names, saveSynonyms: save });
  const preview = (next: number) =>
    void act.run(() => sa.preview(round, parts())).then((p) => {
      if (p) {
        setPv(p);
        setMapping(p.mapping);
        setStep(next);
      }
    });
  const commit = () =>
    void act
      .run(() => sa.commit(round, { ...parts(), sha256: pv?.sha256 }), (r) => `Đã ghi nhận ${r.created} phiếu · ${r.skipped} dòng trùng bỏ qua · ${r.errors} dòng lỗi`)
      .then((r) => r && onDone());
  return (
    <section>
      <div className="dtabs" role="tablist" aria-label="Các bước nhập">
        {["Tải tệp", "Ánh xạ", "Xem trước & ghi nhận"].map((t, i) => (
          <button key={t} type="button" role="tab" aria-selected={step === i + 1} disabled={i + 1 > step}>
            {`${i + 1}. ${t}`}
          </button>
        ))}
      </div>
      {act.view}
      {step === 1 ? (
        <div style={{ display: "grid", gap: 8 }}>
          <p className="muted" style={{ margin: 0 }}>
            Chọn tệp Excel / CSV (gồm tệp câu trả lời Google Form). Có thể nhập nhiều lần; nhập lại cùng tệp không tạo phiếu trùng.
          </p>
          <label htmlFor="imp-file">Tệp phiếu điều tra</label>
          <input id="imp-file" type="file" accept=".csv,.xlsx" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
          <div>
            <button type="button" className="btn btn-primary" disabled={!file || act.busy} onClick={() => preview(2)}>
              Đọc tệp
            </button>
          </div>
        </div>
      ) : null}
      {step === 2 && pv ? (
        <div style={{ display: "grid", gap: 8 }}>
          <p className="muted" style={{ margin: 0 }}>
            Hệ thống gợi ý ánh xạ cột theo tên cột của mẫu Google Form hiện hành. Tên mặt hàng, tên xã được chuẩn hóa (bỏ khoảng trắng,
            không phân biệt hoa thường, bảng từ đồng nghĩa).
          </p>
          <Grid
            caption="Ánh xạ cột"
            head={["Trường của biểu mẫu", "Cột của tệp"]}
            rows={pv.fields}
            rowKey={(f) => f.key}
            cells={(f) => [
              `${f.label}${f.required ? " *" : ""}`,
              <select
                key="s"
                aria-label={`Cột cho ${f.label}`}
                className="input"
                value={String(mapping?.[f.key] ?? -1)}
                onChange={(e) => setMapping({ ...(mapping ?? {}), [f.key]: Number(e.target.value) })}
              >
                <option value="-1">— Không dùng —</option>
                {pv.columns.map((c, i) => (
                  <option key={i} value={i}>
                    {c}
                  </option>
                ))}
              </select>,
            ]}
          />
          {(pv.unknown.item?.length ?? 0) + (pv.unknown.commune?.length ?? 0) > 0 ? (
            <section className="card" style={{ padding: 8 }}>
              <strong>Giá trị chưa khớp danh mục</strong>
              {(pv.unknown.item ?? []).map((v) => (
                <div key={`i-${v}`} className="row" style={wrapRow}>
                  <span>{`Mặt hàng “${v}”`}</span>
                  <LoadState s={items}>
                    {(l) => (
                      <Pick
                        id={`map-i-${v}`}
                        label="→"
                        value={names.item[v] ?? ""}
                        all="— Chọn mặt hàng —"
                        onChange={(code) => setNames({ ...names, item: { ...names.item, [v]: code } })}
                        options={l.items.map((x) => ({ code: x.code, name: `${x.name} ${x.variety}` }))}
                      />
                    )}
                  </LoadState>
                </div>
              ))}
              {(pv.unknown.commune ?? []).map((v) => (
                <div key={`c-${v}`} className="row" style={wrapRow}>
                  <span>{`Xã “${v}”`}</span>
                  <LoadState s={units}>
                    {(u) => (
                      <Pick
                        id={`map-c-${v}`}
                        label="→"
                        value={names.commune[v] ?? ""}
                        all="— Chọn xã —"
                        onChange={(code) => setNames({ ...names, commune: { ...names.commune, [v]: code } })}
                        options={u.admin_units.map((x) => ({ code: x.code, name: x.name }))}
                      />
                    )}
                  </LoadState>
                </div>
              ))}
              <label className="row" style={{ gap: 6 }}>
                <input type="checkbox" checked={save} onChange={(e) => setSave(e.target.checked)} />
                Lưu ánh xạ tên để dùng cho các lần nhập sau
              </label>
            </section>
          ) : null}
          <div className="row" style={wrapRow}>
            <button type="button" className="btn" onClick={() => setStep(1)}>
              Quay lại
            </button>
            <button type="button" className="btn btn-primary" disabled={act.busy} onClick={() => preview(3)}>
              Xem trước
            </button>
          </div>
        </div>
      ) : null}
      {step === 3 && pv ? (
        <div style={{ display: "grid", gap: 8 }}>
          {pv.missing.length ? <div className="alert alert-warn">{`Chưa ánh xạ cột bắt buộc: ${pv.missing.join(", ")}`}</div> : null}
          {pv.already_imported_at ? <div className="alert alert-info">{`Tệp này đã được ghi nhận lúc ${vnTime(pv.already_imported_at)} — dòng trùng sẽ bỏ qua.`}</div> : null}
          <div className="row" style={wrapRow}>
            <Kpi label="Hợp lệ" tone="ok" value={pv.ok} />
            <Kpi label="Cảnh báo" tone="warn" value={pv.warnings} />
            <Kpi label="Lỗi" tone={pv.errors ? "bad" : "neutral"} value={pv.errors} />
            <Kpi label="Trùng — bỏ qua" value={pv.skipped} />
          </div>
          <Grid
            caption="Xem trước"
            head={["Dòng", "Xã", "Nhóm cây", "Mặt hàng", "Giá thành / kg", "Kết quả", "Ghi chú"]}
            rows={pv.lines}
            rowKey={(l) => String(l.row)}
            rowStyle={(l) => (l.level === "error" ? { background: RED_BG } : l.level === "warning" ? { background: "#fffbec" } : undefined)}
            cells={(l) => [
              l.row,
              l.commune_name || "—",
              CROP_LABEL[l.crop] ?? "—",
              l.item_name || "—",
              money(l.cost_per_kg),
              <StatusPill key="lv" tone={LEVEL_TONE[l.level]}>{l.skip ? "Bỏ qua" : LEVEL_LABEL[l.level]}</StatusPill>,
              l.messages.join("; "),
            ]}
          />
          <p className="muted" style={{ margin: 0 }}>
            Chỉ ghi nhận dòng hợp lệ; dòng lỗi không làm hỏng cả lần nhập. Giá trị tính toán (tổng chi phí, giá thành) luôn do hệ thống
            tính lại theo CT-05.
          </p>
          <div className="row" style={wrapRow}>
            <button type="button" className="btn" onClick={() => setStep(2)}>
              Quay lại
            </button>
            <button type="button" className="btn btn-primary" disabled={act.busy || pv.missing.length > 0} onClick={commit}>
              Ghi nhận
            </button>
          </div>
        </div>
      ) : null}
    </section>
  );
}

function ImportsPanel({ round }: { round: string }) {
  const sa = useSurveyApi();
  const l = useLoad(() => sa.imports(round), [round]);
  const act = useAction();
  return (
    <LoadState s={l}>
      {(d) => (
        <>
          {act.view}
          <Grid
            caption="Lần nhập"
            head={["Lúc", "Tệp", "Số dòng", "Đã ghi", "Trùng bỏ qua", "Cảnh báo", "Lỗi", ""]}
            rows={d.items}
            rowKey={(i) => i.id}
            empty="Chưa nhập tệp nào."
            cells={(i) => [
              vnTime(i.at),
              i.file_name,
              i.rows,
              i.created,
              i.skipped,
              i.warnings,
              i.errors,
              i.errors + i.warnings ? (
                <button key="dl" type="button" className="btn" disabled={act.busy} onClick={() => void download(act, () => sa.issues(round, i.id))}>
                  Tải danh sách lỗi
                </button>
              ) : null,
            ]}
          />
        </>
      )}
    </LoadState>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// FN-TI-02.12 — Rà soát phiếu
// ---------------------------------------------------------------------------------------------------------------------

function ReviewPanel({ round, onChanged }: { round: string; onChanged: () => void }) {
  const sa = useSurveyApi();
  const [crop, setCrop] = useState("");
  const [item, setItem] = useState("");
  const [flag, setFlag] = useState<"" | "flag" | "noflag">("");
  const [status, setStatus] = useState("");
  const [nonce, setNonce] = useState(0);
  const [open, setOpen] = useState<string | null>(null);
  const act = useAction();
  const l = useLoad(
    () =>
      sa.forms(round, {
        crop: (crop || undefined) as Crop | undefined,
        item,
        flag: flag || undefined,
        status: (status || undefined) as FormStatus | undefined,
      }),
    [round, crop, item, flag, status, nonce],
  );
  const reload = () => {
    setNonce((n) => n + 1);
    onChanged();
  };
  return (
    <LoadState s={l}>
      {(d: SurveyFormList) => (
        <>
          {act.view}
          <div className="row" style={wrapRow}>
            <Pick id="rv-crop" label="Nhóm cây" value={crop} onChange={setCrop} options={CROP_REFS} />
            <Pick id="rv-item" label="Mặt hàng" value={item} onChange={setItem} options={d.items} />
            <Pick id="rv-flag" label="Cờ" value={flag} onChange={(v) => setFlag(v as "" | "flag" | "noflag")} options={[{ code: "flag", name: "Có cờ" }, { code: "noflag", name: "Không cờ" }]} />
            <Pick
              id="rv-status"
              label="Trạng thái"
              value={status}
              onChange={setStatus}
              options={Object.entries(FORM_STATUS)
                .filter(([k]) => k !== "deleted")
                .map(([code, s]) => ({ code, name: s.label }))}
            />
          </div>
          <div className="row" style={wrapRow}>
            <Kpi label="Đã gửi" value={d.kpis.sent} onClick={() => { setFlag(""); setStatus(""); }} />
            <Kpi label="Có cờ" tone="bad" value={d.kpis.flagged} onClick={() => setFlag("flag")} />
            <Kpi label="Chờ xác minh" tone="warn" value={d.kpis.verify} onClick={() => setStatus("verify")} />
            <Kpi label="Đã duyệt" tone="ok" value={d.kpis.approved} onClick={() => setStatus("approved")} />
            <Kpi label="Đã loại" value={d.kpis.rejected} onClick={() => setStatus("rejected")} />
          </div>
          <Grid
            caption="Phiếu điều tra"
            head={["Xã", "Cán bộ điều tra", "Nông hộ", "Mặt hàng", "Giá bán", "Trung vị đợt", "% lệch", "Giá thành / kg", "Cờ", "Trạng thái", ""]}
            rows={d.rows}
            rowKey={(r) => r.id}
            rowStyle={(r) => (r.flag && r.status !== "approved" && r.status !== "rejected" ? { background: RED_BG } : undefined)}
            empty="Không có phiếu phù hợp."
            cells={(r: SurveyFormRow) => [
              r.commune_name,
              r.officer_name || "—",
              r.farmer_name || "—",
              r.item_name,
              r.corrected ? `${money(r.price)} (gốc ${money(r.orig_price)})` : money(r.price),
              r.median ? money(r.median) : "— (dưới 5 phiếu)",
              r.pct ? signedPct(r.pct) : "—",
              money(r.cost_per_kg),
              r.flag ? <StatusPill key="f" tone="bad">{FLAG_LABEL[r.flag]}</StatusPill> : "",
              formPill(r.status),
              <button key="o" type="button" className="btn" onClick={() => setOpen(r.id)}>
                Xem
              </button>,
            ]}
          />
          {d.total > d.rows.length ? <p className="muted">{`Hiển thị ${d.rows.length} / ${d.total} phiếu — lọc để thu hẹp.`}</p> : null}
          {item && d.distribution.length ? (
            <Grid
              caption="Phân bố giá bán theo xã"
              head={["Xã", "Số phiếu", "Thấp nhất", "Cao nhất", "Trung vị"]}
              rows={d.distribution}
              rowKey={(x) => x.commune}
              cells={(x) => [x.commune_name, x.n, money(x.min), money(x.max), money(x.median)]}
            />
          ) : null}
          <div className="row" style={wrapRow}>
            <button type="button" className="btn" disabled={act.busy} onClick={() => void download(act, () => sa.formsExport(round))}>
              Xuất Excel (không gồm SĐT nông hộ)
            </button>
            {d.can_review ? (
              <>
                <button
                  type="button"
                  className="btn"
                  disabled={act.busy}
                  onClick={() => void act.run(() => sa.approveUnflagged(round), (r) => `Đã duyệt ${r.approved} phiếu không có cờ`).then((r) => r && reload())}
                >
                  Duyệt các phiếu không cờ
                </button>
                <button
                  type="button"
                  className="btn btn-primary"
                  disabled={act.busy || !d.can_close}
                  title={d.can_close ? undefined : `Còn ${d.kpis.pending} phiếu chờ rà soát hoặc chờ xác minh`}
                  onClick={() => void act.run(() => sa.act(round, { op: "close", seq: d.round.seq }), "Đã chốt đợt").then((r) => r && reload())}
                >
                  {d.can_close ? "Chốt đợt" : `Chốt đợt (còn ${d.kpis.pending})`}
                </button>
              </>
            ) : null}
          </div>
          {open ? <FormDialog round={round} id={open} canReview={d.can_review} onClose={() => setOpen(null)} onChanged={reload} /> : null}
        </>
      )}
    </LoadState>
  );
}

function FormDialog({
  round,
  id,
  canReview,
  onClose,
  onChanged,
}: {
  round: string;
  id: string;
  canReview: boolean;
  onClose: () => void;
  onChanged: () => void;
}) {
  const sa = useSurveyApi();
  const [f, setF] = useState<SurveyForm | null>(null);
  const l = useLoad(() => sa.form(round, id), [round, id]);
  const [note, setNote] = useState("");
  const [price, setPrice] = useState("");
  const [unable, setUnable] = useState(false);
  const act = useAction();
  const run = (op: "request_verify" | "record_verify" | "approve" | "reject" | "delete") =>
    void act
      .run(() => sa.formAct(round, id, { op, note, price: op === "record_verify" && price ? price : undefined, unverifiable: op === "record_verify" ? unable : undefined }), "Đã cập nhật phiếu")
      .then((r) => {
        if (r) {
          setF(r);
          setNote("");
          setPrice("");
          onChanged();
        }
      });
  return (
    <Dialog
      title="Phiếu điều tra"
      onClose={onClose}
      actions={
        <button type="button" className="btn" onClick={onClose}>
          Đóng
        </button>
      }
    >
      <LoadState s={l}>
        {(d0) => {
          const d = f ?? d0;
          const pending = d.status === "imported" || d.status === "verify" || d.status === "verified";
          return (
            <div style={{ display: "grid", gap: 8 }}>
              {act.view}
              <div>
                <strong>{`${d.item_name} · ${d.commune_name}`}</strong> {formPill(d.status)}{" "}
                {d.flag ? <StatusPill tone="bad">{`${FLAG_LABEL[d.flag]} ${signedPct(d.pct)}`}</StatusPill> : null}
              </div>
              <dl className="kv" style={{ display: "grid", gridTemplateColumns: "minmax(120px, max-content) 1fr", gap: "4px 12px", margin: 0 }}>
                <dt>Nông hộ</dt>
                <dd>{`${d.farmer_name || "—"}${d.farmer_phone ? ` · ${d.farmer_phone}` : ""}`}</dd>
                <dt>Cán bộ điều tra</dt>
                <dd>{`${d.officer_name || "—"}${d.officer_phone ? ` · ${d.officer_phone}` : ""}${d.officer_unit ? ` (${d.officer_unit})` : ""}`}</dd>
                <dt>Diện tích · sản lượng</dt>
                <dd>{`${money(d.area_ha)} ha · ${money(d.output_kg)} kg`}</dd>
                <dt>Giá bán</dt>
                <dd>{d.corrected ? `${money(d.price)} đ/kg (giá gốc ${money(d.orig_price)})` : `${money(d.price)} đ/kg`}</dd>
                {COST_KEYS.map((k) => (
                  <FragmentKV key={k} k={COST_LABEL[k]!} v={`${money(d.costs[k])} đ`} />
                ))}
                <dt>Tổng chi phí</dt>
                <dd>{`${money(d.calc.total_cost)} đ`}</dd>
                <dt>Giá thành</dt>
                <dd>{`${money(d.calc.cost_per_kg)} đ/kg`}</dd>
                <dt>Lợi nhuận</dt>
                <dd>{`${money(d.calc.profit)} đ · ${money(d.calc.profit_per_ha)} đ/ha${d.calc.margin_pct ? ` · ${money(d.calc.margin_pct)}%` : ""}`}</dd>
                {d.note ? (
                  <>
                    <dt>Ghi chú</dt>
                    <dd>{d.note}</dd>
                  </>
                ) : null}
              </dl>
              {canReview && d.status !== "deleted" ? (
                <section className="card" style={{ padding: 8, display: "grid", gap: 6 }}>
                  <label htmlFor="fm-note">Nội dung xác minh / ghi chú / lý do</label>
                  <textarea id="fm-note" className="input" rows={2} value={note} onChange={(e) => setNote(e.target.value)} />
                  {d.status === "verify" ? (
                    <div className="row" style={wrapRow}>
                      <label htmlFor="fm-price">Giá bán đúng (nếu có)</label>
                      <input id="fm-price" className="input" inputMode="decimal" style={{ width: 120 }} value={price} onChange={(e) => setPrice(e.target.value)} />
                      <label className="row" style={{ gap: 6 }}>
                        <input type="checkbox" checked={unable} onChange={(e) => setUnable(e.target.checked)} />
                        Không xác minh được
                      </label>
                    </div>
                  ) : null}
                  <div className="row" style={wrapRow}>
                    {d.status !== "verify" ? (
                      <button type="button" className="btn" disabled={act.busy} onClick={() => run("request_verify")}>
                        Cần xác minh
                      </button>
                    ) : (
                      <button type="button" className="btn btn-primary" disabled={act.busy} onClick={() => run("record_verify")}>
                        Ghi nhận xác minh
                      </button>
                    )}
                    {d.status === "imported" || d.status === "verified" ? (
                      <button type="button" className="btn btn-primary" disabled={act.busy} onClick={() => run("approve")}>
                        Duyệt
                      </button>
                    ) : null}
                    {pending ? (
                      <button type="button" className="btn" disabled={act.busy} onClick={() => run("reject")}>
                        Loại
                      </button>
                    ) : null}
                    <button type="button" className="btn" disabled={act.busy} onClick={() => run("delete")}>
                      Xóa phiếu nhập nhầm
                    </button>
                  </div>
                </section>
              ) : null}
              <details>
                <summary>Nhật ký phiếu</summary>
                <ul>
                  {d.log.map((g) => (
                    <li key={g.seq}>{`${vnTime(g.at)} · ${FORM_STATUS[g.status]?.label ?? g.status}${g.note ? ` · ${g.note}` : ""}${g.price ? ` · giá ${money(g.price)}` : ""}`}</li>
                  ))}
                </ul>
              </details>
            </div>
          );
        }}
      </LoadState>
    </Dialog>
  );
}

function FragmentKV({ k, v }: { k: string; v: string }) {
  return (
    <>
      <dt>{k}</dt>
      <dd>{v}</dd>
    </>
  );
}

// ---------------------------------------------------------------------------------------------------------------------
// FN-TW-01.04 — Báo cáo giá thành sản xuất
// ---------------------------------------------------------------------------------------------------------------------

export function CostReportScreen() {
  const sa = useSurveyApi();
  const [q, setQ] = useState<CostQuery>({});
  const r = useLoad(() => sa.report(q), [q.round, q.compare, q.crop, q.item, q.region, q.commune]);
  const act = useAction();
  const set = (k: keyof CostQuery) => (v: string) => setQ({ ...q, [k]: v || undefined } as CostQuery);
  return (
    <>
      <PageHeader
        title="Giá thành sản xuất"
        badge={<StatusPill tone="info">Chỉ xem số tổng hợp</StatusPill>}
        actions={
          r.data?.can_export && r.data.main ? (
            <button type="button" className="btn" disabled={act.busy} onClick={() => void download(act, () => sa.reportExport(q))}>
              Xuất Excel
            </button>
          ) : null
        }
      >
        Kết quả các đợt điều tra đã chốt — chỉ tính phiếu Đã duyệt; bình quân là trung bình cộng các phiếu, kèm trung vị giá bán và
        giá thành. Không có họ tên, số điện thoại nông hộ.
      </PageHeader>
      {act.view}
      <LoadState s={r}>
        {(d) =>
          !d.main ? (
            <div className="alert alert-info">Chưa có đợt điều tra đã chốt.</div>
          ) : (
            <>
              <div className="row" style={wrapRow}>
                <Pick id="cr-round" label="Đợt" all={null} value={q.round ?? d.main.round.id} onChange={set("round")} options={d.rounds.map((x) => ({ code: x.id, name: x.name }))} />
                <Pick id="cr-compare" label="So sánh với" all="— Không —" value={q.compare ?? ""} onChange={set("compare")} options={d.rounds.filter((x) => x.id !== d.main!.round.id).map((x) => ({ code: x.id, name: x.name }))} />
                <Pick id="cr-crop" label="Nhóm cây" value={q.crop ?? ""} onChange={set("crop")} options={CROP_REFS} />
                <Pick id="cr-region" label="Nhóm vùng" value={q.region ?? ""} onChange={set("region")} options={d.regions} />
                {q.item ? (
                  <button type="button" className="btn" onClick={() => setQ({ ...q, item: undefined })}>
                    Bỏ lọc mặt hàng
                  </button>
                ) : null}
              </div>
              <CostBlock b={d.main} onItem={(code) => setQ({ ...q, item: code })} />
              {d.compare ? (
                <>
                  <h2 style={{ fontSize: 16 }}>{`So sánh: ${d.compare.round.name}`}</h2>
                  <CostBlock b={d.compare} />
                </>
              ) : null}
            </>
          )
        }
      </LoadState>
    </>
  );
}

function CostBlock({ b, onItem }: { b: SurveyCostBlock; onItem?: (code: string) => void }) {
  return (
    <>
      <div className="row" style={wrapRow}>
        <Kpi label="Số phiếu đã duyệt" value={b.kpis.forms} />
        <Kpi label="Giá thành bình quân (đ/kg)" tone="info" value={money(b.kpis.avg_cost_per_kg)} />
        <Kpi label="Giá bán bình quân (đ/kg)" tone="ok" value={money(b.kpis.avg_price)} />
        <Kpi label="Lợi nhuận bình quân / ha" tone="ok" value={money(b.kpis.avg_profit_per_ha)} />
      </div>
      <Grid
        caption={`Giá thành theo mặt hàng — ${b.round.name}`}
        head={[
          "Nhóm cây",
          "Mặt hàng",
          "Số phiếu",
          ...COST_KEYS.map((k) => COST_LABEL[k]!),
          "Tổng chi phí",
          "Năng suất (kg/ha)",
          "Giá bán BQ",
          "Giá bán trung vị",
          "Giá thành BQ",
          "Giá thành trung vị",
          "Lợi nhuận / ha",
          "Tỷ suất LN",
        ]}
        rows={b.rows}
        rowKey={(x) => x.item}
        empty="Không có phiếu đã duyệt phù hợp."
        cells={(x) => [
          CROP_LABEL[x.crop] ?? x.crop,
          onItem ? (
            <button key="i" type="button" className="link" onClick={() => onItem(x.item)}>
              {x.item_name}
            </button>
          ) : (
            x.item_name
          ),
          <span key="n">
            {x.n} {x.small_sample ? <StatusPill tone="warn">Mẫu nhỏ</StatusPill> : null}
          </span>,
          ...COST_KEYS.map((k) => money(x.avg_costs[k])),
          money(x.avg_total_cost),
          money(x.avg_yield_ha),
          money(x.avg_price),
          money(x.median_price),
          money(x.avg_cost_per_kg),
          money(x.median_cost_per_kg),
          money(x.avg_profit_per_ha),
          x.avg_margin_pct ? `${money(x.avg_margin_pct)}%` : "—",
        ]}
      />
      <Grid
        caption="Giá thành – giá bán theo nhóm vùng"
        head={["Mặt hàng", "Nhóm vùng", "Số phiếu", "Giá thành BQ", "Giá bán BQ"]}
        rows={b.regions}
        rowKey={(x) => `${x.item}:${x.region}`}
        cells={(x) => [x.item_name, x.region_name, x.n, money(x.avg_cost_per_kg), money(x.avg_price)]}
      />
      {b.communes.length ? (
        <Grid
          caption="Phân bố theo xã"
          head={["Xã", "Số phiếu", "Giá bán thấp nhất", "Giá bán cao nhất", "Giá bán trung vị", "Giá thành BQ", "Giá thành trung vị"]}
          rows={b.communes}
          rowKey={(x) => x.commune}
          cells={(x) => [x.commune_name, x.n, money(x.min_price), money(x.max_price), money(x.median_price), money(x.avg_cost_per_kg), money(x.median_cost_per_kg)]}
        />
      ) : null}
    </>
  );
}
