import { Suspense } from "react";
import { HrmCatalogScalesScreen } from "@/kn/hrm/CatalogScreen";
export default function Page() { return <Suspense fallback={<p>Đang tải...</p>}><HrmCatalogScalesScreen /></Suspense>; }
