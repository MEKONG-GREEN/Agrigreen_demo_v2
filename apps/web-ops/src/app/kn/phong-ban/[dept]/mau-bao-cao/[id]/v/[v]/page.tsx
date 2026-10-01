"use client";

import { useParams } from "next/navigation";
import { FormEditorScreen } from "@/kn/forms/screens/FormEditorScreen";

/** FN-AD-04.06 B2–B6 — trình thiết kế một phiên bản mẫu. */
export default function FormTemplateEditorPage() {
  const { dept, id, v } = useParams<{ dept: string; id: string; v: string }>();
  return <FormEditorScreen key={`${id}:${v}`} dept={dept} id={id} v={Number(v)} />;
}