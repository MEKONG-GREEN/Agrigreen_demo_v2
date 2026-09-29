"use client";

import { useState, type ChangeEvent, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import type { CropBulkHarvestResult, CropSeason } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { ResultList } from "@/htx/common";
import { harvestRowError, parseDecimalInput, previewYield, toCsv, todayVN } from "@/htx/rules";
import { CoopPicker, ReadOnlyNotice, useCoopFields, useHtxScope, type HtxScope } from "@/htx/scope";
import { useSeasonIndex } from "@/htx/seasons";
import { formatDecimal } from "@/lib/decimal";
import { readUpload, saveBlob, type UploadFile } from "@/lib/files";
import { formatDate } from "@/lib/format";
import { toApiProblem } from "@/lib/useAsync";

interface Draft {
  mass: string;
  date: string;
  moisture: string;
  confirm: boolean;
}

const STATUS_TEXT = { accepted: "Đã lưu", duplicate: "Đã có", rejected: "Lỗi" } as const;

function resultRows(res: CropBulkHarvestResult, label: (row: number, code?: string) => string) {
  return res.rows.map((r) => ({
    key: String(r.row),
    label: label(r.row, r.code),
    ok: r.status === "accepted",
    status: r.needs_confirm ? "Cần xác nhận" : STATUS_TEXT[r.status],
    reason: r.reason,
  }));
}

/** Bảng nhập nhanh: lưu nhiều lô trong một lần bấm (HTX US-YIELD-02 AC-2); bất thường cần xác nhận (US-YIELD-03). */
function QuickGrid({ scope, seasons, onSaved }: { scope: HtxScope; seasons: CropSeason[]; onSaved: () => void }) {
  const { api } = useAuth();
  const fields = useCoopFields(scope.coopRef);
  const today = todayVN();
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [needsConfirm, setNeedsConfirm] = useState<Record<string, string>>({});
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [result, setResult] = useState<CropBulkHarvestResult | null>(null);
  const [key, setKey] = useState(newIdempotencyKey);
  const open = seasons.filter((s) => s.status === "open").sort((a, b) => a.harvest_open_date.localeCompare(b.harvest_open_date) || a.plot_code.localeCompare(b.plot_code));
  const get = (ref: string): Draft => drafts[ref] ?? { mass: "", date: today, moisture: "", confirm: false };
  const set = (ref: string, d: Partial<Draft>) => {
    setDrafts((cur) => ({ ...cur, [ref]: { ...get(ref), ...d } }));
    setKey(newIdempotencyKey());
  };
  const filled = open.filter((s) => get(s.season_ref).mass.trim());
  const errors = new Map(filled.map((s) => [s.season_ref, harvestRowError(get(s.season_ref), today)] as const));
  const hasErrors = [...errors.values()].some(Boolean);

  const save = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (filled.length === 0 || hasErrors) return;
    setBusy(true);
    setProblem(null);
    try {
      const res = await unwrap(
        api.POST("/v1/cropcycle/harvests/bulk", {
          params: { header: { "Idempotency-Key": key } },
          body: {
            cooperative_ref: scope.isManager ? null : scope.coopRef || null,
            rows: filled.map((s) => {
              const d = get(s.season_ref);
              return {
                season_ref: s.season_ref,
                mass_kg: parseDecimalInput(d.mass, 3, true),
                harvest_date: d.date || today,
                moisture_pct: d.moisture ? parseDecimalInput(d.moisture, 2) : null,
                confirm_anomaly: d.confirm,
              };
            }),
          },
        }),
      );
      setResult(res);
      setKey(newIdempotencyKey());
      const confirm: Record<string, string> = {};
      res.rows.forEach((r) => {
        const s = filled[r.row - 1];
        if (s && r.needs_confirm) confirm[s.season_ref] = r.reason ?? "Sản lượng bất thường";
      });
      setNeedsConfirm(confirm);
      setDrafts((cur) => Object.fromEntries(Object.entries(cur).filter(([ref]) => confirm[ref] || res.rows.some((r) => r.status === "rejected" && filled[r.row - 1]?.season_ref === ref))));
      onSaved();
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={(e) => void save(e)} aria-label="Bảng nhập nhanh sản lượng">
      <div className="tbl-wrap">
        <table className="tbl">
          <caption className="sr-only">Bảng nhập nhanh sản lượng các vụ đang mở</caption>
          <thead>
            <tr>
              <th scope="col">Lô ruộng</th>
              <th scope="col">Nông hộ</th>
              <th scope="col">Giống</th>
              <th scope="col" className="num">Diện tích (ha)</th>
              <th scope="col">Thu hoạch từ</th>
              <th scope="col">Sản lượng (kg)</th>
              <th scope="col">Ngày thu hoạch</th>
              <th scope="col">Độ ẩm (%)</th>
              <th scope="col" className="num">t/ha</th>
            </tr>
          </thead>
          <tbody>
            {open.map((s) => {
              const d = get(s.season_ref);
              const err = touched ? errors.get(s.season_ref) : null;
              const early = s.harvest_open_date > today;
              const conf = needsConfirm[s.season_ref];
              const kg = parseDecimalInput(d.mass, 3);
              return (
                <tr key={s.season_ref}>
                  <th scope="row">
                    {s.plot_code}
                    {early ? <div className="mute small">Chưa tới giai đoạn thu hoạch</div> : null}
                  </th>
                  <td>{fields.householdLabel(s.household_ref)}</td>
                  <td>{s.variety_code}</td>
                  <td className="num">{formatDecimal(s.area_ha, 2)}</td>
                  <td>{formatDate(s.harvest_open_date)}</td>
                  <td>
                    <input
                      className="input grid-input"
                      inputMode="decimal"
                      aria-label={`Sản lượng kg lô ${s.plot_code}`}
                      aria-invalid={!!err}
                      value={d.mass}
                      onChange={(e) => set(s.season_ref, { mass: e.target.value, confirm: false })}
                    />
                    {err ? <div className="err small">{err}</div> : null}
                    {conf ? (
                      <div className="alert alert-warn small" style={{ marginTop: 6, padding: 8 }}>
                        <span>
                          {conf}
                          <label className="row" style={{ gap: 4, marginTop: 4 }}>
                            <input type="checkbox" checked={d.confirm} onChange={(e) => set(s.season_ref, { confirm: e.target.checked })} />
                            Xác nhận sản lượng lô {s.plot_code} là đúng
                          </label>
                        </span>
                      </div>
                    ) : null}
                  </td>
                  <td>
                    <input className="input" type="date" max={today} aria-label={`Ngày thu hoạch lô ${s.plot_code}`} value={d.date} onChange={(e) => set(s.season_ref, { date: e.target.value })} />
                  </td>
                  <td>
                    <input className="input grid-input" inputMode="decimal" aria-label={`Độ ẩm lô ${s.plot_code}`} value={d.moisture} onChange={(e) => set(s.season_ref, { moisture: e.target.value })} />
                  </td>
                  <td className="num">{kg ? (previewYield(kg, s.area_ha) ?? "—") : "—"}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      {open.length === 0 ? <Empty>Không có vụ đang mở để khai sản lượng.</Empty> : null}
      {scope.canWrite && open.length > 0 ? (
        <div className="row" style={{ marginTop: 12 }}>
          <button type="submit" className="btn btn-p btn-sm" disabled={busy}>
            {busy ? "Đang lưu…" : `Lưu ${filled.length} lô`}
          </button>
          {touched && filled.length === 0 ? <span className="err small">Nhập sản lượng cho ít nhất một lô.</span> : null}
        </div>
      ) : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {result ? (
        <>
          <p role="status">
            <b>
              {result.accepted} lô đã lưu · {result.duplicate} đã có · {result.rejected} lỗi
            </b>
          </p>
          <ResultList caption="Kết quả lưu bảng nhập nhanh" rows={resultRows(result, (row, code) => code ?? filled[row - 1]?.plot_code ?? `Dòng ${row}`)} />
        </>
      ) : null}
    </form>
  );
}

/** Nhập sản lượng từ Excel/CSV — dòng lỗi bị bỏ qua kèm lý do (HTX US-YIELD-02 AC-1). */
function ImportPanel({ scope, onDone }: { scope: HtxScope; onDone: () => void }) {
  const { api } = useAuth();
  const [file, setFile] = useState<UploadFile | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [result, setResult] = useState<CropBulkHarvestResult | null>(null);
  const [key, setKey] = useState(newIdempotencyKey);
  const ok = !!file && (file.kind === "xlsx" || file.kind === "csv");
  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    setResult(null);
    setProblem(null);
    setFile(f ? await readUpload(f) : null);
    setKey(newIdempotencyKey());
  };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!file || !ok) return;
    setBusy(true);
    setProblem(null);
    try {
      const res = await unwrap(
        api.POST("/v1/cropcycle/harvests/import", {
          params: { header: { "Idempotency-Key": key }, query: { cooperative_ref: scope.isManager ? undefined : scope.coopRef || undefined } },
          body: file.body as unknown as string,
          bodySerializer: (b: unknown) => b as BodyInit,
          headers: { "Content-Type": file.contentType },
        }),
      );
      setResult(res);
      onDone();
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  const template = () =>
    saveBlob(
      new Blob([toCsv([["Mã lô", "Sản lượng kg", "Ngày thu hoạch", "Độ ẩm", "Xác nhận bất thường"], ["TH0000001", "6500", todayVN(), "14.5", ""]])], { type: "text/csv;charset=utf-8" }),
      "mau-nhap-san-luong.csv",
    );
  return (
    <form className="panel stack" style={{ gap: 8, marginBottom: 16 }} onSubmit={(e) => void submit(e)} aria-label="Nhập sản lượng từ tệp">
      <b>Nhập sản lượng từ Excel (.xlsx) hoặc CSV — tối đa 200 dòng</b>
      <span className="small mute">Cột: Mã lô (hoặc Mã vụ), Sản lượng kg (hoặc Sản lượng tấn), Ngày thu hoạch, Độ ẩm, Xác nhận bất thường (x). Dòng lỗi được bỏ qua, dòng hợp lệ vẫn lưu.</span>
      <div className="row" style={{ alignItems: "flex-end" }}>
        <input type="file" aria-label="Tệp sản lượng" accept=".xlsx,.csv" onChange={(e) => void onFile(e)} />
        <button type="submit" className="btn btn-p btn-sm" disabled={!ok || busy}>
          {busy ? "Đang nhập…" : "Nhập tệp"}
        </button>
        <button type="button" className="btn btn-o btn-sm" onClick={template}>
          <Icon name="download" size={15} />
          Tải tệp mẫu
        </button>
      </div>
      {file && !ok ? <span className="err small">Chỉ nhận .xlsx hoặc CSV UTF-8 (không nhận .xls cũ).</span> : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {result ? (
        <>
          <p role="status">
            <b>
              {result.accepted} dòng thành công · {result.rejected} dòng lỗi{result.duplicate ? ` · ${result.duplicate} dòng đã có` : ""}
            </b>
          </p>
          <ResultList caption="Kết quả nhập sản lượng theo dòng" rows={resultRows(result, (row, code) => `Dòng ${row}${code ? ` · ${code}` : ""}`)} />
        </>
      ) : null}
    </form>
  );
}

/** Sản lượng thu hoạch (HTX US-YIELD-02/03). */
export default function HarvestPage() {
  const scope = useHtxScope();
  const [version, setVersion] = useState(0);
  const [showImport, setShowImport] = useState(false);
  const index = useSeasonIndex(scope.coopRef, [version]);
  const reload = () => setVersion((v) => v + 1);
  return (
    <>
      <PageHead
        title="Sản lượng thu hoạch"
        subtitle="Khai sản lượng đóng vụ ngay; sản lượng bằng 0 hoặc vượt ngưỡng theo giống cần xác nhận lại"
        actions={
          scope.canWrite && scope.coopRef ? (
            <button type="button" className="btn btn-o btn-sm" aria-expanded={showImport} onClick={() => setShowImport((v) => !v)}>
              <Icon name="upload" size={15} />
              Nhập từ Excel
            </button>
          ) : null
        }
      />
      <ReadOnlyNotice scope={scope} />
      {showImport && scope.coopRef ? <ImportPanel scope={scope} onDone={reload} /> : null}
      <div className="card sec">
        <div className="row" style={{ marginBottom: 14, alignItems: "flex-end" }}>
          <CoopPicker scope={scope} allowAll={!scope.canWrite} />
        </div>
        {scope.canWrite && !scope.coopRef ? <Empty>Chọn một HTX để nhập sản lượng.</Empty> : null}
        {index.loading && index.seasons.length === 0 ? <Loading /> : null}
        {scope.coopRef || !scope.canWrite ? <QuickGrid key={scope.coopRef} scope={scope} seasons={index.seasons} onSaved={reload} /> : null}
      </div>
    </>
  );
}
