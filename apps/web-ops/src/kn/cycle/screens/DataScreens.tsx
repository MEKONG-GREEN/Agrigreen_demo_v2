"use client";

// Cấp Xã › Dữ liệu (FN-XA-03.03..03.06) trên API P7 /v1/kn/*: HTX & nông hộ (thêm, vô hiệu hóa thay xóa, SĐT che),
// máy móc theo HTX (khai báo thay toàn bộ, tổng xã = Σ HTX), thửa ruộng (danh sách; vẽ ở field-app), vật tư (ghi nhận,
// tổng hợp theo xã). Cấp Tỉnh/Quốc gia chỉ xem (useCycleReadOnly + máy chủ 403 *.read_only).
import { useParams } from "next/navigation";
import { useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { ActionBar, BackLink, DetailLayout, LabelValueBlock, LogPanel, RelatedTabs } from "@/components/patterns/detail";
import { DataTable, FilterBar, FilterChip, PageHeader, RowActions, SearchBox, StatusPill, type Column } from "@/components/patterns/list";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";
import type { KnCooperative, KnPlot, MaterialRecord } from "../api";
import { LoadState, dt, num, useAction, useCommuneNames, useCycleReadOnly, useLoad, ymdLocal } from "../common";

/** Ghi chú phần còn ở nơi khác (nạp Excel, vẽ bản đồ ngoại tuyến…). */
export function GapNotice({ what }: { what: string }) {
  return (
    <div className="alert alert-info" role="note" style={{ marginBottom: 12 }}>
      <Icon name="info" />
      <span>{what}</span>
    </div>
  );
}

function useCommuneFilter() {
  const { kn } = useAuth();
  const communes = kn.context?.communes ?? [];
  const [commune, setCommune] = useState("");
  const chip = <FilterChip label="Xã/phường" value={commune} onChange={setCommune} options={[{ value: "", label: "Tất cả xã phụ trách" }, ...communes.map((c) => ({ value: c.code, label: c.name }))]} />;
  return { commune, chip, communes };
}

/** FN-XA-03.03 Danh sách HTX (Hình 16a). */
export function CoopsScreen() {
  const { collectApi } = useAuth();
  const readOnly = useCycleReadOnly();
  const f = useCommuneFilter();
  const [status, setStatus] = useState<"active" | "inactive" | "all">("active");
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const [adding, setAdding] = useState(false);
  const [form, setForm] = useState({ name: "", commune_code: "", area_ha: "", rep: "", phone: "" });
  const act = useAction();
  const s = useLoad(() => collectApi.cooperatives({ commune: f.commune || undefined, status, q: q || undefined }), [f.commune, status, q, act.note]);
  const name = useCommuneNames(s.data?.items.map((c) => c.commune_code) ?? []);
  const columns: Column<KnCooperative>[] = [
    { key: "name", header: "Tên HTX", cell: (c) => c.name },
    { key: "commune", header: "Xã/phường", cell: (c) => name(c.commune_code) },
    { key: "rep", header: "Người đại diện", cell: (c) => c.representative?.label ?? "—" },
    { key: "phone", header: "SĐT", cell: (c) => c.representative?.masked_phone ?? "—" },
    { key: "area", header: "Diện tích (ha)", cell: (c) => num(c.area_ha), className: "num" },
    { key: "hh", header: "Nông hộ", cell: (c) => c.household_count, className: "num" },
    { key: "st", header: "Trạng thái", cell: (c) => <StatusPill tone={c.active ? "ok" : "neutral"}>{c.active ? "Hoạt động" : "Ngừng hoạt động"}</StatusPill> },
    { key: "act", header: "Thao tác", cell: (c) => <RowActions label={`HTX ${c.name}`} viewHref={`/kn/du-lieu/htx/${c.ref}`} /> },
  ];
  return (
    <>
      <PageHeader
        title="Danh sách HTX"
        actions={
          readOnly ? null : (
            <button type="button" className="btn btn-p" onClick={() => (setForm({ ...form, commune_code: f.communes[0]?.code ?? "" }), setAdding(true))}>
              Thêm HTX
            </button>
          )
        }
      />
      {act.view}
      <FilterBar onReset={() => (setStatus("active"), setQ(""), setPage(1))}>
        {f.chip}
        <FilterChip
          label="Trạng thái"
          value={status}
          defaultValue="active"
          onChange={(v) => (setStatus(v as typeof status), setPage(1))}
          options={[
            { value: "active", label: "Hoạt động" },
            { value: "inactive", label: "Ngừng hoạt động" },
            { value: "all", label: "Tất cả" },
          ]}
        />
        <SearchBox value={q} onChange={(v) => (setQ(v), setPage(1))} placeholder="Tìm theo tên, mã HTX" />
      </FilterBar>
      <LoadState s={s}>
        {(d) => <DataTable caption="Danh sách HTX" columns={columns} rows={d.items.slice((page - 1) * 10, page * 10)} rowKey={(c) => c.ref} page={page} pageSize={10} total={d.items.length} onPage={setPage} empty="Xã phụ trách chưa có HTX." />}
      </LoadState>
      {adding ? (
        <Dialog
          title="Thêm HTX"
          onClose={() => setAdding(false)}
          actions={
            <button
              type="button"
              className="btn btn-p"
              onClick={async () => {
                const r = await act.run(
                  () =>
                    collectApi.createCooperative({
                      name: form.name,
                      commune_code: form.commune_code,
                      ...(form.area_ha ? { area_ha: form.area_ha.replace(",", ".") } : {}),
                      ...(form.rep && form.phone ? { representative: { full_name: form.rep, phone: form.phone } } : {}),
                    }),
                  (x) => `Đã thêm ${x.name} (${x.code}).`,
                );
                if (r) setAdding(false);
              }}
            >
              Lưu
            </button>
          }
        >
          <label className="field">
            <span>Tên HTX*</span>
            <input className="input" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </label>
          <label className="field">
            <span>Xã/phường*</span>
            <select className="input" value={form.commune_code} onChange={(e) => setForm({ ...form, commune_code: e.target.value })}>
              {f.communes.map((c) => (
                <option key={c.code} value={c.code}>
                  {c.name}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span>Diện tích (ha)</span>
            <input className="input" inputMode="decimal" value={form.area_ha} onChange={(e) => setForm({ ...form, area_ha: e.target.value })} />
          </label>
          <label className="field">
            <span>Người đại diện</span>
            <input className="input" value={form.rep} onChange={(e) => setForm({ ...form, rep: e.target.value })} />
          </label>
          <label className="field">
            <span>SĐT người đại diện (lưu ở kho định danh, hiển thị che)</span>
            <input className="input" value={form.phone} onChange={(e) => setForm({ ...form, phone: e.target.value })} />
          </label>
        </Dialog>
      ) : null}
    </>
  );
}

/** FN-XA-03.03 Chi tiết HTX (Hình 16b): Thông tin chung, Nông hộ, Máy móc, Thửa; Thêm nông hộ, Vô hiệu hóa. */
export function CoopDetailScreen() {
  const { id } = useParams<{ id: string }>();
  const { collectApi } = useAuth();
  const readOnly = useCycleReadOnly();
  const act = useAction();
  const s = useLoad(async () => {
    const [c, hh, mm, pl] = await Promise.all([collectApi.cooperative(id), collectApi.households(id), collectApi.coopMachines(id).catch(() => null), collectApi.plots({ cooperative_ref: id }).catch(() => null)]);
    return { c, hh: hh.items, mm, pl: pl?.items ?? [] };
  }, [id, act.note]);
  const [dlg, setDlg] = useState<"hh" | "off" | null>(null);
  const [hh, setHh] = useState({ full_name: "", phone: "", area: "" });
  const [reason, setReason] = useState("");
  useBreadcrumbTail(s.data ? [{ label: s.data.c.name }] : []);
  const name = useCommuneNames(s.data ? [s.data.c.commune_code] : []);
  return (
    <>
      <BackLink href="/kn/du-lieu/htx">Danh sách HTX</BackLink>
      <LoadState s={s}>
        {({ c, hh: households, mm, pl }) => (
          <>
            <PageHeader title={c.name} badge={<StatusPill tone={c.active ? "ok" : "neutral"}>{c.active ? "Hoạt động" : "Ngừng hoạt động"}</StatusPill>} />
            {readOnly || !c.active ? null : (
              <ActionBar>
                <button type="button" className="btn btn-p btn-sm" onClick={() => setDlg("hh")}>
                  Thêm nông hộ
                </button>
                <button type="button" className="btn btn-o btn-sm" onClick={() => setDlg("off")}>
                  Vô hiệu hóa
                </button>
              </ActionBar>
            )}
            {act.view}
            <DetailLayout
              main={
                <>
                  <LabelValueBlock title="Thông tin chung" items={[{ label: "Mã HTX", value: c.code }, { label: "Người đại diện", value: c.representative ? `${c.representative.label} · ${c.representative.masked_phone ?? ""}` : "—" }, { label: "Mã số thuế", value: c.tax_code }, { label: "Lý do ngừng", value: c.status_reason }]} />
                  <LabelValueBlock title="Địa bàn – sản xuất" items={[{ label: "Xã/phường", value: name(c.commune_code) }, { label: "Diện tích (ha)", value: num(c.area_ha) }, { label: "Trụ sở", value: c.head_office }]} />
                  <RelatedTabs
                    tabs={[
                      {
                        id: "hh",
                        label: `Nông hộ (${households.length})`,
                        content: (
                          <ul className="list-plain">
                            {households.map((h) => (
                              <li key={h.ref}>
                                {h.holder.label} · {h.holder.masked_phone ?? "—"} · {num(h.cultivated_area_ha)} ha {h.active ? "" : "(ngừng)"}
                              </li>
                            ))}
                          </ul>
                        ),
                      },
                      { id: "mm", label: `Máy móc (${mm?.total ?? 0})`, content: <ul className="list-plain">{(mm?.lines ?? []).map((l) => <li key={l.machine_type}>{l.machine_type_name}: {l.quantity}</li>)}</ul> },
                      { id: "pl", label: `Thửa ruộng (${pl.length})`, content: <ul className="list-plain">{pl.map((p) => <li key={p.ref}>{p.lot_name ?? p.code} · {num(p.area_ha, 2)} ha</li>)}</ul> },
                    ]}
                  />
                </>
              }
              log={<LogPanel entries={[{ event: "Cập nhật hồ sơ", actor_display: "—", at: c.updated_at }]} />}
            />
            {dlg === "hh" ? (
              <Dialog
                title="Thêm nông hộ"
                onClose={() => setDlg(null)}
                actions={
                  <button type="button" className="btn btn-p" onClick={async () => (await act.run(() => collectApi.addHousehold(id, { holder: { full_name: hh.full_name, phone: hh.phone }, ...(hh.area ? { cultivated_area_ha: hh.area } : {}) }), "Đã thêm nông hộ.")) && setDlg(null)}>
                    Lưu
                  </button>
                }
              >
                <label className="field">
                  <span>Chủ hộ*</span>
                  <input className="input" value={hh.full_name} onChange={(e) => setHh({ ...hh, full_name: e.target.value })} />
                </label>
                <label className="field">
                  <span>SĐT*</span>
                  <input className="input" value={hh.phone} onChange={(e) => setHh({ ...hh, phone: e.target.value })} />
                </label>
                <label className="field">
                  <span>Diện tích canh tác (ha)</span>
                  <input className="input" value={hh.area} onChange={(e) => setHh({ ...hh, area: e.target.value })} />
                </label>
              </Dialog>
            ) : null}
            {dlg === "off" ? (
              <Dialog
                title="Vô hiệu hóa HTX"
                onClose={() => setDlg(null)}
                actions={
                  <button type="button" className="btn btn-p" onClick={async () => (await act.run(() => collectApi.deactivate(id, reason), (r) => `Đã vô hiệu hóa (không xóa). Liên kết: ${r.warnings.households} nông hộ, ${r.warnings.machines ?? 0} máy.`)) && setDlg(null)}>
                    Vô hiệu hóa
                  </button>
                }
              >
                <p className="small">HTX có nông hộ, máy, thửa liên kết vẫn giữ dữ liệu — vô hiệu hóa thay xóa.</p>
                <label className="field">
                  <span>Lý do* (≥ 20 ký tự)</span>
                  <textarea className="textarea" value={reason} onChange={(e) => setReason(e.target.value)} />
                </label>
              </Dialog>
            ) : null}
          </>
        )}
      </LoadState>
    </>
  );
}

/** FN-XA-03.04 Máy móc HTX: trái danh sách HTX + tổng xã theo loại; phải bảng khai báo của HTX đã chọn. */
export function MachinesScreen() {
  const { collectApi } = useAuth();
  const readOnly = useCycleReadOnly();
  const f = useCommuneFilter();
  const act = useAction();
  const coops = useLoad(() => collectApi.cooperatives({ commune: f.commune || undefined }), [f.commune]);
  const overview = useLoad(() => collectApi.machines(f.commune || undefined), [f.commune, act.note]);
  const [ref, setRef] = useState("");
  const cur = useLoad(ref ? () => collectApi.coopMachines(ref) : null, [ref, act.note]);
  const [lines, setLines] = useState<{ machine_type: string; quantity: string; note: string }[] | null>(null);
  const rows = lines ?? (cur.data?.lines ?? []).map((l) => ({ machine_type: l.machine_type, quantity: String(l.quantity), note: l.note ?? "" }));
  const name = useCommuneNames(overview.data?.commune_totals.map((t) => t.commune_code) ?? []);
  const TYPES = [
    ["may_cay", "Máy cày"],
    ["may_cay_xoi", "Máy xới"],
    ["may_gieo_sa", "Máy gieo sạ"],
    ["may_cay_lua", "Máy cấy"],
    ["drone", "Thiết bị bay không người lái"],
    ["may_gat", "Máy gặt đập liên hợp"],
    ["may_say", "Máy sấy"],
  ];
  return (
    <>
      <PageHeader title="Máy móc HTX" />
      <FilterBar>
        {f.chip}
        <FilterChip label="HTX" value={ref} onChange={(v) => (setRef(v), setLines(null))} options={[{ value: "", label: "Chọn HTX" }, ...(coops.data?.items ?? []).map((c) => ({ value: c.ref, label: c.name }))]} />
      </FilterBar>
      {act.view}
      <DetailLayout
        main={
          ref ? (
            <LoadState s={cur}>
              {(m) => (
                <>
                  <h2>{m.name}</h2>
                  <table className="dt" aria-label="Khai báo máy">
                    <thead>
                      <tr>
                        <th scope="col">Loại máy</th>
                        <th scope="col">Số lượng</th>
                        <th scope="col">Ghi chú</th>
                        <th scope="col" />
                      </tr>
                    </thead>
                    <tbody>
                      {rows.map((l, i) => (
                        <tr key={i}>
                          <td>
                            <select className="input" aria-label={`Loại máy dòng ${i + 1}`} disabled={readOnly} value={l.machine_type} onChange={(e) => setLines(rows.map((x, k) => (k === i ? { ...x, machine_type: e.target.value } : x)))}>
                              {TYPES.map(([v, t]) => (
                                <option key={v} value={v}>
                                  {t}
                                </option>
                              ))}
                            </select>
                          </td>
                          <td>
                            <input className="input" aria-label={`Số lượng dòng ${i + 1}`} disabled={readOnly} inputMode="numeric" value={l.quantity} onChange={(e) => setLines(rows.map((x, k) => (k === i ? { ...x, quantity: e.target.value } : x)))} />
                          </td>
                          <td>
                            <input className="input" disabled={readOnly} value={l.note} onChange={(e) => setLines(rows.map((x, k) => (k === i ? { ...x, note: e.target.value } : x)))} />
                          </td>
                          <td>{readOnly ? null : <button type="button" className="btn btn-o btn-sm" aria-label={`Xóa dòng ${i + 1}`} onClick={() => setLines(rows.filter((_, k) => k !== i))}>Xóa</button>}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                  {readOnly ? null : (
                    <div className="row" style={{ gap: 8, marginTop: 8 }}>
                      <button type="button" className="btn btn-o btn-sm" onClick={() => setLines([...rows, { machine_type: "may_cay", quantity: "1", note: "" }])}>
                        + Thêm dòng
                      </button>
                      <button
                        type="button"
                        className="btn btn-p btn-sm"
                        onClick={async () =>
                          (await act.run(() => collectApi.putMachines(ref, rows.map((l) => ({ machine_type: l.machine_type, quantity: Number(l.quantity), note: l.note || null }))), (r) => `Đã lưu khai báo máy (${r.total} máy).`)) && setLines(null)
                        }
                      >
                        Lưu
                      </button>
                    </div>
                  )}
                </>
              )}
            </LoadState>
          ) : (
            <p className="mute">Chọn một HTX để khai báo máy.</p>
          )
        }
        log={
          <aside className="card sec" aria-label="Tổng xã theo loại máy">
            <h2>Tổng xã theo loại máy</h2>
            <ul className="list-plain">
              {(overview.data?.commune_totals ?? []).map((t) => (
                <li key={`${t.commune_code}-${t.machine_type}`}>
                  {name(t.commune_code)} · {t.machine_type_name}: <b>{t.quantity}</b>
                </li>
              ))}
            </ul>
          </aside>
        }
      />
    </>
  );
}

/** FN-XA-03.05 Vẽ thửa ruộng — danh sách thửa v7.7 (diện tích tự tính so với khai báo); vẽ trên bản đồ ở field-app. */
export function PlotsScreen() {
  const { collectApi } = useAuth();
  const f = useCommuneFilter();
  const [page, setPage] = useState(1);
  const s = useLoad(() => collectApi.plots({ commune: f.commune || undefined }), [f.commune]);
  const name = useCommuneNames(s.data?.items.map((p) => p.commune_code) ?? []);
  const columns: Column<KnPlot>[] = [
    { key: "code", header: "Mã thửa", cell: (p) => p.code },
    { key: "lot", header: "Tên lô", cell: (p) => p.lot_name ?? "—" },
    { key: "commune", header: "Xã", cell: (p) => name(p.commune_code) },
    { key: "farmer", header: "Nông dân", cell: (p) => p.farmer?.label ?? "—" },
    { key: "area", header: "Diện tích tự tính (ha)", cell: (p) => num(p.area_ha, 2), className: "num" },
    { key: "decl", header: "Khai báo (ha)", cell: (p) => num(p.declared_area_ha, 2), className: "num" },
    { key: "dev", header: "Chênh lệch", cell: (p) => (p.area_deviation_pct ? `${num(p.area_deviation_pct)} %` : "—") },
    { key: "st", header: "Trạng thái", cell: (p) => <StatusPill tone={p.status === "active" ? "ok" : "neutral"}>{p.status === "active" ? "Đang dùng" : p.status === "draft" ? "Nháp" : "Ngừng"}</StatusPill> },
  ];
  return (
    <>
      <PageHeader title="Vẽ thửa ruộng" />
      <GapNotice what="Vẽ lô mới (chấm điểm, hình tròn, hình vuông, chặn chồng lấn, ngoại tuyến) dùng ứng dụng di động Cán bộ xã; web xem danh sách thửa và tải GeoJSON/KML." />
      <FilterBar>{f.chip}</FilterBar>
      <LoadState s={s}>{(d) => <DataTable caption="Thửa ruộng" columns={columns} rows={d.items.slice((page - 1) * 20, page * 20)} rowKey={(p) => p.ref} page={page} pageSize={20} total={d.items.length} onPage={setPage} empty="Chưa có thửa." />}</LoadState>
    </>
  );
}

const MATERIAL_OPTIONS = [
  ["phan_ure", "Phân urê (tấn)"],
  ["phan_npk", "Phân NPK (tấn)"],
  ["thuoc_bvtv", "Thuốc BVTV (lít)"],
  ["bay_den", "Bẫy đèn (cái)"],
];

/** FN-XA-03.06 Vật tư: ghi nhận theo xã + thời điểm; lịch sử; tổng hợp theo xã (Cấp Tỉnh xem). */
export function MaterialsScreen() {
  const { collectApi } = useAuth();
  const readOnly = useCycleReadOnly();
  const f = useCommuneFilter();
  const act = useAction();
  const [page, setPage] = useState(1);
  const [line, setLine] = useState({ material_code: "phan_ure", quantity: "", note: "" });
  const [day, setDay] = useState(() => ymdLocal(new Date()));
  const list = useLoad(() => collectApi.materials(f.commune || undefined), [f.commune, act.note]);
  const sum = useLoad(() => collectApi.materialSummary(f.commune || undefined), [f.commune, act.note]);
  const name = useCommuneNames([...(list.data?.items.map((m) => m.commune_code) ?? []), ...(sum.data?.items.map((m) => m.commune_code) ?? [])]);
  const commune = f.commune || f.communes[0]?.code || "";
  const columns: Column<MaterialRecord>[] = [
    { key: "on", header: "Ngày ghi nhận", cell: (m) => m.recorded_on },
    { key: "commune", header: "Xã", cell: (m) => name(m.commune_code) },
    { key: "mat", header: "Vật tư", cell: (m) => m.material_name },
    { key: "q", header: "Số lượng", cell: (m) => `${num(m.quantity, 4)} ${m.unit}` },
    { key: "note", header: "Ghi chú", cell: (m) => m.note ?? "—" },
    { key: "by", header: "Người ghi", cell: (m) => `${m.actor_display ?? "—"} · ${dt(m.recorded_at)}` },
  ];
  return (
    <>
      <PageHeader title="Vật tư" />
      <FilterBar>{f.chip}</FilterBar>
      {act.view}
      {readOnly ? null : (
        <section className="card sec row" style={{ gap: 8, flexWrap: "wrap", marginBottom: 12 }} aria-label="Ghi nhận vật tư">
          <label className="field">
            <span>Thời điểm ghi nhận</span>
            <input className="input" type="date" value={day} onChange={(e) => setDay(e.target.value)} />
          </label>
          <label className="field">
            <span>Loại vật tư</span>
            <select className="input" value={line.material_code} onChange={(e) => setLine({ ...line, material_code: e.target.value })}>
              {MATERIAL_OPTIONS.map(([v, t]) => (
                <option key={v} value={v}>
                  {t}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span>Số lượng</span>
            <input className="input" inputMode="decimal" value={line.quantity} onChange={(e) => setLine({ ...line, quantity: e.target.value })} />
          </label>
          <label className="field">
            <span>Ghi chú</span>
            <input className="input" value={line.note} onChange={(e) => setLine({ ...line, note: e.target.value })} />
          </label>
          <button
            type="button"
            className="btn btn-p btn-sm"
            disabled={!commune || !line.quantity}
            onClick={() => void act.run(() => collectApi.recordMaterials({ commune_code: commune, recorded_on: day, lines: [{ material_code: line.material_code, quantity: line.quantity.replace(",", "."), ...(line.note ? { note: line.note } : {}) }] }), `Đã ghi nhận vật tư cho ${name(commune)}.`)}
          >
            Lưu
          </button>
        </section>
      )}
      <section className="card sec" aria-label="Tổng hợp vật tư theo xã" style={{ marginBottom: 12 }}>
        <h2>Tổng hợp theo xã</h2>
        <ul className="list-plain">
          {(sum.data?.items ?? []).map((x) => (
            <li key={`${x.commune_code}-${x.material_code}`}>
              {name(x.commune_code)} · {x.material_name}: <b>{num(x.quantity, 4)}</b> {x.unit} ({x.records} lần)
            </li>
          ))}
          {!sum.data?.items.length ? <li className="mute">Chưa có ghi nhận.</li> : null}
        </ul>
      </section>
      <LoadState s={list}>{(d) => <DataTable caption="Lịch sử ghi nhận" columns={columns} rows={d.items.slice((page - 1) * 20, page * 20)} rowKey={(m) => m.id} page={page} pageSize={20} total={d.items.length} onPage={setPage} empty="Chưa có ghi nhận vật tư." />}</LoadState>
    </>
  );
}
