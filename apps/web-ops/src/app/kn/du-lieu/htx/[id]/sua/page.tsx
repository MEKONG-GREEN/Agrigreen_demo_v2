"use client";

import { ProducerEditRoute } from "@/kn/cycle/screens/ProducerScreens";

/** TT-08 "có sửa": Sửa hồ sơ KN riêng loại HTX trên trang riêng (R-A2, #67). */
export default function Page() {
  return <ProducerEditRoute kind="cooperative" />;
}
