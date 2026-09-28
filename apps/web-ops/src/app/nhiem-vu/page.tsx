"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { unwrap } from "@/api/client";
import { usePagedList } from "@/api/pagination";
import type { ExtTaskPriority, ExtTaskStatus } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { SlaCountdown, useNow } from "@/components/SlaCountdown";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDecimal } from "@/lib/decimal";
import { formatDateTime } from "@/lib/format";
import { useAsync } from "@/lib/useAsync";

const CATEGORY: Record<string, string> = {
  sau_benh: "Sâu bệnh",
  ky_thuat: "Kỹ thuật canh tác",
  co_gioi: "Cơ giới hoá",
  khac: "Khác",
};
const STATUS: Record<ExtTaskStatus, { text: string; cls: string }> = {
  new: { text: "Mới", cls: "c-info" },
  assigned: { text: "Đã phân công", cls: "c-warn" },
  accepted: { text: "Đã tiếp nhận", cls: "c-violet" },
  in_progress: { text: "Đang xử lý", cls: "c-violet" },
  resolved: { text: "Đã hoàn thành", cls: "c-ok" },
};
const PRIORITY: Record<ExtTaskPriority, string> = { low: "Thấp", normal: "Bình thường", high: "Cao", urgent: "Khẩn" };
const SOURCE: Record<string, string> = { officer: "Cán bộ KN", coop_manager: "Quản lý HTX", htx_app: "App HTX" };

const ACTION: Record<string, string> = {
  created: "Tạo",
  assigned: "Phân công",
  accepted: "Tiếp nhận",
  progress: "Cập nhật tiến độ",
  resolved: "Hoàn thành",
  escalated: "Leo thang lên tỉnh",
  reopened: "Mở lại",
};

/** Chi tiết một nhiệm vụ (mở từ thông báo: /nhiem-vu?task=…) — GET /v1/extension/tasks/{id}. */
function TaskDetail({ id, now, onClose }: { id: string; now: number; onClose: () => void }) {
  const { api } = useAuth();
  const d = useAsync(() => unwrap(api.GET("/v1/extension/tasks/{id}", { params: { path: { id } } })), [api, id]);
  const t = d.data?.task;
  return (
    <section className="card sec hl" aria-labelledby="task-detail" style={{ marginBottom: 16 }} data-testid="task-detail">
      <div className="sec-h">
        <h2 id="task-detail">Nhiệm vụ được nhắc trong thông báo</h2>
        <button type="button" className="btn btn-o btn-sm" onClick={onClose}>
          Đóng
        </button>
      </div>
      {d.error ? <ProblemAlert problem={d.error} onRetry={d.reload} /> : null}
      {d.loading && !d.data ? <Loading /> : null}
      {t ? (
        <div className="grid2" style={{ alignItems: "start" }}>
          <dl className="kv">
            <dt>Nội dung</dt>
            <dd>{t.description}</dd>
            <dt>HTX / địa bàn</dt>
            <dd>
              {t.cooperative_name ?? "—"} · {t.commune_name ?? t.commune_code}
            </dd>
            <dt>Phân công</dt>
            <dd>{t.assignee_name ?? (t.assignee_ref ? "Đã phân công" : "Chưa phân công")}</dd>
            <dt>Trạng thái</dt>
            <dd>
              <span className={`chip ${STATUS[t.status].cls}`}>{STATUS[t.status].text}</span>
            </dd>
            <dt>SLA</dt>
            <dd>
              <SlaCountdown dueAt={t.due_at} resolvedAt={t.resolved_at} now={now} />
            </dd>
          </dl>
          <div>
            <h3 style={{ fontSize: 14, marginBottom: 6 }}>Lịch sử (sổ chỉ ghi thêm)</h3>
            <ol style={{ marginLeft: 18 }}>
              {(d.data?.events ?? []).map((e) => (
                <li key={e.event_id} className="small" style={{ marginBottom: 4 }}>
                  <b style={{ display: "inline" }}>{ACTION[e.action] ?? e.action}</b> · {formatDateTime(e.event_time)}
                  {e.note ? <div className="mute">{e.note}</div> : null}
                </li>
              ))}
            </ol>
          </div>
        </div>
      ) : null}
    </section>
  );
}

