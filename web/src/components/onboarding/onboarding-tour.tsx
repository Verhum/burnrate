"use client";

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { TOUR_STEPS } from "./tour-steps";

interface Rect {
  top: number;
  left: number;
  width: number;
  height: number;
}

interface OnboardingTourProps {
  open: boolean;
  step: number;
  onNext: () => void;
  onBack: () => void;
  onGoTo: (step: number) => void;
  /** Finish or skip — both end the tour and mark it done. */
  onDismiss: (reason: "finished" | "skipped") => void;
}

const SPOTLIGHT_PAD = 6;
const CARD_WIDTH = 380;
const CARD_GAP = 14;
const VIEWPORT_MARGIN = 12;
/** The spotlight target may still be mounting after a tab switch. */
const ANCHOR_RETRY_FRAMES = 30;

function findAnchor(anchor: string): HTMLElement | null {
  return document.querySelector<HTMLElement>(`[data-tour="${anchor}"]`);
}

/** Resolves the spotlight rect for a step, retrying while the target mounts. */
function useAnchorRect(anchor: string | undefined, open: boolean): Rect | null {
  // Keyed by anchor so a rect measured for the previous step is never reused.
  const [found, setFound] = useState<{ anchor: string; rect: Rect } | null>(
    null
  );

  useEffect(() => {
    if (!open || !anchor) return;

    let frame = 0;
    let raf = 0;
    let cancelled = false;

    const measure = (el: HTMLElement) => {
      const r = el.getBoundingClientRect();
      setFound({
        anchor,
        rect: { top: r.top, left: r.left, width: r.width, height: r.height },
      });
    };

    const locate = () => {
      if (cancelled) return;
      const el = findAnchor(anchor);
      if (el) {
        measure(el);
        return;
      }
      // Target not mounted yet (tab still switching) — keep looking briefly.
      if (frame++ < ANCHOR_RETRY_FRAMES) raf = requestAnimationFrame(locate);
    };
    // Measure after paint: the step may have just switched tabs.
    raf = requestAnimationFrame(locate);

    const track = () => {
      const el = findAnchor(anchor);
      if (el) measure(el);
    };
    window.addEventListener("resize", track);
    window.addEventListener("scroll", track, true);

    return () => {
      cancelled = true;
      cancelAnimationFrame(raf);
      window.removeEventListener("resize", track);
      window.removeEventListener("scroll", track, true);
    };
  }, [anchor, open]);

  return found && found.anchor === anchor ? found.rect : null;
}

/**
 * Places the card below the spotlight, flipping above it when space runs out
 * and finally overlapping it when the target is taller than the viewport.
 * The card is always fully on screen.
 */
function cardPosition(rect: Rect | null, cardHeight: number): React.CSSProperties {
  if (typeof window === "undefined" || !rect) {
    return {
      top: "50%",
      left: "50%",
      transform: "translate(-50%, -50%)",
      width: CARD_WIDTH,
    };
  }

  // Until the card has been measured, assume a typical two-paragraph step.
  const height = cardHeight || 240;
  const spotlightTop = rect.top - SPOTLIGHT_PAD;
  const spotlightBottom = rect.top + rect.height + SPOTLIGHT_PAD;
  const needed = height + CARD_GAP + VIEWPORT_MARGIN;

  let top: number;
  if (window.innerHeight - spotlightBottom >= needed) {
    top = spotlightBottom + CARD_GAP;
  } else if (spotlightTop >= needed) {
    top = spotlightTop - CARD_GAP - height;
  } else {
    top = Math.max(
      VIEWPORT_MARGIN,
      window.innerHeight - height - VIEWPORT_MARGIN
    );
  }

  const left = Math.min(
    Math.max(rect.left, VIEWPORT_MARGIN),
    Math.max(VIEWPORT_MARGIN, window.innerWidth - CARD_WIDTH - VIEWPORT_MARGIN)
  );

  return { top, left, width: CARD_WIDTH };
}

