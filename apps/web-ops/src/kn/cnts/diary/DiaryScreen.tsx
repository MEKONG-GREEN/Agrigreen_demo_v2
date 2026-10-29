"use client";

// Nhật ký sản xuất của Cấp Xã (W11.48 — BRD CNTS v3.7 UR-XA-11 BM-08/09, UR-XA-12 BM-19, FN-10; prototype A menu Xã "Nhật ký
// sản xuất", resources "diaries"): chọn hộ mô hình được phân công → lứa nuôi (NC-02, NC-03; kết thúc lứa với hạch toán NC-15)
// → lần ghi có chỉ số theo mẫu, bảng phụ (mua thức ăn, điều trị, tiêm phòng, xuất bán, ý kiến cán bộ), ảnh (nén ≤ 500 KB,
// máy chủ mã hóa bằng khóa chủ hộ), vị trí GPS lúc ghi (không sửa được). Lưu nháp, gửi; yêu cầu bổ sung → sửa, gửi lại; đã xác
// thực → điều chỉnh có lý do. Ghi ngoại tuyến trên App xã: chưa (xem gia-dinh/W11.48) — web gửi mã lần ghi và thời điểm thiết bị.
import { useMemo, useState } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { createCntsModelApi } from "@/kn/cnts/model/api";
import { ReasonBox } from "@/kn/cnts/model/parts";
import {
  accounting,
  compressImage,
  cost6Label,
  createCntsDiaryApi,
  createPhotoDrafts,
  currentFix,
  dmy,
  expectedAlive,
  FORM_LABEL,
  kgPerHead,
  KIND_LABEL,
  kindsOf,
  lineAmount,
  metricErrors,
  metricsOf,
  money,
  PHOTO_KIND_LABEL,
  soldOn,
  SUPPLY_ITEMS,
  AMOUNT_OF,
  TABLE_FIELDS,
  TABLE_LABEL,
  tablesOf,
  vn,
  waterWarnings,
  type CntsDiaryApi,
  type CycleClose,
  type CycleDetail,
  type CycleSave,
  type EntryDetail,
  type EntryKind,
  type EntrySave,
  type Form,
  type Photo,
  type Tables,
} from "./api";
import { Checks, CycleHeader, GrowthSection, LocationInfo, MetricsTable, Photos, StatusBadge, SuppliesView, TablesView } from "./parts";

const todayYmd = () => new Date().toLocaleDateString("sv-SE");

