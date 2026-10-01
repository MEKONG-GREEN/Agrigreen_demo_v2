"use client";

import { useParams } from "next/navigation";
import { FormListScreen } from "@/kn/forms/screens/FormListScreen";

/** FN-AD-04.06 — Phòng ban › <Phòng ban> › Mẫu báo cáo (Admin, Hình 27). */
export default function FormTemplatesPage() {
  const { dept } = useParams<{ dept: string }>();
  return <FormListScreen dept={dept} />;
}