export function OnboardingTour({
  open,
  step,
  onNext,
  onBack,
  onGoTo,
  onDismiss,
}: OnboardingTourProps) {
  const current = TOUR_STEPS[step];
  const rect = useAnchorRect(current?.anchor, open);
  const cardRef = useRef<HTMLDivElement>(null);
  const [cardHeight, setCardHeight] = useState(0);

  // Steps differ in length, so the card is measured to keep it on screen.
  useLayoutEffect(() => {
    const h = cardRef.current?.offsetHeight ?? 0;
    if (h && h !== cardHeight) setCardHeight(h);
  }, [step, open, cardHeight]);

  const isFirst = step === 0;
  const isLast = step === TOUR_STEPS.length - 1;

  const handleKey = useCallback(
    (e: KeyboardEvent) => {
      if (!open) return;
      // Capture phase: keep the app's global shortcuts from firing behind us.
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        onDismiss("skipped");
        return;
      }
      if (e.key === "ArrowRight" || e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        e.stopPropagation();
        if (isLast) onDismiss("finished");
        else onNext();
        return;
      }
      if (e.key === "ArrowLeft") {
        e.preventDefault();
        e.stopPropagation();
        onBack();
        return;
      }
      // Swallow everything else so tab/filter keys don't move the app around.
      e.stopPropagation();
    },
    [open, isLast, onNext, onBack, onDismiss]
  );

  useEffect(() => {
    if (!open) return;
    document.addEventListener("keydown", handleKey, true);
    return () => document.removeEventListener("keydown", handleKey, true);
  }, [open, handleKey]);

  if (!open || !current) return null;

  return (
    <div className="fixed inset-0 z-50 font-mono">
      {/* Blocks interaction with the app while the tour is running. When there
          is no spotlight this also draws the dim; otherwise the spotlight's
          box-shadow does, so the two never stack. */}
      <div
        className={`absolute inset-0 ${rect ? "" : "bg-black/70"}`}
        onClick={() => onDismiss("skipped")}
      />

      {rect && (
        <div
          className="absolute pointer-events-none outline outline-1 outline-amber transition-all duration-200"
          style={{
            top: rect.top - SPOTLIGHT_PAD,
            left: rect.left - SPOTLIGHT_PAD,
            width: rect.width + SPOTLIGHT_PAD * 2,
            height: rect.height + SPOTLIGHT_PAD * 2,
            boxShadow: "0 0 0 9999px rgba(0, 0, 0, 0.7)",
          }}
        />
      )}

      <div
        ref={cardRef}
        className="absolute bg-surface max-w-[calc(100vw-24px)] max-h-[calc(100vh-24px)] overflow-y-auto"
        style={cardPosition(rect, cardHeight)}
        role="dialog"
        aria-modal="true"
        aria-label="Onboarding tutorial"
      >
        <div className="px-5 pt-4 pb-3 flex items-start gap-3">
          <div className="flex-1 min-w-0">
            <p className="text-[9px] font-bold uppercase tracking-widest text-amber mb-1.5">
              Tutorial · {String(step + 1).padStart(2, "0")} /{" "}
              {String(TOUR_STEPS.length).padStart(2, "0")}
            </p>
            <h2 className="text-[13px] font-bold text-primary tracking-wide">
              {current.title}
            </h2>
          </div>
          <button
            onClick={() => onDismiss("skipped")}
            aria-label="Close tutorial"
            className="text-muted hover:text-dim text-lg leading-none cursor-pointer bg-transparent border-none font-mono"
          >
            ×
          </button>
        </div>

        <div className="px-5 pb-3 flex flex-col gap-2">
          {current.body.map((paragraph) => (
            <p key={paragraph} className="text-[11px] leading-relaxed text-dim">
              {paragraph}
            </p>
          ))}

          {current.facts && (
            <div className="flex flex-col gap-1 mt-0.5">
              {current.facts.map((f) => (
                <div key={f.label} className="flex items-baseline gap-2">
                  <span className="text-[10px] font-bold uppercase tracking-wider text-primary bg-elevated px-1.5 py-0.5 min-w-[64px] text-center">
                    {f.label}
                  </span>
                  <span className="text-[10px] text-dim">{f.desc}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="px-5 pb-4 flex items-center gap-3">
          <div className="flex items-center gap-1">
            {TOUR_STEPS.map((s, i) => (
              <button
                key={s.id}
                onClick={() => onGoTo(i)}
                aria-label={`Go to step ${i + 1}: ${s.title}`}
                aria-current={i === step ? "step" : undefined}
                className={`w-1.5 h-1.5 border-none cursor-pointer transition-colors ${
                  i === step
                    ? "bg-amber"
                    : i < step
                      ? "bg-warm hover:bg-dim"
                      : "bg-raised hover:bg-warm"
                }`}
              />
            ))}
          </div>

          <div className="ml-auto flex items-center gap-1.5">
            {!isLast && (
              <button
                onClick={() => onDismiss("skipped")}
                className="px-2.5 py-1 text-[9px] font-bold uppercase tracking-wider font-mono
                  bg-transparent text-muted hover:text-dim hover:bg-elevated
                  border-none cursor-pointer transition-colors"
              >
                Skip
              </button>
            )}
            {!isFirst && (
              <button
                onClick={onBack}
                className="px-2.5 py-1 text-[9px] font-bold uppercase tracking-wider font-mono
                  bg-elevated text-muted hover:bg-raised hover:text-dim
                  border-none cursor-pointer transition-colors"
              >
                Back
              </button>
            )}
            <button
              onClick={() => (isLast ? onDismiss("finished") : onNext())}
              className="px-3 py-1 text-[9px] font-bold uppercase tracking-wider font-mono
                bg-amber/15 text-amber hover:bg-amber hover:text-surface
                border-none cursor-pointer transition-colors"
            >
              {isLast ? "Done" : "Next"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
