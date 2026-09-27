"use client";

import { useEffect } from "react";
import { getUserManager } from "@/auth/oidc";
import { config } from "@/config";

/** silent_redirect_uri: chạy trong iframe ẩn để gia hạn token (prompt=none). */
export default function SilentCallbackPage() {
  useEffect(() => {
    if (config.mock) return;
    void getUserManager()
      .signinSilentCallback()
      .catch(() => undefined);
  }, []);
  return null;
}
