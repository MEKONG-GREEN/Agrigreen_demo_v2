"use client";

// Thành phần dùng chung của hai màn Lịch công tác: chọn tuần, thẻ mục công tác, lưới Sáng/Chiều × 7 ngày,
// hộp thoại lý do, khung Log, tra tên xã/HTX.
import { useEffect, useMemo, useState, type ReactNode } from "react";
import type { ApiClient } from "@/api/client";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { LogPanel, type LogEntry } from "@/components/patterns/detail";
import { FilterChip, StatusPill } from "@/components/patterns/list";
import type { WorkScheduleApi } from "./api";
import { logEventLabel, placeBySession, SESSIONS, SESSION_LABEL, statusLabel, STATUS_TONE, timeRangeLabel, type KnLogEntry, type WsItemInput, type WsSchedule, type WsSession } from "./model";
import { addDays, currentWeekStart, dayColumnLabel, weekDays, weekRangeLabel } from "./week";

// ---------------------------------------------------------------------------
// Tra tên xã, HTX (masterdata / registry) — có bộ nhớ đệm trong tab
// ---------------------------------------------------------------------------
const COMMUNE_CACHE = new Map<string, string>();
const COOP_CACHE = new Map<string, { ref: string; name: string }[]>();
const COOP_NAMES = new Map<string, string>();

/** Tên xã theo mã: lấy từ ngữ cảnh phiên trước, thiếu thì tra GET /v1/masterdata/admin-units/{code}. */
export function useCommuneNames(api: ApiClient, codes: readonly string[], known: readonly { code: string; name: string }[] = []): (code: string | null | undefined) => string {
  const [, setTick] = useState(0);
  const knownKey = known.map((k) => k.code).join(",");
  const key = [...new Set(codes)].sort().join(",");
  useEffect(() => {
    const own = new Set(knownKey.split(","));
    const missing = key.split(",").filter((c) => c && !own.has(c) && !COMMUNE_CACHE.has(c));
    if (!missing.length) return;
    let active = true;
    void Promise.all(
      missing.map(async (code) => {
        try {
          const res = await api.GET("/v1/masterdata/admin-units/{code}", { params: { path: { code } } });
          COMMUNE_CACHE.set(code, res.data?.name ?? code);
        } catch {
          COMMUNE_CACHE.set(code, code);
        }
      }),
    ).then(() => {
      if (active) setTick((t) => t + 1);
    });
    return () => {
      active = false;
    };
  }, [api, key, knownKey]);
  return (code) => (code ? (known.find((k) => k.code === code)?.name ?? COMMUNE_CACHE.get(code) ?? code) : "");
}

/** HTX của một xã (GET /v1/cooperatives?admin_unit=…) cho ô "Địa điểm: HTX". */
export function useCoops(api: ApiClient, communeCode: string | null | undefined): { ref: string; name: string }[] {
  const [, setTick] = useState(0);
  useEffect(() => {
    if (!communeCode || COOP_CACHE.has(communeCode)) return;
    let active = true;
    api
      .GET("/v1/cooperatives", { params: { query: { admin_unit: communeCode, limit: 200 } } })
      .then((res) => {
        const list = (res.data?.items ?? []).map((c) => ({ ref: c.ref, name: c.name }));
        list.forEach((c) => COOP_NAMES.set(c.ref, c.name));
        COOP_CACHE.set(communeCode, list);
      })
      .catch(() => COOP_CACHE.set(communeCode, []))
      .finally(() => {
        if (active) setTick((t) => t + 1);
      });
    return () => {
      active = false;
    };
  }, [api, communeCode]);
  return communeCode ? (COOP_CACHE.get(communeCode) ?? []) : [];
}

/** Nạp sẵn tên HTX của các xã xuất hiện trên lưới (thẻ mục hiện tên HTX). */
export function usePreloadCoops(api: ApiClient, communeCodes: readonly string[]): (ref: string | null | undefined) => string | null {
  const [, setTick] = useState(0);
  const key = [...new Set(communeCodes.filter(Boolean))].sort().join(",");
  useEffect(() => {
    const missing = key.split(",").filter((c) => c && !COOP_CACHE.has(c));
    if (!missing.length) return;
    let active = true;
    void Promise.all(
      missing.map((code) =>
        api
          .GET("/v1/cooperatives", { params: { query: { admin_unit: code, limit: 200 } } })
          .then((res) => {
            const list = (res.data?.items ?? []).map((c) => ({ ref: c.ref, name: c.name }));
            list.forEach((c) => COOP_NAMES.set(c.ref, c.name));
            COOP_CACHE.set(code, list);
          })
          .catch(() => COOP_CACHE.set(code, [])),
      ),
    ).then(() => {
      if (active) setTick((t) => t + 1);
    });
    return () => {
      active = false;
    };
  }, [api, key]);
  return (ref) => (ref ? (COOP_NAMES.get(ref) ?? null) : null);
}

