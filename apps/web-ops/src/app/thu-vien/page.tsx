"use client";

import { useEffect, useState } from "react";
import { unwrap } from "@/api/client";
import { usePagedList } from "@/api/pagination";
import type { DocumentKind } from "@/api/pending/content";
import { useAuth } from "@/auth/AuthProvider";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatInt } from "@/lib/decimal";
import { formatDate } from "@/lib/format";

const KIND: Record<DocumentKind, { text: string; cls: string }> = {
  quy_trinh: { text: "Quy trình kỹ thuật", cls: "c-ok" },
  so_tay: { text: "Sổ tay", cls: "c-info" },
  video: { text: "Video", cls: "c-violet" },
  van_ban: { text: "Văn bản", cls: "c-warn" },
  infographic: { text: "Infographic", cls: "c-mute" },
};

export default function LibraryPage() {
  const { api } = useAuth();
  const [q, setQ] = useState("");
  const [dq, setDq] = useState("");
  const [kind, setKind] = useState<"" | DocumentKind>("");
  useEffect(() => {
    const t = setTimeout(() => setDq(q), 300);
    return () => clearTimeout(t);
  }, [q]);

  const list = usePagedList(
    (cursor) => unwrap(api.GET("/v1/documents", { params: { query: { q: dq || undefined, kind: kind || undefined, cursor, limit: 12 } } })),
    [api, dq, kind],
  );

  return (
    <>
      <PageHead title="Thư viện số" subtitle="Quy trình kỹ thuật do TW ban hành, bổ sung đặc thù của tỉnh, sổ tay và video" />
      <div className="card sec" style={{ marginBottom: 16 }}>
        <form className="row" role="search" onSubmit={(e) => e.preventDefault()}>
          <div className="field" style={{ flex: "1 1 280px" }}>
            <label htmlFor="doc-q">Tìm tài liệu</label>
            <input id="doc-q" className="input" type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Ví dụ: tưới ướt khô xen kẽ, rầy nâu…" />
          </div>
          <div className="field">
            <label htmlFor="doc-kind">Loại tài liệu</label>
            <select id="doc-kind" className="select" value={kind} onChange={(e) => setKind(e.target.value as "" | DocumentKind)}>
              <option value="">Tất cả</option>
              {Object.entries(KIND).map(([k, v]) => (
                <option key={k} value={k}>
                  {v.text}
                </option>
              ))}
            </select>
          </div>
        </form>
      </div>
      {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
      <ul className="doc-grid" style={{ listStyle: "none" }}>
        {list.items.map((d) => (
          <li key={d.id} className="card doc">
            <div className="row">
              <span className={`chip ${KIND[d.kind].cls}`}>{KIND[d.kind].text}</span>
              <span className="chip c-mute">{d.topic}</span>
              {d.has_local_supplement ? <span className="chip c-warn">Có bổ sung của tỉnh</span> : null}
            </div>
            <h3>{d.title}</h3>
            <p className="mute small">{d.summary}</p>
            <div className="mute small" style={{ marginTop: "auto" }}>
              {d.issuer_name} · Phiên bản {d.version} · {formatDate(d.published_at)} · {formatInt(d.read_count)} lượt đọc
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
