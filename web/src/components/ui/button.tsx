import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from "react";

type ButtonVariant =
  | "primary"
  | "secondary"
  | "accent"
  | "done"
  | "danger"
  | "ghost";
type ButtonSize = "sm" | "md";

/**
 * Amber accent shared by the header's "Add +" and the task list's "+ Add".
 * Exported so the header — which sets its own padding — stays in sync.
 */
export const accentButtonClasses =
  "bg-amber/15 text-amber hover:bg-amber hover:text-surface";

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  children: ReactNode;
}

const variantClasses: Record<ButtonVariant, string> = {
  primary: "bg-raised text-dim hover:bg-warm hover:text-primary",
  secondary: "bg-elevated text-muted hover:bg-raised hover:text-dim",
  accent: accentButtonClasses,
  done: "bg-sage text-surface hover:bg-sage/80",
  // Irreversible actions: delete a task, kill a live agent.
  danger: "bg-danger/15 text-danger hover:bg-danger hover:text-surface",
  ghost: "bg-transparent text-muted hover:text-dim hover:bg-elevated",
};

const sizeClasses: Record<ButtonSize, string> = {
  sm: "px-2.5 py-1 text-[9px]",
  md: "px-3 py-1.5 text-[10px]",
};

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ variant = "primary", size = "md", className = "", children, disabled, ...props }, ref) => (
    <button
      ref={ref}
      className={`font-mono font-bold uppercase tracking-wider cursor-pointer border-none transition-colors
        ${variantClasses[variant]} ${sizeClasses[size]}
        ${disabled ? "opacity-40 cursor-not-allowed" : ""}
        ${className}`}
      disabled={disabled}
      {...props}
    >
      {children}
    </button>
  )
);
Button.displayName = "Button";