// ---------------------------------------------------------------------------
// Chọn tuần (Thứ Hai đầu tuần, giờ Việt Nam)
// ---------------------------------------------------------------------------
export function WeekPicker({ value, onChange }: { value: string; onChange: (week: string) => void }) {
  const [now] = useState(() => Date.now());
  const cur = currentWeekStart(now);
  const options = useMemo(() => {
    const weeks = Array.from({ length: 13 }, (_, i) => addDays(cur, (i - 8) * 7));
    if (!weeks.includes(value)) weeks.push(value);
    weeks.sort();
    return weeks.map((w) => ({
      value: w,
      label: `${weekRangeLabel(w)}${w === cur ? " (tuần này)" : w === addDays(cur, 7) ? " (tuần sau)" : ""}`,
    }));
  }, [cur, value]);
  return (
    <span className="ws-week">
      <button type="button" className="ws-week-nav" aria-label="Tuần trước" onClick={() => onChange(addDays(value, -7))}>
        <Icon name="chevronLeft" size={15} />
      </button>
      <FilterChip label="Tuần" value={value} defaultValue="" options={options} onChange={onChange} />
      <button type="button" className="ws-week-nav" aria-label="Tuần sau" onClick={() => onChange(addDays(value, 7))}>
        <Icon name="chevronRight" size={15} />
      </button>
    </span>
  );
}

// ---------------------------------------------------------------------------
// Nhãn trạng thái
// ---------------------------------------------------------------------------
export function ScheduleTags({ s, compact = false }: { s: Pick<WsSchedule, "status" | "amendment" | "late" | "locked" | "review_overdue"> | null; compact?: boolean }) {
  if (!s) return <StatusPill tone="neutral">Chưa đăng ký</StatusPill>;
  return (
    <>
      <StatusPill tone={STATUS_TONE[s.status]}>{statusLabel(s)}</StatusPill>
      {s.late ? <StatusPill tone="bad">Đăng ký muộn</StatusPill> : null}
      {s.locked && !compact ? (
        <StatusPill tone="neutral">
          <Icon name="lock" size={12} /> Đã khóa
        </StatusPill>
      ) : null}
      {s.review_overdue && !compact ? <StatusPill tone="warn">Quá hạn duyệt</StatusPill> : null}
    </>
  );
}

// ---------------------------------------------------------------------------
// Thẻ mục công tác
// ---------------------------------------------------------------------------
export type CardItem = WsItemInput & { conflict?: boolean; changed?: boolean; removed?: boolean };

