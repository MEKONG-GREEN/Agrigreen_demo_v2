"use client";

import { KhtcCatalogScreen } from "@/kn/khtc/CatalogScreen";
import { Suspense } from "react";
import { Loading } from "@/components/ui";

/** FN-AD-04.01 Danh mục KHTC — Admin › Phòng ban › Phòng Kế hoạch – Tài chính (W11.61). */
export default function Page() {
  return <Suspense fallback={<Loading />}><KhtcCatalogScreen /></Suspense>;
}
