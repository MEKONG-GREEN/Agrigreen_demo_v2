"use client";

import { useParams } from "next/navigation";
import { FormDetailScreen } from "@/kn/forms/screens/FormDetailScreen";

/** FN-AD-04.06 — chi tiết mẫu: phiên bản, gán loại báo cáo, Log. */
export default function FormTemplateDetailPage() {
  const { dept, id } = useParams<{ dept: string; id: string }>();
  return <FormDetailScreen dept={dept} id={id} />;
}