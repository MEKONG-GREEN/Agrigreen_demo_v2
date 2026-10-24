"use client";

// FN-08 Thông tin làm việc (BRD HCTH v3.0; màn `tl` và `tl.d` của prototype HCTH — W11.29). Danh sách nhân sự với ngày
// vào làm/nghỉ việc, ba thẻ số liệu bấm để lọc nhanh, lọc theo đơn vị/trạng thái/tháng biến động, hộp thoại "Cập nhật
// thông tin làm việc" và trang chi tiết có khối "Ảnh hưởng đến báo cáo" (NS01/NS02). Chỉ dùng API có sẵn của W11.22:
// danh sách /v1/hrm/staff (tải đủ các trang, tối đa 100/lần), sự kiện work_start/work_end qua /events, /replace, /void
// (sổ chỉ ghi thêm). Dữ liệu cá nhân chỉ trong bộ nhớ của màn.
import Link from "next/link";
import { useParams } from "next/navigation";
import { useMemo, useState } from "react";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { BackLink, LabelValueBlock, LogPanel, type LogEntry } from "@/components/patterns/detail";
import { DataTable, FilterBar, FilterChip, PageHeader, SearchBox, StatusPill, type Column } from "@/components/patterns/list";
import { LoadState, useAction, useLoad } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";
import { useHrmApi } from "./CatalogScreen";
import { STATUS_LABEL, vnDate, type HrmStaffDetail, type HrmStaffItem, type StaffApi } from "./staff-api";
import { useStaffApi } from "./StaffScreens";
import { KpiGrid, KpiTile } from "./ui";
import {
  currentSpell,
  filterWork,
  monthLabel,
  planWork,
  recentMonths,
  reportImpact,
  validateWork,
  vnMonth,
  workKpis,
  type WorkFilter,
  type WorkForm,
} from "./work-info";

const PAGE = 20;
export const WORK_ROUTE = "/kn/nhan-su/ho-so/thong-tin-lam-viec";
const PROFILE_ROUTE = "/kn/nhan-su/ho-so";

function WorkStatus({ s }: { s: string }) {
  return (
    <StatusPill tone={s === "active" ? "ok" : s === "left" ? "neutral" : "warn"}>
      {s === "active" ? "Đang làm việc" : (STATUS_LABEL[s] ?? s)}
    </StatusPill>
  );
}

/** Tải đủ danh sách trong phạm vi (máy chủ giới hạn 100 dòng/lần). */
async function loadAll(sa: StaffApi, unit?: string, q?: string): Promise<{ day: string; items: HrmStaffItem[] }> {
  const items: HrmStaffItem[] = [];
  let day = "";
  for (let offset = 0; ; offset += 100) {
    const r = await sa.list({ unit, q, offset, limit: 100 });
    day = r.day;
    items.push(...r.items);
    if (items.length >= r.total || r.items.length === 0) break;
  }
  return { day, items };
}

function useNames() {
  const sa = useStaffApi();
  const ha = useHrmApi();
  const units = useLoad(() => sa.units(), []);
  const pos = useLoad(() => ha.list("position"), []);
  const reasons = useLoad(() => ha.list("leave_reason"), []);
  return {
    units: units.data?.items ?? [],
    reasons: (reasons.data?.items ?? []).filter((x) => x.active),
    unitName: (code: string) => units.data?.items.find((u) => u.code === code)?.name ?? code,
    posName: (code: string) => pos.data?.items.find((p) => p.code === code)?.name ?? code,
    reasonName: (code: string) => reasons.data?.items.find((p) => p.code === code)?.name ?? code,
  };
}

function unitText(s: HrmStaffItem["current"], unitName: (c: string) => string): string {
  if (s.unit) return unitName(s.unit);
  if (s.mgmt_unit) return `Xã ${s.commune} · ${unitName(s.mgmt_unit)}`;
  return "—";
}

// --------------------------------------------------------------------------------------------------------------------
// Danh sách (tl).
// --------------------------------------------------------------------------------------------------------------------

