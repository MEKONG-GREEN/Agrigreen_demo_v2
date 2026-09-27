"use client";

import { useEffect, useState } from "react";
import { unwrap } from "@/api/client";
import { usePagedList } from "@/api/pagination";
import type { ContentKind } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDate } from "@/lib/format";

const KIND: Record<ContentKind, { text: string; cls: string }> = {
  procedure: { text: "Quy trình kỹ thuật", cls: "c-ok" },
  guide: { text: "Hướng dẫn", cls: "c-info" },
  supplement: { text: "Bổ sung đặc thù địa phương", cls: "c-warn" },
  alert: { text: "Cảnh báo khẩn", cls: "c-bad" },
  news: { text: "Tin tức", cls: "c-violet" },
};

const CATEGORY: Record<string, string> = {
  technique: "Kỹ thuật",
  pest_warning: "Cảnh báo sâu bệnh",
  policy: "Chính sách",
  market: "Thị trường",
  event: "Sự kiện",
};

const AUDIENCE = { national: "Toàn quốc", province: "Cấp tỉnh", commune: "Cấp xã" } as const;

/** GET /v1/documents — tài liệu ĐÃ XUẤT BẢN trong phạm vi (toàn quốc + tỉnh + xã của mình). */
export default function LibraryPage() {
  const { api } = useAuth();
  const [q, setQ] = useState("");
  const [dq, setDq] = useState("");
  const [kind, setKind] = useState<"" | ContentKind>("");
  const [category, setCategory] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setDq(q), 300);
    return () => clearTimeout(t);
  }, [q]);

  const list = usePagedList(
    (cursor) =>
      unwrap(
        api.GET("/v1/documents", {
          params: { query: { q: dq || undefined, kind: kind || undefined, category: category || undefined, cursor, limit: 12 } },
        }),
      ),
    [api, dq, kind, category],
  );

  return (
    <>
      <PageHead title="Thư viện số" subtitle="Quy trình kỹ thuật do TW ban hành, bổ sung đặc thù của tỉnh, hướng dẫn, cảnh báo và tin tức" />
      <div className="card sec" style={{ marginBottom: 16 }}>
        <form className="row" role="search" onSubmit={(e) => e.preventDefault()}>
          <div className="field" style={{ flex: "1 1 280px" }}>
            <label htmlFor="doc-q">Tìm tài liệu</label>
            <input
              id="doc-q"
              className="input"
              type="search"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="Không cần gõ dấu — ví dụ: tuoi uot kho, ray nau…"
            />
          </div>
          <div className="field">
            <label htmlFor="doc-kind">Loại tài liệu</label>
            <select id="doc-kind" className="select" value={kind} onChange={(e) => setKind(e.target.value as "" | ContentKind)}>
              <option value="">Tất cả</option>
              {Object.entries(KIND).map(([k, v]) => (
                <option key={k} value={k}>
                  {v.text}
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="doc-cat">Chuyên mục</label>
            <select id="doc-cat" className="select" value={category} onChange={(e) => setCategory(e.target.value)}>
              <option value="">Tất cả</option>
              {Object.entries(CATEGORY).map(([k, v]) => (
                <option key={k} value={k}>
                  {v}
                </option>
              ))}
            </select>
          </div>
        </form>
      </div>
      {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
      <ul className="doc-grid" style={{ listStyle: "none" }}>
        {list.items.map((d) => (
          <li key={d.ref} className="card doc">
            <div className="row">
              <span className={`chip ${KIND[d.kind].cls}`}>{KIND[d.kind].text}</span>
              {d.category ? <span className="chip c-mute">{CATEGORY[d.category] ?? d.category}</span> : null}
              <span className="chip c-mute">{AUDIENCE[d.audience.level]}</span>
            </div>
            <h3>{d.title ?? "(Chưa có tiêu đề)"}</h3>
            {d.summary ? <p className="mute small">{d.summary}</p> : null}
            <div className="mute small" style={{ marginTop: "auto" }}>
              {d.published_version_no ? `Phiên bản ${d.published_version_no} · ` : ""}Xuất bản {formatDate(d.published_at)}
            </div>
          </li>
        ))}
      </ul>
      {list.loading ? <Loading /> : null}
      {!list.loading && !list.error && list.items.length === 0 ? <Empty>Không tìm thấy tài liệu phù hợp.</Empty> : null}
      <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
    </>
  );
}
