"use client";

import { useEffect } from "react";
import { useSSEStore } from "@/stores/sse-store";
import { apiReady } from "@/lib/api/client";

export function useSSE() {
  const connect = useSSEStore((s) => s.connect);
  const disconnect = useSSEStore((s) => s.disconnect);
  const connectionStatus = useSSEStore((s) => s.connectionStatus);

  useEffect(() => {
    apiReady.then(() => connect());
    return () => disconnect();
  }, [connect, disconnect]);

  return { connectionStatus };
}
