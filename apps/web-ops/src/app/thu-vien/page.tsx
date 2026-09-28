"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useMemo, useState } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages, usePagedList } from "@/api/pagination";
import type { ApiProblem } from "@/api/problem";
import type { ContentKind } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Icon } from "@/components/Icon";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Tabs } from "@/components/Tabs";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDate, formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";

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

const kindOf = (k: string | null | undefined) => (k && k in KIND ? KIND[k as ContentKind] : null);

/** Nút lưu/bỏ lưu vào danh mục cá nhân (US-LIB-05: POST /v1/documents/{ref}/save | unsave). */
function SaveButton({ docRef, saved, onChanged }: { docRef: string; saved: boolean; onChanged: () => void }) {
  const { api } = useAuth();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const toggle = async () => {
    setBusy(true);
    setProblem(null);
    const header = { "Idempotency-Key": newIdempotencyKey() };
    try {
      if (saved) await unwrap(api.POST("/v1/documents/{ref}/unsave", { params: { path: { ref: docRef }, header } }));
      else await unwrap(api.POST("/v1/documents/{ref}/save", { params: { path: { ref: docRef }, header }, body: {} }));
      onChanged();
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="stack" style={{ gap: 6 }}>
      <button type="button" className={`btn btn-sm ${saved ? "btn-p" : "btn-o"}`} onClick={() => void toggle()} disabled={busy} aria-pressed={saved}>
        <Icon name="bookmark" size={15} />
        {busy ? "Đang lưu…" : saved ? "Đã lưu — bỏ lưu" : "Lưu đọc ngoại tuyến"}
      </button>
      {problem ? <ProblemAlert problem={problem} /> : null}
    </div>
  );
}

/** Tài liệu được nhắc trong thông báo (/thu-vien?doc=…) — GET /v1/documents/{ref}. */
function DocumentDetail({ docRef, saved, onChanged, onClose }: { docRef: string; saved: boolean; onChanged: () => void; onClose: () => void }) {
  const { api } = useAuth();
  const d = useAsync(() => unwrap(api.GET("/v1/documents/{ref}", { params: { path: { ref: docRef } } })), [api, docRef]);
  const doc = d.data?.document;
  return (
    <section className="card sec hl" style={{ marginBottom: 16 }} aria-labelledby="doc-detail" data-testid="doc-detail">
      <div className="sec-h">
        <h2 id="doc-detail">{doc?.title ?? "Tài liệu"}</h2>
        <button type="button" className="btn btn-o btn-sm" onClick={onClose}>
          Đóng
        </button>
      </div>
      {d.error ? <ProblemAlert problem={d.error} onRetry={d.reload} /> : null}
      {d.loading && !d.data ? <Loading /> : null}
      {doc && d.data ? (
        <div className="stack" style={{ gap: 10 }}>
          <div className="row">
            <span className={`chip ${KIND[doc.kind].cls}`}>{KIND[doc.kind].text}</span>
            <span className="chip c-mute">{AUDIENCE[doc.audience.level]}</span>
            <span className="mute small">
              Phiên bản {d.data.version.version_no} · xuất bản {formatDate(doc.published_at)}
            </span>
          </div>
          {/* Nội dung hiển thị dạng văn bản thuần (không chèn HTML). */}
          <p style={{ whiteSpace: "pre-wrap" }}>{d.data.version.body}</p>
          {d.data.supplements.length > 0 ? (
            <p className="small">Bổ sung đặc thù địa phương: {d.data.supplements.map((s) => s.title).join("; ")}</p>
          ) : null}
          <div>
            <SaveButton docRef={doc.ref} saved={saved} onChanged={onChanged} />
          </div>
        </div>
      ) : null}
    </section>
  );
}

function Saved({ refresh, onChanged }: { refresh: number; onChanged: () => void }) {
  const { api } = useAuth();
  const list = usePagedList((cursor) => unwrap(api.GET("/v1/library", { params: { query: { cursor, limit: 20 } } })), [api, refresh]);
  return (
    <>
      <p className="mute small" style={{ marginBottom: 10 }}>
        Tài liệu đã lưu được tải xuống app hiện trường để đọc khi không có mạng. Tài liệu đã gỡ vẫn hiện với nhãn “đã gỡ”.
      </p>
      {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
      <ul className="list-plain">
        {list.items.map((i) => {
          const k = kindOf(i.kind);
          return (
            <li key={i.document_ref} className="row" style={{ justifyContent: "space-between", alignItems: "flex-start" }}>
              <div style={{ flex: "1 1 320px" }}>
                <div className="row" style={{ gap: 6 }}>
                  {k ? <span className={`chip ${k.cls}`}>{k.text}</span> : null}
                  {!i.available ? <span className="chip c-mute">Đã gỡ</span> : null}
                </div>
                <b>{i.title ?? "(Không có tiêu đề)"}</b>
                {i.note ? <div className="small">Ghi chú: {i.note}</div> : null}
                <div className="mute small">Lưu lúc {formatDateTime(i.saved_at)}</div>
              </div>
              <SaveButton docRef={i.document_ref} saved onChanged={onChanged} />
            </li>
          );
        })}
      </ul>
      {list.loading ? <Loading /> : null}
      {!list.loading && !list.error && list.items.length === 0 ? <Empty>Chưa lưu tài liệu nào.</Empty> : null}
      <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
    </>
  );
}

/** GET /v1/documents — tài liệu ĐÃ XUẤT BẢN trong phạm vi; danh mục cá nhân GET /v1/library (US-LIB-05). */
function LibraryInner() {
  const { api } = useAuth();
  const params = useSearchParams();
  const router = useRouter();
  const focus = params.get("doc");
  const [tab, setTab] = useState(params.get("tab") === "saved" ? "saved" : "all");
  const [q, setQ] = useState("");
  const [dq, setDq] = useState("");
  const [kind, setKind] = useState<"" | ContentKind>("");
  const [category, setCategory] = useState("");
  const [version, setVersion] = useState(0);
  useEffect(() => {
    const t = setTimeout(() => setDq(q), 300);
    return () => clearTimeout(t);
  }, [q]);

  const saved = useAsync(
    () => collectPages((cursor) => unwrap(api.GET("/v1/library", { params: { query: { cursor, limit: 200 } } })), 2000),
    [api, version],
  );
  const savedSet = useMemo(() => new Set((saved.data ?? []).map((i) => i.document_ref)), [saved.data]);
  const bump = () => setVersion((v) => v + 1);

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
      {focus ? (
        <DocumentDetail key={focus} docRef={focus} saved={savedSet.has(focus)} onChanged={bump} onClose={() => router.replace("/thu-vien")} />
      ) : null}
      <div className="card sec">
        <Tabs
          label="Thư viện"
          value={tab}
          onChange={setTab}
          tabs={[
            { id: "all", label: "Tất cả tài liệu" },
            { id: "saved", label: `Đã lưu${saved.data ? ` (${saved.data.length})` : ""}` },
          ]}
        >
          {tab === "saved" ? (
            <Saved refresh={version} onChanged={bump} />
          ) : (
            <>
              <form className="row" role="search" onSubmit={(e) => e.preventDefault()} style={{ marginBottom: 16 }}>
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
                    <SaveButton docRef={d.ref} saved={savedSet.has(d.ref)} onChanged={bump} />
                  </li>
                ))}
              </ul>
              {list.loading ? <Loading /> : null}
              {!list.loading && !list.error && list.items.length === 0 ? <Empty>Không tìm thấy tài liệu phù hợp.</Empty> : null}
              <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
            </>
          )}
        </Tabs>
      </div>
    </>
  );
}

export default function LibraryPage() {
  return (
    <Suspense fallback={<Loading />}>
      <LibraryInner />
    </Suspense>
  );
}
