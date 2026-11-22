import { Suspense } from "react";
import { HrmScaleScreen } from "@/kn/hrm/ScaleScreens";
export default function Page() { return <Suspense fallback={<p>Đang tải...</p>}><HrmScaleScreen create /></Suspense>; }
