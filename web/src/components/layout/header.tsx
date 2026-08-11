"use client";

import { useSSE } from "@/hooks/use-sse";
import { useBurnState } from "@/hooks/use-burn-state";
import { burnColor } from "@/lib/burn-color";
import { copyToClipboard } from "@/lib/clipboard";
import { SUPPORT_EMAIL } from "@/lib/support";
import { toast } from "@/lib/toast";
import { accentButtonClasses } from "@/components/ui";

function copyEmail() {
  copyToClipboard(SUPPORT_EMAIL).then((ok) =>
    ok
      ? toast.success(`Copied ${SUPPORT_EMAIL}`)
      : toast.error("Couldn't copy email")
  );
}

export function Header({ onAddTask, onVoiceTask, isRecording }: { onAddTask?: () => void; onVoiceTask?: () => void; isRecording?: boolean }) {
  useSSE();

  const { pct } = useBurnState();

  // Logo ramps gray -> red in step with how much of the window is burned.
  const brandColor = burnColor(pct);

  return (
    <header className="flex items-center px-6 py-2.5 bg-surface gap-2.5">
      <div className="flex items-center gap-2.5">
        <div className="relative w-4 h-4">
          <span
            className="absolute top-[5px] left-0 w-4 h-[5px] block transition-colors duration-500"
            style={{ backgroundColor: brandColor }}
          />
          <span
            className="absolute top-0 left-[5px] w-[5px] h-4 block transition-colors duration-500"
            style={{ backgroundColor: brandColor }}
          />
        </div>
        <span
          className="text-[13px] font-bold tracking-wide transition-colors duration-500"
          style={{ color: brandColor }}
        >
          burnrate
        </span>
      </div>
      <div className="ml-auto flex items-center gap-2.5">
        <button
          onClick={copyEmail}
          title={`Copy ${SUPPORT_EMAIL}`}
          className="px-3 py-1 bg-transparent text-muted hover:text-dim hover:bg-elevated
            text-[10px] font-bold uppercase tracking-wider cursor-pointer border-none font-mono transition-colors"
        >
          Help
        </button>
        {onVoiceTask && (
          <button
            onClick={onVoiceTask}
            title={isRecording ? "Recording... click to view" : "Voice task"}
            className={`px-2 py-1 cursor-pointer border-none transition-colors ${
              isRecording
                ? "text-danger bg-elevated animate-pulse"
                : "bg-transparent text-muted hover:text-dim hover:bg-elevated"
            }`}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" aria-hidden>
              <rect x="9" y="2" width="6" height="12" rx="3" />
              <path d="M5 11a1 1 0 012 0 5 5 0 0010 0 1 1 0 012 0 7 7 0 01-6 6.93V21h3v2H8v-2h3v-3.07A7 7 0 015 11z" />
            </svg>
          </button>
        )}
        {onAddTask && (
          <button
            onClick={onAddTask}
            className={`px-3 py-1 ${accentButtonClasses}
              text-[10px] font-bold uppercase tracking-wider cursor-pointer border-none font-mono transition-colors`}
          >
            Add +
          </button>
        )}
      </div>
    </header>
  );
}
