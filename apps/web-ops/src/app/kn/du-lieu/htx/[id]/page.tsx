"use client";

import { useParams } from "next/navigation";
import { Icon } from "@/components/Icon";
import { ActionBar, BackLink, DetailLayout, LabelValueBlock, LogPanel, RelatedTabs } from "@/components/patterns/detail";
import { StatusPill } from "@/components/patterns/list";
import { DEMO_COOP_LOG, DEMO_COOPS } from "@/kn/demo";
import { DemoNotice, RequiresMock, useKnReadOnly } from "@/kn/ui";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";

/** Mẫu Detail (FN-XA-03.03 chi tiết HTX, Design Sample "Screens / Detail", Hình 16b) — dữ liệu minh họa. */
export default function KnCoopDetailPage() {
  const { id } = useParams<{ id: string }>();
  const coop = DEMO_COOPS.find((c) => c.id === id);
  const readOnly = useKnReadOnly();
  useBreadcrumbTail(coop ? [{ label: coop.name }] : []);
  if (!coop) {
    return (
      <RequiresMock title="Chi tiết HTX">
        <BackLink href="/kn/du-lieu/htx">Danh sách HTX</BackLink>
        <div className="card sec" role="alert">
          Không tìm thấy HTX.
        </div>
      </RequiresMock>
    );
  }
  return (
    <RequiresMock title="Chi tiết HTX">
      <BackLink href="/kn/du-lieu/htx">Danh sách HTX</BackLink>
      <div className="pg-head">
        <h1>{coop.name}</h1>
      </div>
      <DemoNotice />
      {readOnly ? null : (
        <ActionBar>
          <button type="button" className="btn btn-p btn-sm">
            <Icon name="edit" size={15} /> Sửa
          </button>
          <button type="button" className="btn btn-o btn-sm">
            Thêm nông hộ
          </button>
          <button type="button" className="btn btn-o btn-sm">
            Tạm dừng
          </button>
        </ActionBar>
      )}
      <DetailLayout
        main={
          <>
            <LabelValueBlock
              title="Thông tin chung"
              items={[
                { label: "Tên HTX", value: coop.name },
                { label: "Xã/phường", value: coop.commune },
                { label: "Người đại diện", value: coop.representative },
                { label: "Trạng thái", value: <StatusPill tone={coop.status === "active" ? "ok" : "neutral"}>{coop.status === "active" ? "Hoạt động" : "Tạm dừng"}</StatusPill> },
              ]}
            />
            <LabelValueBlock
              title="Sản xuất"
              items={[
                { label: "Diện tích (ha)", value: coop.area_ha },
                { label: "Số nông hộ", value: coop.members },
                { label: "Cây trồng chính", value: "Lúa" },
                { label: "Mô hình", value: "1 phải 5 giảm" },
              ]}
            />
            <RelatedTabs
              tabs={[
                {
                  id: "ho",
                  label: "Nông hộ thành viên",
                  content: <LabelValueBlock items={[{ label: "Số hộ", value: coop.members }, { label: "Tổng diện tích", value: `${coop.area_ha} ha` }]} />,
                },
                { id: "may", label: "Máy móc", content: <LabelValueBlock items={[{ label: "Máy gặt đập liên hợp", value: 3 }, { label: "Máy cấy", value: 2 }]} /> },
              ]}
            />
          </>
        }
        log={<LogPanel entries={DEMO_COOP_LOG} />}
      />
    </RequiresMock>
  );
}
