"use client";

// FN-10 Sổ quyết định nâng bậc (BRD HCTH v3.0; W11.25, dom/hrm) theo bố cục prototype HCTH: sổ (Số QĐ, Ngày, Cơ quan
// ban hành, Loại, Danh sách gốc, Số người, Tệp, Trạng thái), lọc năm/loại/cơ quan + tìm theo số hoặc tên viên chức,
// "Xuất Excel", "+ Nhập quyết định đã ban hành", "+ Thêm quyết định" (chọn danh sách đã xuất, số/ngày/cơ quan, tệp, bảng
// người có ô chọn + ngày hưởng). Lưu → máy chủ cập nhật bậc, hệ số trong hồ sơ cùng giao dịch; tệp vào Kho tài liệu.
// Chi tiết: thông tin, người có tên (bậc/hệ số cũ → mới, ngày hưởng), lịch sử; hủy = sự kiện bù có lý do.
import Link from "next/link";
import { useParams } from "next/navigation";
import { useMemo, useState } from "react";
import { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { BackLink } from "@/components/patterns/detail";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import {
  createPromotionApi,
  DECISION_KIND_LABEL,
  FORM_LABEL,
  ISSUERS,
  vnNum,
  type Decision,
  type DecisionInput,
  type PromoKind,
  type PromotionApi,
} from "./promotion-api";
import { createStaffApi, vnDate } from "./staff-api";

export const DECISION_BASE = "/kn/nhan-su/quyet-dinh-nang-bac";
const KIND_CODE: Record<PromoKind, string> = { nltx: "NLTX", nlth: "NLTH", tnvk: "TNVK" };

function usePromotionApi(): PromotionApi {
  const { api } = useAuth();
  return useMemo(() => createPromotionApi(api), [api]);
}

function DecisionPill({ d }: { d: Decision }) {
  return d.status === "cancelled" ? (
    <StatusPill tone="bad">Đã hủy</StatusPill>
  ) : (
    <StatusPill tone="ok">Đã cập nhật bậc, hệ số</StatusPill>
  );
}

function bacText(p: Decision["people"][number], kind: string): [string, string] {
  if (kind === "tnvk")
    return ["bậc cuối", `TNVK ${p.pct_from ? `${p.pct_from}%` : "—"} → ${p.pct_to}%`];
  return [`${p.from_step || "—"} → ${p.to_step}`, `${vnNum(p.from_coeff)} → ${vnNum(p.to_coeff)}`];
}

/** Lưu quyết định; số trùng → hỏi lại (hrm.decision_warnings) rồi gửi kèm acknowledge_warnings. */
async function saveDecision(pa: PromotionApi, body: DecisionInput, file: File | null) {
  let r;
  try {
    r = await pa.createDecision(body);
  } catch (e) {
    if (e instanceof ApiProblem && e.code === "hrm.decision_warnings") {
      if (!window.confirm(`${e.message}\nVẫn lưu quyết định?`)) throw e;
      r = await pa.createDecision({ ...body, acknowledge_warnings: true });
    } else throw e;
  }
  if (file) await pa.attachFile(r.id, file);
  return r;
}

// --------------------------------------------------------------------------------------------------------------------
// FN-10 B1 — sổ quyết định.
// --------------------------------------------------------------------------------------------------------------------

export function HrmDecisionListScreen() {
  const pa = usePromotionApi();
  const [year, setYear] = useState(String(new Date().getFullYear()));
  const [kind, setKind] = useState<"" | PromoKind>("");
  const [issuer, setIssuer] = useState("");
  const [q, setQ] = useState("");
  const [nonce, setNonce] = useState(0);
  const [dlg, setDlg] = useState<"" | "add" | "old">("");
  const act = useAction();
  const list = useLoad(
    () =>
      pa.decisions({
        ...(year ? { year: Number(year) } : {}),
        ...(kind ? { kind } : {}),
        ...(issuer ? { issuer } : {}),
        ...(q ? { q } : {}),
      }),
    [year, kind, issuer, q, nonce],
  );
  const years = Array.from({ length: 6 }, (_, i) => String(new Date().getFullYear() - i));
  return (
    <>
      <PageHeader
        title="Sổ quyết định nâng bậc"
        actions={
          <div className="row">
            <button
              type="button"
              className="btn btn-o btn-sm"
              disabled={act.busy}
              onClick={() =>
                void act.run(async () => {
                  const f = await pa.decisionBook(year ? Number(year) : undefined);
                  saveBlob(f.blob, f.name);
                  return f;
                }, "Đã xuất sổ quyết định ra Excel.")
              }
            >
              Xuất Excel
            </button>
            <button type="button" className="btn btn-o btn-sm" onClick={() => setDlg("old")}>
              + Nhập quyết định đã ban hành
            </button>
            <button type="button" className="btn btn-p btn-sm" onClick={() => setDlg("add")}>
              + Thêm quyết định
            </button>
          </div>
        }
      >
        Lưu quyết định nâng bậc (NLTX, NLTH, TNVK) đã ký ngoài hệ thống; hệ thống
        cập nhật bậc, hệ số trong hồ sơ của người có tên. Quyết định không xóa —
        hủy phải có lý do và được lưu vết.
      </PageHeader>
      {act.view}
      <div className="card filters" role="search">
        <label className="small">
          Năm{" "}
          <select aria-label="Năm" value={year} onChange={(e) => setYear(e.target.value)}>
            {years.map((y) => (
              <option key={y}>{y}</option>
            ))}
            <option value="">Tất cả</option>
          </select>
        </label>
        <label className="small">
          Loại{" "}
          <select aria-label="Loại quyết định" value={kind} onChange={(e) => setKind(e.target.value as "" | PromoKind)}>
            <option value="">Tất cả</option>
            {(["nltx", "nlth", "tnvk"] as PromoKind[]).map((k) => (
              <option key={k} value={k}>
                {KIND_CODE[k]}
              </option>
            ))}
          </select>
        </label>
        <label className="small">
          Cơ quan ban hành{" "}
          <select aria-label="Cơ quan ban hành" value={issuer} onChange={(e) => setIssuer(e.target.value)}>
            <option value="">Tất cả</option>
            {ISSUERS.map((x) => (
              <option key={x}>{x}</option>
            ))}
          </select>
        </label>
        <input
          type="search"
          aria-label="Số quyết định, tên viên chức"
          placeholder="Số quyết định, tên viên chức"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
      </div>
      <LoadState s={list}>
        {(l) => (
          <div className="card dt-card">
            <div className="tbl-wrap">
              <table className="dt">
                <caption className="sr-only">Sổ quyết định nâng bậc</caption>
                <thead>
                  <tr>
                    <th>Số QĐ</th>
                    <th>Ngày QĐ</th>
                    <th>Cơ quan ban hành</th>
                    <th>Loại</th>
                    <th>Danh sách gốc</th>
                    <th>Số người</th>
                    <th>Tệp</th>
                    <th>Trạng thái</th>
                  </tr>
                </thead>
                <tbody>
                  {l.items.length === 0 ? (
                    <tr>
                      <td colSpan={8} className="mute">
                        Chưa có quyết định phù hợp bộ lọc.
                      </td>
                    </tr>
                  ) : null}
                  {l.items.map((d) => (
                    <tr key={d.id}>
                      <td>
                        <Link href={`${DECISION_BASE}/${d.id}`}>
                          <b>{d.decision_no}</b>
                        </Link>
                      </td>
                      <td>{vnDate(d.decided_on)}</td>
                      <td>{d.issuer}</td>
                      <td>{KIND_CODE[d.kind]}</td>
                      <td>{d.source}</td>
                      <td>{d.count}</td>
                      <td>{d.doc_id ? "Có tệp" : <span className="mute">Chưa có tệp</span>}</td>
                      <td>
                        <DecisionPill d={d} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p className="mute small" style={{ padding: "8px 14px" }}>
              Hiển thị {l.items.length} quyết định{year ? ` năm ${year}` : ""}.
            </p>
          </div>
        )}
      </LoadState>
      {dlg === "add" ? (
        <AddDecisionDialog
          pa={pa}
          onClose={() => setDlg("")}
          onDone={() => {
            setDlg("");
            setNonce((n) => n + 1);
          }}
        />
      ) : null}
      {dlg === "old" ? (
        <OldDecisionDialog
          pa={pa}
          onClose={() => setDlg("")}
          onDone={() => {
            setDlg("");
            setYear("");
            setNonce((n) => n + 1);
          }}
        />
      ) : null}
    </>
  );
}

function AddDecisionDialog({ pa, onClose, onDone }: { pa: PromotionApi; onClose: () => void; onDone: () => void }) {
  const exports = useLoad(() => pa.exports(), []);
  const [src, setSrc] = useState("");
  const [no, setNo] = useState("");
  const [day, setDay] = useState("");
  const [issuer, setIssuer] = useState(ISSUERS[0]!);
  const [file, setFile] = useState<File | null>(null);
  const [unsel, setUnsel] = useState<Record<string, boolean>>({});
  const [eff, setEff] = useState<Record<string, string>>({});
  const act = useAction();
  const avail = (exports.data?.items ?? []).filter((x) => !x.decision_no);
  const cur = src || avail[0]?.id || "";
  const detail = useLoad(cur ? () => pa.exportDetail(cur) : null, [cur]);
  const people = (detail.data?.items ?? []).filter((i) => i.status !== "decided");
  const chosen = people.filter((i) => !unsel[i.case_key]);
  return (
    <Dialog
      title="Thêm quyết định từ danh sách"
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={act.busy || !cur || !no.trim() || !day || !chosen.length}
          onClick={() =>
            void act
              .run(
                () =>
                  saveDecision(
                    pa,
                    {
                      export_id: cur,
                      decision_no: no.trim(),
                      decided_on: day,
                      issuer,
                      people: chosen.map((i) => ({
                        case_key: i.case_key,
                        ...(eff[i.case_key] && eff[i.case_key] !== i.due ? { effective_on: eff[i.case_key] } : {}),
                      })),
                    },
                    file,
                  ),
                (r) =>
                  `Đã lưu quyết định. ${r.updated || chosen.length} hồ sơ được cập nhật${file ? "; tệp lưu ở Kho tài liệu" : ""}${r.returned ? `; ${r.returned} người không có tên trở lại bảng theo dõi` : ""}.`,
              )
              .then((r) => r && onDone())
          }
        >
          Lưu quyết định
        </button>
      }
    >
      {act.view}
      <LoadState s={exports}>
        {() =>
          avail.length === 0 ? (
            <div className="alert alert-warn" role="note">
              <span>
                Chưa có danh sách nào đã xuất mà chưa có quyết định. Xuất danh sách ở “Danh sách nâng bậc” trước
                (quyết định phải đã được ký ngoài hệ thống).
              </span>
            </div>
          ) : (
            <>
              <div className="row">
                <label className="small">
                  Danh sách đã xuất{" "}
                  <select aria-label="Danh sách đã xuất" value={cur} onChange={(e) => setSrc(e.target.value)}>
                    {avail.map((x) => (
                      <option key={x.id} value={x.id}>
                        {x.file_name} · {FORM_LABEL[x.form]} ({x.count} người)
                      </option>
                    ))}
                  </select>
                </label>
                <label className="small">
                  Số quyết định{" "}
                  <input aria-label="Số quyết định" placeholder="Ví dụ 540/QĐ-SNNMT" value={no} onChange={(e) => setNo(e.target.value)} />
                </label>
                <label className="small">
                  Ngày quyết định{" "}
                  <input type="date" aria-label="Ngày quyết định" value={day} onChange={(e) => setDay(e.target.value)} />
                </label>
                <label className="small">
                  Cơ quan ban hành{" "}
                  <select aria-label="Cơ quan ban hành" value={issuer} onChange={(e) => setIssuer(e.target.value)}>
                    {ISSUERS.map((x) => (
                      <option key={x}>{x}</option>
                    ))}
                  </select>
                </label>
                <label className="small">
                  Tệp quyết định{" "}
                  <input
                    type="file"
                    aria-label="Tệp quyết định"
                    accept=".pdf,.doc,.docx,.jpg,.jpeg,.png"
                    onChange={(e) => setFile(e.target.files?.[0] ?? null)}
                  />
                </label>
              </div>
              {!file ? <p className="mute small">Chưa đính kèm tệp quyết định (cảnh báo — vẫn lưu được, đính kèm sau ở màn chi tiết).</p> : null}
              <h3 className="h3">Người có tên trong quyết định</h3>
              <p className="mute small">
                Mặc định chọn tất cả; bỏ chọn người không có tên trong quyết định, sửa ngày hưởng nếu quyết định ghi khác.
              </p>
              <LoadState s={detail}>
                {() => (
                  <div className="tbl-wrap">
                    <table className="dt">
                      <caption className="sr-only">Người trong danh sách</caption>
                      <thead>
                        <tr>
                          <th>Chọn</th>
                          <th>Họ và tên</th>
                          <th>Hiện tại → sau nâng</th>
                          <th>Ngày hưởng</th>
                        </tr>
                      </thead>
                      <tbody>
                        {people.map((i) => (
                          <tr key={i.case_key}>
                            <td>
                              <input
                                type="checkbox"
                                aria-label={`Chọn ${i.name || i.code}`}
                                checked={!unsel[i.case_key]}
                                onChange={(e) => setUnsel((u) => ({ ...u, [i.case_key]: !e.target.checked }))}
                              />
                            </td>
                            <td>{i.name || i.code}</td>
                            <td>
                              {i.kind === "tnvk"
                                ? `TNVK ${i.pct_from || "0"}% → ${i.pct_to}%`
                                : `Bậc ${i.from_step} · ${vnNum(i.from_coeff)} → Bậc ${i.to_step} · ${vnNum(i.to_coeff)}`}
                            </td>
                            <td>
                              <input
                                type="date"
                                aria-label={`Ngày hưởng của ${i.name || i.code}`}
                                value={eff[i.case_key] ?? i.due}
                                onChange={(e) => setEff((x) => ({ ...x, [i.case_key]: e.target.value }))}
                              />
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </LoadState>
            </>
          )
        }
      </LoadState>
    </Dialog>
  );
}

type OldRow = { staff: string; to: string; day: string };

function OldDecisionDialog({ pa, onClose, onDone }: { pa: PromotionApi; onClose: () => void; onDone: () => void }) {
  const { api } = useAuth();
  const sa = useMemo(() => createStaffApi(api), [api]);
  const staff = useLoad(() => sa.list({ type: "vien_chuc", status: "active", limit: 500 }), []);
  const [kind, setKind] = useState<PromoKind>("nltx");
  const [no, setNo] = useState("");
  const [day, setDay] = useState("");
  const [issuer, setIssuer] = useState(ISSUERS[0]!);
  const [file, setFile] = useState<File | null>(null);
  const [rows, setRows] = useState<OldRow[]>([{ staff: "", to: "", day: "" }]);
  const act = useAction();
  const ok = no.trim() && day && rows.every((r) => r.staff && r.to && r.day);
  const setRow = (i: number, p: Partial<OldRow>) => setRows((rs) => rs.map((r, j) => (j === i ? { ...r, ...p } : r)));
  return (
    <Dialog
      title="Nhập quyết định đã ban hành"
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={act.busy || !ok}
          onClick={() =>
            void act
              .run(
                () =>
                  saveDecision(
                    pa,
                    {
                      kind,
                      decision_no: no.trim(),
                      decided_on: day,
                      issuer,
                      people: rows.map((r) => ({
                        staff_id: r.staff,
                        effective_on: r.day,
                        ...(kind === "tnvk" ? { pct_to: r.to } : { to_step: Number(r.to) }),
                      })),
                    },
                    file,
                  ),
                (r) => `Đã lưu quyết định. ${r.updated || rows.length} hồ sơ được cập nhật; ngày hưởng làm mốc xếp bậc.`,
              )
              .then((r) => r && onDone())
          }
        >
          Lưu quyết định
        </button>
      }
    >
      {act.view}
      <div className="alert alert-info" role="note">
        <span>
          Dùng cho quyết định đã ban hành ngoài hệ thống (ví dụ quyết định cũ của người mới thêm hồ sơ).{" "}
          <b>Ngày hưởng</b> là mốc xếp bậc để hệ thống tính các lần nâng sau.
        </span>
      </div>
      <div className="row">
        <label className="small">
          Số quyết định{" "}
          <input aria-label="Số quyết định" placeholder="Ví dụ 120/QĐ-SNNMT" value={no} onChange={(e) => setNo(e.target.value)} />
        </label>
        <label className="small">
          Ngày quyết định{" "}
          <input type="date" aria-label="Ngày quyết định" value={day} onChange={(e) => setDay(e.target.value)} />
        </label>
        <label className="small">
          Cơ quan ban hành{" "}
          <select aria-label="Cơ quan ban hành" value={issuer} onChange={(e) => setIssuer(e.target.value)}>
            {ISSUERS.map((x) => (
              <option key={x}>{x}</option>
            ))}
          </select>
        </label>
        <label className="small">
          Loại quyết định{" "}
          <select aria-label="Loại quyết định" value={kind} onChange={(e) => setKind(e.target.value as PromoKind)}>
            {(["nltx", "nlth", "tnvk"] as PromoKind[]).map((k) => (
              <option key={k} value={k}>
                {DECISION_KIND_LABEL[k]}
              </option>
            ))}
          </select>
        </label>
        <label className="small">
          Tệp quyết định{" "}
          <input type="file" aria-label="Tệp quyết định" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
        </label>
      </div>
      <h3 className="h3">Người có tên trong quyết định</h3>
      <LoadState s={staff}>
        {(s) => (
          <div className="tbl-wrap">
            <table className="dt">
              <caption className="sr-only">Người có tên trong quyết định</caption>
              <thead>
                <tr>
                  <th>Viên chức</th>
                  <th>Bậc, hệ số hiện tại</th>
                  <th>{kind === "tnvk" ? "% TNVK sau quyết định" : "Bậc sau quyết định"}</th>
                  <th>Ngày hưởng</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {rows.map((r, i) => {
                  const st = s.items.find((x) => x.id === r.staff);
                  return (
                    <tr key={i}>
                      <td>
                        <select aria-label={`Viên chức dòng ${i + 1}`} value={r.staff} onChange={(e) => setRow(i, { staff: e.target.value })}>
                          <option value="">— Chọn viên chức —</option>
                          {s.items.map((x) => (
                            <option key={x.id} value={x.id}>
                              {x.label || x.code} ({x.code})
                            </option>
                          ))}
                        </select>
                      </td>
                      <td>{st ? `Bậc ${st.current.step || "—"}` : "—"}</td>
                      <td>
                        <input
                          type="number"
                          min={kind === "tnvk" ? 0 : 1}
                          max={kind === "tnvk" ? 100 : 99}
                          style={{ width: 110 }}
                          aria-label={`${kind === "tnvk" ? "% TNVK" : "Bậc"} dòng ${i + 1}`}
                          value={r.to}
                          onChange={(e) => setRow(i, { to: e.target.value })}
                        />
                      </td>
                      <td>
                        <input type="date" aria-label={`Ngày hưởng dòng ${i + 1}`} value={r.day} onChange={(e) => setRow(i, { day: e.target.value })} />
                      </td>
                      <td>
                        {rows.length > 1 ? (
                          <button
                            type="button"
                            className="btn btn-o btn-sm"
                            aria-label={`Bỏ dòng ${i + 1}`}
                            onClick={() => setRows((rs) => rs.filter((_, j) => j !== i))}
                          >
                            ×
                          </button>
                        ) : null}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </LoadState>
      <button type="button" className="btn btn-o btn-sm" onClick={() => setRows((rs) => [...rs, { staff: "", to: "", day: "" }])}>
        + Thêm người
      </button>
    </Dialog>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// FN-10 — chi tiết quyết định.
// --------------------------------------------------------------------------------------------------------------------

const ROW_LABEL: Record<string, string> = {
  issue: "Lưu quyết định, cập nhật bậc, hệ số trong hồ sơ",
  attach: "Đính kèm tệp quyết định (Kho tài liệu)",
  cancel: "Hủy quyết định",
};

export function HrmDecisionDetailScreen() {
  const { id } = useParams<{ id: string }>();
  const pa = usePromotionApi();
  const [nonce, setNonce] = useState(0);
  const [cancel, setCancel] = useState(false);
  const [reason, setReason] = useState("");
  const act = useAction();
  const d = useLoad(() => pa.decision(id), [id, nonce]);
  const reload = () => setNonce((n) => n + 1);
  return (
    <>
      <BackLink href={DECISION_BASE}>Sổ quyết định</BackLink>
      {act.view}
      <LoadState s={d}>
        {(q) => (
          <>
            <PageHeader
              title={`Quyết định ${q.decision_no}`}
              badge={<DecisionPill d={q} />}
              actions={
                q.status === "cancelled" ? null : (
                  <div className="row">
                    {q.doc_id ? (
                      <a className="btn btn-o btn-sm" href={`/kn/nhan-su/kho-tai-lieu/${q.doc_id}`}>
                        Mở tệp ở Kho tài liệu
                      </a>
                    ) : (
                      <label className="btn btn-o btn-sm">
                        Đính kèm tệp
                        <input
                          type="file"
                          aria-label="Đính kèm tệp quyết định"
                          style={{ display: "none" }}
                          onChange={(e) => {
                            const f = e.target.files?.[0];
                            if (f) void act.run(() => pa.attachFile(q.id, f), "Đã lưu tệp vào Kho tài liệu.").then(reload);
                          }}
                        />
                      </label>
                    )}
                    <button type="button" className="btn btn-d btn-sm" onClick={() => setCancel(true)}>
                      Hủy quyết định
                    </button>
                  </div>
                )
              }
            >
              {DECISION_KIND_LABEL[q.kind]}
            </PageHeader>
            <div className="card">
              <h2 className="h3">Thông tin quyết định</h2>
              <dl className="kv">
                <dt>Số quyết định</dt>
                <dd>{q.decision_no}</dd>
                <dt>Loại</dt>
                <dd>{DECISION_KIND_LABEL[q.kind]}</dd>
                <dt>Ngày quyết định</dt>
                <dd>{vnDate(q.decided_on)}</dd>
                <dt>Danh sách gốc</dt>
                <dd>{q.source}</dd>
                <dt>Cơ quan ban hành</dt>
                <dd>{q.issuer}</dd>
                <dt>Tệp quyết định</dt>
                <dd>{q.doc_id ? "Đã lưu ở Kho tài liệu (thư mục Quyết định nâng bậc)" : "Chưa có tệp"}</dd>
                {q.cancel_reason ? (
                  <>
                    <dt>Lý do hủy</dt>
                    <dd>{q.cancel_reason}</dd>
                  </>
                ) : null}
              </dl>
            </div>
            <div className="card dt-card">
              <div className="tbl-wrap">
                <table className="dt">
                  <caption>Người có tên trong quyết định ({q.people.length})</caption>
                  <thead>
                    <tr>
                      <th>Họ và tên</th>
                      <th>Bậc cũ → mới</th>
                      <th>Hệ số bậc cũ → mới</th>
                      <th>Ngày hưởng</th>
                    </tr>
                  </thead>
                  <tbody>
                    {q.people.map((p) => {
                      const [a, b] = bacText(p, q.kind);
                      return (
                        <tr key={p.staff_id}>
                          <td>
                            <Link href={`/kn/nhan-su/ho-so/${p.staff_id}`}>{p.name || p.staff_id.slice(0, 8)}</Link>
                          </td>
                          <td>{a}</td>
                          <td>{b}</td>
                          <td>{vnDate(p.effective_on)}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </div>
            <div className="card">
              <h2 className="h3">Lịch sử</h2>
              <ul className="plain">
                {(q.history ?? []).map((h) => (
                  <li key={h.id}>
                    {new Date(h.recorded_at).toLocaleString("vi-VN")} · {ROW_LABEL[h.action] ?? h.action}
                    {h.reason ? `: ${h.reason}` : ""}
                  </li>
                ))}
              </ul>
            </div>
            {cancel ? (
              <Dialog
                title={`Hủy quyết định ${q.decision_no}`}
                onClose={() => setCancel(false)}
                actions={
                  <button
                    type="button"
                    className="btn btn-d btn-sm"
                    disabled={act.busy || reason.trim().length < 10}
                    onClick={() =>
                      void act
                        .run(() => pa.cancelDecision(q.id, reason.trim()), "Đã hủy quyết định, khôi phục bậc và hệ số trước đó.")
                        .then(() => {
                          setCancel(false);
                          reload();
                        })
                    }
                  >
                    Hủy quyết định
                  </button>
                }
              >
                <div className="alert alert-warn" role="note">
                  <span>Quyết định không bị xóa. Hủy phải có lý do (ít nhất 10 ký tự) và được lưu vết; bậc, hệ số trước đó được khôi phục.</span>
                </div>
                <label className="small" style={{ display: "block" }}>
                  Lý do hủy
                  <textarea aria-label="Lý do hủy" rows={3} style={{ width: "100%" }} value={reason} onChange={(e) => setReason(e.target.value)} />
                </label>
              </Dialog>
            ) : null}
          </>
        )}
      </LoadState>
    </>
  );
}
