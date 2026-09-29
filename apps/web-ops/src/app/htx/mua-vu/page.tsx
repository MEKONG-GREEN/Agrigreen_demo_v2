"use client";

import { useMemo, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages, usePagedList } from "@/api/pagination";
import { ApiProblem } from "@/api/problem";
import type { CropBulkOpenResult, CropPreviousConfig, CropSeason, CropSOPTemplate } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { LocationChip, ReasonDialog, ResultList, StatusChip, useCatalog } from "@/htx/common";
import { SEASON_STATUS, sowingDateError, todayVN } from "@/htx/rules";
import { CoopPicker, ReadOnlyNotice, useCoopFields, useHtxScope, type HtxScope } from "@/htx/scope";
import { formatDecimal } from "@/lib/decimal";
import { formatDate } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";

const YEAR = Number(todayVN().slice(0, 4));

/** Phiên bản SOP đã ban hành để chọn khi mở vụ (bỏ trống = tự gán theo giống, HTX US-SEASON-01 AC-1). */
function usePublishedSops(): { t: CropSOPTemplate; versionId: string; no: number }[] {
  const { api } = useAuth();
  const res = useAsync(async () => {
    const templates = await collectPages((cursor) => unwrap(api.GET("/v1/cropcycle/sops", { params: { query: { cursor, limit: 200 } } })), 400);
    const details = await Promise.all(templates.map((t) => unwrap(api.GET("/v1/cropcycle/sops/{ref}", { params: { path: { ref: t.ref } } }))));
    return details.flatMap((d) => {
      const v = d.versions.filter((x) => x.status === "published").at(-1);
      return v ? [{ t: d, versionId: v.id, no: v.version_no }] : [];
    });
  }, [api]);
  return res.data ?? [];
}

type PlotResult = { key: string; label: string; ok: boolean; status: string; reason?: string };

