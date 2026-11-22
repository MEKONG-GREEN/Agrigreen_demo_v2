"use client";

import { ProducerEditRoute } from "@/kn/cycle/screens/ProducerScreens";

/** TT-08 "có sửa" (#102): Sửa Nông hộ trên trang riêng. */
export default function Page() {
  return <ProducerEditRoute kind="household" />;
}