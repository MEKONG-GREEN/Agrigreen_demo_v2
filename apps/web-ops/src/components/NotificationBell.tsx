"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import { unwrap } from "@/api/client";
import type { ApiProblem } from "@/api/problem";
import type { Notification } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { deepLinkHref } from "@/lib/deepLink";
import { toApiProblem } from "@/lib/useAsync";
import { useInterval } from "@/lib/useInterval";
import { KIND_LABEL, markAllRead, markRead, NOTIFY_CHANGED_EVENT, NOTIFY_POLL_MS, relativeTime, unreadCount } from "@/notify/notify";
import { Icon } from "./Icon";
import { ProblemAlert } from "./ProblemAlert";

/** Chuông thông báo ở thanh trên: số chưa đọc (hỏi lại mỗi 60 giây) và bảng hộp thư gần đây. */
export function NotificationBell() {
  const { api } = useAuth();
  const router = useRouter();
  const [count, setCount] = useState<number | null>(null);
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<Notification[] | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const wrap = useRef<HTMLDivElement>(null);

  const refreshCount = useCallback(() => {
    unreadCount(api).then(setCount, () => undefined); // lỗi tạm: giữ số cũ, lượt sau hỏi lại
  }, [api]);
  useInterval(refreshCount, NOTIFY_POLL_MS);
  useEffect(() => {
    window.addEventListener(NOTIFY_CHANGED_EVENT, refreshCount);
    return () => window.removeEventListener(NOTIFY_CHANGED_EVENT, refreshCount);
  }, [refreshCount]);

  const loadList = useCallback(() => {
    setProblem(null);
    unwrap(api.GET("/v1/notifications", { params: { query: { limit: 8 } } })).then(
      (p) => setItems(p.items),
      (e: unknown) => setProblem(toApiProblem(e)),
    );
  }, [api]);

  // Đóng khi bấm ra ngoài hoặc nhấn Esc.
  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (wrap.current && !wrap.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onDoc);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const toggle = () => {
    const next = !open;
    setOpen(next);
    if (next) loadList();
  };

  const openItem = async (n: Notification) => {
    try {
      if (!n.read_at) await markRead(api, n.id);
    } catch {
      // không chặn điều hướng khi đánh dấu đã đọc lỗi
    }
    setOpen(false);
    refreshCount();
    router.push(deepLinkHref(n.deep_link) ?? "/thong-bao");
  };

  const readAll = async () => {
    try {
      await markAllRead(api);
      loadList();
      refreshCount();
    } catch (e) {
      setProblem(toApiProblem(e));
    }
  };

  const label = count ? `Thông báo: ${count} chưa đọc` : "Thông báo";
  return (
    <div className="bell-wrap" ref={wrap}>
      <button type="button" className="bell" aria-label={label} aria-haspopup="dialog" aria-expanded={open} onClick={toggle} data-testid="bell">
        <Icon name="bell" />
        {count ? (
          <span className="bell-count" aria-hidden="true" data-testid="bell-count">
            {count > 99 ? "99+" : count}
          </span>
        ) : null}
      </button>
      {open ? (
        <div className="bell-panel card" role="dialog" aria-label="Hộp thư thông báo">
          <div className="sec-h" style={{ marginBottom: 8 }}>
            <h2>Thông báo</h2>
            <button type="button" className="btn btn-o btn-sm" onClick={() => void readAll()} disabled={!count}>
              Đánh dấu tất cả đã đọc
            </button>
          </div>
          {problem ? <ProblemAlert problem={problem} onRetry={loadList} /> : null}
          {items && items.length === 0 ? <p className="mute small">Không có thông báo.</p> : null}
          <ul className="bell-list">
            {(items ?? []).map((n) => (
              <li key={n.id}>
                <button type="button" onClick={() => void openItem(n)} className={n.read_at ? "" : "unread"}>
                  <span className="row" style={{ gap: 6 }}>
                    <span className={`chip ${n.severity === "urgent" ? "c-bad" : "c-info"}`}>{KIND_LABEL[n.kind] ?? n.kind}</span>
                    <span className="mute small">{relativeTime(n.created_at)}</span>
                  </span>
                  <b>{n.title}</b>
                  <span className="small mute">{n.body}</span>
                </button>
              </li>
            ))}
          </ul>
          <p style={{ marginTop: 8 }}>
            <Link href="/thong-bao" onClick={() => setOpen(false)}>
              Xem tất cả thông báo
            </Link>
          </p>
        </div>
      ) : null}
    </div>
  );
}
