interface ProgressProps {
  value: number;
  max?: number;
  className?: string;
}

export function Progress({ value, max = 100, className = "" }: ProgressProps) {
  const pct = Math.min(100, Math.max(0, (value / max) * 100));
  return (
    <div className={`relative h-2.5 bg-elevated overflow-hidden ${className}`}>
      <div
        className="absolute inset-y-0 left-0 bg-warm transition-[width] duration-1000 ease-out"
        style={{ width: `${pct}%` }}
      />
    </div>
  );
}
