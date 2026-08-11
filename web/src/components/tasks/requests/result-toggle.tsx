"use client";

import type { RequestResult } from "@/lib/api/types";

const OPTIONS: { value: RequestResult; label: string; active: string }[] = [
  { value: "pass", label: "Pass", active: "bg-sage text-surface" },
  { value: "fail", label: "Fail", active: "bg-danger text-surface" },
  { value: "blocked", label: "Blocked", active: "bg-amber text-surface" },
];

interface ResultToggleProps {
  value: RequestResult | null;
  onChange: (value: RequestResult | null) => void;
  disabled?: boolean;
}

/**
 * Pass / fail / blocked verdict for a demo response. Clicking the active one
 * clears it.
 *
 * Idle chips are `raised`, not `elevated`: this renders on both the task card
 * and the home-page banner's `elevated` panel, and an elevated chip on an
 * elevated panel is an invisible button.
 */
export function ResultToggle({ value, onChange, disabled }: ResultToggleProps) {
  return (
    <div className="flex items-center gap-px">
      {OPTIONS.map((opt) => (
        <button
          key={opt.value}
          type="button"
          disabled={disabled}
          aria-pressed={value === opt.value}
          onClick={() => onChange(value === opt.value ? null : opt.value)}
          className={`text-[9px] font-bold uppercase tracking-wider px-2.5 py-1 font-mono border-none
            cursor-pointer transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
              value === opt.value
                ? opt.active
                : "bg-raised text-dim hover:text-primary"
            }`}
        >
          {opt.label}
        </button>
      ))}
    </div>
  );
}
