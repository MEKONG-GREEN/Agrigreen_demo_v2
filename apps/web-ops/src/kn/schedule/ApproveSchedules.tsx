"use client";

// Màn Cấp Tỉnh "Lịch công tác · duyệt lịch Cấp Xã" (FN-TI-02.07, Hình 12). Admin: chỉ xem (không có nút duyệt).
// Tab Chờ duyệt / Đã duyệt / Chưa đăng ký / Lịch của tôi; lưới tuần dòng = cán bộ (kèm xã phụ trách), cột = 7 ngày.
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import type { ApiClient } from "@/api/client";
import type { ApiProblem } from "@/api/problem";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { FilterBar, FilterChip, PageHeader, SearchBox, StatusPill } from "@/components/patterns/list";
import { saveBlob } from "@/lib/files";
import { toApiProblem } from "@/lib/useAsync";
import type { WorkScheduleApi } from "./api";
import { placeByDay, STATUS_LABEL, type WsCounts, type WsListItem, type WsStatus, type WsUnregistered } from "./model";
import { ItemCard, ReasonDialog, ScheduleTags, useCommuneNames, usePreloadCoops, WeekPicker } from "./parts";
import { RegisterSchedule } from "./RegisterSchedule";
import { dayColumnLabel, nextWeekStart, weekDays, weekRangeLabel } from "./week";

export const SCHEDULE_ROUTE = "/kn/nhiem-vu/lich-cong-tac";

type View = "list" | "unregistered" | "mine";

interface Loaded {
  key: string;
  list: WsListItem[];
  all: WsListItem[];
  counts: WsCounts;
  unregistered: WsUnregistered[];
}

const SKIP_LABEL: Record<string, string> = {
  self_approval_forbidden: "lịch của chính bạn — cần người duyệt khác",
  "extension.schedule_state_conflict": "lịch không còn ở trạng thái Chờ duyệt",
  "extension.schedule_version_conflict": "lịch vừa được cán bộ sửa — mở lại để xem bản mới",
  "extension.week_locked": "tuần đã kết thúc",
  "extension.schedule_not_found": "không tìm thấy hoặc ngoài phạm vi",
  "extension.not_schedule_approver": "cấp quản lý của bạn không được duyệt",
};

const fold = (s: string) => s.normalize("NFD").replace(/[̀-ͯ]/g, "").replace(/đ/g, "d").replace(/Đ/g, "D").toLowerCase();

