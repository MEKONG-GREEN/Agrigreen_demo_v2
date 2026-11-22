"use client";

import { Suspense } from "react";
import { KnqgScreen } from "@/kn/cnts/knqg/KnqgScreen";

/** FN-19, UR-TINH-20…24, UR-QG-01 — mẫu biểu dự án KNQG Mẫu 01, 03, 04, 05, 07 (W11.53). ?doc= mở thẳng biểu (W11.53b). */
export default function Page() {
  return (
    <Suspense fallback={null}>
      <KnqgScreen />
    </Suspense>
  );
}
