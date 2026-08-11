import { forwardRef, type TextareaHTMLAttributes } from "react";

interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: string;
  error?: string;
}

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(
  ({ label, error, className = "", ...props }, ref) => (
    <div className="flex flex-col gap-1">
      {label && <label className="text-[9px] font-bold uppercase tracking-wider text-dim">{label}</label>}
      <textarea
        ref={ref}
        className={`bg-elevated text-primary px-3 py-2 text-xs font-mono border-none outline-none
          resize-y min-h-[80px] placeholder:text-muted focus:bg-raised transition-colors ${className}`}
        {...props}
      />
      {error && <span className="text-[10px] text-amber">{error}</span>}
    </div>
  )
);
Textarea.displayName = "Textarea";
