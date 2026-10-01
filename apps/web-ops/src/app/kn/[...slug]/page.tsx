"use client";

import { ComingSoon, useMenuLabel } from "@/kn/ui";

/** Giữ chỗ cho các màn KN v7.7 có trong menu nhưng chưa dựng (đợt 3, P6-màn). Quyền xem do menu quyết định. */
export default function KnPlaceholderPage() {
  const label = useMenuLabel();
  return <ComingSoon title={label ?? "Màn hình"} />;
}
