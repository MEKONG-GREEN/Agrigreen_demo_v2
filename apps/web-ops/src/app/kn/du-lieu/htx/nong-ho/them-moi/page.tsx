"use client";

import { ProducerCreateRoute } from "@/kn/cycle/screens/ProducerScreens";

/** TT-08 (#102): Thêm hồ sơ (Loại mặc định Nông hộ) trên trang riêng. */
export default function Page() {
  return <ProducerCreateRoute kind="household" />;
}