/** GET /v1/extension/tasks — nhiệm vụ hỗ trợ trong phạm vi, sắp theo hạn SLA; tên cán bộ/HTX/xã do backend trả (G-09). */
function TasksInner() {
  const { api } = useAuth();
  const now = useNow();
  const params = useSearchParams();
  const router = useRouter();
  const focus = params.get("task");
  const [status, setStatus] = useState<"" | ExtTaskStatus>("");
  const [priority, setPriority] = useState<"" | ExtTaskPriority>("");
  const [overdue, setOverdue] = useState(false);

  const list = usePagedList(
    (cursor) =>
      unwrap(
        api.GET("/v1/extension/tasks", {
          params: { query: { status: status || undefined, priority: priority || undefined, overdue: overdue || undefined, cursor, limit: 20 } },
        }),
      ),
    [api, status, priority, overdue],
  );

  return (
    <>
      <PageHead title="Nhiệm vụ hỗ trợ" subtitle="Yêu cầu hỗ trợ từ HTX, phản ánh sâu bệnh, cơ giới hoá — hạn SLA tính từ lúc tạo" />
      {focus ? <TaskDetail key={focus} id={focus} now={now} onClose={() => router.replace("/nhiem-vu")} /> : null}
      <div className="card sec">
        <div className="row" style={{ marginBottom: 14 }}>
          <div className="field">
            <label htmlFor="t-status">Trạng thái</label>
            <select id="t-status" className="select" value={status} onChange={(e) => setStatus(e.target.value as "" | ExtTaskStatus)}>
              <option value="">Tất cả</option>
              {Object.entries(STATUS).map(([k, v]) => (
                <option key={k} value={k}>
                  {v.text}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="t-priority">Mức ưu tiên</label>
            <select id="t-priority" className="select" value={priority} onChange={(e) => setPriority(e.target.value as "" | ExtTaskPriority)}>
              <option value="">Tất cả</option>
              {Object.entries(PRIORITY).map(([k, v]) => (
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
                <th scope="col">Nội dung</th>
                <th scope="col">Nhóm</th>
                <th scope="col">HTX / địa bàn</th>
                <th scope="col">Phân công</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">SLA</th>
              </tr>
            </thead>
            <tbody>
              {list.items.map((t) => (
                <tr key={t.task_id}>
                  <td style={{ maxWidth: 360 }}>
                    <div style={{ fontWeight: 600 }}>{t.description}</div>
                    <div className="mute small">
                      Ưu tiên {PRIORITY[t.priority as ExtTaskPriority] ?? t.priority} · {SOURCE[t.source] ?? t.source} · Tạo {formatDateTime(t.created_at)}
                    </div>
                  </td>
                  <td>{t.category === "sau_benh" ? <span className="chip c-bad">{CATEGORY[t.category]}</span> : (CATEGORY[t.category] ?? t.category)}</td>
                  <td>
                    {t.cooperative_ref ? (
                      <Link href={`/htx/${t.cooperative_ref}`} style={{ fontWeight: 600 }}>
                        {t.cooperative_name ?? "Xem HTX"}
                      </Link>
                    ) : null}
                    <div className="small">{t.commune_name ?? t.commune_code}</div>
                  </td>
                  <td>
                    {t.assignee_ref ? (
                      <span className="chip c-ok">{t.assignee_name ?? "Đã phân công"}</span>
                    ) : (
                      <span className="chip c-warn">Chưa phân công</span>
                    )}
                    {t.escalated_at ? <div className="mute small">Đã chuyển lên tỉnh</div> : null}
                  </td>
                  <td>
                    <span className={`chip ${STATUS[t.status].cls}`}>{STATUS[t.status].text}</span>
                  </td>
                  <td>
                    <SlaCountdown dueAt={t.due_at} resolvedAt={t.resolved_at} now={now} />
                    <div className="mute small">SLA {formatDecimal(t.sla_hours, 1)} giờ</div>
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

export default function TasksPage() {
  return (
    <Suspense fallback={<Loading />}>
      <TasksInner />
    </Suspense>
  );
}
