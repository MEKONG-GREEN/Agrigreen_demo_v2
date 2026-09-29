"use client";

import { Markdown } from "@/lib/markdown";

/** Nhãn chuyên mục như App HTX (màn Tin tức, HTX US-NEWS-01). */
export const NEWS_CATEGORY: Readonly<Record<string, string>> = {
  technique: "Kỹ thuật",
  pest_warning: "Cảnh báo sâu bệnh",
  policy: "Chính sách",
  market: "Thị trường",
  event: "Sự kiện",
};

export interface PreviewArticle {
  title: string;
  category: string | null;
  summary: string;
  body: string;
  coverUrl: string | null;
  dateLabel: string;
}

/**
 * Khung mô phỏng điện thoại 360 × 720 (HTX US-CMS-01 AC-1): ảnh bìa, chuyên mục, tiêu đề, tóm tắt, nội dung
 * theo đúng bố cục màn đọc tin của App HTX. Nội dung Markdown hiển thị ở chế độ tắt HTML thô.
 */
export function PhonePreview({ article, label = "Xem trước trên điện thoại" }: { article: PreviewArticle; label?: string }) {
  return (
    <figure
      role="figure"
      aria-label={label}
      style={{
        width: 360,
        maxWidth: "100%",
        height: 720,
        margin: "0 auto",
        border: "12px solid #1f2933",
        borderRadius: 36,
        background: "#fff",
        overflow: "hidden",
        display: "flex",
        flexDirection: "column",
        boxShadow: "0 8px 24px rgba(0,0,0,.18)",
      }}
    >
      <div style={{ height: 24, background: "#1f2933", color: "#fff", fontSize: 11, display: "flex", justifyContent: "space-between", padding: "0 14px", alignItems: "center" }} aria-hidden="true">
        <span>9:41</span>
        <span>AgriGreen HTX</span>
      </div>
      <div style={{ padding: "10px 14px", borderBottom: "1px solid #e5e7eb", fontWeight: 600 }} aria-hidden="true">
        ← Tin tức
      </div>
      <article style={{ overflowY: "auto", flex: 1 }} data-testid="phone-article">
        {/* eslint-disable-next-line @next/next/no-img-element -- ảnh bìa là blob: cục bộ hoặc URL tải có hạn, không qua next/image */}
        {article.coverUrl ? <img src={article.coverUrl} alt="Ảnh bìa bài viết" style={{ width: "100%", height: 180, objectFit: "cover", display: "block" }} /> : null}
        <div style={{ padding: "12px 14px", fontSize: 15, lineHeight: 1.5 }}>
          {article.category ? (
            <span className="chip c-info" style={{ marginBottom: 6, display: "inline-block" }}>
              {NEWS_CATEGORY[article.category] ?? article.category}
            </span>
          ) : null}
          <h1 style={{ fontSize: 20, lineHeight: 1.3, margin: "4px 0 6px" }}>{article.title || "(Chưa có tiêu đề)"}</h1>
          <div className="mute small" style={{ marginBottom: 8 }}>
            {article.dateLabel}
          </div>
          {article.summary ? <p style={{ fontWeight: 600 }}>{article.summary}</p> : null}
          <div className="md-body">
            <Markdown source={article.body} />
          </div>
        </div>
      </article>
    </figure>
  );
}
