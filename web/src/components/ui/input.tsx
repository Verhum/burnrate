import { forwardRef, type InputHTMLAttributes } from "react";

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  error?: string;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ label, error, className = "", ...props }, ref) => (
    <div className="flex flex-col gap-1">
      {label && <label className="text-[9px] font-bold uppercase tracking-wider text-dim">{label}</label>}
      <input
        ref={ref}
        className={`bg-elevated text-primary px-3 py-2 text-xs font-mono border-none outline-none
          placeholder:text-muted focus:bg-raised transition-colors ${className}`}
        {...props}
      />
      {error && <span className="text-[10px] text-amber">{error}</span>}
    </div>
  )
);
Input.displayName = "Input";