export function DiaryScreen() {
  const { api } = useAuth();
  const dapi = useMemo(() => (api ? createCntsDiaryApi(api) : null), [api]);
  const mapi = useMemo(() => (api ? createCntsModelApi(api) : null), [api]);
  const households = useLoad(mapi ? () => mapi.households({ status: "active" }) : null, [mapi]);
  const [hh, setHh] = useState("");
  const cycles = useLoad(dapi && hh ? () => dapi.cycles({ household_ref: hh }) : null, [dapi, hh]);
  const [cycleRef, setCycleRef] = useState<string | null>(null);
  const [newCycle, setNewCycle] = useState(false);
  if (!dapi) return null;
  return (
    <div className="page">
      <PageHeader title="Nhật ký sản xuất">
        <p className="mute">
          Nhật ký nuôi BM-08 (gà lai thương phẩm), BM-09 (vịt xiêm), BM-19 (thủy sản) của hộ mô hình được phân công. Vị trí lúc ghi được so với tọa độ của
          hộ/điểm (quá 200 m bị đánh dấu); lần ghi đã xác thực không sửa — dùng Điều chỉnh có lý do.
        </p>
      </PageHeader>
      <div className="row">
        <label>
          Hộ mô hình{" "}
          <select aria-label="Hộ mô hình" value={hh} onChange={(e) => (setHh(e.target.value), setCycleRef(null), setNewCycle(false))}>
            <option value="">— Chọn hộ —</option>
            {(households.data ?? []).map((h) => (
              <option key={h.ref} value={h.ref}>
                {h.owner_label ?? h.ref.slice(0, 8)} · {h.site_name}
              </option>
            ))}
          </select>
        </label>
      </div>
      {hh ? (
        <LoadState s={cycles}>
          {(l) => (
            <section className="card sec" aria-label="Lứa nuôi">
              <h3>Lứa nuôi</h3>
              <table className="table">
                <thead>
                  <tr>
                    <th>Lứa</th>
                    <th>Mẫu</th>
                    <th>Ngày nhận giống</th>
                    <th>Số con đầu kỳ</th>
                    <th>Hiện có</th>
                    <th>Lần ghi</th>
                    <th>Lợi nhuận</th>
                  </tr>
                </thead>
                <tbody>
                  {l.items.length ? (
                    l.items.map((c) => (
                      <tr key={c.ref} className={cycleRef === c.ref ? "selected" : undefined}>
                        <td>
                          <button className="link" onClick={() => (setCycleRef(c.ref), setNewCycle(false))}>
                            {c.breed}
                          </button>{" "}
                          {c.status === "closed" ? <StatusPill tone="neutral">Đã kết thúc</StatusPill> : <StatusPill tone="ok">Đang nuôi</StatusPill>}
                        </td>
                        <td>{c.form}</td>
                        <td>{dmy(c.received_date)}</td>
                        <td>{c.initial_count}</td>
                        <td>{c.suggest.current_heads ?? "—"}</td>
                        <td>{Object.values(c.suggest.counts).reduce((a, b) => a + b, 0)}</td>
                        <td>{money(c.accounting.profit)}</td>
                      </tr>
                    ))
                  ) : (
                    <tr>
                      <td colSpan={7} className="empty">
                        Hộ chưa có lứa nuôi.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
              {l.can_create && !newCycle ? (
                <button className="btn btn-primary" onClick={() => (setNewCycle(true), setCycleRef(null))}>
                  Mở lứa mới
                </button>
              ) : null}
              {newCycle ? (
                <CycleForm
                  dapi={dapi}
                  household={hh}
                  onDone={(ref) => {
                    setNewCycle(false);
                    cycles.reload();
                    if (ref) setCycleRef(ref);
                  }}
                />
              ) : null}
            </section>
          )}
        </LoadState>
      ) : null}
      {cycleRef ? <CyclePanel key={cycleRef} dapi={dapi} cycleRef={cycleRef} onChanged={cycles.reload} /> : null}
    </div>
  );
}

function CycleForm({ dapi, household, onDone }: { dapi: CntsDiaryApi; household: string; onDone: (ref?: string) => void }) {
  const act = useAction();
  const [f, setF] = useState({
    form: "",
    breed: "",
    received_date: todayYmd(),
    initial_count: "",
    initial_weight_g: "",
    age_days: "",
    note: "",
  });
  const [supplies, setSupplies] = useState<Supply[]>([]);
  const set = (k: keyof typeof f) => (e: { target: { value: string } }) => setF({ ...f, [k]: e.target.value });
  const bad =
    !f.breed.trim() ||
    !/^\d+$/.test(f.initial_count) ||
    Number(f.initial_count) < 1 ||
    supplies.some((s) => (!s.item?.trim() && !s.kind.trim()) || !s.date || !/^\d+(\.\d+)?$/.test(s.quantity));
  return (
    <div className="card sec" aria-label="Mở lứa nuôi">
      <h4>Mở lứa nuôi (NC-02)</h4>
      {act.view}
      <div className="form-grid">
        <label>
          Mẫu nhật ký
          <select value={f.form} onChange={set("form")}>
            <option value="">Theo mẫu của điểm</option>
            {(Object.keys(FORM_LABEL) as Form[]).map((k) => (
              <option key={k} value={k}>
                {FORM_LABEL[k]}
              </option>
            ))}
          </select>
        </label>
        <label>
          Giống / đối tượng nuôi
          <input value={f.breed} onChange={set("breed")} maxLength={200} />
        </label>
        <label>
          Ngày nhận giống
          <input type="date" value={f.received_date} onChange={set("received_date")} />
        </label>
        <label>
          Số con đầu kỳ
          <input inputMode="numeric" value={f.initial_count} onChange={set("initial_count")} />
        </label>
        <label>
          Trọng lượng nhận (g/con)
          <input inputMode="decimal" value={f.initial_weight_g} onChange={set("initial_weight_g")} />
        </label>
        <label>
          Ngày tuổi lúc nhận
          <input inputMode="numeric" value={f.age_days} onChange={set("age_days")} />
        </label>
        <label>
          Ghi chú
          <input value={f.note} onChange={set("note")} maxLength={1000} />
        </label>
      </div>
      <SupplyRows form={(f.form || "BM-08") as Form} rows={supplies} onChange={setSupplies} received={f.received_date} />
      <div className="row">
        <button
          className="btn btn-primary"
          disabled={act.busy || bad}
          onClick={async () => {
            const r = await act.run(() =>
              dapi.createCycle({
                household_ref: household,
                ...(f.form ? { form: f.form as Form } : {}),
                breed: f.breed.trim(),
                received_date: f.received_date,
                initial_count: Number(f.initial_count),
                initial_weight_g: f.initial_weight_g || null,
                age_days: f.age_days ? Number(f.age_days) : null,
                supplies: supplies.map((s) => ({ ...s, item: s.item?.trim() ?? "", kind: s.kind.trim() })),
                note: f.note,
              }),
            );
            if (r) onDone(r.ref);
          }}
        >
          Mở lứa
        </button>
        <button className="btn" onClick={() => onDone()}>
          Hủy bỏ
        </button>
      </div>
    </div>
  );
}

type Supply = NonNullable<CycleSave["supplies"]>[number];

/** Mục 1 "Thông tin chung" của sổ (NC-03): vật tư, ngày nhận, số lượng, chủng loại vật tư. */
function SupplyRows({ form, rows, onChange, received }: { form: Form; rows: Supply[]; onChange: (r: Supply[]) => void; received: string }) {
  const set = (i: number, k: keyof Supply, v: string) => onChange(rows.map((x, j) => (j === i ? { ...x, [k]: v } : x)));
  const listId = `supply-items-${form}`;
  return (
    <fieldset>
      <legend>Vật tư nhận (NC-03, mục 1 của sổ)</legend>
      <datalist id={listId}>
        {SUPPLY_ITEMS[form].map((x) => (
          <option key={x} value={x} />
        ))}
      </datalist>
      {rows.map((r, i) => (
        <div className="row" key={i}>
          <input aria-label={`Vật tư ${i + 1}`} placeholder="Vật tư" list={listId} value={r.item ?? ""} onChange={(e) => set(i, "item", e.target.value)} maxLength={200} />
          <input aria-label={`Vật tư ${i + 1} ngày nhận`} type="date" value={r.date} onChange={(e) => set(i, "date", e.target.value)} />
          <input aria-label={`Vật tư ${i + 1} số lượng`} placeholder="Số lượng" inputMode="decimal" value={r.quantity} onChange={(e) => set(i, "quantity", e.target.value)} />
          <input aria-label={`Vật tư ${i + 1} đơn vị`} placeholder="Đơn vị" value={r.unit} onChange={(e) => set(i, "unit", e.target.value)} maxLength={20} />
          <input aria-label={`Vật tư ${i + 1} chủng loại`} placeholder="Chủng loại vật tư" value={r.kind} onChange={(e) => set(i, "kind", e.target.value)} maxLength={200} />
          <button className="btn" onClick={() => onChange(rows.filter((_, j) => j !== i))}>
            Xóa dòng
          </button>
        </div>
      ))}
      <div className="row">
        <button className="btn" onClick={() => onChange([...rows, { item: "", date: received, quantity: "", unit: "", kind: "" }])}>
          Thêm vật tư
        </button>
        {!rows.length ? (
          <button className="btn" onClick={() => onChange(SUPPLY_ITEMS[form].map((item) => ({ item, date: received, quantity: "", unit: "", kind: "" })))}>
            Điền danh mục vật tư theo sổ
          </button>
        ) : null}
      </div>
    </fieldset>
  );
}

function CyclePanel({ dapi, cycleRef, onChanged }: { dapi: CntsDiaryApi; cycleRef: string; onChanged: () => void }) {
  const cy = useLoad(() => dapi.cycle(cycleRef), [dapi, cycleRef]);
  const entries = useLoad(() => dapi.entries({ cycle_ref: cycleRef }), [dapi, cycleRef]);
  const [editing, setEditing] = useState<string | "new" | null>(null);
  const [closing, setClosing] = useState(false);
  const [reopening, setReopening] = useState(false);
  const act = useAction();
  const reload = () => (cy.reload(), entries.reload(), onChanged());
  return (
    <LoadState s={cy}>
      {(c) => (
        <section className="card sec" aria-label="Lứa nuôi đã chọn">
          <h3>
            {c.breed} · {FORM_LABEL[c.form]} {c.status === "closed" ? <StatusPill tone="neutral">Đã kết thúc</StatusPill> : null}
          </h3>
          <CycleHeader c={c} />
          <p className="mute">
            {c.suggest.current_heads !== null ? `Hiện có ${c.suggest.current_heads} con` : "Chưa có lần ghi"}
            {c.suggest.last_entry_date ? ` · ghi gần nhất ${dmy(c.suggest.last_entry_date)}` : ""}
          </p>
          <SuppliesView supplies={c.supplies} />
          {act.view}
          {c.status === "closed" ? (
            <p>
              Hạch toán: tổng chi {money(c.accounting.total_cost)} · tổng thu {money(c.accounting.revenue_amount)}
              {c.accounting.revenue_heads ? ` · ${c.accounting.revenue_heads} con` : ""}
              {kgPerHead(c.accounting.revenue_output_kg, c.accounting.revenue_heads) !== null
                ? ` · P ${vn(kgPerHead(c.accounting.revenue_output_kg, c.accounting.revenue_heads))} kg/con`
                : ""}{" "}
              · lợi nhuận <b>{money(c.accounting.profit)}</b>
              {c.accounting.unit_cost ? ` · giá thành ${money(c.accounting.unit_cost)}/kg` : ""}
            </p>
          ) : null}
          <div className="row">
            {c.can_edit && c.status === "open" ? (
              <>
                <button className="btn btn-primary" onClick={() => setEditing("new")}>
                  Ghi nhật ký
                </button>
                <button className="btn" onClick={() => setClosing(true)}>
                  Kết thúc lứa, hạch toán
                </button>
              </>
            ) : null}
            {c.can_edit && c.status === "closed" ? (
              <button className="btn" onClick={() => setReopening(true)}>
                Mở lại lứa
              </button>
            ) : null}
          </div>
          {reopening ? (
            <ReasonBox
              label="Mở lại"
              busy={act.busy}
              onCancel={() => setReopening(false)}
              onConfirm={async (reason) => {
                if (await act.run(() => dapi.reopenCycle(c.ref, c.version, reason), "Đã mở lại lứa")) {
                  setReopening(false);
                  reload();
                }
              }}
            />
          ) : null}
          {closing ? <CloseForm dapi={dapi} c={c} onDone={(ok) => (setClosing(false), ok && reload())} /> : null}
          {editing === "new" ? <EntryForm dapi={dapi} c={c} onDone={(ok) => (setEditing(null), ok && reload())} /> : null}
          <h4>Lần ghi</h4>
          <LoadState s={entries}>
            {(items) => (
              <table className="table">
                <thead>
                  <tr>
                    <th>Ngày ghi</th>
                    <th>Loại</th>
                    <th>Vị trí</th>
                    <th>Cảnh báo</th>
                    <th>Trạng thái</th>
                  </tr>
                </thead>
                <tbody>
                  {items.length ? (
                    items.map((e) => (
                      <tr key={e.ref} className={editing === e.ref ? "selected" : undefined}>
                        <td>
                          <button className="link" onClick={() => setEditing(editing === e.ref ? null : e.ref)}>
                            {dmy(e.entry_date)}
                          </button>
                        </td>
                        <td>{KIND_LABEL[e.entry_kind]}</td>
                        <td>
                          {!e.location.has_fix ? "Không có GPS" : e.location.distance_m ? `${e.location.distance_m} m` : "Điểm chưa có tọa độ"}
                          {e.location.far ? " · xa điểm" : ""}
                          {e.location.mock_suspected ? " · nghi giả lập" : ""}
                        </td>
                        <td>{e.checks.filter((x) => x.level === "warn").length || "—"}</td>
                        <td>
                          <StatusBadge status={e.status} />
                        </td>
                      </tr>
                    ))
                  ) : (
                    <tr>
                      <td colSpan={5} className="empty">
                        Chưa có lần ghi.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            )}
          </LoadState>
          {editing && editing !== "new" ? <EntryPanel key={editing} dapi={dapi} c={c} entryRef={editing} onChanged={reload} /> : null}
          {c.form !== "BM-19" && entries.data ? <GrowthSection c={c} entries={entries.data} /> : null}
        </section>
      )}
    </LoadState>
  );
}

function CloseForm({ dapi, c, onDone }: { dapi: CntsDiaryApi; c: CycleDetail; onDone: (ok: boolean) => void }) {
  const act = useAction();
  const [f, setF] = useState<Partial<CycleClose>>({
    cost_feed: c.suggest.feed_cost !== "0" ? c.suggest.feed_cost : "",
    revenue_heads: c.suggest.sold_heads || null,
    revenue_output_kg: c.suggest.sold_kg !== "0" ? c.suggest.sold_kg : "",
    revenue_amount: c.suggest.sales_amount !== "0" ? c.suggest.sales_amount : "",
  });
  const money0 = (k: keyof CycleClose, label: string) => (
    <label key={k}>
      {label}
      <input inputMode="numeric" value={(f[k] as string | null | undefined) ?? ""} onChange={(e) => setF({ ...f, [k]: e.target.value })} />
    </label>
  );
  const sum = accounting(f);
  return (
    <div className="card sec" aria-label="Hạch toán cuối lứa">
      <h4>Hạch toán kinh tế cuối lứa (NC-15)</h4>
      <p className="mute">
        Gợi ý từ nhật ký: chi thức ăn {money(c.suggest.feed_cost)} (NC-09), thu {money(c.suggest.sales_amount)} từ {c.suggest.sold_heads} con xuất bán (NC-13).
      </p>
      {act.view}
      <div className="form-grid">
        {money0("cost_seed", "Con giống (đ)")}
        {money0("cost_feed", "Thức ăn (đ)")}
        {money0("cost_depreciation", "Khấu hao chuồng trại (đ)")}
        {money0("cost_transport", "Vận chuyển (đ)")}
        {money0("cost_vet", "Thuốc thú y (đ)")}
        {money0("cost_probiotic", `${cost6Label(c.form)} (đ)`)}
        <label>
          Số con hiện tại / bán
          <input
            inputMode="numeric"
            value={f.revenue_heads ?? ""}
            onChange={(e) =>
              setF({
                ...f,
                revenue_heads: e.target.value ? Number(e.target.value) : null,
              })
            }
          />
        </label>
        <label>
          Sản lượng (kg)
          <input inputMode="decimal" value={f.revenue_output_kg ?? ""} onChange={(e) => setF({ ...f, revenue_output_kg: e.target.value })} />
        </label>
        {money0("revenue_price", "Giá bán (đ/kg)")}
        {money0("revenue_amount", "Tổng thu (đ)")}
      </div>
      <p>
        Tổng chi {money(sum.total)} · P {vn(kgPerHead(f.revenue_output_kg, f.revenue_heads))} kg/con · lợi nhuận{" "}
        {sum.profit === null ? "—" : money(sum.profit)}
      </p>
      <div className="row">
        <button
          className="btn btn-primary"
          disabled={act.busy || !f.revenue_amount}
          onClick={async () => {
            const nz = (v: unknown) => (v === "" || v === undefined ? null : (v as string));
            const ok = await act.run(() =>
              dapi.closeCycle(c.ref, {
                base_version: c.version,
                cost_seed: nz(f.cost_seed),
                cost_feed: nz(f.cost_feed),
                cost_depreciation: nz(f.cost_depreciation),
                cost_transport: nz(f.cost_transport),
                cost_vet: nz(f.cost_vet),
                cost_probiotic: nz(f.cost_probiotic),
                revenue_heads: f.revenue_heads ?? null,
                revenue_output_kg: nz(f.revenue_output_kg),
                revenue_price: nz(f.revenue_price),
                revenue_amount: f.revenue_amount ?? "",
              }),
            );
            if (ok) onDone(true);
          }}
        >
          Kết thúc lứa
        </button>
        <button className="btn" onClick={() => onDone(false)}>
          Hủy bỏ
        </button>
      </div>
    </div>
  );
}

type Row = Record<string, string | number>;

function TableRows({ name, rows, onChange }: { name: keyof Tables; rows: Row[]; onChange: (r: Row[]) => void }) {
  const fields = TABLE_FIELDS[name];
  const amount = AMOUNT_OF[name];
  return (
    <fieldset>
      <legend>{TABLE_LABEL[name]}</legend>
      {rows.map((r, i) => (
        <div className="row" key={i}>
          {fields.map((f) => (
            <input
              key={f.key}
              aria-label={`${TABLE_LABEL[name]} ${i + 1} ${f.label}`}
              placeholder={f.label}
              type={f.type === "date" ? "date" : "text"}
              inputMode={f.type === "number" ? "numeric" : undefined}
              value={String(r[f.key] ?? "")}
              onChange={(e) =>
                onChange(
                  rows.map((x, j) =>
                    j === i
                      ? {
                          ...x,
                          [f.key]: f.type === "number" ? Number(e.target.value || 0) : e.target.value,
                        }
                      : x,
                  ),
                )
              }
            />
          ))}
          {amount ? <span className="mute">Thành tiền {money(lineAmount(r[amount[0]], r[amount[1]]))}</span> : null}
          <button className="btn" onClick={() => onChange(rows.filter((_, j) => j !== i))}>
            Xóa dòng
          </button>
        </div>
      ))}
      <button className="btn" onClick={() => onChange([...rows, {}])}>
        Thêm dòng
      </button>
    </fieldset>
  );
}

/** Biểu lần ghi: tạo mới (kèm vị trí lúc ghi), sửa, điều chỉnh (có lý do). */
function EntryForm({
  dapi,
  c,
  entry,
  mode = "create",
  onDone,
}: {
  dapi: CntsDiaryApi;
  c: CycleDetail;
  entry?: EntryDetail;
  mode?: "create" | "edit" | "correct";
  onDone: (ok: boolean) => void;
}) {
  const act = useAction();
  const kinds = kindsOf(c.form);
  const [kind, setKind] = useState<EntryKind>(entry?.entry_kind ?? kinds[0]!);
  const [day, setDay] = useState(entry?.entry_date ?? todayYmd());
  const [m, setM] = useState<Record<string, string>>(entry?.metrics ?? {});
  const [tables, setTables] = useState<Record<string, Row[]>>((entry?.tables as Record<string, Row[]> | undefined) ?? {});
  const [photos, setPhotos] = useState<Photo[]>(entry?.photos ?? []);
  const [photoKind, setPhotoKind] = useState<Photo["kind"]>(c.form === "BM-19" ? "weighing" : "flock");
  const [photoDrafts] = useState(() => createPhotoDrafts());
  const [fix, setFix] = useState<EntrySave["location"] | null>(null);
  const [locating, setLocating] = useState(false);
  const [reason, setReason] = useState("");
  const errs = metricErrors(c.form, kind, m);
  const water = c.form === "BM-19" ? waterWarnings(m) : [];
  const prevAlive = c.suggest.current_heads ?? c.initial_count;
  const hint = c.form !== "BM-19" && mode === "create" && m.alive ? expectedAlive(prevAlive, Number(m.dead || 0), soldOn(tables as Tables, day)) : null;
  const body = (submit: boolean): EntrySave => ({
    entry_kind: kind,
    entry_date: day,
    metrics: Object.fromEntries(Object.entries(m).filter(([, v]) => v.trim() !== "")),
    tables: tables as Tables,
    photo_refs: photos.map((p) => p.file_ref),
    submit,
    ...(mode === "create" ? { cycle_ref: c.ref, ...(fix ? { location: fix } : {}) } : { base_version: entry!.version }),
    ...(mode === "correct" ? { reason } : {}),
  });
  const save = async (submit: boolean) => {
    const r = await act.run(() =>
      mode === "create"
        ? dapi.createEntry(body(submit))
        : mode === "edit"
          ? dapi.updateEntry(entry!.ref, body(submit))
          : dapi.correct(entry!.ref, body(submit)),
    );
    if (r) onDone(true);
  };
  return (
    <div className="card sec" aria-label={mode === "create" ? "Ghi nhật ký" : mode === "edit" ? "Sửa lần ghi" : "Điều chỉnh lần ghi"}>
      <h4>{mode === "create" ? "Ghi nhật ký" : mode === "edit" ? "Sửa lần ghi" : "Điều chỉnh lần ghi đã xác thực"}</h4>
      {act.view}
      <div className="form-grid">
        <label>
          Loại lần ghi
          <select value={kind} onChange={(e) => setKind(e.target.value as EntryKind)} disabled={kinds.length === 1}>
            {kinds.map((k) => (
              <option key={k} value={k}>
                {KIND_LABEL[k]}
              </option>
            ))}
          </select>
        </label>
        <label>
          Ngày ghi
          <input type="date" value={day} max={todayYmd()} onChange={(e) => setDay(e.target.value)} />
        </label>
        {metricsOf(c.form)
          .filter((d) => d.kinds.includes(kind))
          .map((d) => (
            <label key={d.code}>
              {d.label}
              {d.unit ? ` (${d.unit})` : ""}
              {d.required.includes(kind) ? " *" : ""}
              {d.values ? (
                <select value={m[d.code] ?? ""} onChange={(e) => setM({ ...m, [d.code]: e.target.value })}>
                  <option value="">—</option>
                  {d.values.map(([v, l]) => (
                    <option key={v} value={v}>
                      {l}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  inputMode={d.type === "text" ? undefined : "decimal"}
                  value={m[d.code] ?? ""}
                  onChange={(e) => setM({ ...m, [d.code]: e.target.value })}
                  aria-invalid={!!errs[d.code]}
                />
              )}
              {errs[d.code] ? <small className="mute">{errs[d.code]}</small> : null}
            </label>
          ))}
      </div>
      {hint !== null && Number(m.alive) !== hint ? (
        <p className="alert alert-warn">
          NC-05: số con sống phải bằng {prevAlive} − {m.dead || 0} chết − {soldOn(tables as Tables, day)} xuất bán = {hint}
        </p>
      ) : null}
      {water.length ? <p className="alert alert-warn">{water.join(" · ")} — ghi biện pháp xử lý (NT-12).</p> : null}
      {tablesOf(c.form).map((t) => (
        <TableRows key={t} name={t} rows={tables[t] ?? []} onChange={(rows) => setTables({ ...tables, [t]: rows })} />
      ))}
      <fieldset>
        <legend>
          Ảnh (≤ 500 KB sau nén)
          {c.form === "BM-19" && kind === "growth_env" ? " — bắt buộc ảnh cân mẫu" : ""}
        </legend>
        <div className="row">
          <select value={photoKind} onChange={(e) => setPhotoKind(e.target.value as Photo["kind"])} aria-label="Loại ảnh">
            {(Object.keys(PHOTO_KIND_LABEL) as Photo["kind"][]).map((k) => (
              <option key={k} value={k}>
                {PHOTO_KIND_LABEL[k]}
              </option>
            ))}
          </select>
          <input
            type="file"
            accept="image/*"
            capture="environment"
            aria-label="Chọn ảnh"
            onChange={async (e) => {
              const file = e.target.files?.[0];
              e.target.value = "";
              if (!file) return;
              // #172: chọn lại đúng ảnh sau mất phản hồi → cùng id/captured_at + thân tất định → cùng khóa, máy chủ phát lại.
              // Lời gọi tải đứng sau await nén ảnh nên phải bọc bằng `idem` của lượt này.
              const draft = photoDrafts.draft(c.household_ref, photoKind, file);
              const p = await act.run(async (idem) => {
                const blob = await compressImage(file);
                return idem(() => dapi.uploadPhoto(c.household_ref, photoKind, blob, draft));
              }, "Đã tải ảnh");
              if (p) {
                photoDrafts.done(draft);
                setPhotos([...photos, p]);
              }
            }}
          />
        </div>
        <ul className="list-plain">
          {photos.map((p) => (
            <li key={p.file_ref}>
              {PHOTO_KIND_LABEL[p.kind]} · {Math.round(p.size_bytes / 1024)} KB{" "}
              <button className="link" onClick={() => setPhotos(photos.filter((x) => x.file_ref !== p.file_ref))}>
                Bỏ
              </button>
            </li>
          ))}
        </ul>
      </fieldset>
      {mode === "create" ? (
        <div className="row">
          <button
            className="btn"
            disabled={locating}
            onClick={async () => {
              setLocating(true);
              setFix(await currentFix());
              setLocating(false);
            }}
          >
            {locating ? "Đang lấy vị trí…" : "Lấy vị trí GPS"}
          </button>
          <span className="mute">
            {fix
              ? `Vị trí lúc ghi: ${fix.lat.toFixed(6)}, ${fix.lng.toFixed(6)} (± ${fix.accuracy_m ?? "?"} m) — không sửa được sau khi lưu`
              : "Chưa có vị trí GPS (lần ghi sẽ bị đánh dấu)"}
          </span>
        </div>
      ) : null}
      {mode === "correct" ? (
        <label>
          Lý do điều chỉnh
          <input value={reason} onChange={(e) => setReason(e.target.value)} maxLength={1000} />
        </label>
      ) : null}
      <div className="row">
        {mode !== "correct" && entry?.status !== "submitted" ? (
          <button className="btn" disabled={act.busy} onClick={() => save(false)}>
            Lưu nháp
          </button>
        ) : null}
        <button
          className="btn btn-primary"
          disabled={act.busy || Object.keys(errs).length > 0 || (mode === "correct" && reason.trim().length < 5)}
          onClick={() => save(mode !== "correct")}
        >
          {mode === "correct" ? "Gửi điều chỉnh" : entry?.status === "submitted" ? "Lưu" : "Gửi"}
        </button>
        <button className="btn" onClick={() => onDone(false)}>
          Hủy bỏ
        </button>
      </div>
    </div>
  );
}

function EntryPanel({ dapi, c, entryRef, onChanged }: { dapi: CntsDiaryApi; c: CycleDetail; entryRef: string; onChanged: () => void }) {
  const d = useLoad(() => dapi.entry(entryRef), [dapi, entryRef]);
  const [mode, setMode] = useState<"edit" | "correct" | null>(null);
  const act = useAction();
  return (
    <LoadState s={d}>
      {(e) =>
        mode ? (
          <EntryForm dapi={dapi} c={c} entry={e} mode={mode} onDone={(ok) => (setMode(null), ok && (d.reload(), onChanged()))} />
        ) : (
          <section className="card sec" aria-label="Lần ghi">
            <h4>
              {KIND_LABEL[e.entry_kind]} · {dmy(e.entry_date)} <StatusBadge status={e.status} />
            </h4>
            {e.status_reason ? <p className="alert alert-warn">Lý do: {e.status_reason}</p> : null}
            {act.view}
            <MetricsTable form={e.form} entry={e} previous={e.previous?.metrics} />
            <TablesView form={e.form} tables={e.tables} />
            <Checks checks={e.checks} />
            <LocationInfo e={e} />
            <Photos dapi={dapi} photos={e.photos} />
            <div className="row">
              {e.can_edit ? (
                <button className="btn" onClick={() => setMode("edit")}>
                  Sửa
                </button>
              ) : null}
              {e.can_submit ? (
                <button
                  className="btn btn-primary"
                  disabled={act.busy}
                  onClick={async () => {
                    if (await act.run(() => dapi.submit(e.ref, e.version), "Đã gửi lần ghi")) {
                      d.reload();
                      onChanged();
                    }
                  }}
                >
                  Gửi
                </button>
              ) : null}
              {e.can_correct ? (
                <button className="btn" onClick={() => setMode("correct")}>
                  Điều chỉnh
                </button>
              ) : null}
            </div>
            {e.status === "submitted" && !e.can_edit ? <p className="mute">Quá hạn sửa (đến {dmy(e.editable_until)}) — chờ chủ nhiệm xác thực.</p> : null}
          </section>
        )
      }
    </LoadState>
  );
}
