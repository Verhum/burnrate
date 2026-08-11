"use client";

import { MarkdownBody } from "@/components/ui";
import type { DemoBrief as DemoBriefData } from "@/lib/human-requests";

/**
 * The structured half of a `demo` request: what to do, what should happen, and
 * how to get the thing running again now that the agent's run — and its dev
 * server — are gone.
 */
export function DemoBrief({ brief }: { brief: DemoBriefData }) {
  const { steps, expected, look_for, url, revival_steps } = brief;

  return (
    <div className="flex flex-col gap-2 mb-2">
      {steps.length > 0 && (
        <ol className="flex flex-col gap-1 list-none m-0 p-0">
          {steps.map((step, i) => (
            <li key={i} className="flex gap-2 text-xs text-dim">
              <span className="text-muted font-mono tabular-nums shrink-0">
                {String(i + 1).padStart(2, "0")}
              </span>
              <MarkdownBody className="min-w-0">{step}</MarkdownBody>
            </li>
          ))}
        </ol>
      )}

      {url && (
        <div className="text-xs">
          <span className="text-muted">URL </span>
          <a
            href={url}
            target="_blank"
            rel="noopener noreferrer"
            className="text-sage font-mono text-[11px] hover:text-primary break-all"
          >
            {url} ↗
          </a>
        </div>
      )}

      {expected && (
        <div className="text-xs">
          <span className="text-muted">Expected </span>
          <MarkdownBody className="text-dim inline-block align-top">
            {expected}
          </MarkdownBody>
        </div>
      )}

      {look_for && (
        <div className="text-xs">
          <span className="text-muted">Look for </span>
          <MarkdownBody className="text-dim inline-block align-top">
            {look_for}
          </MarkdownBody>
        </div>
      )}

      {revival_steps && (
        <div className="bg-surface px-3 py-2">
          <div className="text-[9px] font-bold uppercase tracking-widest text-muted mb-1">
            Bring it up
          </div>
          <pre className="text-[11px] text-dim font-mono whitespace-pre-wrap m-0">
            {[
              revival_steps.cwd ? `cd ${revival_steps.cwd}` : null,
              revival_steps.command,
              revival_steps.port ? `# → http://localhost:${revival_steps.port}` : null,
            ]
              .filter(Boolean)
              .join("\n")}
          </pre>
        </div>
      )}
    </div>
  );
}
