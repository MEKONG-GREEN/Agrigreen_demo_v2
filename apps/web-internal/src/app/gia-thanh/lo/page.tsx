"use client";

import { useState } from "react";
import { idem, unwrap } from "@/api/client";
import type { CostEntry, CostingAllocation } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { KV, Money, When } from "@/components/g4kit";
import { ActionStatus, Chip, Provisional, Ref, RefPicker, Section, SelectField, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, KpiTile, Loading, PageHead } from "@/components/ui";
import { ALLOCATION_KIND_LABEL, COST_CATEGORY_LABEL, DIMENSION_LABEL, MANUAL_CATEGORIES } from "@/lib/g4labels";
import { formatKg, formatTons, isKg } from "@/lib/kg";
import { formatVnd, isMoney, shortHash } from "@/lib/money";
import { validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function AllocationPanel({ id, onChanged }: { id: string; onChanged: () => void }) {
  const { api, principal } = useAuth();
  const [cur, setCur] = useState(id);
  const a = useAsync(() => unwrap(api.GET("/v1/costing/allocations/{id}", { params: { path: { id: cur } } })), [api, cur]);
  const hist = useAsync(async () => (a.data?.source_ref ? ((await unwrap(api.GET("/v1/costing/sources/{id}/allocations", { params: { path: { id: a.data.source_ref } } }))).items ?? []) : []), [api, a.data?.source_ref, a.data?.id]);
  const act = useAction();
  const [reason, setReason] = useState("");
  const [asOf, setAsOf] = useState("");
  if (a.loading && !a.data) return <Loading />;
  if (a.error) return <ProblemAlert problem={a.error} />;
  const x = a.data!;
  const lines = (x.lines ?? []) as unknown as { lot_ref?: string; mass_kg?: string; amount?: string; ratio?: string }[];
  return (
    <div className="panel stack" aria-label="Kết quả phân bổ">
      <KV
        items={[
          ["Loại", ALLOCATION_KIND_LABEL[x.kind ?? ""] ?? x.kind],
          ["Nguồn", <span key="s"><Ref id={x.source_ref} /> · sự việc <When at={x.source_event_time} /></span>],
          ["Khoá phân bổ", <span key="b" className="row">{x.basis} <Provisional what="Q-2" /></span>],
          ["Tổng", <span key="t"><Money v={x.total_amount} /> · {formatKg(x.total_kg)}</span>],
          ["Ghim quy tắc", <span key="r" className="mono small">{x.rule_version} · {Object.entries(x.param_pins ?? {}).map(([k, v]) => `${k}#${v}`).join(", ")}</span>],
          ["Băm đầu vào / kết quả", <span key="h" className="mono small" title={`${x.inputs_sha256} / ${x.result_sha256}`}>{shortHash(x.inputs_sha256)} / {shortHash(x.result_sha256)}</span>],
          ["Thay / bị thay", <span key="sp"><Ref id={x.supersedes_id} /> / <Ref id={x.superseded_by} /></span>],
          ...(x.correction_reason ? ([["Lý do tính lại", x.correction_reason]] as [string, string][]) : []),
        ]}
      />
      <div className="tbl-wrap">
        <table className="tbl" aria-label="Dòng phân bổ">
          <thead>
            <tr>
              <th>Lô</th>
              <th className="num">Khối lượng</th>
              <th className="num">Số tiền</th>
            </tr>
          </thead>
          <tbody>
            {lines.map((l, i) => (
              <tr key={i}>
                <td>
                  <Ref id={l.lot_ref} />
                </td>
                <td className="num">{formatKg(l.mass_kg)}</td>
                <td className="num">
                  <Money v={l.amount} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <ActionStatus a={act} />
      {hasRole(principal, ROLES.costAccountant) && !x.superseded_by ? (
        <div className="grid3">
          <TextField label="Lý do tính lại" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự — kết quả MỚI supersedes, kết quả cũ không đổi (R-13)" />
          <TextField label="Tham số theo quyết định đến (as_of)" type="datetime-local" value={asOf} onChange={setAsOf} hint="Bỏ trống = mọi quyết định đã ghi" />
          <div className="row">
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={act.busy || !validReason(reason)}
              onClick={() =>
                void act
                  .run(() => unwrap(api.POST("/v1/costing/allocations/{id}/recompute", { params: { path: { id: cur }, header: idem() }, body: { reason: reason.trim(), ...(asOf ? { as_of: new Date(asOf).toISOString() } : {}) } })), (r) =>
                    r.changed === false ? "Tính lại không đổi (cùng băm) — không ghi kết quả mới" : "Đã ghi kết quả phân bổ mới (supersedes_id) + bút toán delta",
                  )
                  .then((r) => {
                    if (r) {
                      setReason("");
                      if (r.changed !== false && r.id) setCur(r.id);
                      hist.reload();
                      onChanged();
                    }
                  })
              }
            >
              Tính lại phân bổ
            </button>
          </div>
        </div>
      ) : null}
      {hist.data && hist.data.length > 1 ? (
        <ol className="list-plain small" aria-label="Lịch sử kết quả phân bổ">
          {hist.data.map((h: CostingAllocation) => (
            <li key={h.id}>
              <When at={h.recorded_at} /> · <Ref id={h.id} /> {h.superseded_by ? <span className="chip c-mute">bị thay</span> : <span className="chip c-ok">hiệu lực</span>} {h.correction_reason ?? ""}
            </li>
          ))}
        </ol>
      ) : null}
    </div>
  );
}

function AdjustForm({ e, onDone }: { e: CostEntry; onDone: (msg: string) => void }) {
  const { api } = useAuth();
  const act = useAction();
  const [amount, setAmount] = useState(e.amount ?? "");
  const [reason, setReason] = useState("");
  return (
    <div className="panel stack">
      <b>Điều chỉnh bút toán #{e.seq} ({COST_CATEGORY_LABEL[e.category ?? ""]}) — ghi ở kỳ ĐANG MỞ, trỏ supersedes_id</b>
      <div className="grid3">
        <TextField label="Số tiền mới (VND)" value={amount} onChange={setAmount} inputMode="decimal" />
        <TextField label="Lý do điều chỉnh" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
        <div className="row">
          <button
            type="button"
            className="btn btn-p btn-sm"
            disabled={act.busy || !isMoney(amount.trim()) || amount.trim().startsWith("-") || !validReason(reason)}
            onClick={() =>
              void act.run(() => unwrap(api.POST("/v1/costing/entries/{id}/adjust", { params: { path: { id: e.id! }, header: idem() }, body: { amount: amount.trim(), reason: reason.trim() } }))).then((r) => {
                if (r) onDone(`Đã ghi bút toán điều chỉnh ở kỳ ${r.period_code} (Δ ${formatVnd(r.delta_amount)})${r.late_from ? ` cho kỳ đã đóng ${r.late_from}` : ""} — dòng cũ giữ nguyên`);
              })
            }
          >
            Ghi điều chỉnh
          </button>
        </div>
      </div>
      <ActionStatus a={act} />
    </div>
  );
}

function ManualEntry({ lot, onDone }: { lot: string; onDone: () => void }) {
  const { api } = useAuth();
  const act = useAction();
  const [category, setCategory] = useState<(typeof MANUAL_CATEGORIES)[number]>("collection_labour");
  const [amount, setAmount] = useState("");
  const [mass, setMass] = useState("");
  const [note, setNote] = useState("");
  return (
    <div className="stack">
      <div className="grid4">
        <SelectField label="Nhóm chi phí" value={category} onChange={(c) => setCategory(c as typeof category)} options={MANUAL_CATEGORIES.map((c) => ({ value: c, label: COST_CATEGORY_LABEL[c]! }))} />
        <TextField label="Số tiền (VND)" value={amount} onChange={setAmount} inputMode="decimal" />
        <TextField label="Khối lượng (kg)" value={mass} onChange={setMass} inputMode="decimal" hint="Tuỳ chọn" />
        <TextField label="Ghi chú" value={note} onChange={setNote} />
      </div>
      <ActionStatus a={act} />
      <div className="row">
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={act.busy || !isMoney(amount.trim()) || amount.trim().startsWith("-") || (!!mass && !isKg(mass.trim()))}
          onClick={() => void act.run(() => unwrap(api.POST("/v1/costing/entries", { params: { header: idem() }, body: { lot_ref: lot, category, amount: amount.trim(), ...(mass.trim() ? { mass_kg: mass.trim() } : {}), ...(note.trim() ? { note: note.trim() } : {}) } })), (r) => `Đã ghi bút toán vào kỳ ${r.period_code}`).then((r) => r && onDone())}
        >
          Ghi bút toán chi phí
        </button>
      </div>
    </div>
  );
}

export default function LotCostPage() {
  const { api, principal } = useAuth();
  const [lot, setLot] = useState("");
  const [tick, setTick] = useState(0);
  const valid = UUID.test(lot);
  const cost = useAsync(async () => (valid ? unwrap(api.GET("/v1/costing/lots/{id}/cost", { params: { path: { id: lot } } })) : null), [api, lot, valid, tick]);
  const entries = useAsync(async () => (valid ? ((await unwrap(api.GET("/v1/costing/lots/{id}/entries", { params: { path: { id: lot }, query: { limit: 500 } } }))).items ?? []) : []), [api, lot, valid, tick]);
  const [adjust, setAdjust] = useState<CostEntry | null>(null);
  const [alloc, setAlloc] = useState<string | null>(null);
  const [msg, setMsg] = useState<string | null>(null);
  const acc = hasRole(principal, ROLES.costAccountant);
  const refresh = () => {
    setAdjust(null);
    setTick((t) => t + 1);
  };
  const superseded = new Set((entries.data ?? []).map((e) => e.supersedes_id).filter(Boolean));
  const c = cost.data;
  return (
    <div className="stack">
      <PageHead
        title="Giá thành lô & phân bổ"
        subtitle="Giá thành = gom + kho + bốc xếp + vận chuyển + sơ chế (proto buildCostChain); đ/t = tổng / tấn giao, tấn giao = 0 → không xác định. Không gồm giá mua rơm (quyết toán HTX, D03), doanh thu, carbon, khấu hao. Sửa = bút toán điều chỉnh, không sửa dòng cũ."
      />
      <Section title="Chọn lô">
        <RefPicker label="Mã lô" kind="lot" value={lot} onChange={setLot} />
      </Section>
      {valid ? (
        <>
          {cost.loading ? <Loading /> : null}
          {cost.error ? <ProblemAlert problem={cost.error} /> : null}
          {c ? (
            <>
              <div className="grid4">
                <KpiTile label="Tổng chi phí" value={formatVnd(c.total_cost)} color="blue" icon="chart" />
                <KpiTile label="Tấn giao" value={formatTons(c.delivered_kg)} color="green" icon="upload" />
                <KpiTile label="Giá thành đ/t" value={c.cost_per_tonne_undetermined ? null : formatVnd(c.cost_per_tonne)} missingHint="Không xác định — lô chưa có tấn giao" color="orange" icon="param" />
                <KpiTile label="Doanh thu (tách riêng)" value={formatVnd(c.revenue)} color="violet" icon="report" />
              </div>
              {c.cost_per_tonne_undetermined ? <p className="notice">Giá thành đ/t: không xác định (tấn giao = 0).</p> : null}
              <Section title="Theo nhóm chi phí">
                <div className="tbl-wrap">
                  <table className="tbl" aria-label="Giá thành theo nhóm">
                    <thead>
                      <tr>
                        <th>Nhóm</th>
                        <th className="num">Số tiền</th>
                        <th className="num">Khối lượng</th>
                        <th className="num">Số bút toán</th>
                      </tr>
                    </thead>
                    <tbody>
                      {(c.categories ?? []).map((x) => (
                        <tr key={x.category}>
                          <td>{COST_CATEGORY_LABEL[x.category ?? ""]}</td>
                          <td className="num">
                            <Money v={x.amount} />
                          </td>
                          <td className="num">{formatKg(x.mass_kg)}</td>
                          <td className="num">{x.entries}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <p className="small mute">{c.note}</p>
              </Section>
            </>
          ) : null}
          <Section title="Sổ bút toán của lô (lot:<id>)">
            {entries.data && !entries.data.length ? <Empty>Lô chưa có bút toán giá thành.</Empty> : null}
            {entries.data?.length ? (
              <div className="tbl-wrap">
                <table className="tbl" aria-label="Bút toán giá thành">
                  <thead>
                    <tr>
                      <th>#</th>
                      <th>Kỳ</th>
                      <th>Nhóm</th>
                      <th className="num">Số tiền</th>
                      <th className="num">Δ vào kỳ</th>
                      <th>Chiều</th>
                      <th>Nguồn</th>
                      <th>Loại</th>
                      <th></th>
                    </tr>
                  </thead>
                  <tbody>
                    {entries.data.map((e) => (
                      <tr key={e.id}>
                        <td>{e.seq}</td>
                        <td className="mono">
                          {e.period_code}
                          {e.late_from ? <span className="small mute"> (cho {e.late_from})</span> : null}
                        </td>
                        <td>{COST_CATEGORY_LABEL[e.category ?? ""]}</td>
                        <td className="num">
                          <Money v={e.amount} />
                        </td>
                        <td className="num">
                          <Money v={e.delta_amount} />
                        </td>
                        <td className="small">
                          {DIMENSION_LABEL[e.dimension_kind ?? ""]} <Ref id={e.dimension_ref} />
                        </td>
                        <td className="small">
                          {e.source_kind} <Ref id={e.source_ref} />
                          {e.allocation_ref ? (
                            <button type="button" className="btn btn-o btn-sm" onClick={() => setAlloc(e.allocation_ref!)} aria-label={`Xem phân bổ bút toán ${e.seq}`}>
                              Phân bổ
                            </button>
                          ) : null}
                        </td>
                        <td className="small">
                          {e.entry_kind === "adjusting" ? <span className="chip c-info">điều chỉnh</span> : null} {superseded.has(e.id) ? <Chip status="superseded" label="Bị thay" /> : null}
                          {e.correction_reason ? <div>{e.correction_reason}</div> : null}
                        </td>
                        <td>
                          {acc && !superseded.has(e.id) ? (
                            <button
                              type="button"
                              className="btn btn-o btn-sm"
                              onClick={() => {
                                setMsg(null);
                                setAdjust(e);
                              }}
                              aria-label={`Điều chỉnh bút toán ${e.seq}`}
                            >
                              Điều chỉnh
                            </button>
                          ) : null}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}
            {msg ? (
              <div className="alert alert-ok" role="status">
                <span>{msg}</span>
              </div>
            ) : null}
            {adjust ? (
              <AdjustForm
                key={adjust.id}
                e={adjust}
                onDone={(m) => {
                  setMsg(m);
                  refresh();
                }}
              />
            ) : null}
          </Section>
          {alloc ? (
            <Section title="Kết quả phân bổ">
              <AllocationPanel key={alloc} id={alloc} onChanged={refresh} />
            </Section>
          ) : null}
          {acc ? (
            <Section title="Ghi tay bút toán chi phí (kỳ theo thời điểm sự việc, kỳ phải mở)">
              <ManualEntry lot={lot} onDone={refresh} />
            </Section>
          ) : null}
        </>
      ) : null}
    </div>
  );
}