export function HrmWorkInfoScreen() {
  const sa = useStaffApi();
  const names = useNames();
  const access = useLoad(() => sa.access(), []);
  const enabled = access.data?.enabled === true && access.data.perms["profile.view"] !== "none";
  const canEdit = enabled && access.data?.perms["profile.edit"] !== "none";
  const [unit, setUnit] = useState("");
  const [q, setQ] = useState("");
  const [f, setF] = useState<WorkFilter>({ status: "", month: "", move: "" });
  const [page, setPage] = useState(1);
  const [nonce, setNonce] = useState(0);
  const [editing, setEditing] = useState<string | null>(null);
  const act = useAction();
  const all = useLoad(enabled ? () => loadAll(sa, unit || undefined, q || undefined) : null, [enabled, unit, q, nonce]);
  const km = f.month || vnMonth();
  const months = useMemo(() => recentMonths(vnMonth(), 12), []);
  const set = (patch: Partial<WorkFilter>) => {
    setF((x) => ({ ...x, ...patch }));
    setPage(1);
  };
  const cols: Column<HrmStaffItem>[] = [
    {
      key: "name",
      header: "Họ và tên",
      cell: (r) => <Link href={`${WORK_ROUTE}/${r.id}`}>{r.label || "(ẩn)"}</Link>,
    },
    { key: "code", header: "Mã nhân viên", cell: (r) => r.code },
    { key: "unit", header: "Đơn vị", cell: (r) => unitText(r.current, names.unitName) },
    { key: "pos", header: "Chức danh", cell: (r) => (r.current.position ? names.posName(r.current.position) : "—") },
    { key: "start", header: "Ngày vào làm", cell: (r) => vnDate(r.current.started_on) },
    { key: "end", header: "Ngày nghỉ việc", cell: (r) => vnDate(r.current.ended_on) },
    { key: "st", header: "Trạng thái", cell: (r) => <WorkStatus s={r.current.status} /> },
    {
      key: "acts",
      header: "",
      cell: (r) => (
        <span className="row-acts">
          <Link href={`${WORK_ROUTE}/${r.id}`} aria-label={`Xem thông tin làm việc ${r.code}`} title="Xem">
            <Icon name="eye" size={17} />
          </Link>
          {canEdit ? (
            <button type="button" aria-label={`Cập nhật ${r.code}`} title="Cập nhật" onClick={() => setEditing(r.id)}>
              <Icon name="edit" size={16} />
            </button>
          ) : null}
        </span>
      ),
    },
  ];
  return (
    <>
      <PageHeader
        title="Thông tin làm việc"
        actions={
          enabled && access.data?.perms.export !== "none" ? (
            <button
              type="button"
              className="btn btn-o btn-sm"
              disabled={act.busy}
              onClick={() =>
                void act.run(
                  () =>
                    sa.exportCsv({
                      unit: unit || undefined,
                      status: f.status || undefined,
                    }),
                  (r) => (saveBlob(r.blob, r.name), "Đã xuất danh sách (thao tác được ghi nhật ký)."),
                )
              }
            >
              Xuất CSV
            </button>
          ) : null
        }
      >
        Ngày vào làm, nghỉ việc của nhân sự. Hồ sơ không bị xóa: nghỉ việc chỉ đổi trạng thái; số liệu NS01, NS02 tự cập nhật.
      </PageHeader>
      <LoadState s={access}>
        {(a) =>
          !a.enabled || a.perms["profile.view"] === "none" ? (
            <div className="alert alert-info" role="note">
              <span>Bạn không có quyền xem thông tin làm việc của nhân sự.</span>
            </div>
          ) : (
            <>
              {act.view}
              <FilterBar
                onReset={() => {
                  setUnit("");
                  setQ("");
                  set({ status: "", month: "", move: "" });
                }}
              >
                <SearchBox value={q} onChange={(v) => (setQ(v), setPage(1))} placeholder="Mã nhân viên hoặc số điện thoại" />
                <FilterChip
                  label="Đơn vị"
                  value={unit}
                  options={[{ value: "", label: "Tất cả" }, ...names.units.map((u) => ({ value: u.code, label: u.name }))]}
                  onChange={(v) => (setUnit(v), setPage(1))}
                />
                <FilterChip
                  label="Trạng thái"
                  value={f.status}
                  options={[
                    { value: "", label: "Tất cả" },
                    { value: "active", label: "Đang làm việc" },
                    { value: "left", label: "Đã nghỉ việc" },
                  ]}
                  onChange={(v) => set({ status: v as WorkFilter["status"] })}
                />
                <FilterChip
                  label="Vào làm / nghỉ việc trong"
                  value={f.month}
                  options={[{ value: "", label: "Tất cả các tháng" }, ...months.map((m) => ({ value: m, label: monthLabel(m) }))]}
                  onChange={(v) => set({ month: v })}
                />
                <FilterChip
                  label="Biến động"
                  value={f.move}
                  options={[
                    { value: "", label: "Tất cả" },
                    { value: "vao", label: "Vào làm" },
                    { value: "nghi", label: "Nghỉ việc" },
                  ]}
                  onChange={(v) => set({ move: v as WorkFilter["move"] })}
                />
              </FilterBar>
              <LoadState s={all}>
                {(l) => {
                  const k = workKpis(l.items, km);
                  const rows = filterWork(l.items, f, km);
                  const shown = rows.slice((page - 1) * PAGE, page * PAGE);
                  return (
                    <>
                      <KpiGrid label="Chỉ số thông tin làm việc">
                        <KpiTile
                          label="Đang làm việc"
                          value={k.active}
                          active={f.status === "active" && !f.month && !f.move}
                          onClick={() => set({ status: "active", month: "", move: "" })}
                        />
                        <KpiTile
                          label="Vào làm trong tháng"
                          value={k.joined}
                          sub={monthLabel(km)}
                          active={f.move === "vao"}
                          onClick={() => set({ status: "", month: km, move: "vao" })}
                        />
                        <KpiTile
                          label="Nghỉ việc trong tháng"
                          value={k.left}
                          sub={monthLabel(km)}
                          active={f.move === "nghi"}
                          onClick={() => set({ status: "", month: km, move: "nghi" })}
                        />
                      </KpiGrid>
                      <DataTable
                        caption="Thông tin làm việc"
                        columns={cols}
                        rows={shown}
                        rowKey={(r) => r.id}
                        page={page}
                        pageSize={PAGE}
                        total={rows.length}
                        onPage={setPage}
                      />
                      <p className="mute small">
                        Bấm thẻ số liệu để lọc nhanh; bấm tên để xem chi tiết, bấm biểu tượng bút để sửa ngày vào làm, nghỉ việc.
                        Số liệu tại ngày {vnDate(l.day)}.
                      </p>
                    </>
                  );
                }}
              </LoadState>
            </>
          )
        }
      </LoadState>
      {editing ? (
        <WorkDialogLoader
          sa={sa}
          id={editing}
          reasons={names.reasons}
          onClose={() => setEditing(null)}
          onDone={(msg) => {
            setEditing(null);
            act.setNote(msg);
            setNonce((n) => n + 1);
          }}
        />
      ) : null}
    </>
  );
}

