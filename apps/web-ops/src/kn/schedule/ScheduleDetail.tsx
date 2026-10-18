"use client";

// Màn Detail của một lịch tuần (FN-TI-02.07, mẫu Detail §4.0): quay lại, thanh nút Duyệt / Yêu cầu chỉnh sửa,
// các khối Nhãn – Giá trị, lưới mục công tác, so bản đã duyệt ↔ bản điều chỉnh, các phiên bản, khung Log bên phải.
import { useEffect, useMemo, useState } from "react";
import type { ApiClient } from "@/api/client";
import type { ApiProblem } from "@/api/problem";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Loading } from "@/components/ui";
import { ActionBar, BackLink, DetailLayout, LabelValueBlock, RelatedTabs } from "@/components/patterns/detail";
import { formatDateTime } from "@/lib/format";
import { toApiProblem } from "@/lib/useAsync";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";
import type { WorkScheduleApi } from "./api";
import { diffItems, ITEM_FIELD_LABEL, itemSummary, SESSION_LABEL, type WsDetail, type WsItem } from "./model";
import { ReasonDialog, ScheduleLog, ScheduleTags, SessionGrid, useCommuneNames, usePreloadCoops } from "./parts";
import { dayColumnLabel, vnDeadlineLabel, weekRangeLabel } from "./week";

const VERSION_ACTION: Record<string, string> = { draft_saved: "Lưu nháp", submitted: "Gửi duyệt", amended: "Gửi điều chỉnh" };

