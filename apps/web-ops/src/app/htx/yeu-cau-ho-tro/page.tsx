"use client";

import Link from "next/link";
import { useState } from "react";
import { unwrap } from "@/api/client";
import { collectPages } from "@/api/pagination";
import type { ExtTaskStatus, SupportCategory } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { SlaCountdown, useNow } from "@/components/SlaCountdown";
import { Empty, Loading, PageHead } from "@/components/ui";
import { StatusChip } from "@/htx/common";
import { SUPPORT_CATEGORY, TASK_STATUS } from "@/htx/rules";
import { CoopPicker, useCoopFields, useHtxScope } from "@/htx/scope";
import { ASSIGNMENT_RULE, gpsLabel, photoLabel, summarizeSupport, SUPPORT_URGENCY } from "@/htx/support";
import { formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

const CATEGORIES: SupportCategory[] = ["dich_hai", "benh_lua", "thoi_tiet", "ky_thuat", "may_moc", "khac"];

/**
 * Yêu cầu hỗ trợ của nông dân và nhiệm vụ tự sinh cho cán bộ khuyến nông (HTX US-SUPPORT-01, KN US-TASK-01/03).
 * Nguồn: GET /v1/extension/support-requests (quản lý HTX: HTX của mình; KN: trong phạm vi) — kèm ảnh (mã tệp),
 * trạng thái GPS và nhiệm vụ hiện hành (trạng thái, cán bộ, ghi chú cuối, hạn).
 */
export default function SupportRequestsPage() {
  const { api } = useAuth();
  const scope = useHtxScope();
  const fields = useCoopFields(scope.coopRef);
  const now = useNow();
  const [status, setStatus] = useState<"" | ExtTaskStatus>("");
  const [category, setCategory] = useState<"" | SupportCategory>("");
  const [urgency, setUrgency] = useState<"" | "binh_thuong" | "khan">("");
  const [gps, setGps] = useState<"" | "true" | "false">("");
  const list = useAsync(
    () =>
      collectPages(
        (cursor) =>
          unwrap(
            api.GET("/v1/extension/support-requests", {
              params: {
                query: {
                  task_status: status || undefined,
                  category: category || undefined,
                  urgency: urgency || undefined,
                  gps_missing: gps === "" ? undefined : gps === "true",
                  cooperative_ref: scope.coopRef || undefined,
                  cursor,
                  limit: 200,
                },
              },
            }),
          ),
        1000,
      ),
    [api, status, category, urgency, gps, scope.coopRef],
  );
  const items = list.data ?? [];
  const sum = summarizeSupport(items);

  return (
    <>
      <PageHead title="Yêu cầu hỗ trợ" subtitle="Nông dân gửi từ App HTX — mỗi yêu cầu tự sinh một nhiệm vụ cho cán bộ khuyến nông đang trực" />
      <div className="alert alert-info small" style={{ marginBottom: 14 }} role="note">
        <Icon name="info" />
        <span>
          Danh sách chỉ hiện số ảnh, mã tệp ảnh và trạng thái GPS; toạ độ lô ruộng chỉ mở trong nhiệm vụ của cán bộ khuyến nông (theo mục đích hỗ trợ hiện trường). Yêu cầu thiếu GPS vẫn được nhận và gắn nhãn “Thiếu vị trí GPS”.
        </span>
      </div>
      <div className="card sec">
        <form className="row" role="search" aria-label="Lọc yêu cầu hỗ trợ" style={{ marginBottom: 14, alignItems: "flex-end" }} onSubmit={(e) => e.preventDefault()}>
          <CoopPicker scope={scope} />
          <div className="field">
            <label htmlFor="sr-status">Trạng thái nhiệm vụ</label>
            <select id="sr-status" className="select" value={status} onChange={(e) => setStatus(e.target.value as typeof status)}>
              <option value="">Tất cả</option>
              {Object.entries(TASK_STATUS).map(([k, v]) => (
                <option key={k} value={k}>
                  {v.text}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="sr-category">Loại sự cố</label>
            <select id="sr-category" className="select" value={category} onChange={(e) => setCategory(e.target.value as typeof category)}>
              <option value="">Tất cả</option>
              {CATEGORIES.map((c) => (
                <option key={c} value={c}>
                  {SUPPORT_CATEGORY[c] ?? c}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="sr-urgency">Mức khẩn</label>
            <select id="sr-urgency" className="select" value={urgency} onChange={(e) => setUrgency(e.target.value as typeof urgency)}>
              <option value="">Tất cả</option>
              {Object.entries(SUPPORT_URGENCY).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="sr-gps">Vị trí GPS</label>
            <select id="sr-gps" className="select" value={gps} onChange={(e) => setGps(e.target.value as typeof gps)}>
              <option value="">Tất cả</option>
              <option value="true">Thiếu vị trí GPS</option>
              <option value="false">Có vị trí GPS</option>
            </select>
          </div>
          <span className="small mute" role="status" style={{ paddingBottom: 8 }}>
            {sum.total} yêu cầu · {sum.open} chưa xử lý xong
            {sum.urgentOpen ? ` · ${sum.urgentOpen} khẩn` : ""}
            {sum.unassigned ? ` · ${sum.unassigned} chưa có cán bộ` : ""}
          </span>
        </form>
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        {list.loading && !list.data ? <Loading /> : null}
        {items.length > 0 ? (
          <div className="tbl-wrap">
            <table className="tbl">
              <caption className="sr-only">Yêu cầu hỗ trợ và nhiệm vụ tự sinh</caption>
              <thead>
                <tr>
                  <th scope="col">Gửi lúc</th>
                  <th scope="col">Sự cố</th>
                  <th scope="col">Ảnh & vị trí</th>
                  <th scope="col">Nông hộ / HTX</th>
                  <th scope="col">Nhiệm vụ</th>
                  <th scope="col">Cán bộ phụ trách</th>
                  <th scope="col">Hạn xử lý</th>
                </tr>
              </thead>
              <tbody>
                {items.map((r) => {
                  const t = r.task;
                  const g = gpsLabel(r);
                  return (
                    <tr key={r.id}>
                      <td>
                        {formatDateTime(r.received_at)}
                        {r.event_time !== r.received_at ? <div className="mute small">Bấm gửi {formatDateTime(r.event_time)}</div> : null}
                      </td>
                      <td style={{ maxWidth: 360 }}>
                        <b>{SUPPORT_CATEGORY[r.category] ?? r.category}</b>
                        {r.urgency === "khan" ? (
                          <span className="chip c-bad" style={{ marginLeft: 6 }}>
                            Khẩn
                          </span>
                        ) : null}
                        <div className="small">{r.description}</div>
                        {t.last_note ? <div className="mute small">Ghi chú: {t.last_note}</div> : null}
                      </td>
                      <td>
                        {r.photo_count > 0 ? (
                          <details>
                            <summary>{photoLabel(r.photo_count)}</summary>
                            <ul className="small mute" aria-label="Mã tệp ảnh" style={{ margin: 0, paddingLeft: 16 }}>
                              {r.photo_refs.map((p) => (
                                <li key={p}>
                                  <code>{p.slice(-12)}</code>
                                </li>
                              ))}
                            </ul>
                          </details>
                        ) : (
                          <span className="mute">{photoLabel(0)}</span>
                        )}
                        <div>
                          <span className={`chip ${g.missing ? "c-warn" : "c-ok"}`}>{g.text}</span>
                        </div>
                      </td>
                      <td>
                        {scope.coopRef ? fields.householdLabel(r.household_ref) : (t.cooperative_name ?? scope.coopName(r.cooperative_ref))}
                        <div className="mute small">{t.commune_name ?? r.commune_code}</div>
                      </td>
                      <td>
                        <StatusChip map={TASK_STATUS} value={t.status} />
                        <div className="small">
                          <Link href={`/nhiem-vu?task=${encodeURIComponent(t.task_id)}`}>Mở nhiệm vụ</Link>
                        </div>
                      </td>
                      <td>
                        {t.assignee_name ?? (t.assignee_ref ? "Đã giao" : <span className="mute">Chưa giao — chờ quản trị tỉnh</span>)}
                        <div className="mute small">{ASSIGNMENT_RULE[r.assignment_rule] ?? r.assignment_rule}</div>
                      </td>
                      <td>{t.status === "resolved" ? `Xong ${formatDateTime(t.resolved_at)}` : <SlaCountdown dueAt={t.due_at} now={now} />}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : null}
        {!list.loading && !list.error && items.length === 0 ? <Empty>Chưa có yêu cầu hỗ trợ.</Empty> : null}
      </div>
    </>
  );
}
