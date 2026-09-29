"use client";

import { useState } from "react";
import { unwrap } from "@/api/client";
import { collectPages } from "@/api/pagination";
import type { ExtTaskStatus } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { SlaCountdown, useNow } from "@/components/SlaCountdown";
import { Empty, Loading, PageHead } from "@/components/ui";
import { StatusChip } from "@/htx/common";
import { SUPPORT_CATEGORY, TASK_STATUS } from "@/htx/rules";
import { CoopPicker, useCoopFields, useHtxScope } from "@/htx/scope";
import { formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

/**
 * Yêu cầu hỗ trợ của nông dân và nhiệm vụ tự sinh cho cán bộ khuyến nông (HTX US-SUPPORT-01, KN US-TASK-01).
 * Nguồn: GET /v1/extension/tasks với source = htx_app (quản lý HTX: nhiệm vụ của HTX mình; KN: trong phạm vi).
 */
export default function SupportRequestsPage() {
  const { api } = useAuth();
  const scope = useHtxScope();
  const fields = useCoopFields(scope.coopRef);
  const now = useNow();
  const [status, setStatus] = useState<"" | ExtTaskStatus>("");
  const tasks = useAsync(
    () => collectPages((cursor) => unwrap(api.GET("/v1/extension/tasks", { params: { query: { status: status || undefined, cursor, limit: 200 } } })), 1000),
    [api, status],
  );
  const items = (tasks.data ?? []).filter((t) => t.source === "htx_app" && (!scope.coopRef || t.cooperative_ref === scope.coopRef)).sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at));
  const open = items.filter((t) => t.status !== "resolved").length;

  return (
    <>
      <PageHead title="Yêu cầu hỗ trợ" subtitle="Nông dân gửi từ App HTX — mỗi yêu cầu tự sinh một nhiệm vụ cho cán bộ khuyến nông đang trực" />
      <div className="alert alert-info small" style={{ marginBottom: 14 }} role="note">
        <Icon name="info" />
        <span>
          Ảnh và vị trí kèm yêu cầu chỉ mở được trong nhiệm vụ của cán bộ khuyến nông (theo mục đích hỗ trợ hiện trường). Yêu cầu thiếu GPS vẫn được nhận và gắn nhãn “Thiếu vị trí GPS”.
        </span>
      </div>
      <div className="card sec">
        <form className="row" role="search" style={{ marginBottom: 14, alignItems: "flex-end" }} onSubmit={(e) => e.preventDefault()}>
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
          <span className="small mute" role="status" style={{ paddingBottom: 8 }}>
            {items.length} yêu cầu · {open} chưa xử lý xong
          </span>
        </form>
        {tasks.error ? <ProblemAlert problem={tasks.error} onRetry={tasks.reload} /> : null}
        {tasks.loading && !tasks.data ? <Loading /> : null}
        {items.length > 0 ? (
          <div className="tbl-wrap">
            <table className="tbl">
              <caption className="sr-only">Yêu cầu hỗ trợ và nhiệm vụ tự sinh</caption>
              <thead>
                <tr>
                  <th scope="col">Gửi lúc</th>
                  <th scope="col">Sự cố</th>
                  <th scope="col">Nông hộ / HTX</th>
                  <th scope="col">Nhiệm vụ</th>
                  <th scope="col">Cán bộ phụ trách</th>
                  <th scope="col">Hạn xử lý</th>
                </tr>
              </thead>
              <tbody>
                {items.map((t) => (
                  <tr key={t.task_id}>
                    <td>{formatDateTime(t.created_at)}</td>
                    <td style={{ maxWidth: 360 }}>
                      <b>{SUPPORT_CATEGORY[t.category] ?? t.category}</b>
                      {t.priority === "urgent" ? <span className="chip c-bad" style={{ marginLeft: 6 }}>Khẩn</span> : null}
                      <div className="small">{t.description}</div>
                      {t.last_note ? <div className="mute small">Ghi chú: {t.last_note}</div> : null}
                    </td>
                    <td>
                      {scope.coopRef ? fields.householdLabel(t.household_ref) : (t.cooperative_name ?? scope.coopName(t.cooperative_ref))}
                      <div className="mute small">{t.commune_name ?? t.commune_code}</div>
                    </td>
                    <td>
                      <StatusChip map={TASK_STATUS} value={t.status} />
                    </td>
                    <td>{t.assignee_name ?? (t.assignee_ref ? "Đã giao" : <span className="mute">Chưa giao — chờ quản trị tỉnh</span>)}</td>
                    <td>{t.status === "resolved" ? `Xong ${formatDateTime(t.resolved_at)}` : <SlaCountdown dueAt={t.due_at} now={now} />}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
        {!tasks.loading && !tasks.error && items.length === 0 ? <Empty>Chưa có yêu cầu hỗ trợ.</Empty> : null}
      </div>
    </>
  );
}
