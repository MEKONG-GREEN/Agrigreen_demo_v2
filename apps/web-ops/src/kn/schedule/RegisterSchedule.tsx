"use client";

// Màn Cấp Xã "Lịch công tác" — đăng ký lịch tuần (FN-XA-03.08, Hình 21). Dùng lại cho tab "Lịch của tôi" của Cấp Tỉnh.
// Lưới Sáng/Chiều × 7 ngày, khung "Mục công tác", Sao chép tuần trước, Lưu nháp, Gửi duyệt, khung Log.
import { useEffect, useMemo, useState } from "react";
import type { ApiClient } from "@/api/client";
import { uuidv7 } from "@/api/idempotency";
import type { ApiProblem } from "@/api/problem";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { PageHeader } from "@/components/patterns/list";
import { toApiProblem } from "@/lib/useAsync";
import type { WorkScheduleApi } from "./api";
import {
  conflictIndexes,
  diffItems,
  itemSummary,
  looksLikePersonalNumber,
  SESSION_LABEL,
  timeError,
  toInput,
  type WsDetail,
  type WsItemInput,
  type WsMine,
  type WsSession,
} from "./model";
import { ScheduleLog, ScheduleTags, SessionGrid, useCommuneNames, useCoops, usePreloadCoops, WeekPicker, type CardItem } from "./parts";
import { dayColumnLabel, nextWeekStart, vnDeadlineLabel, weekDays } from "./week";

export interface CommuneOption {
  code: string;
  name: string;
}

type Item = WsItemInput & { id: string };

interface Local {
  week: string;
  mine: WsMine;
  items: Item[];
  base: number | null;
  dirty: boolean;
}

interface FormState {
  index: number | null;
  day: string;
  session: WsSession;
  start_time: string;
  end_time: string;
  content: string;
  commune_code: string;
  cooperative_ref: string;
  location: string;
  participants: string;
  note: string;
}

const s = (v: string | null | undefined) => v ?? "";
const n = (v: string) => (v.trim() ? v.trim() : null);

function fromDetail(week: string, mine: WsMine, detail: WsDetail | null): Local {
  const items = (detail?.items ?? []).map((x) => ({ ...toInput(x), id: x.id ?? uuidv7() }));
  return { week, mine: { ...mine, schedule: detail }, items, base: detail?.schedule.version ?? null, dirty: false };
}

