"use client";

import type { HumanRequest } from "@/lib/api/types";
import { requestSummary } from "@/lib/human-requests";
import { RequestDetail } from "./request-detail";

const KIND_PREFIX: Record<string, string> = {
  demo: "Demo: ",
  capture_approval: "Capture: ",
};

interface RequestCardProps {
  request: HumanRequest;
  /** Called after a response lands, so the comment thread can refetch. */
  onResponded: () => void;
}

/** One pending request, inside the task detail view. */
export function RequestCard({ request, onResponded }: RequestCardProps) {
  return (
    <div className="mb-3 last:mb-0">
      {/* A derived one-liner, not request.title: ask_human writes the whole
          markdown question into that field, and the full text renders below. */}
      <div className="text-xs text-primary font-bold mb-1">
        {KIND_PREFIX[request.kind] ?? ""}
        {requestSummary(request)}
        {request.live && (
          <span className="ml-2 text-[9px] text-amber font-bold uppercase tracking-wider">
            LIVE
          </span>
        )}
      </div>

      <RequestDetail request={request} onResponded={onResponded} />
    </div>
  );
}
