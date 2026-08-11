"use client";

import { useMemo } from "react";
import { useRequestStore } from "@/stores/request-store";
import { requestsForTask } from "@/lib/human-requests";
import { RequestCard } from "./request-card";

interface TaskRequestsProps {
  taskId: number;
  /** Drives the heading: a parked task is waiting on you specifically. */
  awaitingHuman: boolean;
  /** Called after a response lands, so the comment thread can refetch. */
  onResponded: () => void;
}

/**
 * The pending requests for one task. Reads the shared store rather than
 * fetching, so a request raised mid-run appears the moment SSE delivers it.
 */
export function TaskRequests({ taskId, awaitingHuman, onResponded }: TaskRequestsProps) {
  // Filtering inside the selector would hand zustand a fresh array on every
  // read; select the stable list and derive here (lib/selector-stability.test.ts).
  const pending = useRequestStore((s) => s.pending);
  const requests = useMemo(() => requestsForTask(pending, taskId), [pending, taskId]);

  if (requests.length === 0) return null;

  return (
    <div className="bg-amber/10 border-l-2 border-amber px-6 py-3">
      <div className="text-[9px] font-bold uppercase tracking-widest text-amber mb-2">
        {awaitingHuman ? "Waiting for you" : "Needs attention"} ({requests.length})
      </div>
      {requests.map((req) => (
        <RequestCard key={req.id} request={req} onResponded={onResponded} />
      ))}
    </div>
  );
}
