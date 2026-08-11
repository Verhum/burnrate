type SpinnerSize = "sm" | "md" | "lg";

const sizeMap: Record<SpinnerSize, string> = {
  sm: "w-3 h-3",
  md: "w-5 h-5",
  lg: "w-8 h-8",
};

export function Spinner({ size = "md" }: { size?: SpinnerSize }) {
  return (
    <div
      className={`${sizeMap[size]} border-2 border-raised border-t-dim animate-spin`}
      style={{ borderRadius: "50% !important" }}
    />
  );
}