function WorkDialogLoader({
  sa,
  id,
  reasons,
  onClose,
  onDone,
}: {
  sa: StaffApi;
  id: string;
  reasons: { code: string; name: string }[];
  onClose: () => void;
  onDone: (msg: string) => void;
}) {
  const d = useLoad(() => sa.get(id), [id]);
  if (!d.data)
    return (
      <Dialog title="Cập nhật thông tin làm việc" onClose={onClose}>
        <LoadState s={d}>{() => null}</LoadState>
      </Dialog>
    );
  return <WorkDialog sa={sa} d={d.data} reasons={reasons} onClose={onClose} onDone={onDone} />;
}

/** Hộp thoại "Cập nhật thông tin làm việc · <họ tên>" (prototype `tlModal`). */
export function WorkDialog({
  sa,
  d,
  reasons,
  onClose,
  onDone,
}: {
  sa: StaffApi;
  d: HrmStaffDetail;
  reasons: { code: string; name: string }[];
  onClose: () => void;
  onDone: (msg: string) => void;
}) {
  const sp = currentSpell(d.events);
  const [v, setV] = useState<WorkForm>({
    start: sp.start?.valid_from ?? "",
    end: sp.end?.valid_from ?? "",
    leaveReason: sp.end?.payload.reason ?? "",
    decisionNo: sp.end?.payload.decision_no ?? "",
    note: "",
  });
  const [err, setErr] = useState<{ message: string; fields: string[] } | null>(null);
  const act = useAction();
  const ops = planWork(d.events, v);
  const needNote = ops.some((o) => o.op !== "add");
  const save = async () => {
    const bad = validateWork(d.events, v);
    setErr(bad);
    if (bad) return;
    // P0-WEB-IDEMKEY: mỗi lời gọi sau await phải bọc idem() để bấm lại không ghi trùng sự kiện đã lưu.
    const r = await act.run(async (idem) => {
      for (const o of ops) {
        if (o.op === "add") await idem(() => sa.addEvent(d.id, o.body));
        else if (o.op === "replace") await idem(() => sa.replaceEvent(d.id, o.event, o.body));
        else await idem(() => sa.voidEvent(d.id, o.event, o.reason));
      }
      return true;
    });
    if (r)
      onDone(
        v.end
          ? "Đã lưu. Hồ sơ chuyển “Đã nghỉ việc” từ ngày nghỉ; NS02 và NS01 cập nhật."
          : "Đã lưu thông tin làm việc.",
      );
  };
  const invalid = (k: string) => (err?.fields.includes(k) ? true : undefined);
  return (
    <Dialog
      title={`Cập nhật thông tin làm việc · ${d.profile.full_name || d.code}`}
      onClose={onClose}
      actions={
        <>
          <button type="button" className="btn btn-o btn-sm" onClick={onClose}>
            Hủy
          </button>
          <button type="button" className="btn btn-p btn-sm" disabled={act.busy} onClick={() => void save()}>
            Lưu
          </button>
        </>
      }
    >
      {act.view}
      {err ? (
        <div className="alert alert-bad" role="alert">
          <span>{err.message}</span>
        </div>
      ) : null}
      <div className="grid2">
        <label className="field">
          <span>Ngày vào làm *</span>
          <input type="date" aria-invalid={invalid("start")} value={v.start} onChange={(e) => setV({ ...v, start: e.target.value })} />
        </label>
        <div>
          <label className="field">
            <span>Ngày nghỉ việc</span>
            <input type="date" aria-invalid={invalid("end")} value={v.end} onChange={(e) => setV({ ...v, end: e.target.value })} />
          </label>
          <small className="mute">Để trống nếu đang làm việc.</small>
        </div>
        <label className="field">
          <span>Lý do nghỉ</span>
          <select
            aria-invalid={invalid("leaveReason")}
            value={v.leaveReason}
            disabled={!v.end}
            onChange={(e) => setV({ ...v, leaveReason: e.target.value })}
          >
            <option value="">— Chọn —</option>
            {reasons.map((r) => (
              <option key={r.code} value={r.code}>
                {r.name}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          <span>Số quyết định (nếu có)</span>
          <input value={v.decisionNo} disabled={!v.end} onChange={(e) => setV({ ...v, decisionNo: e.target.value })} />
        </label>
        {needNote ? (
          <div style={{ gridColumn: "1 / -1" }}>
            <label className="field">
              <span>Lý do sửa (10–500 ký tự) *</span>
              <textarea rows={2} aria-invalid={invalid("note")} value={v.note} onChange={(e) => setV({ ...v, note: e.target.value })} />
            </label>
            <small className="mute">Dòng cũ được giữ trong lịch sử (sổ chỉ ghi thêm).</small>
          </div>
        ) : null}
      </div>
      <p className="mute small">Tệp quyết định nghỉ việc tải lên ở Hồ sơ dữ liệu (thư mục của nhân viên).</p>
    </Dialog>
  );
}

// --------------------------------------------------------------------------------------------------------------------
// Chi tiết (tl.d).
// --------------------------------------------------------------------------------------------------------------------

const WORK_KIND_LABEL: Record<string, string> = { work_start: "Vào làm", work_end: "Nghỉ việc" };

/** Lịch sử của khối làm việc: mọi dòng work_start/work_end (kể cả dòng đã thay/hủy) mới nhất trước. */
export function workHistory(d: HrmStaffDetail): LogEntry[] {
  return d.events
    .filter((e) => e.kind === "work_start" || e.kind === "work_end")
    .sort((a, b) => b.version - a.version)
    .map((e) => ({
      event: `${e.void ? "Hủy" : e.supersedes ? "Sửa" : "Ghi"} ${WORK_KIND_LABEL[e.kind] ?? e.kind} ${vnDate(e.valid_from)}${
        e.effective ? "" : " (không còn hiệu lực)"
      }`,
      actor_display: "HCTH",
      at: e.recorded_at,
      reason: e.reason || null,
    }));
}

export function HrmWorkInfoDetailScreen() {
  const { id } = useParams<{ id: string }>();
  const sa = useStaffApi();
  const names = useNames();
  const access = useLoad(() => sa.access(), []);
  const s = useLoad(() => sa.get(id), [id]);
  const [editing, setEditing] = useState(false);
  const act = useAction();
  useBreadcrumbTail(s.data ? [{ label: s.data.profile.full_name || s.data.code }] : []);
  const canEdit = access.data?.enabled === true && access.data.perms["profile.edit"] !== "none" && s.data?.can["profile.edit"] !== false;
  return (
    <>
      <BackLink href={WORK_ROUTE}>Thông tin làm việc</BackLink>
      <LoadState s={s}>
        {(d) => {
          const c = d.current;
          const sp = currentSpell(d.events);
          const imp = reportImpact(c.ended_on);
          return (
            <>
              <PageHeader
                title={`${d.profile.full_name || "(ẩn)"} · ${d.code}`}
                badge={<WorkStatus s={c.status} />}
                actions={
                  <>
                    {canEdit ? (
                      <button type="button" className="btn btn-p btn-sm" onClick={() => setEditing(true)}>
                        Sửa
                      </button>
                    ) : null}
                    <Link className="btn btn-o btn-sm" href={`${PROFILE_ROUTE}/${d.id}`}>
                      Mở hồ sơ nhân viên
                    </Link>
                  </>
                }
              />
              {act.view}
              <div className="detail-grid">
                <div className="card detail-main">
                  <LabelValueBlock
                    title="Thông tin làm việc"
                    items={[
                      { label: "Đơn vị", value: unitText(c, names.unitName) },
                      { label: "Chức danh", value: c.position ? names.posName(c.position) : "" },
                      { label: "Ngày vào làm", value: vnDate(c.started_on) },
                      { label: "Ngày nghỉ việc", value: vnDate(c.ended_on) },
                      { label: "Lý do nghỉ", value: c.leave_reason ? names.reasonName(c.leave_reason) : "" },
                      { label: "Số quyết định nghỉ việc", value: sp.end?.payload.decision_no ?? "" },
                    ]}
                  />
                  <LabelValueBlock
                    title="Ảnh hưởng đến báo cáo"
                    items={[
                      { label: "Danh sách NS02", value: imp.ns02 },
                      { label: "Danh sách NS01", value: imp.ns01 },
                    ]}
                  />
                </div>
                <LogPanel title="Lịch sử" entries={workHistory(d)} empty="Chưa có dòng vào làm/nghỉ việc." />
              </div>
              {editing ? (
                <WorkDialog
                  sa={sa}
                  d={d}
                  reasons={names.reasons}
                  onClose={() => setEditing(false)}
                  onDone={(msg) => {
                    setEditing(false);
                    act.setNote(msg);
                    s.reload();
                  }}
                />
              ) : null}
            </>
          );
        }}
      </LoadState>
    </>
  );
}