export function RegisterSchedule({
  ws,
  api,
  communes,
  level,
  title = "Lịch làm việc hằng tuần",
  embedded = false,
  week: weekProp,
  onWeekChange,
  onChanged,
}: {
  ws: WorkScheduleApi;
  api: ApiClient;
  communes: CommuneOption[];
  level: "commune" | "province";
  title?: string;
  /** Trong tab "Lịch của tôi" của Cấp Tỉnh: không có tiêu đề trang riêng. */
  embedded?: boolean;
  week?: string;
  onWeekChange?: (w: string) => void;
  onChanged?: () => void;
}) {
  const [weekState, setWeekState] = useState(() => nextWeekStart());
  const week = weekProp ?? weekState;
  const setWeek = (w: string) => {
    setWeekState(w);
    onWeekChange?.(w);
  };
  const [reloadKey, setReloadKey] = useState(0);
  const [local, setLocal] = useState<Local | null>(null);
  const [loadError, setLoadError] = useState<{ week: string; problem: ApiProblem } | null>(null);
  const [form, setForm] = useState<FormState | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState<"draft" | "submit" | "copy" | null>(null);
  const [confirmCopy, setConfirmCopy] = useState(false);
  const [logKey, setLogKey] = useState(0);

  useEffect(() => {
    let active = true;
    ws.mine(week).then(
      (mine) => {
        if (!active) return;
        setLocal(fromDetail(week, mine, mine.schedule));
        setLoadError(null);
      },
      (e: unknown) => {
        if (active) setLoadError({ week, problem: toApiProblem(e) });
      },
    );
    return () => {
      active = false;
    };
  }, [ws, week, reloadKey]);

  const cur = local && local.week === week ? local : null;
  const detail = cur?.mine.schedule ?? null;
  const sched = detail?.schedule ?? null;
  const status = sched?.status ?? null;
  const locked = cur?.mine.locked ?? false;
  const editable = !!cur && !locked && (detail ? detail.can.edit : true);
  const canDraft = editable && (status === null || status === "draft" || status === "changes_requested");
  const canSubmit = editable && !!cur && cur.items.length > 0 && (cur.dirty || status === null || status === "draft" || status === "changes_requested");
  const canCopy = editable && !!cur?.mine.previous_available && canDraft;

  // Mục khác bản đã duyệt (điều chỉnh) → đánh dấu; mục bỏ so với bản đã duyệt liệt kê riêng.
  const approvedBase = useMemo(() => {
    if (!detail) return [];
    if (detail.schedule.status === "approved") return detail.items;
    return detail.approved_items;
  }, [detail]);
  const view = useMemo(() => {
    const items = cur?.items ?? [];
    const conflicts = conflictIndexes(items);
    const diff = approvedBase.length ? diffItems(approvedBase, items) : null;
    const changed = new Set([...(diff?.added ?? []), ...(diff?.modified.map((m) => m.after) ?? [])].map((x) => x.id));
    const cards: CardItem[] = items.map((it, i) => ({ ...it, conflict: conflicts.has(i), changed: changed.has(it.id) }));
    return { cards, conflicts: conflicts.size, removed: diff?.removed ?? [], changedCount: changed.size };
  }, [cur, approvedBase]);

  const allCommunes = useMemo(() => [...new Set([...communes.map((c) => c.code), ...(cur?.items.map((x) => x.commune_code ?? "") ?? [])])].filter(Boolean), [communes, cur]);
  const communeName = useCommuneNames(api, allCommunes, communes);
  const coopName = usePreloadCoops(api, allCommunes);
  const coops = useCoops(api, form?.commune_code || null);

  const days = weekDays(week);

  const openNew = (day: string, session: WsSession) => {
    if (!editable) return;
    setFormError(null);
    setForm({
      index: null,
      day,
      session,
      start_time: "",
      end_time: "",
      content: "",
      commune_code: level === "commune" && communes.length === 1 ? communes[0]!.code : "",
      cooperative_ref: "",
      location: "",
      participants: "",
      note: "",
    });
  };
  const openEdit = (index: number) => {
    const it = cur?.items[index];
    if (!it || !editable) return;
    setFormError(null);
    setForm({
      index,
      day: it.day,
      session: it.session,
      start_time: s(it.start_time),
      end_time: s(it.end_time),
      content: it.content,
      commune_code: s(it.commune_code),
      cooperative_ref: s(it.cooperative_ref),
      location: s(it.location),
      participants: s(it.participants),
      note: s(it.note),
    });
  };

  const saveItem = () => {
    if (!form || !cur) return;
    const draft = { start_time: n(form.start_time), end_time: n(form.end_time) };
    if (form.content.trim().length < 3) return setFormError("Nội dung công việc bắt buộc (ít nhất 3 ký tự).");
    const te = timeError(draft);
    if (te) return setFormError(te);
    if (looksLikePersonalNumber(form.participants) || looksLikePersonalNumber(form.note)) return setFormError("Không ghi số điện thoại, số CCCD vào thành phần phối hợp hoặc ghi chú.");
    const prev = form.index !== null ? cur.items[form.index] : undefined;
    const item: Item = {
      id: prev?.id ?? uuidv7(),
      day: form.day,
      session: form.session,
      ...draft,
      content: form.content.trim(),
      commune_code: n(form.commune_code),
      cooperative_ref: n(form.cooperative_ref),
      location: n(form.location),
      participants: n(form.participants),
      note: n(form.note),
    };
    const items = [...cur.items];
    if (form.index !== null) items[form.index] = item;
    else items.push(item);
    setLocal({ ...cur, items, dirty: true });
    setForm(null);
    setNotice(null);
  };

  const removeItem = () => {
    if (!form || form.index === null || !cur) return;
    setLocal({ ...cur, items: cur.items.filter((_, i) => i !== form.index), dirty: true });
    setForm(null);
  };

  const persist = async (action: "draft" | "submit") => {
    if (!cur) return;
    setBusy(action);
    setProblem(null);
    setNotice(null);
    try {
      const d = await ws.save({ week_start: week, action, base_version: cur.base, items: cur.items.map(toInput) });
      setLocal(fromDetail(week, cur.mine, d));
      setForm(null);
      setLogKey((k) => k + 1);
      setNotice(
        action === "draft"
          ? "Đã lưu nháp."
          : d.schedule.amendment
            ? "Đã gửi điều chỉnh — phần điều chỉnh chờ Cấp Tỉnh duyệt lại; bản đã duyệt vẫn hiệu lực."
            : "Đã gửi duyệt — lịch ở trạng thái Chờ duyệt.",
      );
      onChanged?.();
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(null);
    }
  };

  const copyPrevious = async () => {
    setConfirmCopy(false);
    setBusy("copy");
    setProblem(null);
    setNotice(null);
    try {
      const d = await ws.copyPrevious(week);
      if (cur) setLocal(fromDetail(week, cur.mine, d));
      setLogKey((k) => k + 1);
      setNotice(`Đã sao chép ${d.items.length} mục từ tuần trước (ngày dời +7) thành nháp.`);
      onChanged?.();
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(null);
    }
  };

  const actions = (
    <>
      {editable ? (
        <>
          <button type="button" className="btn btn-o" disabled={!canCopy || busy !== null} onClick={() => (cur?.items.length ? setConfirmCopy(true) : void copyPrevious())}>
            <Icon name="refresh" size={15} /> Sao chép tuần trước
          </button>
          <button type="button" className="btn btn-o" disabled={!canDraft || busy !== null || !cur?.dirty} onClick={() => void persist("draft")}>
            {busy === "draft" ? "Đang lưu…" : "Lưu nháp"}
          </button>
          <button type="button" className="btn btn-p" disabled={!canSubmit || busy !== null} onClick={() => void persist("submit")}>
            <Icon name="send" size={15} /> {busy === "submit" ? "Đang gửi…" : "Gửi duyệt"}
          </button>
        </>
      ) : null}
    </>
  );

  const err = loadError && loadError.week === week ? loadError.problem : null;
  const reason = sched?.last_reason && (status === "changes_requested" || status === "draft") ? sched.last_reason : null;

  return (
    <div>
      {embedded ? (
        <div className="row" style={{ justifyContent: "flex-end", marginBottom: 12 }}>
          {actions}
        </div>
      ) : (
        <PageHeader title={title} actions={actions} />
      )}

      <div className="card ws-bar" role="group" aria-label="Tuần và trạng thái">
        <WeekPicker value={week} onChange={setWeek} />
        {cur ? (
          <>
            <ScheduleTags s={sched} />
            <span className="ws-due">Hạn đăng ký: {vnDeadlineLabel(cur.mine.submit_due_at)}</span>
            {!sched?.submitted_at && cur.mine.late_now && !locked ? <span className="pill pill-warn">Quá hạn — gửi bây giờ sẽ mang nhãn “Đăng ký muộn”</span> : null}
            {cur.dirty ? <span className="ws-dirty">Có thay đổi chưa lưu</span> : null}
          </>
        ) : null}
      </div>

      {err ? <ProblemAlert problem={err} onRetry={() => setReloadKey((k) => k + 1)} /> : null}
      {!cur && !err ? <Loading /> : null}

      {cur ? (
        <>
          {locked ? (
            <div className="alert alert-info" role="note" style={{ marginBottom: 12 }}>
              <Icon name="lock" />
              <span>
                <b>Tuần đã kết thúc — lịch bị khóa, chỉ xem.</b>
              </span>
            </div>
          ) : null}
          {reason ? (
            <div className="alert alert-bad" role="note" aria-label="Yêu cầu chỉnh sửa" style={{ marginBottom: 12 }}>
              <Icon name="alert" />
              <span>
                <b>
                  Cấp Tỉnh yêu cầu chỉnh sửa{sched?.reviewer_display ? ` (${sched.reviewer_display})` : ""}:
                </b>
                <span className="ws-reason">{reason}</span>
                <div className="small">Sửa các mục theo yêu cầu rồi bấm “Gửi duyệt” để gửi lại.</div>
              </span>
            </div>
          ) : null}
          {status === "approved" && editable ? (
            <div className="alert alert-info" role="note" style={{ marginBottom: 12 }}>
              <Icon name="info" />
              <span>
                <b>Lịch đã duyệt.</b> Mọi điều chỉnh phải được Cấp Tỉnh duyệt lại; bản đã duyệt vẫn hiệu lực tới khi duyệt bản mới.
                {view.changedCount ? ` ${view.changedCount} mục đang điều chỉnh (viền xanh) — bấm “Gửi duyệt” để gửi phần điều chỉnh.` : ""}
              </span>
            </div>
          ) : null}
          {sched?.amendment && status === "pending" ? (
            <div className="alert alert-warn" role="note" style={{ marginBottom: 12 }}>
              <Icon name="clock" />
              <span>
                <b>Phần điều chỉnh đang chờ duyệt lại.</b> Bản đã duyệt (phiên bản {sched.approved_version}) vẫn hiệu lực; các mục đánh dấu “điều chỉnh” là phần thay đổi.
              </span>
            </div>
          ) : null}
          {problem ? (
            <div style={{ marginBottom: 12 }}>
              <ProblemAlert
                problem={problem}
                onRetry={
                  problem.code === "extension.schedule_version_conflict" || problem.code === "extension.schedule_state_conflict"
                    ? () => {
                        setProblem(null);
                        setReloadKey((k) => k + 1);
                      }
                    : undefined
                }
              />
            </div>
          ) : null}
          {notice ? (
            <div className="alert alert-info" role="status" style={{ marginBottom: 12 }}>
              <Icon name="check" />
              <span>{notice}</span>
            </div>
          ) : null}

          <div className="ws-layout">
            <div>
              <div className="card" style={{ padding: 0, overflow: "hidden" }}>
                <SessionGrid
                  weekStart={week}
                  items={view.cards}
                  communeName={communeName}
                  coopName={coopName}
                  onAdd={editable ? openNew : undefined}
                  onEdit={editable ? openEdit : undefined}
                  caption="Lịch làm việc theo buổi"
                />
              </div>
              <p className="ws-note">
                {editable ? "Bấm vào ô để thêm mục công tác; trùng thời gian được cảnh báo trước khi lưu (tô vàng)." : "Mục trùng giờ tô vàng."}
                {view.conflicts ? <b style={{ color: "var(--warning)" }}> Có {view.conflicts} mục trùng thời gian — vẫn lưu, gửi được; Cấp Tỉnh sẽ thấy cảnh báo.</b> : null}
              </p>
              {view.removed.length ? (
                <div className="card sec" style={{ marginTop: 10 }}>
                  <h2 style={{ fontSize: 14, marginBottom: 8 }}>Mục bỏ so với bản đã duyệt</h2>
                  <ul className="ws-diff">
                    {view.removed.map((it) => (
                      <li key={it.id} className="d-del">
                        {itemSummary(it, dayColumnLabel)}
                      </li>
                    ))}
                  </ul>
                </div>
              ) : null}
            </div>

            <div className="ws-side">
              {editable ? (
                <ItemForm
                  form={form}
                  setForm={setForm}
                  days={days}
                  communes={communes}
                  level={level}
                  coops={coops}
                  error={formError}
                  onSave={saveItem}
                  onRemove={removeItem}
                  onCancel={() => setForm(null)}
                  onNew={() => openNew(days[0]!, "morning")}
                />
              ) : null}
              <ScheduleLog ws={ws} scheduleId={sched?.id ?? null} refreshKey={logKey} />
            </div>
          </div>
        </>
      ) : null}

      {confirmCopy ? (
        <Dialog
          title="Sao chép tuần trước"
          onClose={() => setConfirmCopy(false)}
          actions={
            <button type="button" className="btn btn-p btn-sm" onClick={() => void copyPrevious()}>
              Sao chép
            </button>
          }
        >
          <p>Thay các mục hiện có của tuần này bằng lịch tuần trước (ngày dời +7)? Thao tác lưu thành nháp mới.</p>
        </Dialog>
      ) : null}
    </div>
  );
}

