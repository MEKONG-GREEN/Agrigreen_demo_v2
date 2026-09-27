"use client";

import Link from "next/link";
import { useState } from "react";
import { unwrap } from "@/api/client";
import { usePagedList } from "@/api/pagination";
import type { TaskKind, TaskPriority, TaskStatus } from "@/api/pending/extension";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { SlaCountdown, useNow } from "@/components/SlaCountdown";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";

const KIND: Record<TaskKind, string> = {
  hoi_dap: "Hỏi đáp kỹ thuật",
  ho_tro_ky_thuat: "Hỗ trợ kỹ thuật",
  phan_anh_khan: "Phản ánh khẩn",
  hau_kiem: "Hậu kiểm nhật ký",
};
const STATUS: Record<TaskStatus, { text: string; cls: string }> = {
  open: { text: "Mới", cls: "c-info" },
  in_progress: { text: "Đang xử lý", cls: "c-violet" },
  resolved: { text: "Đã xử lý", cls: "c-ok" },
  closed: { text: "Đã đóng", cls: "c-mute" },
};
const PRIORITY: Record<TaskPriority, string> = { low: "Thấp", normal: "Bình thường", high: "Cao", urgent: "Khẩn" };

export default function TasksPage() {
  const { api } = useAuth();
  const now = useNow();
  const [status, setStatus] = useState<"" | TaskStatus>("");
  const [kind, setKind] = useState<"" | TaskKind>("");
  const [overdue, setOverdue] = useState(false);

  const list = usePagedList(
    (cursor) =>
      unwrap(
        api.GET("/v1/tasks", {
          params: { query: { status: status || undefined, kind: kind || undefined, overdue: overdue || undefined, cursor, limit: 20 } },
        }),
      ),
    [api, status, kind, overdue],
  );

  return (
    <>
      <PageHead title="Nhiệm vụ hỗ trợ" subtitle="Hỏi đáp từ HTX, phản ánh khẩn, hậu kiểm — sắp theo hạn SLA gần nhất" />
      <div className="card sec">
        <div className="row" style={{ marginBottom: 14 }}>
          <div className="field">
            <label htmlFor="t-status">Trạng thái</label>
            <select id="t-status" className="select" value={status} onChange={(e) => setStatus(e.target.value as "" | TaskStatus)}>
              <option value="">Tất cả</option>
              {Object.entries(STATUS).map(([k, v]) => (
                <option key={k} value={k}>
                  {v.text}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="t-kind">Loại nhiệm vụ</label>
            <select id="t-kind" className="select" value={kind} onChange={(e) => setKind(e.target.value as "" | TaskKind)}>
              <option value="">Tất cả</option>
              {Object.entries(KIND).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </div>
          <label className="row" style={{ alignSelf: "flex-end", paddingBottom: 8 }}>
            <input type="checkbox" checked={overdue} onChange={(e) => setOverdue(e.target.checked)} />
            Chỉ nhiệm vụ quá hạn
          </label>
        </div>
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">Danh sách nhiệm vụ hỗ trợ</caption>
            <thead>
              <tr>
                <th scope="col">Nhiệm vụ</th>
                <th scope="col">Loại</th>
                <th scope="col">HTX / địa bàn</th>
                <th scope="col">Người xử lý</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">SLA</th>
              </tr>
            </thead>
            <tbody>
              {list.items.map((t) => (
                <tr key={t.id}>
                  <td>
                    <div style={{ fontWeight: 600 }}>{t.title}</div>
                    <div className="mute small">
                      {t.code} · Ưu tiên {PRIORITY[t.priority]} · Tạo {formatDateTime(t.created_at)}
                    </div>
                  </td>
                  <td>{t.kind === "phan_anh_khan" ? <span className="chip c-bad">{KIND[t.kind]}</span> : KIND[t.kind]}</td>
                  <td>
                    {t.cooperative_id ? <Link href={`/htx/${t.cooperative_id}`}>{t.cooperative_name}</Link> : "—"}
                    <div className="mute small">
                      {t.commune_name}, {t.province_name}
                    </div>
                  </td>
                  <td>{t.assignee ?? <span className="chip c-warn">Chưa phân công</span>}</td>
                  <td>
                    <span className={`chip ${STATUS[t.status].cls}`}>{STATUS[t.status].text}</span>
                  </td>
                  <td>
                    <SlaCountdown dueAt={t.due_at} resolvedAt={t.resolved_at} now={now} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {list.loading ? <Loading /> : null}
        {!list.loading && !list.error && list.items.length === 0 ? <Empty>Không có nhiệm vụ phù hợp.</Empty> : null}
        <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
      </div>
    </>
  );
}
