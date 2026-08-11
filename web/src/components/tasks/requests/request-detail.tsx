"use client";

import { useMemo } from "react";
import type { HumanRequest } from "@/lib/api/types";
import { MarkdownBody } from "@/components/ui";
import { parseDemoBody } from "@/lib/human-requests";
import { DemoBrief } from "./demo-brief";
import { RequestReplyForm } from "./reply-form";
import { CaptureApprovalActions } from "./capture-approval-actions";

interface RequestDetailProps {
  request: HumanRequest;
  /** Called after a response lands, so the comment thread can refetch. */
  onResponded?: () => void;
}

/**
 * The answerable half of a request: the full question, plus whatever it takes
 * to answer it. Shared verbatim by the task-detail card and the home-page
 * banner — the banner exists so a request can be answered without opening the
 * task, which only holds if both surfaces offer the same affordances.
 *
 * Renders on a dark surface (`bg-surface`/`bg-elevated`); the banner wraps it
 * in one rather than restyling the controls.
 */
export function RequestDetail({ request, onResponded }: RequestDetailProps) {
  // Demo bodies are JSON briefs; an unparseable one falls back to raw markdown
  // rather than to a blank card.
  const brief = useMemo(
    () => (request.kind === "demo" ? parseDemoBody(request.body) : null),
    [request.kind, request.body]
  );

  return (
    <>
      {brief ? (
        <DemoBrief brief={brief} />
      ) : (
        request.body && (
          <MarkdownBody className="text-xs text-dim mb-2">{request.body}</MarkdownBody>
        )
      )}

      {request.kind === "capture_approval" ? (
        <CaptureApprovalActions requestId={request.id} onResponded={onResponded} />
      ) : (
        <RequestReplyForm request={request} onResponded={onResponded} />
      )}
    </>
  );
}