export function ItemCard({
  item,
  communeName,
  coopName,
  onClick,
  showSession = false,
}: {
  item: CardItem;
  communeName: (c: string | null | undefined) => string;
  coopName: (ref: string | null | undefined) => string | null;
  onClick?: () => void;
  showSession?: boolean;
}) {
  const t = timeRangeLabel(item);
  const place = [communeName(item.commune_code), coopName(item.cooperative_ref), item.location].filter(Boolean).join(" · ");
  const cls = `ws-card${item.conflict ? " is-conflict" : ""}${item.changed ? " is-changed" : ""}${item.removed ? " is-removed" : ""}`;
  const body = (
    <>
      {showSession || t ? (
        <span className="ws-time">
          {showSession ? `${SESSION_LABEL[item.session]}${t ? " " : ""}` : ""}
          {t}
        </span>
      ) : null}
      {item.content}
      {item.conflict ? <span className="ws-flag f-conflict">trùng giờ</span> : null}
      {item.changed ? <span className="ws-flag f-changed">điều chỉnh</span> : null}
      {place ? <span className="ws-place">{place}</span> : null}
    </>
  );
  const title = `${item.content}${item.conflict ? " (trùng giờ)" : ""}${item.changed ? " (điều chỉnh, chờ duyệt lại)" : ""}`;
  return onClick ? (
    <button type="button" className={cls} onClick={onClick} title={title} data-testid="ws-item">
      {body}
    </button>
  ) : (
    <div className={cls} title={title} data-testid="ws-item">
      {body}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Lưới Sáng/Chiều × 7 ngày (màn Cấp Xã, Lịch của tôi, Detail)
// ---------------------------------------------------------------------------
export function SessionGrid({
  weekStart,
  items,
  communeName,
  coopName,
  onAdd,
  onEdit,
  caption = "Lịch tuần theo buổi",
}: {
  weekStart: string;
  items: CardItem[];
  communeName: (c: string | null | undefined) => string;
  coopName: (ref: string | null | undefined) => string | null;
  onAdd?: (day: string, session: WsSession) => void;
  onEdit?: (index: number) => void;
  caption?: string;
}) {
  const days = weekDays(weekStart);
  const indexed = items.map((it, index) => ({ ...it, index }));
  const grid = placeBySession(indexed, days);
  return (
    <div className="ws-grid-wrap">
      <table className="ws-grid">
        <caption className="sr-only">{caption}</caption>
        <thead>
          <tr>
            <th scope="col">Buổi</th>
            {days.map((d) => (
              <th key={d} scope="col">
                {dayColumnLabel(d)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {SESSIONS.map((s) => (
            <tr key={s}>
              <th scope="row">{SESSION_LABEL[s]}</th>
              {days.map((d, di) => (
                <td
                  key={d}
                  className={onAdd ? "ws-cell-add" : undefined}
                  onClick={onAdd ? (e) => e.target === e.currentTarget && onAdd(d, s) : undefined}
                  data-day={d}
                  data-session={s}
                >
                  {grid[s][di]!.map((it) => (
                    <ItemCard key={it.id ?? it.index} item={it} communeName={communeName} coopName={coopName} onClick={onEdit ? () => onEdit(it.index) : undefined} />
                  ))}
                  {onAdd ? (
                    <button type="button" className="ws-add" aria-label={`Thêm mục ${dayColumnLabel(d)} ${SESSION_LABEL[s]}`} onClick={() => onAdd(d, s)}>
                      +
                    </button>
                  ) : null}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Hộp thoại lý do "Yêu cầu chỉnh sửa" (bắt buộc ≥ 10 ký tự)
// ---------------------------------------------------------------------------
export const MIN_REASON = 10;

export function ReasonDialog({ title, intro, busy, onClose, onSubmit }: { title: string; intro?: ReactNode; busy?: boolean; onClose: () => void; onSubmit: (reason: string) => void }) {
  const [reason, setReason] = useState("");
  const [touched, setTouched] = useState(false);
  const len = reason.trim().length;
  const tooShort = len < MIN_REASON;
  return (
    <Dialog
      title={title}
      onClose={onClose}
      actions={
        <button
          type="button"
          className="btn btn-p btn-sm"
          disabled={busy}
          onClick={() => {
            setTouched(true);
            if (!tooShort) onSubmit(reason.trim());
          }}
        >
          {busy ? "Đang gửi…" : "Gửi yêu cầu"}
        </button>
      }
    >
      {intro ? <div style={{ marginBottom: 10 }}>{intro}</div> : null}
      <div className="field">
        <label htmlFor="ws-reason">Lý do yêu cầu chỉnh sửa (bắt buộc, tối thiểu {MIN_REASON} ký tự)</label>
        <textarea
          id="ws-reason"
          className="textarea"
          rows={4}
          maxLength={1000}
          value={reason}
          aria-invalid={touched && tooShort}
          aria-describedby="ws-reason-help"
          onChange={(e) => setReason(e.target.value)}
        />
        <span id="ws-reason-help" className={touched && tooShort ? "err" : "small mute"}>
          {touched && tooShort ? `Lý do cần ít nhất ${MIN_REASON} ký tự (hiện ${len}).` : `${len}/1000 ký tự. Cán bộ nhận thông báo kèm lý do này.`}
        </span>
      </div>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Khung Log (Detail) — GET …/{id}/log, mới nhất trước, "Xem thêm" theo cursor
// ---------------------------------------------------------------------------
export function toLogEntry(e: KnLogEntry): LogEntry {
  return { event: logEventLabel(e.event), actor_display: e.actor_display ?? "—", at: e.at, changes: e.changes, reason: e.reason };
}

export function ScheduleLog({ ws, scheduleId, refreshKey }: { ws: WorkScheduleApi; scheduleId: string | null; refreshKey?: unknown }) {
  const [rows, setRows] = useState<{ id: string | null; items: KnLogEntry[]; next: number | null }>({ id: null, items: [], next: null });
  const [loadingMore, setLoadingMore] = useState(false);
  useEffect(() => {
    if (!scheduleId) return;
    let active = true;
    ws.log(scheduleId).then(
      (p) => {
        if (active) setRows({ id: scheduleId, items: p.items, next: p.next_cursor });
      },
      () => {
        if (active) setRows({ id: scheduleId, items: [], next: null });
      },
    );
    return () => {
      active = false;
    };
  }, [ws, scheduleId, refreshKey]);
  const items = scheduleId && rows.id === scheduleId ? rows.items : [];
  const more = async () => {
    if (!scheduleId || rows.next === null) return;
    setLoadingMore(true);
    try {
      const p = await ws.log(scheduleId, rows.next);
      setRows((r) => ({ id: r.id, items: [...r.items, ...p.items], next: p.next_cursor }));
    } finally {
      setLoadingMore(false);
    }
  };
  return (
    <div>
      <LogPanel entries={items.map(toLogEntry)} empty={scheduleId ? "Chưa có sự việc nào." : "Lịch tuần chưa được tạo."} />
      {scheduleId && rows.id === scheduleId && rows.next !== null ? (
        <button type="button" className="btn btn-o btn-sm" style={{ marginTop: 8 }} onClick={() => void more()} disabled={loadingMore}>
          {loadingMore ? "Đang tải…" : "Xem thêm"}
        </button>
      ) : null}
    </div>
  );
}