export function ScheduleDetail({ ws, api, id, backHref, readOnly = false }: { ws: WorkScheduleApi; api: ApiClient; id: string; backHref: string; readOnly?: boolean }) {
  const [reloadKey, setReloadKey] = useState(0);
  const [data, setData] = useState<{ key: string; detail: WsDetail } | null>(null);
  const [loadError, setLoadError] = useState<{ key: string; problem: ApiProblem } | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [asking, setAsking] = useState(false);
  const [logKey, setLogKey] = useState(0);
  const key = `${id}|${reloadKey}`;

  useEffect(() => {
    let active = true;
    ws.get(id).then(
      (detail) => {
        if (active) setData({ key, detail });
      },
      (e: unknown) => {
        if (active) setLoadError({ key, problem: toApiProblem(e) });
      },
    );
    return () => {
      active = false;
    };
  }, [ws, id, key]);

  const detail = data?.detail ?? null;
  const err = loadError && loadError.key === key ? loadError.problem : null;
  const s = detail?.schedule ?? null;
  useBreadcrumbTail(s ? [{ label: s.owner_display ?? "Lịch tuần" }] : []);

  const codes = useMemo(() => [...new Set([...(s?.commune_codes ?? []), ...(detail?.items.map((i) => i.commune_code ?? "") ?? [])])].filter(Boolean), [s, detail]);
  const communeName = useCommuneNames(api, codes);
  const coopName = usePreloadCoops(api, codes);

  const act = async (fn: () => Promise<WsDetail>, done: string) => {
    setBusy(true);
    setProblem(null);
    setNotice(null);
    try {
      const d = await fn();
      setData({ key, detail: d });
      setNotice(done);
      setLogKey((k) => k + 1);
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
      setAsking(false);
    }
  };

  if (err) {
    return (
      <>
        <BackLink href={backHref}>Lịch công tác</BackLink>
        <ProblemAlert problem={err} onRetry={() => setReloadKey((k) => k + 1)} />
      </>
    );
  }
  if (!detail || !s) {
    return (
      <>
        <BackLink href={backHref}>Lịch công tác</BackLink>
        <Loading />
      </>
    );
  }

  const canAct = !readOnly && (detail.can.approve || detail.can.request_changes);
  const diff = detail.approved_items.length ? diffItems(detail.approved_items, detail.items) : null;
  const sum = (it: WsItem) => itemSummary(it, dayColumnLabel);

  return (
    <>
      <BackLink href={backHref}>Lịch công tác</BackLink>
      <div className="pg-head">
        <h1>
          Lịch công tác tuần {weekRangeLabel(s.week_start)} · {s.owner_display ?? "—"}
        </h1>
      </div>
      {canAct ? (
        <ActionBar>
          {detail.can.approve ? (
            <button type="button" className="btn btn-p btn-sm" disabled={busy} onClick={() => void act(() => ws.approve(s.id, s.version), "Đã duyệt lịch.")}>
              <Icon name="check" size={15} /> Duyệt
            </button>
          ) : null}
          {detail.can.request_changes ? (
            <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={() => setAsking(true)}>
              <Icon name="edit" size={15} /> Yêu cầu chỉnh sửa
            </button>
          ) : null}
        </ActionBar>
      ) : null}
      {problem ? (
        <div style={{ marginBottom: 12 }}>
          <ProblemAlert problem={problem} onRetry={() => setReloadKey((k) => k + 1)} />
        </div>
      ) : null}
      {notice ? (
        <div className="alert alert-info" role="status" style={{ marginBottom: 12 }}>
          <Icon name="check" />
          <span>{notice}</span>
        </div>
      ) : null}
      {detail.warnings.length ? (
        <div className="alert alert-warn" role="note" style={{ marginBottom: 12 }}>
          <Icon name="alert" />
          <span>
            {detail.warnings.map((w, i) => (
              <div key={`${w.code}-${i}`}>{w.code === "time_overlap" ? `${w.item_ids.length} mục trùng thời gian (tô vàng).` : w.code === "late_registration" ? "Đăng ký muộn (gửi sau hạn đăng ký)." : w.message}</div>
            ))}
          </span>
        </div>
      ) : null}
      <DetailLayout
        main={
          <>
            <LabelValueBlock
              title="Thông tin lịch"
              items={[
                { label: "Cán bộ", value: s.owner_display },
                { label: "Cấp", value: s.owner_level === "province" ? "Cấp Tỉnh" : "Cấp Xã" },
                { label: "Xã phụ trách", value: s.commune_codes.map(communeName).join(", ") },
                { label: "Trạng thái", value: <ScheduleTags s={s} /> },
                { label: "Tuần", value: weekRangeLabel(s.week_start) },
                { label: "Phiên bản", value: s.approved_version && s.approved_version !== s.version ? `v${s.version} (đã duyệt: v${s.approved_version})` : `v${s.version}` },
                { label: "Gửi lúc", value: s.submitted_at ? formatDateTime(s.submitted_at) : "Chưa gửi" },
                { label: "Hạn đăng ký", value: vnDeadlineLabel(s.submit_due_at) },
                { label: "Hạn duyệt", value: `${vnDeadlineLabel(s.review_due_at)}${s.review_overdue ? " · quá hạn" : ""}` },
                { label: "Số mục", value: `${s.item_count}${s.conflict_count ? ` (${s.conflict_count} trùng giờ)` : ""}` },
                { label: "Người duyệt", value: s.reviewer_display ? `${s.reviewer_display}${s.reviewed_at ? ` · ${formatDateTime(s.reviewed_at)}` : ""}` : null },
                { label: "Lý do yêu cầu chỉnh sửa", value: s.last_reason ? <span className="ws-reason">{s.last_reason}</span> : null },
              ]}
            />
            <section className="lv-block">
              <h2>Mục công tác{diff ? " (bản điều chỉnh)" : ""}</h2>
              <SessionGrid
                weekStart={s.week_start}
                items={detail.items.map((it) => ({ ...it, checkin: (detail.checkins ?? []).find((c) => c.item_id === it.id) ?? null }))}
                communeName={communeName}
                coopName={coopName}
                caption="Mục công tác của lịch tuần"
              />
            </section>
            {detail.checkin_enabled ? (
              <section className="lv-block" aria-label="Check-in">
                <h2>Check-in tại địa điểm</h2>
                {(detail.checkins ?? []).length === 0 ? (
                  <p className="mute">Chưa có check-in.</p>
                ) : (
                  <table className="table">
                    <thead>
                      <tr>
                        <th>Mục</th>
                        <th>Thời điểm</th>
                        <th>Cách địa điểm</th>
                        <th>Sai số GPS</th>
                        <th>Ghi chú</th>
                      </tr>
                    </thead>
                    <tbody>
                      {(detail.checkins ?? []).map((c) => {
                        const it = detail.items.find((x) => x.id === c.item_id);
                        return (
                          <tr key={c.id}>
                            <td>{it ? sum(it) : c.item_id.slice(0, 8)}</td>
                            <td>{formatDateTime(c.captured_at)}</td>
                            <td>{c.distance_m === null ? "Chưa có tọa độ địa điểm" : `${Math.round(c.distance_m)} m${c.far ? " · xa" : ""}`}</td>
                            <td>{c.accuracy_m === null ? "—" : `${Math.round(c.accuracy_m)} m${c.weak_accuracy ? " · yếu" : ""}`}</td>
                            <td>{[c.mock_location ? "vị trí giả lập" : "", c.point_stored && !c.location ? "tọa độ đã hết hạn lưu" : ""].filter(Boolean).join(", ") || "—"}</td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                )}
                {detail.missed_checkin_count ? <p className="mute">{detail.missed_checkin_count} mục công tác ngoài cơ quan đã qua ngày chưa check-in.</p> : null}
              </section>
            ) : null}
            <section className="lv-block">
              <RelatedTabs
                label="Liên quan"
                tabs={[
                  ...(diff
                    ? [
                        {
                          id: "diff",
                          label: "So với bản đã duyệt",
                          content: (
                            <ul className="ws-diff" aria-label="So với bản đã duyệt">
                              {diff.added.map((it) => (
                                <li key={it.id} className="d-add">
                                  <b>Thêm:</b> {sum(it)}
                                </li>
                              ))}
                              {diff.modified.map((m) => (
                                <li key={m.after.id} className="d-mod">
                                  <b>Sửa:</b> {sum(m.after)}
                                  {m.fields.map((f) => (
                                    <div className="chg" key={f}>
                                      {ITEM_FIELD_LABEL[f]}: {f === "session" ? SESSION_LABEL[m.before.session] : (m.before[f] ?? "—")} → {f === "session" ? SESSION_LABEL[m.after.session] : (m.after[f] ?? "—")}
                                    </div>
                                  ))}
                                </li>
                              ))}
                              {diff.removed.map((it) => (
                                <li key={it.id} className="d-del">
                                  <b>Bỏ:</b> {sum(it)}
                                </li>
                              ))}
                              {!diff.added.length && !diff.modified.length && !diff.removed.length ? <li>Không khác bản đã duyệt.</li> : null}
                            </ul>
                          ),
                        },
                      ]
                    : []),
                  {
                    id: "versions",
                    label: `Phiên bản (${detail.versions.length})`,
                    content: (
                      <ol className="ws-diff">
                        {[...detail.versions].reverse().map((v) => (
                          <li key={v.version}>
                            <b>v{v.version}</b> · {VERSION_ACTION[v.action] ?? v.action} · {v.actor_display ?? "—"} · {formatDateTime(v.at)} · {v.item_count} mục
                            {s.approved_version === v.version ? " · bản đã duyệt" : ""}
                          </li>
                        ))}
                      </ol>
                    ),
                  },
                ]}
              />
            </section>
          </>
        }
        log={<ScheduleLog ws={ws} scheduleId={s.id} refreshKey={logKey} />}
      />
      {asking ? (
        <ReasonDialog
          title="Yêu cầu chỉnh sửa lịch công tác"
          intro={<p>Lịch của {s.owner_display} — tuần {weekRangeLabel(s.week_start)}.</p>}
          busy={busy}
          onClose={() => setAsking(false)}
          onSubmit={(reason) => void act(() => ws.requestChanges(s.id, s.version, reason), "Đã gửi yêu cầu chỉnh sửa — cán bộ nhận thông báo kèm lý do.")}
        />
      ) : null}
    </>
  );
}
