"use client";

import { useEffect } from "react";
import { useRequestStore } from "@/stores/request-store";
import { apiReady } from "@/lib/api/client";

/**
 * Keeps the pending-request set fresh without polling.
 *
 * The SSE `request` event is the live path, but the hub's per-client buffer is
 * bounded and drops on overflow, and a webview that was backgrounded can miss
 * the whole stream. So: fetch once at startup, and again whenever the window
 * comes back to the foreground. Mount this once, in AppShell.
 */
export function usePendingRequests() {
  const fetchRequests = useRequestStore((s) => s.fetchRequests);

  useEffect(() => {
    apiReady.then(() => fetchRequests());

    const refresh = () => {
      if (document.visibilityState === "hidden") return;
      fetchRequests();
    };
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, [fetchRequests]);
}
