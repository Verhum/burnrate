"use client";

interface ToggleProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label?: string;
  disabled?: boolean;
}

export function Toggle({ checked, onChange, label, disabled }: ToggleProps) {
  return (
    <label className={`inline-flex items-center gap-2 cursor-pointer ${disabled ? "opacity-40" : ""}`}>
      <button
        role="switch"
        aria-checked={checked}
        onClick={() => !disabled && onChange(!checked)}
        className={`relative w-9 h-5 transition-colors border-none cursor-pointer
          ${checked ? "bg-amber" : "bg-raised"}`}
      >
        <span
          className={`absolute top-0.5 w-4 h-4 bg-primary transition-transform
            ${checked ? "left-[18px]" : "left-0.5"}`}
        />
      </button>
      {label && <span className="text-xs text-dim">{label}</span>}
    </label>
  );
}
