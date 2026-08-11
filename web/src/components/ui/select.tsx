import { forwardRef, type SelectHTMLAttributes } from "react";

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  label?: string;
  error?: string;
  options: { value: string; label: string }[];
}

export const Select = forwardRef<HTMLSelectElement, SelectProps>(
  ({ label, error, options, className = "", ...props }, ref) => (
    <div className="flex flex-col gap-1">
      {label && <label className="text-[9px] font-bold uppercase tracking-wider text-dim">{label}</label>}
      <select
        ref={ref}
        className={`bg-elevated text-primary px-3 py-2 text-xs font-mono border-none outline-none
          cursor-pointer focus:bg-raised transition-colors ${className}`}
        {...props}
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>{o.label}</option>
        ))}
      </select>
      {error && <span className="text-[10px] text-amber">{error}</span>}
    </div>
  )
);
Select.displayName = "Select";