/** Mở vụ cho một hoặc nhiều lô của nông hộ (HTX US-SEASON-02), sao chép cấu hình vụ trước (US-SEASON-03). */
function OpenPanel({ scope, onDone }: { scope: HtxScope; onDone: () => void }) {
  const { api } = useAuth();
  const fields = useCoopFields(scope.coopRef);
  const varieties = useCatalog("rice_variety");
  const seasons = useCatalog("season");
  const sops = usePublishedSops();
  const [household, setHousehold] = useState("");
  const [q, setQ] = useState("");
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [copy, setCopy] = useState(false);
  const [seasonCode, setSeasonCode] = useState("TD");
  const [year, setYear] = useState(String(YEAR));
  const [variety, setVariety] = useState("");
  const [sowing, setSowing] = useState(todayVN());
  const [sop, setSop] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [results, setResults] = useState<PlotResult[] | null>(null);
  const [summary, setSummary] = useState<string | null>(null);
  const [prev, setPrev] = useState<{ plot: string; cfg?: CropPreviousConfig; problem?: ApiProblem } | null>(null);
  const [touched, setTouched] = useState(false);

  const visible = fields.plots.filter(
    (p) => (!household || p.properties.household_ref === household) && (!q || p.properties.code.toLowerCase().includes(q.toLowerCase())),
  );
  const codeOf = (ref: string) => fields.plots.find((p) => p.properties.ref === ref)?.properties.code ?? ref.slice(0, 8);
  const sowingErr = sowingDateError(sowing);
  const errors = {
    plots: picked.size === 0 ? "Chọn ít nhất một lô ruộng." : picked.size > 200 ? "Tối đa 200 lô một lần." : null,
    variety: !copy && !variety ? "Chọn giống lúa." : null,
    sowing: sowingErr,
  };
  const invalid = Object.values(errors).some(Boolean);

  const toggle = (ref: string) =>
    setPicked((cur) => {
      const next = new Set(cur);
      if (next.has(ref)) next.delete(ref);
      else next.add(ref);
      return next;
    });

  const checkPrevious = async () => {
    const plot = [...picked][0];
    if (!plot) return;
    try {
      const cfg = await unwrap(api.GET("/v1/cropcycle/plots/{ref}/previous-config", { params: { path: { ref: plot } } }));
      setPrev({ plot, cfg });
    } catch (e) {
      setPrev({ plot, problem: toApiProblem(e) });
    }
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (invalid) return;
    setBusy(true);
    setProblem(null);
    setResults(null);
    try {
      const plots = [...picked];
      if (copy) {
        // Sao chép cấu hình vụ trước: mỗi lô một lệnh (giống + SOP lấy từ vụ vừa hoàn thành của chính lô đó).
        const out: PlotResult[] = [];
        for (const plot of plots) {
          try {
            await unwrap(
              api.POST("/v1/cropcycle/seasons", {
                params: { header: { "Idempotency-Key": newIdempotencyKey() } },
                body: { plot_ref: plot, season_code: seasonCode, season_year: Number(year), variety_code: variety || "OM5451", sowing_date: sowing, copy_previous: true, gps: { source: "no_device" } },
              }),
            );
            out.push({ key: plot, label: codeOf(plot), ok: true, status: "Đã mở vụ" });
          } catch (err) {
            out.push({ key: plot, label: codeOf(plot), ok: false, status: "Bỏ qua", reason: toApiProblem(err).message });
          }
        }
        setResults(out);
        setSummary(`${out.filter((r) => r.ok).length} lô đã mở vụ (sao chép cấu hình vụ trước) · ${out.filter((r) => !r.ok).length} lô bỏ qua`);
      } else {
        const res: CropBulkOpenResult = await unwrap(
          api.POST("/v1/cropcycle/seasons/bulk-open", {
            params: { header: { "Idempotency-Key": newIdempotencyKey() } },
            body: { plot_refs: plots, season_code: seasonCode, season_year: Number(year), variety_code: variety, sowing_date: sowing, sop_version_id: sop || null },
          }),
        );
        setResults(res.items.map((i) => ({ key: i.plot_ref, label: i.code ?? codeOf(i.plot_ref), ok: i.status === "opened", status: i.status === "opened" ? "Đã mở vụ" : "Bỏ qua", reason: i.reason })));
        setSummary(`${res.opened} lô đã mở vụ · ${res.skipped} lô bỏ qua`);
      }
      setPicked(new Set());
      onDone();
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form className="panel stack" style={{ gap: 12, marginBottom: 16 }} onSubmit={(e) => void submit(e)} aria-label="Mở vụ cho nông hộ">
      <b>Mở vụ cho nông hộ / nhiều lô ruộng</b>
      <span className="small mute">
        Chọn nông hộ rồi tích các lô ruộng. Lô đang canh tác vụ khác hoặc ngoài HTX sẽ bị bỏ qua kèm lý do; các lô hợp lệ được mở trong cùng một lần xác nhận.
      </span>
      <div className="row" style={{ alignItems: "flex-end" }}>
        <div className="field" style={{ minWidth: 220 }}>
          <label htmlFor="open-hh">Nông hộ</label>
          <select id="open-hh" className="select" value={household} onChange={(e) => setHousehold(e.target.value)}>
            <option value="">Tất cả nông hộ</option>
            {fields.households.map((h) => (
              <option key={h.household_ref} value={h.household_ref}>
                {fields.householdLabel(h.household_ref)} · {h.masked_phone}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="open-q">Tìm mã lô</label>
          <input id="open-q" className="input" value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
        <button type="button" className="btn btn-o btn-sm" onClick={() => setPicked(new Set(visible.map((p) => p.properties.ref)))}>
          Chọn tất cả lô đang hiện
        </button>
        <button type="button" className="btn btn-o btn-sm" onClick={() => setPicked(new Set())}>
          Bỏ chọn
        </button>
      </div>
      {fields.loading ? <Loading label="Đang tải lô ruộng…" /> : null}
      <fieldset style={{ border: 0, padding: 0, margin: 0 }}>
        <legend className="small">
          Lô ruộng ({picked.size} đã chọn / {visible.length})
        </legend>
        <div style={{ maxHeight: 220, overflow: "auto", display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(240px, 1fr))", gap: 4 }}>
          {visible.map((p) => (
            <label key={p.properties.ref} className="row small" style={{ gap: 6 }}>
              <input type="checkbox" checked={picked.has(p.properties.ref)} onChange={() => toggle(p.properties.ref)} />
              <span>
                <b style={{ display: "inline" }}>{p.properties.code}</b> · {formatDecimal(p.properties.area_ha, 2)} ha · {fields.householdLabel(p.properties.household_ref)}
              </span>
            </label>
          ))}
        </div>
      </fieldset>
      {touched && errors.plots ? <span className="err small">{errors.plots}</span> : null}
      <label className="row small">
        <input type="checkbox" checked={copy} onChange={(e) => setCopy(e.target.checked)} />
        Sao chép cấu hình vụ trước (giống lúa + quy trình SOP của vụ vừa hoàn thành trên từng lô)
      </label>
      {picked.size === 1 ? (
        <div className="row small">
          <button type="button" className="btn btn-o btn-sm" onClick={() => void checkPrevious()}>
            Xem cấu hình vụ trước của lô {codeOf([...picked][0]!)}
          </button>
          {prev?.cfg ? (
            <span>
              Vụ {prev.cfg.season_code} {prev.cfg.season_year} · giống <b style={{ display: "inline" }}>{prev.cfg.variety_code}</b> · SOP phiên bản {prev.cfg.sop_version_no}
            </span>
          ) : null}
          {prev?.problem ? <span className="err">{prev.problem.message}</span> : null}
        </div>
      ) : null}
      <div className="row" style={{ alignItems: "flex-start" }}>
        <div className="field">
          <label htmlFor="open-season">Vụ</label>
          <select id="open-season" className="select" value={seasonCode} onChange={(e) => setSeasonCode(e.target.value)}>
            {seasons.map((s) => (
              <option key={s.code} value={s.code}>
                {s.name}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="open-year">Năm</label>
          <input id="open-year" className="input" type="number" min={2020} max={2100} value={year} onChange={(e) => setYear(e.target.value)} style={{ width: 100 }} />
        </div>
        {!copy ? (
          <>
            <div className="field">
              <label htmlFor="open-variety">Giống lúa</label>
              <select id="open-variety" className="select" value={variety} aria-invalid={touched && !!errors.variety} onChange={(e) => setVariety(e.target.value)}>
                <option value="">— Chọn giống —</option>
                {varieties.map((v) => (
                  <option key={v.code} value={v.code}>
                    {v.name}
                  </option>
                ))}
              </select>
              {touched && errors.variety ? <span className="err">{errors.variety}</span> : null}
            </div>
            <div className="field">
              <label htmlFor="open-sop">Quy trình SOP</label>
              <select id="open-sop" className="select" value={sop} onChange={(e) => setSop(e.target.value)}>
                <option value="">Tự gán theo giống lúa</option>
                {sops.map((s) => (
                  <option key={s.versionId} value={s.versionId}>
                    {s.t.name} (phiên bản {s.no})
                  </option>
                ))}
              </select>
            </div>
          </>
        ) : null}
        <div className="field">
          <label htmlFor="open-sowing">Ngày gieo sạ</label>
          <input id="open-sowing" className="input" type="date" value={sowing} aria-invalid={touched && !!errors.sowing} onChange={(e) => setSowing(e.target.value)} />
          {touched && errors.sowing ? <span className="err">{errors.sowing}</span> : null}
        </div>
      </div>
      <div>
        <button type="submit" className="btn btn-p btn-sm" disabled={busy}>
          {busy ? "Đang mở vụ…" : `Mở vụ cho ${picked.size} lô`}
        </button>
      </div>
      {problem ? <ProblemAlert problem={problem} /> : null}
      {summary ? (
        <p role="status">
          <b>{summary}</b>
        </p>
      ) : null}
      {results ? <ResultList caption="Kết quả mở vụ theo lô" rows={results} /> : null}
    </form>
  );
}

/** Sửa vụ đang mở bằng bản ghi bù (giống, ngày sạ) — kèm lý do. */
function CorrectDialog({ season, onClose, onDone }: { season: CropSeason; onClose: () => void; onDone: () => void }) {
  const { api } = useAuth();
  const varieties = useCatalog("rice_variety");
  const [variety, setVariety] = useState(season.variety_code);
  const [sowing, setSowing] = useState(season.sowing_date);
  const sowingErr = sowing !== season.sowing_date ? sowingDateError(sowing) : null;
  return (
    <ReasonDialog
      title={`Sửa vụ lô ${season.plot_code}`}
      label="Lý do sửa (ít nhất 10 ký tự, ghi vào sổ)"
      confirmText="Lưu bản ghi bù"
      onClose={onClose}
      run={async (reason, key) => {
        if (sowingErr) throw new ApiProblem({ type: "", title: sowingErr, status: 422, code: "cropcycle.sowing_date_out_of_window" });
        await unwrap(
          api.POST("/v1/cropcycle/seasons/{ref}/corrections", {
            params: { path: { ref: season.season_ref }, header: { "Idempotency-Key": key } },
            body: {
              supersedes_id: season.current_event_id,
              correction_reason: reason,
              variety_code: variety !== season.variety_code ? variety : null,
              sowing_date: sowing !== season.sowing_date ? sowing : null,
            },
          }),
        );
        onDone();
      }}
    >
      <p className="small mute">Bản ghi gốc được giữ nguyên; bản ghi bù thay thế giá trị hiện hành (RB-1). GPS và thời điểm mở vụ không sửa được.</p>
      <div className="row">
        <div className="field">
          <label htmlFor="cor-variety">Giống lúa</label>
          <select id="cor-variety" className="select" value={variety} onChange={(e) => setVariety(e.target.value)}>
            {varieties.map((v) => (
              <option key={v.code} value={v.code}>
                {v.name}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="cor-sowing">Ngày gieo sạ</label>
          <input id="cor-sowing" className="input" type="date" value={sowing} onChange={(e) => setSowing(e.target.value)} />
          {sowingErr ? <span className="err">{sowingErr}</span> : null}
        </div>
      </div>
    </ReasonDialog>
  );
}

function SeasonDetailDialog({ season, onClose }: { season: CropSeason; onClose: () => void }) {
  const { api } = useAuth();
  const d = useAsync(() => unwrap(api.GET("/v1/cropcycle/seasons/{ref}", { params: { path: { ref: season.season_ref } } })), [api, season.season_ref]);
  const ACTION: Record<string, string> = { opened: "Mở vụ", corrected: "Sửa (bản ghi bù)", completed: "Hoàn thành vụ", cancelled: "Hủy vụ" };
  return (
    <Dialog title={`Vụ ${season.season_code} ${season.season_year} — lô ${season.plot_code}`} onClose={onClose}>
      {d.error ? <ProblemAlert problem={d.error} /> : null}
      {d.loading && !d.data ? <Loading /> : null}
      {d.data ? (
        <div className="stack" style={{ gap: 10 }}>
          {d.data.progress ? (
            <p className="small">
              Tiến độ SOP (chỉ nhật ký đã duyệt): <b style={{ display: "inline" }}>{d.data.progress.sop_steps_done}/{d.data.progress.sop_steps}</b> bước · nhật ký chờ duyệt {d.data.progress.pending_logs}
            </p>
          ) : null}
          <ol style={{ margin: "0 0 0 18px" }} aria-label="Lịch sử vụ">
            {d.data.events.map((ev) => (
              <li key={ev.id} className="small" style={{ marginBottom: 4 }}>
                <b style={{ display: "inline" }}>{ACTION[ev.action] ?? ev.action}</b> · {formatDate(ev.event_time)} · giống {ev.variety_code} · sạ {formatDate(ev.sowing_date)}
                {ev.correction_reason ? <div className="mute">Lý do: {ev.correction_reason}</div> : null}
              </li>
            ))}
          </ol>
        </div>
      ) : null}
    </Dialog>
  );
}

/** Vụ mùa của HTX (HTX US-SEASON-02/03/04): lọc, mở vụ hộ/hàng loạt, sửa/hủy có lý do. KN chỉ xem. */
export default function SeasonsPage() {
  const { api } = useAuth();
  const scope = useHtxScope();
  const seasons = useCatalog("season");
  const varieties = useCatalog("rice_variety");
  const [status, setStatus] = useState<"" | "open" | "completed" | "cancelled">("open");
  const [seasonCode, setSeasonCode] = useState("");
  const [year, setYear] = useState("");
  const [variety, setVariety] = useState("");
  const [showOpen, setShowOpen] = useState(false);
  const [version, setVersion] = useState(0);
  const [dialog, setDialog] = useState<{ kind: "correct" | "cancel" | "detail"; s: CropSeason } | null>(null);
  const fields = useCoopFields(scope.coopRef);

  const list = usePagedList(
    (cursor) =>
      unwrap(
        api.GET("/v1/cropcycle/seasons", {
          params: {
            query: {
              cooperative_ref: scope.coopRef || undefined,
              status: status || undefined,
              season_code: seasonCode || undefined,
              season_year: year ? Number(year) : undefined,
              variety_code: variety || undefined,
              cursor,
              limit: 50,
            },
          },
        }),
      ),
    [api, scope.coopRef, status, seasonCode, year, variety, version],
  );
  const reload = () => setVersion((v) => v + 1);
  const seasonName = useMemo(() => new Map(seasons.map((s) => [s.code, s.name])), [seasons]);

  return (
    <>
      <PageHead
        title="Mùa vụ"
        subtitle="Vụ của từng lô ruộng — mỗi lô tối đa một vụ đang mở; sửa và hủy bằng bản ghi bù kèm lý do"
        actions={
          scope.canWrite && scope.coopRef ? (
            <button type="button" className="btn btn-p btn-sm" aria-expanded={showOpen} onClick={() => setShowOpen((v) => !v)}>
              <Icon name="season" size={15} />
              Mở vụ cho nông hộ
            </button>
          ) : null
        }
      />
      <ReadOnlyNotice scope={scope} />
      {scope.canWrite && !scope.coopRef ? <p className="small mute">Chọn một HTX để mở vụ.</p> : null}
      {showOpen && scope.coopRef ? <OpenPanel scope={scope} onDone={reload} /> : null}
      <div className="card sec">
        <form className="row" role="search" style={{ marginBottom: 14, alignItems: "flex-end" }} onSubmit={(e) => e.preventDefault()}>
          <CoopPicker scope={scope} />
          <div className="field">
            <label htmlFor="f-status">Trạng thái</label>
            <select id="f-status" className="select" value={status} onChange={(e) => setStatus(e.target.value as typeof status)}>
              <option value="">Tất cả</option>
              <option value="open">Đang canh tác</option>
              <option value="completed">Đã hoàn thành vụ</option>
              <option value="cancelled">Đã hủy</option>
            </select>
          </div>
          <div className="field">
            <label htmlFor="f-season">Vụ</label>
            <select id="f-season" className="select" value={seasonCode} onChange={(e) => setSeasonCode(e.target.value)}>
              <option value="">Tất cả</option>
              {seasons.map((s) => (
                <option key={s.code} value={s.code}>
                  {s.name}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="f-year">Năm</label>
            <input id="f-year" className="input" type="number" value={year} placeholder={String(YEAR)} onChange={(e) => setYear(e.target.value)} style={{ width: 100 }} />
          </div>
          <div className="field">
            <label htmlFor="f-variety">Giống</label>
            <select id="f-variety" className="select" value={variety} onChange={(e) => setVariety(e.target.value)}>
              <option value="">Tất cả</option>
              {varieties.map((v) => (
                <option key={v.code} value={v.code}>
                  {v.name}
                </option>
              ))}
            </select>
          </div>
        </form>
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">Danh sách vụ</caption>
            <thead>
              <tr>
                <th scope="col">Lô ruộng</th>
                <th scope="col">Nông hộ</th>
                <th scope="col">Vụ</th>
                <th scope="col">Giống · SOP</th>
                <th scope="col">Ngày sạ</th>
                <th scope="col">Dự kiến thu hoạch</th>
                <th scope="col" className="num">Diện tích (ha)</th>
                <th scope="col">Vị trí</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">Thao tác</th>
              </tr>
            </thead>
            <tbody>
              {list.items.map((s) => (
                <tr key={s.season_ref}>
                  <td>
                    <b>{s.plot_code}</b>
                    {!scope.coopRef ? <div className="mute small">{scope.coopName(s.cooperative_ref)}</div> : null}
                  </td>
                  <td>
                    {scope.coopRef ? fields.householdLabel(s.household_ref) : s.household_ref ? `Nông hộ ${s.household_ref.slice(-4)}` : "—"}
                    {s.on_behalf ? <div className="mute small">Mở hộ</div> : null}
                  </td>
                  <td>
                    {seasonName.get(s.season_code) ?? s.season_code} {s.season_year}
                  </td>
                  <td>
                    {s.variety_code}
                    <div className="mute small">SOP phiên bản {s.sop_version_no ?? "—"}</div>
                  </td>
                  <td>{formatDate(s.sowing_date)}</td>
                  <td>{formatDate(s.expected_harvest_date)}</td>
                  <td className="num">{formatDecimal(s.area_ha, 2)}</td>
                  <td>
                    <LocationChip status={s.location_status} />
                  </td>
                  <td>
                    <StatusChip map={SEASON_STATUS} value={s.status} />
                  </td>
                  <td>
                    <div className="row" style={{ gap: 6 }}>
                      <button type="button" className="btn btn-o btn-sm" onClick={() => setDialog({ kind: "detail", s })} aria-label={`Lịch sử vụ lô ${s.plot_code}`}>
                        Lịch sử
                      </button>
                      {scope.canWrite && s.status === "open" ? (
                        <>
                          <button type="button" className="btn btn-o btn-sm" onClick={() => setDialog({ kind: "correct", s })} aria-label={`Sửa vụ lô ${s.plot_code}`}>
                            Sửa
                          </button>
                          <button type="button" className="btn btn-o btn-sm" onClick={() => setDialog({ kind: "cancel", s })} aria-label={`Hủy vụ lô ${s.plot_code}`}>
                            Hủy
                          </button>
                        </>
                      ) : null}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {list.loading ? <Loading /> : null}
        {!list.loading && !list.error && list.items.length === 0 ? <Empty>Không có vụ phù hợp bộ lọc.</Empty> : null}
        <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
      </div>
      {dialog?.kind === "detail" ? <SeasonDetailDialog season={dialog.s} onClose={() => setDialog(null)} /> : null}
      {dialog?.kind === "correct" ? (
        <CorrectDialog
          season={dialog.s}
          onClose={() => setDialog(null)}
          onDone={() => {
            setDialog(null);
            reload();
          }}
        />
      ) : null}
      {dialog?.kind === "cancel" ? (
        <ReasonDialog
          title={`Hủy vụ lô ${dialog.s.plot_code}`}
          label="Lý do hủy (ít nhất 10 ký tự, ghi vào sổ)"
          confirmText="Hủy vụ"
          danger
          onClose={() => setDialog(null)}
          run={async (reason, key) => {
            await unwrap(
              api.POST("/v1/cropcycle/seasons/{ref}/cancel", {
                params: { path: { ref: dialog.s.season_ref }, header: { "Idempotency-Key": key } },
                body: { supersedes_id: dialog.s.current_event_id, reason },
              }),
            );
            setDialog(null);
            reload();
          }}
        >
          <p className="small mute">Chỉ hủy được vụ mở nhầm, chưa khai sản lượng. Vụ không bị xoá — ghi bản ghi hủy kèm lý do.</p>
        </ReasonDialog>
      ) : null}
    </>
  );
}

