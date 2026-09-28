"use client";

import { useMemo, useState, type FormEvent } from "react";
import { listChildUnits } from "@/api/adapters/units";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { ApiProblem } from "@/api/problem";
import type { ReportExport, ReportParams, ReportTemplate } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { formatInt } from "@/lib/decimal";
import { formatBytes } from "@/lib/files";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { buildPeriod, downloadExport, requestExport, useExportJobs } from "@/reports/exports";

const SEASON_NAME: Record<string, string> = { DX: "Đông Xuân", HT: "Hè Thu", TD: "Thu Đông" };
const STATUS: Record<ReportExport["status"], { text: string; cls: string }> = {
  queued: { text: "Đang dựng tệp…", cls: "c-info" },
  completed: { text: "Sẵn sàng tải", cls: "c-ok" },
  failed: { text: "Lỗi", cls: "c-bad" },
};
const FORMAT_LABEL = { xlsx: "Excel (.xlsx)", pdf: "PDF" } as const;

function isCgh(t: ReportTemplate | undefined) {
  return !!t && t.definition.source.startsWith("mechanization.");
}

function DownloadButton({ job }: { job: ReportExport }) {
  const { api } = useAuth();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  if (job.status !== "completed") return null;
  const go = async () => {
    setBusy(true);
    setProblem(null);
    try {
      await downloadExport(api, job.id);
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="stack" style={{ gap: 6 }}>
      <button type="button" className="btn btn-p btn-sm" onClick={() => void go()} disabled={busy} data-testid={`download-${job.id}`}>
        <Icon name="download" size={15} />
        {busy ? "Đang lấy liên kết…" : "Tải xuống"}
      </button>
      {problem ? <ProblemAlert problem={problem} /> : null}
    </div>
  );
}

/** Báo cáo & xuất dữ liệu (pf/reports): chọn mẫu, phạm vi, kỳ, định dạng → việc xuất 202 → theo dõi → tải. */
export default function ReportsPage() {
  const { api, principal } = useAuth();
  const templates = useAsync(() => unwrap(api.GET("/v1/reports/templates")), [api]);
  const [key, setKey] = useState("");
  const tpl = useMemo(() => templates.data?.items.find((t) => t.key === key) ?? templates.data?.items[0], [templates.data, key]);
  const [format, setFormat] = useState<"xlsx" | "pdf">("xlsx");
  const [unit, setUnit] = useState("");
  const now = new Date();
  const [periodKind, setPeriodKind] = useState<"month" | "quarter" | "year">("month");
  const [year, setYear] = useState(String(now.getFullYear()));
  const [month, setMonth] = useState(String(now.getMonth() + 1));
  const [quarter, setQuarter] = useState(String(Math.floor(now.getMonth() / 3) + 1));
  const [season, setSeason] = useState("HT");
  const [seasonB, setSeasonB] = useState("DX");
  const [seasonYearB, setSeasonYearB] = useState(String(now.getFullYear() - 1));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [idemKey, setIdemKey] = useState(newIdempotencyKey);
  const touch = <T,>(set: (v: T) => void) => (v: T) => {
    set(v);
    setIdemKey(newIdempotencyKey());
  };
  const jobs = useExportJobs(api);

  // Phạm vi: toàn quốc/Cục → chọn tỉnh; tỉnh → tỉnh hoặc xã trực thuộc; xã → xã của mình (máy chủ tự gắn).
  const province = principal?.level === "tinh" ? (principal.scopeUnits.find((u) => u.length === 2) ?? null) : null;
  const pickUnits = principal?.level !== "xa";
  const units = useAsync(() => (pickUnits ? listChildUnits(api, province) : Promise.resolve([])), [api, province, pickUnits]);

  const formats = tpl?.definition.formats ?? [];
  const effectiveFormat = formats.includes(format) ? format : (formats[0] ?? "xlsx");
  const cgh = isCgh(tpl);
  const compare = tpl?.definition.source === "mechanization.season_compare";

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!tpl) return;
    const params: ReportParams = {};
    if (unit) params.unit_code = unit;
    if (cgh) {
      params.season_code = season;
      params.season_year = Number(year);
      if (compare) {
        params.season_code_b = seasonB;
        params.season_year_b = Number(seasonYearB);
      }
    } else {
      params.period = buildPeriod(periodKind, year, month, quarter);
    }
    setBusy(true);
    setProblem(null);
    try {
      const job = await requestExport(api, { template_key: tpl.key, format: effectiveFormat, params }, idemKey);
      jobs.add(job);
      setIdemKey(newIdempotencyKey());
    } catch (err) {
      setProblem(err instanceof ApiProblem ? err : null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <PageHead title="Báo cáo & xuất dữ liệu" subtitle="Xuất Excel/PDF theo mẫu có phiên bản — tệp dựng ở máy chủ, liên kết tải có hạn 10 phút" />
      <div className="alert alert-info" style={{ marginBottom: 16 }}>
        <Icon name="info" />
        <span>
          Mỗi lần xuất được ghi nhật ký và tính vào hạn mức trong ngày (mặc định 50 lần/người, giờ Việt Nam). Số liệu CGH lấy từ kết quả
          cân đối <b style={{ display: "inline" }}>đã lưu</b>, không tính lại. Kỳ còn đơn vị chưa gửi báo cáo được đánh dấu{" "}
          <b style={{ display: "inline" }}>“Số liệu tạm tính”</b>.
        </span>
      </div>
      {templates.error ? <ProblemAlert problem={templates.error} onRetry={templates.reload} /> : null}
      {templates.loading && !templates.data ? <Loading /> : null}
      {templates.data && templates.data.items.length === 0 ? <Empty>Vai của bạn chưa được xuất mẫu báo cáo nào.</Empty> : null}
      {tpl ? (
        <form className="card sec stack" style={{ gap: 12 }} onSubmit={(e) => void submit(e)} aria-labelledby="rp-title">
          <h2 id="rp-title" style={{ fontSize: 15.5 }}>
            Yêu cầu xuất
          </h2>
          <div className="grid2">
            <div className="field">
              <label htmlFor="rp-tpl">Mẫu báo cáo</label>
              <select id="rp-tpl" className="select" value={tpl.key} onChange={(e) => touch(setKey)(e.target.value)}>
                {(templates.data?.items ?? []).map((t) => (
                  <option key={t.key} value={t.key}>
                    {t.definition.title}
                  </option>
                ))}
              </select>
              <span className="mute small">
                <code>{tpl.key}</code> · phiên bản {tpl.version}
                {tpl.definition.provisional_layout ? " · bố cục tạm (chưa có mẫu biểu chính thức của Bộ)" : ""}
              </span>
            </div>
            <div className="field">
              <label htmlFor="rp-unit">Phạm vi</label>
              {pickUnits ? (
                <select id="rp-unit" className="select" value={unit} onChange={(e) => touch(setUnit)(e.target.value)}>
                  <option value="">{province ? `Toàn tỉnh ${principal?.unitName ?? ""}` : "Toàn bộ phạm vi"}</option>
                  {(units.data ?? []).map((u) => (
                    <option key={u.code} value={u.code}>
                      {u.name}
                    </option>
                  ))}
                </select>
              ) : (
                <input id="rp-unit" className="input" value={principal?.unitName ?? "Xã phụ trách"} disabled />
              )}
            </div>
          </div>
          {cgh ? (
            <div className="row">
              <div className="field">
                <label htmlFor="rp-season">{compare ? "Vụ A" : "Vụ"}</label>
                <select id="rp-season" className="select" value={season} onChange={(e) => touch(setSeason)(e.target.value)}>
                  {Object.entries(SEASON_NAME).map(([k, v]) => (
                    <option key={k} value={k}>
                      {v}
                    </option>
                  ))}
                </select>
              </div>
              <div className="field">
                <label htmlFor="rp-year">Năm</label>
                <input id="rp-year" className="input" inputMode="numeric" value={year} onChange={(e) => touch(setYear)(e.target.value)} style={{ width: 100 }} />
              </div>
              {compare ? (
                <>
                  <div className="field">
                    <label htmlFor="rp-season-b">Vụ B</label>
                    <select id="rp-season-b" className="select" value={seasonB} onChange={(e) => touch(setSeasonB)(e.target.value)}>
                      {Object.entries(SEASON_NAME).map(([k, v]) => (
                        <option key={k} value={k}>
                          {v}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="field">
                    <label htmlFor="rp-year-b">Năm vụ B</label>
                    <input id="rp-year-b" className="input" inputMode="numeric" value={seasonYearB} onChange={(e) => touch(setSeasonYearB)(e.target.value)} style={{ width: 100 }} />
                  </div>
                </>
              ) : null}
            </div>
          ) : (
            <div className="row">
              <div className="field">
                <label htmlFor="rp-pk">Kỳ</label>
                <select id="rp-pk" className="select" value={periodKind} onChange={(e) => touch(setPeriodKind)(e.target.value as "month" | "quarter" | "year")}>
                  <option value="month">Tháng</option>
                  <option value="quarter">Quý</option>
                  <option value="year">Năm</option>
                </select>
              </div>
              {periodKind === "month" ? (
                <div className="field">
                  <label htmlFor="rp-month">Tháng</label>
                  <select id="rp-month" className="select" value={month} onChange={(e) => touch(setMonth)(e.target.value)}>
                    {Array.from({ length: 12 }, (_, i) => (
                      <option key={i + 1} value={String(i + 1)}>
                        Tháng {i + 1}
                      </option>
                    ))}
                  </select>
                </div>
              ) : null}
              {periodKind === "quarter" ? (
                <div className="field">
                  <label htmlFor="rp-q">Quý</label>
                  <select id="rp-q" className="select" value={quarter} onChange={(e) => touch(setQuarter)(e.target.value)}>
                    {["1", "2", "3", "4"].map((q) => (
                      <option key={q} value={q}>
                        Quý {q}
                      </option>
                    ))}
                  </select>
                </div>
              ) : null}
              <div className="field">
                <label htmlFor="rp-year">Năm</label>
                <input id="rp-year" className="input" inputMode="numeric" value={year} onChange={(e) => touch(setYear)(e.target.value)} style={{ width: 100 }} />
              </div>
            </div>
          )}
          <fieldset className="plain">
            <legend>Định dạng</legend>
            <div className="row">
              {(["xlsx", "pdf"] as const).map((f) => (
                <label key={f} className="row small" style={{ gap: 6 }}>
                  <input type="radio" name="rp-format" value={f} checked={effectiveFormat === f} disabled={!formats.includes(f)} onChange={() => touch(setFormat)(f)} />
                  {FORMAT_LABEL[f]}
                  {!formats.includes(f) ? <span className="mute">(mẫu không hỗ trợ)</span> : null}
                </label>
              ))}
            </div>
          </fieldset>
          {problem ? (
            <div>
              <ProblemAlert problem={problem} />
              {problem.code === "reports.export_quota_exceeded" && problem.retryAfterS ? (
                <p className="small" style={{ marginTop: 6 }}>
                  Hạn mức được đặt lại sau khoảng {formatInt(Math.ceil(problem.retryAfterS / 3600))} giờ nữa (0 giờ, giờ Việt Nam).
                </p>
              ) : null}
            </div>
          ) : null}
          <div>
            <button type="submit" className="btn btn-p" disabled={busy}>
              {busy ? "Đang gửi yêu cầu…" : "Xuất báo cáo"}
            </button>
          </div>
        </form>
      ) : null}

      <section className="card sec" style={{ marginTop: 16 }} aria-labelledby="rp-history">
        <div className="sec-h">
          <h2 id="rp-history">Lịch sử xuất của tôi</h2>
          <button type="button" className="btn btn-o btn-sm" onClick={jobs.refresh} disabled={jobs.jobs.length === 0}>
            Làm mới
          </button>
        </div>
        {jobs.jobs.length === 0 ? <p className="mute small">Chưa có yêu cầu xuất nào.</p> : null}
        {jobs.jobs.length > 0 ? (
          <div className="tbl-wrap">
            <table className="tbl">
              <caption className="sr-only">Việc xuất báo cáo</caption>
              <thead>
                <tr>
                  <th scope="col">Báo cáo</th>
                  <th scope="col">Tham số</th>
                  <th scope="col">Yêu cầu lúc</th>
                  <th scope="col">Trạng thái</th>
                  <th scope="col">Tệp</th>
                </tr>
              </thead>
              <tbody>
                {jobs.jobs.map((j) => (
                  <tr key={j.id} data-testid="export-row">
                    <td>
                      <b>{j.title}</b>
                      <div className="mute small">
                        {FORMAT_LABEL[j.format]} · mẫu v{j.template_version}
                      </div>
                    </td>
                    <td className="small">
                      {[j.params.unit_code ? `Đơn vị ${j.params.unit_code}` : null, j.params.period ? `Kỳ ${j.params.period}` : null, j.params.season_code ? `Vụ ${SEASON_NAME[j.params.season_code] ?? j.params.season_code} ${j.params.season_year ?? ""}` : null]
                        .filter(Boolean)
                        .join(" · ") || "—"}
                    </td>
                    <td className="small">{formatDateTime(j.requested_at)}</td>
                    <td>
                      <span className={`chip ${STATUS[j.status].cls}`}>{STATUS[j.status].text}</span>
                      {j.provisional ? (
                        <div>
                          <span className="chip c-warn" style={{ marginTop: 4 }}>
                            Số liệu tạm tính
                          </span>
                        </div>
                      ) : null}
                      {j.status === "failed" && j.error_code ? (
                        <div className="small err">
                          Mã lỗi <code>{j.error_code}</code>
                        </div>
                      ) : null}
                    </td>
                    <td>
                      {j.status === "completed" ? (
                        <div className="small mute" style={{ marginBottom: 4 }}>
                          {j.row_count !== null && j.row_count !== undefined ? `${formatInt(j.row_count)} dòng · ` : ""}
                          {formatBytes(j.size_bytes)}
                        </div>
                      ) : null}
                      <DownloadButton job={j} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </section>
    </>
  );
}
