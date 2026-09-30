"use client";

import { useState } from "react";
import { idem, unwrap, unwrapAs } from "@/api/client";
import type { CollectionOrder, CollectionPlanInput, CrewSuggestions, OrderDetail, OrderStatus } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { hasRole, ROLES } from "@/auth/principal";
import { ActionStatus, Chip, Ref, RefPicker, Section, SelectField, SelfHint, TextField, useAction } from "@/components/kit";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, Loading, PageHead } from "@/components/ui";
import { formatDate, formatDateTime } from "@/lib/format";
import { remember } from "@/lib/recent";
import { ORDER_STATUS_LABEL, shortRef, validReason } from "@/lib/rules";
import { useAsync } from "@/lib/useAsync";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const BALES = /^[1-9][0-9]{0,6}$/;
const EVENT_LABEL: Record<string, string> = { planned: "Lập", revised: "Sửa kế hoạch", approved: "Duyệt", rejected: "Từ chối", assigned: "Giao tổ", closed: "Đóng", cancelled: "Huỷ" };

interface PlanForm {
  htx_ref: string;
  commune_code: string;
  window_from: string;
  window_to: string;
  plots: string;
}
const today = () => new Date().toISOString().slice(0, 10);
const EMPTY: PlanForm = { htx_ref: "", commune_code: "", window_from: today(), window_to: today(), plots: "" };

function toPlan(f: PlanForm): CollectionPlanInput {
  return {
    htx_ref: f.htx_ref.trim(),
    ...(f.commune_code.trim() ? { commune_code: f.commune_code.trim() } : {}),
    window_from: f.window_from,
    window_to: f.window_to,
    plot_refs: f.plots.split(/[\s,]+/).filter(Boolean),
  };
}
function planError(f: PlanForm): string | null {
  if (!UUID.test(f.htx_ref.trim())) return "Chọn HTX (UUID)";
  const plots = f.plots.split(/[\s,]+/).filter(Boolean);
  if (!plots.length || plots.some((p) => !UUID.test(p))) return "Nhập ít nhất một mã thửa UUID";
  if (plots.length > 200) return "Tối đa 200 thửa";
  if (!f.window_from || !f.window_to || f.window_to < f.window_from) return "Khung ngày: đến ≥ từ";
  if ((Date.parse(f.window_to) - Date.parse(f.window_from)) / 86_400_000 > 90) return "Khung tối đa 90 ngày";
  return null;
}

function PlanFields({ f, set, knownHtx }: { f: PlanForm; set: (p: Partial<PlanForm>) => void; knownHtx: { id: string; label: string }[] }) {
  return (
    <div className="stack">
      <RefPicker label="HTX" kind="htx" value={f.htx_ref} onChange={(htx_ref) => set({ htx_ref })} extra={knownHtx} hint="Đối chiếu với nền tảng qua Application API (tài khoản dịch vụ MKG)" />
      <div className="grid3">
        <TextField label="Gom từ ngày" type="date" value={f.window_from} onChange={(window_from) => set({ window_from })} />
        <TextField label="Gom đến ngày" type="date" value={f.window_to} onChange={(window_to) => set({ window_to })} />
        <TextField label="Mã xã" value={f.commune_code} onChange={(commune_code) => set({ commune_code })} hint="Bỏ trống → mã xã của HTX" />
      </div>
      <TextField label="Mã thửa (mỗi dòng một UUID)" value={f.plots} onChange={(plots) => set({ plots })} multiline hint="Thửa phải thuộc HTX đã chọn" />
    </div>
  );
}

