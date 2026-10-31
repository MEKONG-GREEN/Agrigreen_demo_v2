"use client";

import { useEffect, useRef, useState, type ChangeEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { usePagedList } from "@/api/pagination";
import { useIdempotentAction } from "@/api/useIdempotentAction";
import { saveArticleDraft } from "@/admin/articleDraft";
import type { ApiProblem } from "@/api/problem";
import type { ContentAdminDocument, ContentCategory, ContentVersion } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { MfaNotice } from "@/components/MfaNotice";
import { NEWS_CATEGORY, PhonePreview, type PreviewArticle } from "@/components/PhonePreview";
import { ProblemAlert } from "@/components/ProblemAlert";
import { Empty, LoadMore, Loading, PageHead } from "@/components/ui";
import { formatDateTime } from "@/lib/format";
import { toApiProblem } from "@/lib/useAsync";

/** Thông báo AC-3 của HTX US-CMS-01. */
const MISSING_TITLE_CATEGORY = "Vui lòng nhập tiêu đề và chọn chuyên mục trước khi xuất bản.";

interface Draft {
  title: string;
  category: ContentCategory | "";
  summary: string;
  body: string;
}

function todayLabel(): string {
  return new Intl.DateTimeFormat("vi-VN", { timeZone: "Asia/Ho_Chi_Minh", day: "2-digit", month: "2-digit", year: "numeric" }).format(new Date());
}

/**
 * Soạn bài viết khuyến nông (HTX US-CMS-01, Admin API — content_editor_*; MFA): tiêu đề, chuyên mục, nội dung
 * Markdown, ảnh bìa. "Xem trước trên điện thoại" dựng khung 360 × 720 như App HTX; sau khi lưu nháp, bản xem
 * trước lấy đúng phiên bản máy chủ đã lọc (GET /v1/documents/{ref}/versions/{no}) — như khi xuất bản.
 */
function Editor({ onSaved }: { onSaved: () => void }) {
  const { adminApi } = useAuth();
  const idem = useIdempotentAction();
  const [d, setD] = useState<Draft>({ title: "", category: "", summary: "", body: "" });
  const [cover, setCover] = useState<File | null>(null);
  const [coverUrl, setCoverUrl] = useState<string | null>(null);
  const [doc, setDoc] = useState<{ ref: string; version: ContentVersion; status: string } | null>(null);
  const [preview, setPreview] = useState<PreviewArticle | null>(null);
  const [previewSource, setPreviewSource] = useState<"local" | "server">("local");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [gate, setGate] = useState<string | null>(null);
  const [dirty, setDirty] = useState(false);

  // Thu hồi URL ảnh bìa cục bộ khi rời màn.
  const coverUrlRef = useRef<string | null>(null);
  useEffect(() => () => {
    if (coverUrlRef.current) URL.revokeObjectURL(coverUrlRef.current);
  }, []);

  const set = (patch: Partial<Draft>) => {
    setD((x) => ({ ...x, ...patch }));
    setDirty(true);
    setGate(null);
  };
  const onCover = (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0] ?? null;
    const ok = !!f && /^image\/(jpeg|png|webp)$/.test(f.type);
    if (coverUrlRef.current) URL.revokeObjectURL(coverUrlRef.current);
    coverUrlRef.current = ok ? URL.createObjectURL(f) : null;
    setCover(ok ? f : null);
    setCoverUrl(coverUrlRef.current);
    setDirty(true);
  };

  const localPreview = () => {
    setPreviewSource("local");
    setPreview({ title: d.title.trim(), category: d.category || null, summary: d.summary.trim(), body: d.body, coverUrl, dateLabel: todayLabel() });
  };

  /** Lưu nháp: tạo tài liệu (phiên bản 1) hoặc thêm phiên bản; ảnh bìa tải lên rồi gắn bằng phiên bản mới. */
  const save = async (): Promise<{ ref: string; version: ContentVersion } | null> => {
    if (!d.title.trim()) {
      setGate("Vui lòng nhập tiêu đề trước khi lưu nháp.");
      return null;
    }
    setBusy(true);
    setProblem(null);
    setNotice(null);
    // #172: một lần bấm Lưu nháp = một lượt thao tác; lỗi giữa chừng (mất phản hồi) giữ khóa → bấm lại cùng nội dung/ảnh
    // được máy chủ phát lại từng bước đã ghi (không tạo tài liệu/ảnh/phiên bản thứ hai).
    const turn = idem.begin();
    try {
      const input = { title: d.title.trim(), summary: d.summary.trim(), body: d.body, body_format: "markdown" as const, category: d.category || undefined };
      const { ref, version } = await saveArticleDraft(adminApi, turn, { ref: doc?.ref ?? null, input, cover });
      turn.complete();
      setDoc({ ref, version, status: "draft" });
      setDirty(false);
      setNotice(`Đã lưu nháp phiên bản ${version.version_no}.`);
      onSaved();
      return { ref, version };
    } catch (e) {
      setProblem(toApiProblem(e));
      return null;
    } finally {
      setBusy(false);
    }
  };

  /** Xem trước đúng như khi xuất bản: phiên bản máy chủ đã lưu và đã lọc. */
  const serverPreview = async () => {
    const saved = !doc || dirty ? await save() : doc;
    if (!saved) return;
    try {
      const r = await unwrap(adminApi.GET("/v1/documents/{ref}/versions/{no}", { params: { path: { ref: saved.ref, no: saved.version.version_no } } }));
      const c = r.version.attachments.find((a) => a.role === "cover");
      setPreviewSource("server");
      setPreview({
        title: r.version.title,
        category: r.version.category ?? null,
        summary: r.version.summary ?? "",
        body: r.version.body,
        coverUrl: c ? (c.url ?? coverUrl) : null,
        dateLabel: todayLabel(),
      });
    } catch (e) {
      setProblem(toApiProblem(e));
    }
  };

  const submit = async () => {
    if (!d.title.trim() || !d.category) {
      setGate(MISSING_TITLE_CATEGORY);
      return;
    }
    const saved = !doc || dirty ? await save() : doc;
    if (!saved) return;
    setBusy(true);
    try {
      await unwrap(adminApi.POST("/v1/documents/{ref}/submit", { params: { path: { ref: saved.ref }, header: { "Idempotency-Key": newIdempotencyKey() } }, body: {} }));
      setDoc({ ...saved, status: "review" });
      setNotice("Đã gửi duyệt. Người duyệt (khác người soạn) xuất bản bài; App HTX nhận bài ở lượt đồng bộ hoặc thông báo đẩy kế tiếp.");
      onSaved();
    } catch (e) {
      setProblem(toApiProblem(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="panel stack" style={{ gap: 10, marginBottom: 16 }} aria-labelledby="cms-new">
      <b id="cms-new">Viết bài mới</b>
      <div className="row" style={{ alignItems: "flex-start" }}>
        <div className="field" style={{ flex: "2 1 320px" }}>
          <label htmlFor="cms-title">Tiêu đề</label>
          <input id="cms-title" className="input" maxLength={300} value={d.title} onChange={(e) => set({ title: e.target.value })} />
        </div>
        <div className="field" style={{ flex: "1 1 180px" }}>
          <label htmlFor="cms-cat">Chuyên mục</label>
          <select id="cms-cat" className="select" value={d.category} onChange={(e) => set({ category: e.target.value as ContentCategory | "" })}>
            <option value="">— Chọn chuyên mục —</option>
            {Object.entries(NEWS_CATEGORY).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div className="field">
        <label htmlFor="cms-sum">Tóm tắt</label>
        <input id="cms-sum" className="input" maxLength={2000} value={d.summary} onChange={(e) => set({ summary: e.target.value })} />
      </div>
      <div className="field">
        <label htmlFor="cms-body">Nội dung (Markdown: # tiêu đề, **đậm**, *nghiêng*, - danh sách, [liên kết](https://…))</label>
        <textarea id="cms-body" className="textarea" rows={10} value={d.body} onChange={(e) => set({ body: e.target.value })} />
      </div>
      <div className="field">
        <label htmlFor="cms-cover">Ảnh bìa (JPEG/PNG/WebP, ≤ 15 MB)</label>
        <input id="cms-cover" type="file" accept="image/jpeg,image/png,image/webp" onChange={onCover} />
      </div>
      {gate ? (
        <p className="err" role="alert">
          {gate}
        </p>
      ) : null}
      {problem ? <ProblemAlert problem={problem} /> : null}
      {notice ? (
        <p role="status" className="small" style={{ color: "var(--g800)" }}>
          {notice}
        </p>
      ) : null}
      <div className="row">
        <button type="button" className="btn btn-o btn-sm" onClick={localPreview}>
          <Icon name="phone" size={15} /> Xem trước trên điện thoại
        </button>
        <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={() => void save()}>
          {busy ? "Đang lưu…" : "Lưu nháp"}
        </button>
        <button type="button" className="btn btn-o btn-sm" disabled={busy} onClick={() => void serverPreview()}>
          Xem trước bản đã lưu (như khi xuất bản)
        </button>
        <button type="button" className="btn btn-p btn-sm" disabled={busy || doc?.status === "review"} onClick={() => void submit()}>
          Gửi duyệt để xuất bản
        </button>
      </div>
      {preview ? (
        <Dialog title={previewSource === "server" ? `Xem trước phiên bản ${doc?.version.version_no ?? ""} (máy chủ đã lọc)` : "Xem trước trên điện thoại"} onClose={() => setPreview(null)}>
          <PhonePreview article={preview} />
          {previewSource === "local" ? <p className="small mute">Bản nháp trên trình duyệt. Lưu nháp để xem đúng nội dung máy chủ sẽ xuất bản.</p> : null}
        </Dialog>
      ) : null}
    </section>
  );
}

const STATUS: Record<string, { text: string; cls: string }> = {
  draft: { text: "Nháp", cls: "c-mute" },
  review: { text: "Chờ duyệt", cls: "c-warn" },
  published: { text: "Đã xuất bản", cls: "c-ok" },
  retired: { text: "Đã gỡ", cls: "c-mute" },
};

function DocRow({ d }: { d: ContentAdminDocument }) {
  const s = STATUS[d.working_status ?? d.status] ?? { text: d.status, cls: "c-mute" };
  return (
    <tr>
      <td>{d.title ?? <span className="mute">(chưa xuất bản) · phiên bản {d.last_version_no}</span>}</td>
      <td>{d.category ? (NEWS_CATEGORY[d.category] ?? d.category) : "—"}</td>
      <td>
        <span className={`chip ${s.cls}`}>{s.text}</span>
      </td>
      <td className="small">{formatDateTime(d.updated_at)}</td>
    </tr>
  );
}

export default function ArticleEditorPage() {
  const { adminApi } = useAuth();
  const [version, setVersion] = useState(0);
  const list = usePagedList((cursor) => unwrap(adminApi.GET("/v1/documents", { params: { query: { kind: "news", cursor, limit: 20 } } })), [adminApi, version]);
  return (
    <>
      <PageHead title="Soạn bài viết khuyến nông" subtitle="Soạn tin tức cho App HTX, xem trước giao diện điện thoại trước khi gửi duyệt xuất bản" />
      <MfaNotice />
      <div className="card sec">
        <Editor onSaved={() => setVersion((v) => v + 1)} />
        <h2 className="sec-h">Bài viết</h2>
        {list.error ? <ProblemAlert problem={list.error} onRetry={list.reload} /> : null}
        <div className="tbl-wrap">
          <table className="tbl">
            <caption className="sr-only">Bài viết tin tức</caption>
            <thead>
              <tr>
                <th scope="col">Tiêu đề</th>
                <th scope="col">Chuyên mục</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">Cập nhật</th>
              </tr>
            </thead>
            <tbody>
              {list.items.map((d) => (
                <DocRow key={d.ref} d={d} />
              ))}
            </tbody>
          </table>
        </div>
        {list.loading ? <Loading /> : null}
        {!list.loading && !list.error && list.items.length === 0 ? <Empty>Chưa có bài viết.</Empty> : null}
        <LoadMore hasMore={list.hasMore} loading={list.loading} onClick={list.loadMore} />
      </div>
    </>
  );
}
