import type { ReactNode } from "react";

interface CardProps {
  className?: string;
  children: ReactNode;
}

export function Card({ className = "", children }: CardProps) {
  return <div className={`bg-surface ${className}`}>{children}</div>;
}

export function CardHeader({ className = "", children }: CardProps) {
  return <div className={`px-6 py-3 ${className}`}>{children}</div>;
}

export function CardBody({ className = "", children }: CardProps) {
  return <div className={`px-6 py-4 ${className}`}>{children}</div>;
}

export function CardFooter({ className = "", children }: CardProps) {
  return <div className={`px-6 py-3 ${className}`}>{children}</div>;
}
