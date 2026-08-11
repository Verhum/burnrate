"use client";

import { useEffect, useState } from "react";
import { client } from "@/lib/api/client";
import type { AchievementsData, Achievement } from "@/lib/api/types";

const ICON_MAP: Record<string, string> = {
  drop: "\u{1F4A7}",
  moon: "\u{1F319}",
  sun: "\u{2600}\u{FE0F}",
  shield: "\u{1F6E1}\u{FE0F}",
  star: "\u{2B50}",
  coin: "\u{1FA99}",
  flame: "\u{1F525}",
  trophy: "\u{1F3C6}",
  bolt: "\u{26A1}",
  rocket: "\u{1F680}",
  layers: "\u{1F4DA}",
  grid: "\u{1F300}",
  code: "\u{1F4BB}",
  wave: "\u{1F40B}",
};

export function UsageAchievements() {
  const [data, setData] = useState<AchievementsData | null>(null);

  useEffect(() => {
    client.getAchievements().then(setData).catch(() => {});
  }, []);

  if (!data || data.total === 0) return null;

  return (
    <div className="bg-surface px-4 py-3 min-w-0">
      <div className="flex items-center justify-between gap-2 mb-2">
        <p className="text-[9px] font-bold uppercase tracking-widest text-dim font-mono">
          ACHIEVEMENTS
        </p>
        <span className="text-[9px] font-mono text-muted shrink-0">
          {data.unlocked}/{data.total}
        </span>
      </div>

      <div className="flex flex-wrap gap-1.5">
        {data.achievements.map((a) => (
          <AchievementBadge key={a.id} achievement={a} />
        ))}
      </div>
    </div>
  );
}

function AchievementBadge({ achievement }: { achievement: Achievement }) {
  const [showTooltip, setShowTooltip] = useState(false);
  const icon = ICON_MAP[achievement.icon] ?? "\u{1F3C5}";

  return (
    <div
      className="relative"
      onMouseEnter={() => setShowTooltip(true)}
      onMouseLeave={() => setShowTooltip(false)}
    >
      <div
        className={`flex items-center justify-center w-[32px] h-[32px] text-[16px] transition-opacity ${
          achievement.unlocked
            ? "bg-elevated opacity-100"
            : "bg-raised opacity-30 grayscale"
        }`}
      >
        {icon}
      </div>

      {showTooltip && (
        <div className="absolute z-50 bottom-full left-1/2 -translate-x-1/2 mb-1.5 pointer-events-none">
          <div className="bg-raised px-2 py-1.5 whitespace-nowrap shadow-lg">
            <p
              className={`text-[10px] font-bold font-mono ${
                achievement.unlocked ? "text-gold" : "text-muted"
              }`}
            >
              {achievement.name}
            </p>
            <p className="text-[9px] font-mono text-dim">
              {achievement.description}
            </p>
            {achievement.unlocked && achievement.unlocked_at && (
              <p className="text-[8px] font-mono text-muted mt-0.5">
                {formatUnlockedAt(achievement.unlocked_at)}
              </p>
            )}
            {!achievement.unlocked &&
              achievement.progress != null &&
              achievement.progress > 0 && (
                <div className="mt-1">
                  <div className="w-full h-[3px] bg-elevated">
                    <div
                      className="h-full bg-warm transition-all"
                      style={{ width: `${Math.round(achievement.progress * 100)}%` }}
                    />
                  </div>
                  {achievement.progress_text && (
                    <p className="text-[8px] font-mono text-muted mt-0.5">
                      {achievement.progress_text}
                    </p>
                  )}
                </div>
              )}
          </div>
        </div>
      )}
    </div>
  );
}

function formatUnlockedAt(ts: string): string {
  try {
    return new Date(ts).toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
      year: "numeric",
    });
  } catch {
    return "";
  }
}
