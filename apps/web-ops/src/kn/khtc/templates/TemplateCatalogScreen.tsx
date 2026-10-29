"use client";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useAuth } from "@/auth/AuthProvider";
import { DataTable, PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, ReadOnlyNotice, useLoad } from "@/kn/cycle/common";
import { TEMPLATE_TITLES, useTemplateApi } from "./api";
import { templateHref } from "./template";

export function TemplateCatalogScreen() {
  const { kn } = useAuth(); const params = useParams<{ dept: string }>();
  if (kn.context?.role !== "admin" || params.dept !== "ke_hoach_tai_chinh") return <ReadOnlyNotice>Mục lục mẫu báo cáo dành cho Admin tại Phòng Kế hoạch – Tài chính.</ReadOnlyNotice>;
  return <Catalog key={kn.context.context_id} />;
}
function Catalog() {
  const api = useTemplateApi(); const state = useLoad(() => api.catalog(), [api]);
  return <><PageHeader title="Mục lục mẫu báo cáo"><p>Cấu hình năm mẫu cố định. Phiên bản phát hành áp dụng từ ngày bắt đầu kỳ; bản soạn đang có giữ phiên bản đã chọn.</p></PageHeader>
    <LoadState s={state}>{(rows) => <DataTable caption="Năm mẫu báo cáo KHTC" rows={rows} rowKey={(row) => row.key} columns={[
      { key: "title", header: "Mẫu báo cáo", cell: (row) => <Link href={templateHref(row.key)}>{TEMPLATE_TITLES[row.key]}</Link> },
      { key: "version", header: "Bản nháp mới nhất", cell: (row) => row.version ? `Phiên bản ${row.version}` : "Khung ban đầu — chưa lưu" },
      { key: "published", header: "Phát hành", cell: (row) => row.publications.length ? <StatusPill tone="ok">Đã phát hành {row.publications.length} lần</StatusPill> : <StatusPill tone="warn">Chưa phát hành</StatusPill> },
    ]} />}</LoadState>
  </>;
}