export function ApproveSchedules({ ws, api, readOnly = false }: { ws: WorkScheduleApi; api: ApiClient; readOnly?: boolean }) {
  const router = useRouter();
  const [week, setWeek] = useState(() => nextWeekStart());
  const [view, setView] = useState<View>("list");
  const [status, setStatus] = useState<WsStatus | "">("pending");
  const [commune, setCommune] = useState("");
  const [search, setSearch] = useState("");
  const [reloadKey, setReloadKey] = useState(0);
  const [data, setData] = useState<Loaded | null>(null);
  const [loadError, setLoadError] = useState<{ key: string; problem: ApiProblem } | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [dialog, setDialog] = useState<"reason" | "bulk" | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [notice, setNotice] = useState<{ tone: "info" | "warn"; lines: string[] } | null>(null);

  const key = `${week}|${status}|${commune}|${reloadKey}`;
  useEffect(() => {
    let active = true;
    Promise.all([
      ws.listAll({ week_start: week, ...(status ? { status } : {}), ...(commune ? { commune_code: commune } : {}) }),
      ws.listAll({ week_start: week }),
      ws.counts(week),
      ws.unregistered(week, commune || undefined),
    ]).then(
      ([list, all, counts, un]) => {
        if (active) setData({ key, list: list.items, all: all.items, counts, unregistered: un.items });
      },
      (e: unknown) => {
        if (active) setLoadError({ key, problem: toApiProblem(e) });
      },
    );
    return () => {
      active = false;
    };
  }, [ws, week, status, commune, key]);

  const cur = data && data.key === key ? data : null;
  const err = loadError && loadError.key === key ? loadError.problem : null;
  // Giữ dữ liệu cũ khi đang tải lại (không nháy trắng lưới).
  const shown = cur ?? data;

  const communeCodes = useMemo(() => {
    const set = new Set<string>();
    shown?.all.forEach((s) => s.commune_codes.forEach((c) => set.add(c)));
    shown?.unregistered.forEach((u) => u.commune_codes.forEach((c) => set.add(c)));
    shown?.all.forEach((s) => s.items.forEach((i) => i.commune_code && set.add(i.commune_code)));
    if (commune) set.add(commune);
    return [...set].sort();
  }, [shown, commune]);
  const communeName = useCommuneNames(api, communeCodes);
  const coopName = usePreloadCoops(api, communeCodes);

  const rows = useMemo(() => (shown?.list ?? []).filter((s) => !search.trim() || fold(s.owner_display ?? "").includes(fold(search.trim()))), [shown, search]);
  const unregRows = useMemo(() => (shown?.unregistered ?? []).filter((u) => !search.trim() || fold(u.actor_display ?? "").includes(fold(search.trim()))), [shown, search]);
  const pendingRows = rows.filter((r) => r.status === "pending" && !r.locked);
  const selectedRows = rows.filter((r) => selected.has(r.id));

  const changeWeek = (w: string) => {
    setWeek(w);
    setSelected(new Set());
    setNotice(null);
  };
  const changeStatus = (st: WsStatus | "") => {
    setStatus(st);
    setView("list");
    setSelected(new Set());
  };

  const reload = () => {
    setSelected(new Set());
    setReloadKey((k) => k + 1);
  };

  const ownerOf = (id: string) => rows.find((r) => r.id === id)?.owner_display ?? id;

  const runEach = async (label: string, fn: (r: WsListItem) => Promise<unknown>) => {
    setBusy(true);
    setProblem(null);
    setNotice(null);
    const okNames: string[] = [];
    const failed: string[] = [];
    for (const r of selectedRows) {
      try {
        await fn(r);
        okNames.push(r.owner_display ?? r.id);
      } catch (e) {
        const p = toApiProblem(e);
        failed.push(`${r.owner_display ?? r.id}: ${SKIP_LABEL[p.code] ?? p.message}`);
      }
    }
    setBusy(false);
    setDialog(null);
    setNotice({ tone: failed.length ? "warn" : "info", lines: [`${label} ${okNames.length} lịch${okNames.length ? `: ${okNames.join(", ")}` : ""}.`, ...failed.map((f) => `Không thực hiện được — ${f}`)] });
    reload();
  };

  const approveSelected = () => void runEach("Đã duyệt", (r) => ws.approve(r.id, r.version));
  const requestChanges = (reason: string) => void runEach("Đã yêu cầu chỉnh sửa", (r) => ws.requestChanges(r.id, r.version, reason));

  const bulkApprove = async () => {
    setBusy(true);
    setProblem(null);
    setNotice(null);
    try {
      const res = await ws.bulkApprove(pendingRows.map((r) => r.id));
      const lines = [`Đã duyệt hàng loạt ${res.approved.length} lịch.`];
      for (const s of res.skipped) lines.push(`Bỏ qua ${ownerOf(s.id)}: ${SKIP_LABEL[s.code] ?? s.code}`);
      setNotice({ tone: res.skipped.length ? "warn" : "info", lines });
      setDialog(null);
      reload();
    } catch (e) {
      setProblem(toApiProblem(e));
      setDialog(null);
    } finally {
      setBusy(false);
    }
  };

  const exportAs = async (format: "xlsx" | "pdf") => {
    setProblem(null);
    try {
      const f = await ws.exportFile({ week_start: week, format, ...(status && view === "list" ? { status } : {}), ...(commune ? { commune_code: commune } : {}) });
      saveBlob(f.blob, f.name);
    } catch (e) {
      setProblem(toApiProblem(e));
    }
  };

  const toggle = (id: string) => {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setSelected(next);
  };

  const counts = shown?.counts;
  const tabId: string = view === "list" ? (status === "pending" ? "pending" : status === "approved" ? "approved" : "filter") : view;
  const tabs: { id: string; label: string }[] = [
    { id: "pending", label: `Chờ duyệt${counts ? ` (${counts.pending})` : ""}` },
    { id: "approved", label: `Đã duyệt${counts ? ` (${counts.approved})` : ""}` },
    ...(tabId === "filter" ? [{ id: "filter", label: `Lọc: ${status ? STATUS_LABEL[status] : "Tất cả trạng thái"} (${rows.length})` }] : []),
    { id: "unregistered", label: `Chưa đăng ký${counts ? ` (${counts.unregistered})` : ""}` },
    ...(readOnly ? [] : [{ id: "mine", label: "Lịch của tôi" }]),
  ];
  const onTab = (id: string) => {
    setSelected(new Set());
    setNotice(null);
    if (id === "pending" || id === "approved") changeStatus(id);
    else if (id === "unregistered" || id === "mine") setView(id);
  };

  const showActions = !readOnly && view === "list";
  const days = weekDays(week);

  return (
    <div>
      <PageHeader
        title={readOnly ? "Lịch công tác · xem lịch Cấp Xã" : "Lịch công tác · duyệt lịch Cấp Xã"}
        actions={
          view === "mine" ? undefined : (
            <>
              <button type="button" className="btn btn-o" onClick={() => void exportAs("xlsx")}>
                <Icon name="download" size={15} /> Xuất Excel
              </button>
              <button type="button" className="btn btn-o" onClick={() => void exportAs("pdf")}>
                <Icon name="download" size={15} /> Xuất PDF
              </button>
              {showActions ? (
                <>
                  <button type="button" className="btn btn-o" disabled={busy || pendingRows.length === 0} onClick={() => setDialog("bulk")}>
                    <Icon name="approve" size={15} /> Duyệt hàng loạt
                  </button>
                  <button type="button" className="btn btn-o" disabled={busy || selectedRows.length === 0} onClick={() => setDialog("reason")}>
                    <Icon name="edit" size={15} /> Yêu cầu chỉnh sửa
                  </button>
                  <button type="button" className="btn btn-p" disabled={busy || selectedRows.length === 0} onClick={approveSelected}>
                    <Icon name="check" size={15} /> Duyệt lịch đã chọn{selectedRows.length ? ` (${selectedRows.length})` : ""}
                  </button>
                </>
              ) : null}
            </>
          )
        }
      />
      {readOnly ? (
        <div className="alert alert-info" role="note" style={{ marginBottom: 12 }}>
          <Icon name="info" />
          <span>Admin chỉ xem lịch công tác; duyệt và yêu cầu chỉnh sửa do Trưởng phòng Cấp Tỉnh thực hiện.</span>
        </div>
      ) : null}

      {view === "mine" ? null : (
        <FilterBar
          onReset={() => {
            setCommune("");
            setSearch("");
            changeStatus("pending");
            changeWeek(nextWeekStart());
          }}
        >
          <WeekPicker value={week} onChange={changeWeek} />
          <FilterChip
            label="Xã/phường"
            value={commune}
            onChange={(v) => {
              setCommune(v);
              setSelected(new Set());
            }}
            options={[{ value: "", label: "Tất cả" }, ...communeCodes.map((c) => ({ value: c, label: communeName(c) }))]}
          />
          <FilterChip
            label="Trạng thái"
            value={view === "list" ? status : ""}
            defaultValue="pending"
            onChange={(v) => changeStatus(v as WsStatus | "")}
            options={[
              { value: "pending", label: STATUS_LABEL.pending },
              { value: "approved", label: STATUS_LABEL.approved },
              { value: "changes_requested", label: STATUS_LABEL.changes_requested },
              { value: "draft", label: STATUS_LABEL.draft },
              { value: "", label: "Tất cả" },
            ]}
          />
          <SearchBox value={search} onChange={setSearch} placeholder="Tìm cán bộ" />
        </FilterBar>
      )}

      <div className="ws-tabs" role="tablist" aria-label="Lịch công tác">
        {tabs.map((t) => (
          <button key={t.id} type="button" role="tab" aria-selected={t.id === tabId} onClick={() => onTab(t.id)}>
            {t.label}
          </button>
        ))}
      </div>

      {problem ? (
        <div style={{ marginBottom: 12 }}>
          <ProblemAlert problem={problem} />
        </div>
      ) : null}
      {notice ? (
        <div className={`alert ${notice.tone === "warn" ? "alert-warn" : "alert-info"}`} role="status" style={{ marginBottom: 12 }}>
          <Icon name={notice.tone === "warn" ? "alert" : "check"} />
          <span>
            {notice.lines.map((l, i) => (
              <div key={i}>{i === 0 ? <b>{l}</b> : l}</div>
            ))}
          </span>
        </div>
      ) : null}

      {view === "mine" ? (
        <RegisterSchedule ws={ws} api={api} communes={[]} level="province" embedded week={week} onWeekChange={changeWeek} onChanged={() => setReloadKey((k) => k + 1)} />
      ) : err ? (
        <ProblemAlert problem={err} onRetry={reload} />
      ) : !shown ? (
        <Loading />
      ) : (
        <div role="tabpanel" aria-label={tabs.find((t) => t.id === tabId)?.label}>
          <div className="card" style={{ padding: 0, overflow: "hidden" }}>
            <div className="ws-grid-wrap">
              <table className="ws-grid ws-officers">
                <caption className="sr-only">Lịch công tác tuần {weekRangeLabel(week)}</caption>
                <thead>
                  <tr>
                    <th scope="col">Cán bộ</th>
                    {days.map((d) => (
                      <th key={d} scope="col">
                        {dayColumnLabel(d)}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {view === "list" ? (
                    rows.length ? (
                      rows.map((r) => {
                        const cells = placeByDay(r.items, days);
                        const selectable = showActions && r.status === "pending" && !r.locked;
                        return (
                          <tr
                            key={r.id}
                            className={`ws-row-click${selected.has(r.id) ? " is-selected" : ""}`}
                            onClick={(e) => {
                              const el = e.target as HTMLElement;
                              if (el.closest("a, button, input, label")) return;
                              router.push(`${SCHEDULE_ROUTE}/${r.id}`);
                            }}
                            data-testid="ws-row"
                          >
                            <th scope="row" className="ws-off">
                              {selectable ? (
                                <label>
                                  <input type="checkbox" checked={selected.has(r.id)} onChange={() => toggle(r.id)} aria-label={`Chọn lịch của ${r.owner_display ?? ""}`} />
                                  <span>
                                    <b>
                                      <Link href={`${SCHEDULE_ROUTE}/${r.id}`}>{r.owner_display ?? "—"}</Link>
                                    </b>
                                  </span>
                                </label>
                              ) : (
                                <b>
                                  <Link href={`${SCHEDULE_ROUTE}/${r.id}`}>{r.owner_display ?? "—"}</Link>
                                </b>
                              )}
                              <span className="ws-communes">{r.owner_level === "province" ? "Cán bộ Cấp Tỉnh" : r.commune_codes.map(communeName).join(", ")}</span>
                              <span className="ws-tags">
                                <ScheduleTags s={r} compact />
                                {r.conflict_count ? <StatusPill tone="warn">{r.conflict_count} mục trùng giờ</StatusPill> : null}
                              </span>
                            </th>
                            {cells.map((c, i) => (
                              <td key={days[i]}>
                                {c.map((it) => (
                                  <ItemCard key={it.id} item={it} communeName={communeName} coopName={coopName} showSession />
                                ))}
                              </td>
                            ))}
                          </tr>
                        );
                      })
                    ) : (
                      <tr>
                        <td colSpan={8} className="empty">
                          {status === "pending" ? "Không có lịch nào đang chờ duyệt trong tuần này." : "Không có lịch phù hợp bộ lọc."}
                        </td>
                      </tr>
                    )
                  ) : unregRows.length ? (
                    unregRows.map((u) => (
                      <tr key={u.actor_ref} data-testid="ws-unregistered">
                        <th scope="row" className="ws-off">
                          <b>{u.schedule_id ? <Link href={`${SCHEDULE_ROUTE}/${u.schedule_id}`}>{u.actor_display ?? "—"}</Link> : (u.actor_display ?? "—")}</b>
                          <span className="ws-communes">{u.owner_level === "province" ? "Cán bộ Cấp Tỉnh" : u.commune_codes.map(communeName).join(", ")}</span>
                        </th>
                        <td colSpan={7}>
                          <StatusPill tone="bad">Chưa đăng ký</StatusPill> {u.status === "draft" ? <span className="small mute">Mới lưu nháp, chưa gửi duyệt</span> : null}
                        </td>
                      </tr>
                    ))
                  ) : (
                    <tr>
                      <td colSpan={8} className="empty">
                        Mọi cán bộ trong phạm vi đã gửi lịch tuần này.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
          <p className="ws-note">Mục trùng giờ tô vàng. Cán bộ chưa đăng ký sau hạn được nhắc tự động. Bấm một dòng để xem chi tiết và Log.</p>
        </div>
      )}

      {dialog === "reason" ? (
        <ReasonDialog
          title="Yêu cầu chỉnh sửa lịch công tác"
          intro={<p>Gửi yêu cầu chỉnh sửa cho {selectedRows.map((r) => r.owner_display).join(", ")} — tuần {weekRangeLabel(week)}.</p>}
          busy={busy}
          onClose={() => setDialog(null)}
          onSubmit={requestChanges}
        />
      ) : null}
      {dialog === "bulk" ? (
        <Dialog
          title="Duyệt hàng loạt"
          onClose={() => setDialog(null)}
          actions={
            <button type="button" className="btn btn-p btn-sm" disabled={busy} onClick={() => void bulkApprove()}>
              {busy ? "Đang duyệt…" : `Duyệt ${pendingRows.length} lịch`}
            </button>
          }
        >
          <p>
            Duyệt tất cả {pendingRows.length} lịch đang Chờ duyệt của tuần {weekRangeLabel(week)} theo bộ lọc hiện tại? Lịch của chính bạn và lịch đã đổi trạng thái sẽ được bỏ
            qua.
          </p>
        </Dialog>
      ) : null}
    </div>
  );
}
