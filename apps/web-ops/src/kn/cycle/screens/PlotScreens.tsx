"use client";

// Cấp Xã › Dữ liệu › Vẽ thửa ruộng (FN-XA-03.05) — TT-11 (#104, PRD kn-bugfix-brd-alignment):
// - Sửa được thửa; bắt buộc Tên thửa, Chủ hộ, Xã/phường, Cây trồng (R-A4).
// - Diện tích tính từ hình vẽ, không nhập (máy chủ tính lại trên hình; số ở web chỉ để xem trước).
// - Chồng lấn thửa khác: máy chủ chặn (409 geo.plot_overlap, #113) — web báo rõ và không lưu phần nào sau đó.
// Hình, tên, chủ hộ, HTX ở pf/cooperative (/v1/kn/plots); cây trồng ở hồ sơ KN riêng (/v1/kn/plot-profiles, #119).
// #162 mục 1: ô HTX chọn theo hồ sơ KN riêng (HtxRecordPicker, giá trị = htx_ref); thửa cũ tham chiếu HTX chưa có hồ sơ KN
// liên kết vẫn hiện, ghi rõ "chưa liên kết hồ sơ KN". Ghi thửa vẫn qua /v1/kn/plots (mục 3 chờ PO).
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { fieldErrorMap, type ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { SelectField, TextField, validateForm, type FieldRule } from "@/components/form";
import { DetailPage, LogPanel } from "@/components/patterns/detail";
import { DataTable, FilterBar, PageHeader, RowActions, StatusPill, type Column } from "@/components/patterns/list";
import type { AnyFeatureCollection } from "@/components/LayerMap";
import { PiiHint } from "@/kn/pii";
import { boundsOf, m2ToHa, polygonAreaM2, polygonGeometry, validatePolygon, type LngLat } from "../plotGeometry";
import type { KnPlot, KnPlotDetail, KnPlotUpdate, PlotShapes } from "../api";
import { LoadState, num, useAction, useCommuneNames, useCycleReadOnly, useLoad } from "../common";
import { GapNotice, useCommuneFilter } from "./DataScreens";
import { HtxRecordPicker } from "./HtxRecordPicker";
import { PlotDrawPanel, PlotEditorMap } from "./PlotMap";

/** Khoảng diện tích máy chủ nhận cho thửa (geo.area_out_of_range: 0,001 – 1.000 ha). */
export const PLOT_AREA_MIN_HA = 0.001;
export const PLOT_AREA_MAX_HA = 1000;

/** Diện tích trắc địa (ha) của nét vẽ đa giác; < 3 đỉnh → 0. */
export function drawnAreaHa(coords: readonly LngLat[]): number {
  return coords.length < 3 ? 0 : m2ToHa(polygonAreaM2([[...coords, coords[0]!]]));
}

/** Kiểm hình thửa trước khi lưu (≥ 3 điểm phân biệt, không thẳng hàng, diện tích trong khoảng máy chủ nhận). */
export function plotDraftError(coords: readonly LngLat[]): string | undefined {
  if (coords.length === 0) return "Bắt buộc vẽ ranh giới thửa trên bản đồ.";
  const bad = validatePolygon(coords);
  if (bad) return bad;
  const ha = drawnAreaHa(coords);
  if (ha < PLOT_AREA_MIN_HA) return "Diện tích phải từ 0,001 ha.";
  if (ha > PLOT_AREA_MAX_HA) return "Diện tích không được lớn hơn 1.000 ha.";
  return undefined;
}

/** Vòng ngoài của hình thửa (Polygon hoặc đa giác đầu của MultiPolygon), bỏ điểm khép vòng — để vẽ lại. */
export function outerRing(geometry: { type: string; coordinates: unknown[] } | null | undefined): LngLat[] {
  if (!geometry) return [];
  const ring = (geometry.type === "MultiPolygon" ? (geometry.coordinates[0] as unknown[] | undefined)?.[0] : geometry.coordinates[0]) as LngLat[] | undefined;
  if (!ring?.length) return [];
  const [a, b] = [ring[0]!, ring[ring.length - 1]!];
  return ring.length > 1 && a[0] === b[0] && a[1] === b[1] ? ring.slice(0, -1) : ring.slice();
}

type PlotForm = { commune_code: string; lot_name: string; farmer_name: string; crop_code: string; cooperative_ref: string; note: string; reason: string };

/** Quy tắc form thửa (TT-11). Sửa: Chủ hộ để trống = giữ chủ hộ hiện tại (tên thật nằm ở kho định danh, web chỉ thấy nhãn che). */
export function plotFormRules(mode: "create" | "edit", hasFarmer: boolean, boundaryChanged: boolean): Partial<Record<keyof PlotForm, FieldRule>> {
  return {
    commune_code: { kind: "text", required: mode === "create" },
    lot_name: { kind: "text", required: true, maxLength: 200 },
    farmer_name: { kind: "text", required: mode === "create" || !hasFarmer, maxLength: 200 },
    crop_code: { kind: "text", required: true },
    note: { kind: "text", maxLength: 1000 },
    ...(boundaryChanged ? { reason: { kind: "text", required: true, maxLength: 500 } as FieldRule } : {}),
  };
}

/** Thân PUT /v1/kn/plots/{ref}: chỉ trường đã đổi (trường vắng = giữ nguyên). Không đổi gì → null (không gọi). */
export function plotUpdateBody(plot: Pick<KnPlot, "lot_name" | "note" | "cooperative_ref">, f: Pick<PlotForm, "lot_name" | "farmer_name" | "cooperative_ref" | "note">): KnPlotUpdate | null {
  const b: KnPlotUpdate = {};
  if (f.lot_name.trim() !== (plot.lot_name ?? "")) b.lot_name = f.lot_name.trim();
  if (f.farmer_name.trim()) b.farmer_name = f.farmer_name.trim();
  if ((f.cooperative_ref || null) !== (plot.cooperative_ref ?? null)) b.cooperative_ref = f.cooperative_ref || null;
  if (f.note.trim() !== (plot.note ?? "")) b.note = f.note.trim() || null;
  return Object.keys(b).length ? b : null;
}

/** Lớp ngữ cảnh: các thửa khác của xã (xanh), thửa đang sửa (cam) khi không vẽ lại. */
function contextOf(shapes: PlotShapes | null | undefined, currentRef?: string, showCurrent = false): AnyFeatureCollection | null {
  if (!shapes) return null;
  return {
    type: "FeatureCollection",
    features: shapes.features
      .filter((f) => showCurrent || f.properties?.ref !== currentRef)
      .map((f) => ({ ...f, properties: { ...(f.properties ?? {}), ...(f.properties?.ref === currentRef ? { fill: "#f0871a", stroke: "#b85c00" } : {}) } })),
  };
}

function shapesBounds(shapes: PlotShapes | null | undefined): [LngLat, LngLat] | undefined {
  return boundsOf((shapes?.features ?? []).flatMap((f) => outerRing(f.geometry as { type: string; coordinates: unknown[] })));
}

/** Báo chồng lấn rõ ràng (máy chủ chặn, không có "vẫn lưu"). */
function OverlapNotice({ problem }: { problem: ApiProblem | null }) {
  if (problem?.code !== "geo.plot_overlap") return null;
  return (
    <div className="alert alert-bad" role="alert" style={{ marginBottom: 12 }}>
      <span>Thửa chồng lấn thửa đã có trong xã nên không được lưu. Vẽ lại ranh giới để không đè lên các thửa tô màu trên bản đồ.</span>
    </div>
  );
}

function useCropOptions() {
  const { collectApi } = useAuth();
  const s = useLoad(() => collectApi.crops(), []);
  // Chỉ mục lá đang dùng (mục nhóm is_group = "true" không phải cây trồng cụ thể).
  return (s.data?.items ?? []).filter((c) => c.active !== false && c.attrs?.is_group !== "true").map((c) => ({ value: c.code, label: c.name }));
}

/**
 * Chỉ xem thửa ruộng: theo máy chủ (cooperative/plot knWriter và hồ sơ cây trồng #119) chỉ Cấp Xã/Admin ghi thửa.
 * Admin ghi qua chế độ hỗ trợ (vai đóng = kn_xa); ngoài đó đã chỉ xem theo useCycleReadOnly. Nên Cấp Tỉnh và
 * Cấp Quốc gia chỉ xem. Không đổi useCycleReadOnly chung vì Cấp Tỉnh còn quyền ghi ở màn khác (review Codex #104).
 */
export function usePlotReadOnly(): boolean {
  const { kn } = useAuth();
  const cycleReadOnly = useCycleReadOnly();
  return cycleReadOnly || kn.context?.role !== "kn_xa";
}

/** Diện tích tự tính — chỉ đọc (TT-11: không nhập). */
function AreaReadout({ ha, note }: { ha: string; note: string }) {
  return (
    <div className="field">
      <label htmlFor="plot-area">Diện tích tự tính (ha)</label>
      <input id="plot-area" className="input" value={ha} readOnly aria-readonly />
      <span className="mute hint">{note}</span>
    </div>
  );
}

/** FN-XA-03.05 Vẽ thửa ruộng — danh sách thửa (diện tích tự tính, cây trồng ở hồ sơ KN); Thêm/Sửa trên trang riêng. */
export function PlotsScreen() {
  const { collectApi } = useAuth();
  const readOnly = usePlotReadOnly();
  const f = useCommuneFilter();
  const [page, setPage] = useState(1);
  const s = useLoad(async () => {
    const [plots, crops] = await Promise.all([collectApi.plots({ commune: f.commune || undefined }), collectApi.plotCrops(f.commune || undefined).catch(() => null)]);
    return { items: plots.items, crops: new Map((crops?.items ?? []).map((c) => [c.plot_ref, c.crop_name])), cropsLoaded: !!crops };
  }, [f.commune]);
  const name = useCommuneNames(s.data?.items.map((p) => p.commune_code) ?? []);
  const columns: Column<KnPlot>[] = [
    { key: "code", header: "Mã thửa", cell: (p) => p.code },
    { key: "lot", header: "Tên thửa", cell: (p) => p.lot_name ?? "—" },
    { key: "commune", header: "Xã/phường", cell: (p) => name(p.commune_code) },
    { key: "farmer", header: "Chủ hộ", cell: (p) => p.farmer?.label ?? "—" },
    {
      key: "crop",
      header: "Cây trồng",
      cell: (p) => s.data?.crops.get(p.ref) ?? (s.data?.cropsLoaded ? <StatusPill tone="warn">Chưa khai báo</StatusPill> : "—"),
    },
    { key: "area", header: "Diện tích tự tính (ha)", cell: (p) => num(p.area_ha, 4), className: "num" },
    { key: "st", header: "Trạng thái", cell: (p) => <StatusPill tone={p.status === "active" ? "ok" : "neutral"}>{p.status === "active" ? "Đang dùng" : p.status === "draft" ? "Nháp" : "Ngừng"}</StatusPill> },
    { key: "act", header: "Thao tác", cell: (p) => <RowActions label={`thửa ${p.code}`} viewHref={readOnly ? `/kn/du-lieu/ve-thua/${p.ref}` : undefined} editHref={readOnly ? undefined : `/kn/du-lieu/ve-thua/${p.ref}`} /> },
  ];
  return (
    <>
      <PageHeader
        title="Vẽ thửa ruộng"
        actions={
          readOnly ? null : (
            <Link className="btn btn-p" href="/kn/du-lieu/ve-thua/them-moi">
              Thêm thửa
            </Link>
          )
        }
      />
      <GapNotice what="Web vẽ thửa dạng đa giác (bấm từng đỉnh hoặc nhập tọa độ). Vẽ hình tròn, hình vuông và vẽ ngoại tuyến dùng ứng dụng di động Cán bộ xã." />
      <FilterBar>{f.chip}</FilterBar>
      <LoadState s={s}>{(d) => <DataTable caption="Thửa ruộng" columns={columns} rows={d.items.slice((page - 1) * 20, page * 20)} rowKey={(p) => p.ref} page={page} pageSize={20} total={d.items.length} onPage={setPage} empty="Chưa có thửa." />}</LoadState>
    </>
  );
}

/** Thêm thửa (TT-11): vẽ ranh giới đa giác, diện tích tự tính; lưu thửa rồi cây trồng trong cùng một lượt thao tác. */
export function PlotCreateScreen() {
  const { collectApi } = useAuth();
  const router = useRouter();
  const readOnly = usePlotReadOnly();
  const { communes } = useCommuneFilter();
  const act = useAction();
  const crops = useCropOptions();
  const [form, setForm] = useState<PlotForm>(() => ({ commune_code: communes.length === 1 ? communes[0]!.code : "", lot_name: "", farmer_name: "", crop_code: "", cooperative_ref: "", note: "", reason: "" }));
  const [coords, setCoords] = useState<LngLat[]>([]);
  const [errors, setErrors] = useState<Partial<Record<keyof PlotForm | "geometry", string>>>({});
  const shapes = useLoad(form.commune_code ? () => collectApi.plotShapes(form.commune_code).catch(() => null) : null, [form.commune_code]);
  const context = useMemo(() => contextOf(shapes.data), [shapes.data]);
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  const set = (k: keyof PlotForm) => (v: string) => setForm((x) => ({ ...x, [k]: v }));
  const err = (k: keyof PlotForm | "geometry") => errors[k] ?? server[k];
  const save = async () => {
    const v = validateForm(form, plotFormRules("create", false, false));
    const g = plotDraftError(coords);
    setErrors({ ...v.errors, ...(g ? { geometry: g } : {}) });
    if (!v.valid || g) return;
    const b = v.values;
    const r = await act.run(async (idem) => {
      const p = await collectApi.createPlot({
        commune_code: b.commune_code,
        lot_name: b.lot_name.trim(),
        farmer_name: b.farmer_name.trim(),
        ...(b.cooperative_ref ? { cooperative_ref: b.cooperative_ref } : {}),
        ...(b.note.trim() ? { note: b.note.trim() } : {}),
        shape: "polygon",
        geometry: polygonGeometry(coords) as KnPlotDetail["geometry"],
      });
      // Sau await: bọc bằng idem của CÙNG lượt để bấm lại sau mất phản hồi giữ khóa của mọi bước (#129).
      await idem(() => collectApi.putPlotCrop(p.ref, b.crop_code));
      return p;
    });
    if (r) router.push(`/kn/du-lieu/ve-thua/${r.ref}`);
  };
  const ha = drawnAreaHa(coords);
  // Vào thẳng URL khi không có quyền ghi thửa (Cấp Tỉnh/Quốc gia/Admin ngoài hỗ trợ): không hiện form, không có Lưu.
  if (readOnly)
    return (
      <DetailPage back={{ href: "/kn/du-lieu/ve-thua", label: "Vẽ thửa ruộng" }} title="Thêm thửa">
        <p className="alert alert-warn">Chỉ Cấp Xã được thêm thửa. Tài khoản của bạn chỉ xem danh sách thửa.</p>
      </DetailPage>
    );
  return (
    <DetailPage
      back={{ href: "/kn/du-lieu/ve-thua", label: "Vẽ thửa ruộng" }}
      title="Thêm thửa"
      actions={
        <>
          <Link className="btn btn-o" href="/kn/du-lieu/ve-thua">
            Hủy
          </Link>
          <button type="button" className="btn btn-p" disabled={act.busy} onClick={() => void save()}>
            Lưu
          </button>
        </>
      }
    >
      {act.view}
      <OverlapNotice problem={act.problem} />
      <div className="grid2">
        <SelectField label="Xã/phường" required value={form.commune_code} onChange={(v) => (set("commune_code")(v), set("cooperative_ref")(""))} error={err("commune_code")} options={communes.map((c) => ({ value: c.code, label: c.name }))} />
        <TextField label="Tên thửa" required value={form.lot_name} onChange={set("lot_name")} error={err("lot_name")} maxLength={200} />
        <TextField label="Chủ hộ" required value={form.farmer_name} onChange={set("farmer_name")} error={err("farmer_name")} maxLength={200} hint="Lưu ở kho định danh, hiển thị che." />
        <SelectField label="Cây trồng" required value={form.crop_code} onChange={set("crop_code")} error={err("crop_code")} options={crops} />
        {form.commune_code ? (
          <HtxRecordPicker value={form.cooperative_ref} onChange={set("cooperative_ref")} commune={form.commune_code} error={err("cooperative_ref")} placeholder="Không thuộc HTX" />
        ) : (
          <SelectField label="HTX" value="" onChange={() => undefined} disabled placeholder="Chọn Xã/phường trước" options={[]} />
        )}
        <AreaReadout ha={coords.length >= 3 ? num(ha.toFixed(4), 4) : ""} note="Tính từ hình vẽ, không nhập. Máy chủ tính lại khi lưu." />
        <TextField label="Ghi chú" multiline value={form.note} onChange={set("note")} error={err("note")} maxLength={1000} hint={<PiiHint text={form.note} />} />
      </div>
      <section aria-label="Ranh giới thửa" className="stack" style={{ marginTop: 12 }}>
        <h2>
          Ranh giới thửa<span className="req" aria-hidden="true"> *</span>
        </h2>
        <PlotEditorMap label="Bản đồ vẽ thửa" context={context} drawing coords={coords} onCoords={(c) => (setCoords(c), setErrors((x) => ({ ...x, geometry: undefined })))} focus={coords.length ? null : { key: `xa-${form.commune_code}`, bounds: shapesBounds(shapes.data) }} />
        <PlotDrawPanel coords={coords} onCoords={(c) => (setCoords(c), setErrors((x) => ({ ...x, geometry: undefined })))} />
        {err("geometry") ? (
          <span className="err" role="alert">
            {err("geometry")}
          </span>
        ) : null}
      </section>
    </DetailPage>
  );
}

/** Sửa thửa (TT-11): thuộc tính, cây trồng và (tùy chọn) vẽ lại ranh giới — phiên bản mới, chặn chồng lấn. */
export function PlotEditScreen() {
  const { id } = useParams<{ id: string }>();
  const { collectApi } = useAuth();
  const router = useRouter();
  const readOnly = usePlotReadOnly();
  const act = useAction();
  const crops = useCropOptions();
  const s = useLoad(async () => {
    const [plot, profile, log] = await Promise.all([collectApi.plot(id), collectApi.plotProfile(id), collectApi.plotLog(id).catch(() => null)]);
    return { plot, profile, log: log?.items ?? [] };
  }, [id]);
  const commune = s.data?.plot.commune_code ?? "";
  const shapes = useLoad(commune ? () => collectApi.plotShapes(commune).catch(() => null) : null, [commune]);
  const name = useCommuneNames(commune ? [commune] : []);
  const [form, setForm] = useState<PlotForm | null>(null);
  const [redraw, setRedraw] = useState(false);
  const [coords, setCoords] = useState<LngLat[]>([]);
  const [errors, setErrors] = useState<Partial<Record<keyof PlotForm | "geometry", string>>>({});
  const server = act.problem ? fieldErrorMap(act.problem) : {};
  const err = (k: keyof PlotForm | "geometry") => errors[k] ?? server[k] ?? (k === "geometry" ? server.supersedes_boundary_id : undefined);
  const context = useMemo(() => contextOf(shapes.data, id, !redraw), [shapes.data, id, redraw]);
  const back = "/kn/du-lieu/ve-thua";
  return (
    <LoadState s={s}>
      {({ plot, profile, log }) => {
        const f: PlotForm = form ?? { commune_code: plot.commune_code, lot_name: plot.lot_name ?? "", farmer_name: "", crop_code: profile.crop_code ?? "", cooperative_ref: plot.cooperative_ref ?? "", note: plot.note ?? "", reason: "" };
        const set = (k: keyof PlotForm) => (v: string) => setForm({ ...f, [k]: v });
        const locked = readOnly || plot.status === "retired";
        const startRedraw = () => (setRedraw(true), setCoords(outerRing(plot.geometry)));
        const save = async () => {
          const v = validateForm(f, plotFormRules("edit", !!plot.farmer?.label, redraw));
          const g = redraw ? plotDraftError(coords) : undefined;
          const reasonShort = redraw && f.reason.trim() && f.reason.trim().length < 10 ? "Tối thiểu 10 ký tự." : undefined;
          setErrors({ ...v.errors, ...(g ? { geometry: g } : {}), ...(reasonShort ? { reason: reasonShort } : {}) });
          if (!v.valid || g || reasonShort) return;
          const body = plotUpdateBody(plot, f);
          const ok = await act.run(async (idem) => {
            // Ranh giới trước: chồng lấn (409) thì dừng, không ghi thuộc tính hay cây trồng. Mọi bước bọc idem của cùng
            // lượt (bước sau await) để bấm lại sau mất phản hồi dùng lại khóa cũ (#129).
            if (redraw) await idem(() => collectApi.correctPlotBoundary(id, { supersedes_boundary_id: plot.boundary_id, geometry: polygonGeometry(coords) as KnPlotDetail["geometry"], shape: "polygon", reason: f.reason.trim() }));
            if (body) await idem(() => collectApi.updatePlot(id, body));
            if (f.crop_code !== profile.crop_code) await idem(() => collectApi.putPlotCrop(id, f.crop_code));
            return true;
          });
          if (ok) router.push(back);
        };
        const ha = redraw ? (coords.length >= 3 ? num(drawnAreaHa(coords).toFixed(4), 4) : "") : num(plot.area_ha, 4);
        return (
          <DetailPage
            back={{ href: back, label: "Vẽ thửa ruộng" }}
            title={`${locked ? "Thửa" : "Sửa thửa"} ${plot.code}`}
            badge={<StatusPill tone={plot.status === "active" ? "ok" : "neutral"}>{plot.status === "active" ? "Đang dùng" : plot.status === "draft" ? "Nháp" : "Ngừng"}</StatusPill>}
            actions={
              locked ? null : (
                <>
                  <Link className="btn btn-o" href={back}>
                    Hủy
                  </Link>
                  <button type="button" className="btn btn-p" disabled={act.busy} onClick={() => void save()}>
                    Lưu
                  </button>
                </>
              )
            }
            log={<LogPanel entries={log.map((e) => ({ event: e.event, actor_display: e.actor_display ?? "—", at: e.at, reason: e.reason }))} />}
          >
            {act.view}
            <OverlapNotice problem={act.problem} />
            {plot.status === "retired" ? <p className="mute">Thửa đã ngừng dùng — chỉ xem.</p> : readOnly ? <p className="mute">Chỉ xem — chỉ Cấp Xã sửa thửa.</p> : null}
            {!profile.crop_code && !locked ? <p className="alert alert-warn">Thửa chưa khai báo cây trồng — chọn cây trồng rồi Lưu.</p> : null}
            <div className="grid2">
              <div className="field">
                <label htmlFor="plot-commune">
                  Xã/phường<span className="req" aria-hidden="true"> *</span>
                </label>
                <input id="plot-commune" className="input" value={name(plot.commune_code)} readOnly aria-readonly />
                <span className="mute hint">Theo vị trí thửa, không đổi khi sửa.</span>
              </div>
              <TextField label="Tên thửa" required disabled={locked} value={f.lot_name} onChange={set("lot_name")} error={err("lot_name")} maxLength={200} />
              <TextField
                label="Chủ hộ"
                required={!plot.farmer?.label}
                disabled={locked}
                value={f.farmer_name}
                onChange={set("farmer_name")}
                error={err("farmer_name")}
                maxLength={200}
                placeholder={plot.farmer?.label ? `Hiện tại: ${plot.farmer.label}` : undefined}
                hint={plot.farmer?.label ? `Hiện tại: ${plot.farmer.label}. Để trống = giữ nguyên; nhập họ tên để đổi chủ hộ.` : "Bắt buộc nhập (thửa chưa có chủ hộ)."}
              />
              <SelectField label="Cây trồng" required disabled={locked} value={f.crop_code} onChange={set("crop_code")} error={err("crop_code")} options={crops} />
              <HtxRecordPicker value={f.cooperative_ref} onChange={set("cooperative_ref")} commune={plot.commune_code} disabled={locked} error={err("cooperative_ref")} placeholder="Không thuộc HTX" />
              <AreaReadout ha={ha} note={redraw ? "Tính từ hình đang vẽ lại, không nhập." : "Tính từ hình đã lưu, không nhập."} />
              <TextField label="Ghi chú" multiline disabled={locked} value={f.note} onChange={set("note")} error={err("note")} maxLength={1000} hint={<PiiHint text={f.note} />} />
            </div>
            <section aria-label="Ranh giới thửa" className="stack" style={{ marginTop: 12 }}>
              <div className="row" style={{ gap: 8, alignItems: "center" }}>
                <h2 style={{ margin: 0 }}>Ranh giới thửa</h2>
                {locked ? null : redraw ? (
                  <button type="button" className="btn btn-o btn-sm" onClick={() => (setRedraw(false), setCoords([]), setErrors((x) => ({ ...x, geometry: undefined, reason: undefined })))}>
                    Bỏ vẽ lại
                  </button>
                ) : (
                  <button type="button" className="btn btn-o btn-sm" onClick={startRedraw}>
                    Sửa ranh giới
                  </button>
                )}
              </div>
              <PlotEditorMap
                label="Bản đồ thửa"
                context={context}
                drawing={redraw}
                coords={coords}
                onCoords={(c) => (setCoords(c), setErrors((x) => ({ ...x, geometry: undefined })))}
                focus={{ key: `thua-${plot.ref}-${redraw ? "ve" : "xem"}`, bounds: boundsOf(redraw && coords.length ? coords : outerRing(plot.geometry)) }}
              />
              {redraw ? (
                <>
                  <PlotDrawPanel coords={coords} onCoords={(c) => (setCoords(c), setErrors((x) => ({ ...x, geometry: undefined })))} />
                  {err("geometry") ? (
                    <span className="err" role="alert">
                      {err("geometry")}
                    </span>
                  ) : null}
                  <TextField label="Lý do sửa ranh giới" required multiline value={f.reason} onChange={set("reason")} error={err("reason")} maxLength={500} hint={<PiiHint text={f.reason} />} />
                </>
              ) : null}
            </section>
          </DetailPage>
        );
      }}
    </LoadState>
  );
}
