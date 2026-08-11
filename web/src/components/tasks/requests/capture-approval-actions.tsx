"use client";

import { useState } from "react";
import { client } from "@/lib/api/client";
import { apiErrorMessage } from "@/lib/api/errors";
import { toast } from "@/lib/toast";
import { useRequestStore } from "@/stores/request-store";

interface CaptureApprovalActionsProps {
  requestId: number;
  /** Called after the verdict lands, so the comment thread can refetch. */
  onResponded?: () => void;
}

/** Approve / approve-for-the-rest-of-the-run / deny an agent's screen capture. */
export function CaptureApprovalActions({
  requestId,
  onResponded,
}: CaptureApprovalActionsProps) {
  const [submitting, setSubmitting] = useState(false);
  const fetchRequests = useRequestStore((s) => s.fetchRequests);

  const run = async (label: string, fn: () => Promise<unknown>) => {
    if (submitting) return;
    setSubmitting(true);
    try {
      await fn();
      onResponded?.();
      await fetchRequests();
    } catch (err) {
      toast.error(`${label} failed`, apiErrorMessage(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="flex items-center gap-1.5">
      <button
        className="text-[9px] font-bold uppercase tracking-wider text-surface bg-sage px-2.5 py-1 hover:bg-sage/80 transition-colors cursor-pointer border-none font-mono disabled:opacity-40"
        disabled={submitting}
        onClick={() => run("Approve", () => client.approveRequest(requestId, "once"))}
      >
        Approve
      </button>
      <button
        className="text-[9px] font-bold uppercase tracking-wider text-surface bg-sage/70 px-2.5 py-1 hover:bg-sage/60 transition-colors cursor-pointer border-none font-mono disabled:opacity-40"
        disabled={submitting}
        onClick={() => run("Approve", () => client.approveRequest(requestId, "run"))}
      >
        Approve all for run
      </button>
      <button
        className="text-[9px] font-bold uppercase tracking-wider text-surface bg-danger px-2.5 py-1 hover:bg-danger/80 transition-colors cursor-pointer border-none font-mono disabled:opacity-40"
        disabled={submitting}
        onClick={() => run("Deny", () => client.denyRequest(requestId))}
      >
        Deny
      </button>
    </div>
  );
}