function ItemForm({
  form,
  setForm,
  days,
  communes,
  level,
  coops,
  error,
  onSave,
  onRemove,
  onCancel,
  onNew,
}: {
  form: FormState | null;
  setForm: (f: FormState | null) => void;
  days: string[];
  communes: CommuneOption[];
  level: "commune" | "province";
  coops: { ref: string; name: string }[];
  error: string | null;
  onSave: () => void;
  onRemove: () => void;
  onCancel: () => void;
  onNew: () => void;
}) {
  if (!form) {
    return (
      <section className="card ws-form" aria-label="Mục công tác">
        <h2>Mục công tác</h2>
        <p className="small mute">Bấm vào ô trên lưới tuần (hoặc nút dưới đây) để thêm mục; bấm một thẻ để sửa.</p>
        <div className="ws-acts">
          <button type="button" className="btn btn-o btn-sm" onClick={onNew}>
            + Thêm mục
          </button>
        </div>
      </section>
    );
  }
  const set = (patch: Partial<FormState>) => setForm({ ...form, ...patch });
  const pii = looksLikePersonalNumber(form.participants) || looksLikePersonalNumber(form.note);
  return (
    <section className="card ws-form" aria-label="Mục công tác">
      <h2>{form.index === null ? "Mục công tác" : "Sửa mục công tác"}</h2>
      <div className="ws-two">
        <div className="field">
          <label htmlFor="ws-day" className="req">
            Ngày
          </label>
          <select id="ws-day" className="select" value={form.day} onChange={(e) => set({ day: e.target.value })}>
            {days.map((d) => (
              <option key={d} value={d}>
                {dayColumnLabel(d)}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="ws-session" className="req">
            Buổi
          </label>
          <select id="ws-session" className="select" value={form.session} onChange={(e) => set({ session: e.target.value as WsSession })}>
            <option value="morning">{SESSION_LABEL.morning}</option>
            <option value="afternoon">{SESSION_LABEL.afternoon}</option>
          </select>
        </div>
        <div className="field">
          <label htmlFor="ws-start">Giờ bắt đầu</label>
          <input id="ws-start" type="time" className="input" value={form.start_time} onChange={(e) => set({ start_time: e.target.value })} />
        </div>
        <div className="field">
          <label htmlFor="ws-end">Giờ kết thúc</label>
          <input id="ws-end" type="time" className="input" value={form.end_time} onChange={(e) => set({ end_time: e.target.value })} />
        </div>
      </div>
      <div className="field">
        <label htmlFor="ws-content" className="req">
          Nội dung công việc
        </label>
        <input id="ws-content" className="input" maxLength={500} value={form.content} onChange={(e) => set({ content: e.target.value })} />
      </div>
      <div className="field">
        <label htmlFor="ws-commune">Địa điểm: xã/phường</label>
        <select id="ws-commune" className="select" value={form.commune_code} onChange={(e) => set({ commune_code: e.target.value, cooperative_ref: "" })}>
          <option value="">{level === "commune" ? "— Không chọn —" : "— Không chọn / ngoài danh sách —"}</option>
          {communes.map((c) => (
            <option key={c.code} value={c.code}>
              {c.name}
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label htmlFor="ws-coop">Địa điểm: HTX</label>
        <select id="ws-coop" className="select" value={form.cooperative_ref} disabled={!form.commune_code} onChange={(e) => set({ cooperative_ref: e.target.value })}>
          <option value="">{form.commune_code ? (coops.length ? "— Không chọn —" : "— Xã chưa có HTX trong danh sách —") : "— Chọn xã trước —"}</option>
          {coops.map((c) => (
            <option key={c.ref} value={c.ref}>
              {c.name}
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label htmlFor="ws-location">Địa điểm cụ thể</label>
        <input id="ws-location" className="input" maxLength={200} placeholder="Ấp, cánh đồng, trụ sở…" value={form.location} onChange={(e) => set({ location: e.target.value })} />
      </div>
      <div className="field">
        <label htmlFor="ws-participants">Thành phần phối hợp</label>
        <input id="ws-participants" className="input" maxLength={500} aria-describedby="ws-participants-hint" value={form.participants} onChange={(e) => set({ participants: e.target.value })} />
        <span id="ws-participants-hint" className="hint">
          Chỉ ghi đơn vị, chức danh hoặc họ tên — không ghi số điện thoại, số CCCD.
        </span>
      </div>
      <div className="field">
        <label htmlFor="ws-note">Ghi chú</label>
        <textarea id="ws-note" className="textarea" rows={2} maxLength={1000} value={form.note} onChange={(e) => set({ note: e.target.value })} />
      </div>
      {error || pii ? (
        <p className="err" role="alert">
          {error ?? "Không ghi số điện thoại, số CCCD vào thành phần phối hợp hoặc ghi chú."}
        </p>
      ) : null}
      <div className="ws-acts">
        <button type="button" className="btn btn-o btn-sm" onClick={onSave}>
          <Icon name="check" size={14} /> Lưu mục
        </button>
        {form.index !== null ? (
          <button type="button" className="btn btn-o btn-sm" onClick={onRemove}>
            <Icon name="trash" size={14} /> Xóa mục
          </button>
        ) : null}
        <button type="button" className="btn btn-o btn-sm" onClick={onCancel}>
          Hủy
        </button>
      </div>
      <p className="small mute">“Lưu mục” chỉ ghi vào lịch đang soạn — bấm “Lưu nháp” hoặc “Gửi duyệt” để lưu lên hệ thống.</p>
    </section>
  );
}

