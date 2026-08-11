"use client";

import { useState } from "react";
import type { LogEvent } from "@/lib/api/types";
import { Badge } from "@/components/ui";
import { formatDuration } from "@/lib/format";

interface RunEventsProps {
  events: LogEvent[];
  searchQuery?: string;
}

function highlightText(text: string, query: string) {
  if (!query) return text;
  const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const parts = text.split(new RegExp(`(${escaped})`, "gi"));
  return parts.map((part, i) =>
    part.toLowerCase() === query.toLowerCase() ? (
      <span key={i} className="bg-amber/40 text-primary px-0.5">
        {part}
      </span>
    ) : (
      part
    )
  );
}

function ExpandableText({
  text,
  maxLen = 200,
  searchQuery,
}: {
  text: string;
  maxLen?: number;
  searchQuery?: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const truncated = text.length > maxLen && !expanded;
  const display = truncated ? text.slice(0, maxLen) + "..." : text;

  return (
    <span>
      <span className="whitespace-pre-wrap break-all">
        {searchQuery ? highlightText(display, searchQuery) : display}
      </span>
      {text.length > maxLen && (
        <button
          type="button"
          onClick={() => setExpanded(!expanded)}
          className="ml-1 text-amber text-[9px] font-bold uppercase tracking-widest cursor-pointer bg-transparent border-none font-mono hover:text-gold"
        >
          {expanded ? "collapse" : "expand"}
        </button>
      )}
    </span>
  );
}

function ExpandableJson({
  data,
  searchQuery,
}: {
  data: unknown;
  searchQuery?: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const json = JSON.stringify(data, null, 2);

  return (
    <span>
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        className="text-amber text-[9px] font-bold uppercase tracking-widest cursor-pointer bg-transparent border-none font-mono hover:text-gold"
      >
        {expanded ? "hide details" : "show details"}
      </button>
      {expanded && (
        <pre className="mt-1 p-2 bg-surface text-xs text-primary overflow-x-auto max-h-[300px] overflow-y-auto font-mono">
          {searchQuery ? highlightText(json, searchQuery) : json}
        </pre>
      )}
    </span>
  );
}

function EventRow({
  event,
  searchQuery,
}: {
  event: LogEvent;
  searchQuery?: string;
}) {
  switch (event.type) {
    case "init":
      return null;

    case "assistant_text":
      return (
        <div className="bg-surface px-3 py-1.5 text-xs text-primary font-mono">
          <span className="text-dim mr-1" title="Assistant">
            &#128172;
          </span>
          <ExpandableText
            text={event.text ?? ""}
            maxLen={500}
            searchQuery={searchQuery}
          />
        </div>
      );

    case "tool_use":
      return (
        <div className="bg-surface px-3 py-1.5 text-xs font-mono">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-amber font-bold">{event.tool_name ?? "tool"}</span>
            {event.input_summary && (
              <span className="text-dim">
                {searchQuery
                  ? highlightText(event.input_summary, searchQuery)
                  : event.input_summary}
              </span>
            )}
          </div>
          {event.input_full != null && (
            <div className="mt-1">
              <ExpandableJson data={event.input_full} searchQuery={searchQuery} />
            </div>
          )}
        </div>
      );

    case "tool_result":
      return (
        <div className="bg-surface px-3 py-1.5 text-xs text-primary font-mono">
          <span className="text-sage mr-1" title="Result">
            &#8594;
          </span>
          <ExpandableText
            text={event.output ?? ""}
            maxLen={300}
            searchQuery={searchQuery}
          />
        </div>
      );

    case "result":
      return (
        <div className="bg-surface px-3 py-1.5 flex items-center gap-2 flex-wrap font-mono">
          {event.cost_usd != null && (
            <span className="text-xs text-muted tabular-nums">${event.cost_usd.toFixed(4)}</span>
          )}
          {event.num_turns != null && (
            <span className="text-xs text-muted tabular-nums">{event.num_turns} turns</span>
          )}
          {event.duration != null && (
            <span className="text-xs text-muted tabular-nums">{formatDuration(event.duration)}</span>
          )}
          {event.is_error && <Badge variant="failed">error</Badge>}
        </div>
      );

    case "rate_limit":
      return (
        <div className="bg-surface px-3 py-1.5 flex items-center gap-2 text-xs text-amber font-mono">
          <span title="Rate limit">&#9888;</span>
          <span>
            {searchQuery
              ? highlightText(event.message ?? "Rate limited", searchQuery)
              : event.message ?? "Rate limited"}
          </span>
        </div>
      );

    default:
      return (
        <div className="bg-surface px-3 py-1.5 text-xs text-dim font-mono">
          <span className="mr-1">?</span>
          <ExpandableText
            text={event.raw ?? JSON.stringify(event)}
            maxLen={200}
            searchQuery={searchQuery}
          />
        </div>
      );
  }
}

export function RunEvents({ events, searchQuery }: RunEventsProps) {
  if (events.length === 0) {
    return (
      <p className="font-mono text-xs text-muted text-center py-4">
        No events recorded
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-0.5">
      {events.map((event, i) => (
        <EventRow key={i} event={event} searchQuery={searchQuery} />
      ))}
    </div>
  );
}