function OrderPanel({ id, onChanged, knownHtx }: { id: string; onChanged: () => void; knownHtx: { id: string; label: string }[] }) {
  const { api, principal } = useAuth();
  const d = useAsync(() => unwrapAs<OrderDetail>(api.GET("/v1/fieldops/orders/{id}", { params: { path: { id } } })), [api, id]);
  const act = useAction();
  const [reason, setReason] = useState("");
  const [crew, setCrew] = useState("");
  const [bales, setBales] = useState("");
  const [rank, setRank] = useState<number | null>(null);
  const [sugg, setSugg] = useState<CrewSuggestions | null>(null);
  const [revise, setRevise] = useState<PlanForm | null>(null);
  const sup = hasRole(principal, ROLES.fieldSupervisor);
  if (d.loading) return <Loading />;
  if (d.error) return <ProblemAlert problem={d.error} />;
  const o = d.data!.order;
  const after = (msg: string) => (r: unknown) => {
    if (r) {
      d.reload();
      onChanged();
      setReason("");
      setRevise(null);
    }
    return msg;
  };
  const p = () => ({ path: { id }, header: idem() });
  const run = (fn: () => Promise<unknown>, msg: string) => act.run(fn, msg).then(after(msg));
  const reviseErr = revise ? planError(revise) : null;
  return (
    <div className="stack">
      <dl className="kv">
        <dt>Trạng thái</dt>
        <dd className="row">
          <Chip status={o.status} label={ORDER_STATUS_LABEL[o.status ?? ""]} /> {o.published ? <span className="chip c-info">Đã công bố collection_schedule</span> : null}
        </dd>
        <dt>Mã lệnh = mã lô gom</dt>
        <dd>
          <Ref id={o.order_id} label={o.order_id ?? ""} />
        </dd>
        <dt>HTX · xã</dt>
        <dd>
          <Ref id={o.htx_ref} /> · {o.commune_code}
        </dd>
        <dt>Khung gom</dt>
        <dd>
          {formatDate(o.window_from)} → {formatDate(o.window_to)} · phiên bản {o.revision}
        </dd>
        <dt>Thửa</dt>
        <dd>{o.plot_refs?.map((pl) => <Ref key={pl} id={pl} />)}</dd>
        <dt>Tổ gom</dt>
        <dd>
          <Ref id={o.crew_ref} />
        </dd>
        <dt>Người lập / duyệt</dt>
        <dd>
          <Ref id={o.created_by} /> / <Ref id={o.approved_by} />
        </dd>
        {o.reason ? (
          <>
            <dt>Lý do</dt>
            <dd>{o.reason}</dd>
          </>
        ) : null}
      </dl>
      <ol className="list-plain small" aria-label="Lịch sử lệnh gom">
        {d.data!.history.map((e) => (
          <li key={e.id}>
            {formatDateTime(e.event_time)} · <b>{EVENT_LABEL[e.kind] ?? e.kind}</b> (v{e.revision}) · <Ref id={e.actor_ref} />
            {e.reason ? ` — ${e.reason}` : ""}
          </li>
        ))}
      </ol>
      <ActionStatus a={act} />
      {sup && o.status !== "closed" && o.status !== "cancelled" ? (
        <div className="panel stack">
          {o.status === "planned" ? <SelfHint proposer={o.created_by} /> : null}
          <div className="grid2">
            <TextField label="Lý do (từ chối / huỷ)" value={reason} onChange={setReason} hint="Ít nhất 10 ký tự" />
            {o.status === "approved" || o.status === "assigned" ? (
              <>
                <TextField label="Số cuộn dự kiến" value={bales} onChange={setBales} hint="Khối lượng việc — kiểm năng lực/ngày của tổ (FM-06)" />
                <TextField
                  label="Mã tổ gom"
                  value={crew}
                  onChange={(v) => {
                    setCrew(v);
                    setRank(null);
                  }}
                  hint="UUID tổ gom trong sổ tổ gom, hoặc chọn từ gợi ý"
                />
              </>
            ) : null}
          </div>
          {sugg ? (
            <ul className="list-plain small" aria-label="Gợi ý tổ gom">
              {sugg.suggestions?.length ? null : <li>Không tổ nào đang hoạt động còn đủ năng lực (FM-06/07).</li>}
              {sugg.suggestions?.map((g) => (
                <li key={g.crew_ref} className="row">
                  <b>#{g.rank}</b> {g.code} · {g.name} · tải cao nhất {g.peak_load}/{g.capacity_per_day} {g.capacity_unit}/ngày{g.local ? " · cùng xã" : ""}
                  <button
                    type="button"
                    className="btn btn-o btn-sm"
                    onClick={() => {
                      setCrew(g.crew_ref ?? "");
                      setRank(g.rank ?? null);
                    }}
                  >
                    Chọn {g.code}
                  </button>
                </li>
              ))}
              {sugg.unfit?.map((g) => (
                <li key={g.crew_ref}>
                  {g.code}: {g.unfit === "over_capacity" ? `vượt năng lực ngày ${formatDate(g.over_day)}` : "khác đơn vị năng lực"}
                </li>
              ))}
            </ul>
          ) : null}
          <div className="row">
            {o.status === "planned" ? (
              <>
                <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void run(() => unwrap(api.POST("/v1/fieldops/orders/{id}/decision", { params: p(), body: { decision: "approved" } })), "Đã duyệt lệnh gom — công bố collection_schedule")}>
                  Duyệt lệnh gom
                </button>
                <button
                  type="button"
                  className="btn btn-d btn-sm"
                  disabled={act.busy || !validReason(reason)}
                  onClick={() => void run(() => unwrap(api.POST("/v1/fieldops/orders/{id}/decision", { params: p(), body: { decision: "rejected", reason: reason.trim() } })), "Đã từ chối lệnh gom (→ đã huỷ)")}
                >
                  Từ chối lệnh gom
                </button>
                <button
                  type="button"
                  className="btn btn-o btn-sm"
                  onClick={() => setRevise({ htx_ref: o.htx_ref ?? "", commune_code: o.commune_code ?? "", window_from: o.window_from ?? today(), window_to: o.window_to ?? today(), plots: (o.plot_refs ?? []).join("\n") })}
                >
                  Sửa kế hoạch
                </button>
              </>
            ) : null}
            {o.status === "approved" || o.status === "assigned" ? (
              <>
                <button
                  type="button"
                  className="btn btn-o btn-sm"
                  disabled={act.busy || !BALES.test(bales.trim())}
                  onClick={() =>
                    void act
                      .run(() => unwrapAs<CrewSuggestions>(api.GET("/v1/fieldops/orders/{id}/crew-suggestions", { params: { path: { id }, query: { expected_bales: Number(bales.trim()) } } })), "Đã gợi ý tổ gom")
                      .then((r) => {
                        if (r) setSugg(r);
                      })
                  }
                >
                  Gợi ý tổ gom
                </button>
                <button
                  type="button"
                  className="btn btn-p btn-sm"
                  disabled={act.busy || !UUID.test(crew.trim()) || !BALES.test(bales.trim())}
                  onClick={() =>
                    void run(
                      () =>
                        unwrap(
                          api.POST("/v1/fieldops/orders/{id}/assign", {
                            params: p(),
                            body: rank ? { crew_ref: crew.trim(), expected_bales: Number(bales.trim()), mode: "suggested", suggestion_rank: rank } : { crew_ref: crew.trim(), expected_bales: Number(bales.trim()), mode: "manual" },
                          }),
                        ),
                      "Đã giao tổ gom",
                    )
                  }
                >
                  {o.status === "assigned" ? "Đổi tổ gom" : "Giao tổ gom"}
                </button>
              </>
            ) : null}
            {o.status === "assigned" ? (
              <button type="button" className="btn btn-o btn-sm" disabled={act.busy} onClick={() => void run(() => unwrap(api.POST("/v1/fieldops/orders/{id}/close", { params: p() })), "Đã đóng lệnh gom")}>
                Đóng lệnh gom
              </button>
            ) : null}
            <button
              type="button"
              className="btn btn-d btn-sm"
              disabled={act.busy || !validReason(reason)}
              onClick={() => void run(() => unwrap(api.POST("/v1/fieldops/orders/{id}/cancel", { params: p(), body: { reason: reason.trim() } })), "Đã huỷ lệnh gom (không xoá — sự kiện cancelled, RB-1)")}
            >
              Huỷ lệnh gom
            </button>
          </div>
        </div>
      ) : null}
      {revise ? (
        <div className="panel stack">
          <b>Sửa kế hoạch (phiên bản {(o.revision ?? 1) + 1} — người sửa thành người lập)</b>
          <PlanFields f={revise} set={(x) => setRevise({ ...revise, ...x })} knownHtx={knownHtx} />
          {reviseErr ? <p className="pill-bad">{reviseErr}</p> : null}
          <div className="row">
            <button
              type="button"
              className="btn btn-p btn-sm"
              disabled={!!reviseErr || act.busy}
              onClick={() => void run(() => unwrap(api.POST("/v1/fieldops/orders/{id}/revisions", { params: p(), body: toPlan(revise) })), "Đã lưu phiên bản kế hoạch mới — chờ người khác duyệt")}
            >
              Lưu kế hoạch
            </button>
            <button type="button" className="btn btn-o btn-sm" onClick={() => setRevise(null)}>
              Huỷ sửa
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

export default function OrdersPage() {
  const { api, principal } = useAuth();
  const [status, setStatus] = useState<"" | OrderStatus>("");
  const list = useAsync(async () => (await unwrap(api.GET("/v1/fieldops/orders", { params: { query: { limit: 200, ...(status ? { status } : {}) } } }))).items ?? [], [api, status]);
  const [sel, setSel] = useState("");
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState<PlanForm>(EMPTY);
  const act = useAction();
  const knownHtx = Array.from(new Set((list.data ?? []).map((o) => o.htx_ref!))).map((h) => ({ id: h, label: `HTX ${shortRef(h)}` }));
  const err = planError(form);

  const create = async () => {
    const r = await act.run(() => unwrap(api.POST("/v1/fieldops/orders", { params: { header: idem() }, body: toPlan(form) })), "Đã lập lệnh gom (chờ duyệt) — chưa công bố tới khi người KHÁC duyệt");
    if (r?.order?.order_id) {
      remember("lot", r.order.order_id, `Lô gom ${shortRef(r.order.order_id)}`);
      setCreating(false);
      setForm(EMPTY);
      setSel(r.order.order_id);
      list.reload();
    }
  };

  return (
    <div className="stack">
      <PageHead
        title="Lệnh gom"
        subtitle="Lập → duyệt (người KHÁC người lập, R-56) → giao tổ → đóng; huỷ bằng sự kiện có lý do. Duyệt thì công bố van collection_schedule cho HTX."
        actions={
          hasRole(principal, ROLES.fieldSupervisor) ? (
            <button type="button" className="btn btn-p btn-sm" onClick={() => setCreating(true)}>
              Lập lệnh gom
            </button>
          ) : null
        }
      />
      <ActionStatus a={act} />
      {creating ? (
        <Section title="Lập lệnh gom">
          <PlanFields f={form} set={(x) => setForm({ ...form, ...x })} knownHtx={knownHtx} />
          {err ? <p className="small mute">{err}</p> : null}
          <div className="row" style={{ marginTop: 10 }}>
            <button type="button" className="btn btn-p btn-sm" disabled={!!err || act.busy} onClick={() => void create()}>
              Gửi lệnh gom
            </button>
            <button type="button" className="btn btn-o btn-sm" onClick={() => setCreating(false)}>
              Huỷ
            </button>
          </div>
        </Section>
      ) : null}
      <Section title="Danh sách lệnh">
        <div className="stack">
          <SelectField label="Lọc trạng thái" value={status} onChange={(v) => setStatus(v as typeof status)} options={[{ value: "", label: "Tất cả" }, ...Object.entries(ORDER_STATUS_LABEL).map(([value, label]) => ({ value, label }))]} />
          {list.loading ? <Loading /> : null}
          {list.error ? <ProblemAlert problem={list.error} /> : null}
          {list.data && !list.data.length ? <Empty>Không có lệnh gom.</Empty> : null}
          {list.data?.length ? (
            <div className="tbl-wrap">
              <table className="tbl" aria-label="Lệnh gom">
                <thead>
                  <tr>
                    <th>Lệnh</th>
                    <th>HTX</th>
                    <th>Khung gom</th>
                    <th className="num">Số thửa</th>
                    <th>Trạng thái</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {list.data.map((o: CollectionOrder) => (
                    <tr key={o.order_id} aria-selected={o.order_id === sel}>
                      <td>
                        <Ref id={o.order_id} />
                      </td>
                      <td>
                        <Ref id={o.htx_ref} />
                      </td>
                      <td>
                        {formatDate(o.window_from)} → {formatDate(o.window_to)}
                      </td>
                      <td className="num">{o.plot_refs?.length}</td>
                      <td>
                        <Chip status={o.status} label={ORDER_STATUS_LABEL[o.status ?? ""]} />
                      </td>
                      <td>
                        <button type="button" className="btn btn-o btn-sm" onClick={() => setSel(o.order_id!)} aria-label={`Mở lệnh ${shortRef(o.order_id)}`}>
                          Mở
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </div>
      </Section>
      {sel ? (
        <Section title="Chi tiết lệnh gom">
          <OrderPanel key={sel} id={sel} onChanged={list.reload} knownHtx={knownHtx} />
        </Section>
      ) : null}
    </div>
  );
}
