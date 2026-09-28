"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { unwrap } from "@/api/client";
import { usePagedList } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import type { Notification } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { deepLinkHref } from "@/lib/deepLink";
import { formatDateTime } from "@/lib/format";
import { toApiProblem } from "@/lib/useAsync";
import { announceNotificationsChanged, KIND_LABEL, markAllRead, markRead, relativeTime } from "@/notify/notify";

/** Hộp thư thông báo (pf/notify): chưa đọc/tất cả, đánh dấu đã đọc, mở đúng màn hình theo deep_link. */
export default function InboxPage() {
  const { api } = useAuth();
  const router = useRouter();
  const [unread, setUnread] = useState(false);
  const [version, setVersion] = useState(0);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const list = usePagedList(
    (cursor) => unwrap(api.GET("/v1/notifications", { params: { query: { unread: unread || undefined, cursor, limit: 20 } } })),
    [api, unread, version],
  );

  const changed = () => {
    setVersion((v) => v + 1);
    announceNotificationsChanged();
  };

  const open = async (n: Notification) => {
    setProblem(null);
    try {
      if (!n.read_at) await markRead(api, n.id);
      announceNotificationsChanged();
    } catch (e) {
      setProblem(toApiProblem(e));
    }
    const href = deepLinkHref(n.deep_link);
    if (href) router.push(href);
    else changed();
  };

  const readAll = async () => {
    setProblem(null);
    try {
      await markAllRead(api);
      changed();
    } catch (e) {
      setProblem(toApiProblem(e));
    }
  };

  return (
    <>
      <PageHead
        title="Thông báo"
        subtitle="Chỉ đạo, cảnh báo, tài liệu mới, nhiệm vụ leo thang, xung đột đồng bộ — nội dung chỉ đọc qua máy chủ trong nước"
        actions={
          <button type="button" className="btn btn-o btn-sm" onClick={() => void readAll()}>
            Đánh dấu tất cả đã đọc
          </button>
        }
      />
      {problem ? <ProblemAlert problem={problem} /> : null}
      <div className="card sec inbox">
        <div className="sec-h">
          <label className="row small">
            <input type="checkbox" checked={unread} onChange={(e) => setUnread(e.target.checked)} />
            Chỉ thông báo chưa đọc
          </label>
          <span className="mute small">Tự cập nhật huy hiệu mỗi 60 giây</span>
        </div>
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        <ul className="list-plain">
          {list.items.map((n) => (
            <li key={n.id} style={{ padding: 0 }}>
              <button type="button" className={`item${n.read_at ? "" : " unread"}`} onClick={() => void open(n)}>
                <span className="row" style={{ gap: 6 }}>
                  <span className={`chip ${n.severity === "urgent" ? "c-bad" : "c-info"}`}>{KIND_LABEL[n.kind] ?? n.kind}</span>
                  {n.topic ? <span className="chip c-mute">Theo chủ đề</span> : null}
                  {!n.read_at ? <span className="chip c-violet">Chưa đọc</span> : null}
                  <span className="mute small" title={formatDateTime(n.created_at)}>
                    {relativeTime(n.created_at)}
                  </span>
                </span>
                <b>{n.title}</b>
                <span className="small">{n.body}</span>
                {!deepLinkHref(n.deep_link) ? <span className="mute small">Không có màn hình liên kết trên web.</span> : null}
              </button>
            </li>
          ))}
        </ul>
        {list.loading ? <Loading /> : null}
        {!list.loading && !list.error && list.items.length === 0 ? <Empty>Không có thông báo.</Empty> : null}
        <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
      </div>
    </>
  );
}
