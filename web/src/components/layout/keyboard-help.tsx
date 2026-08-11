"use client";

interface KeyboardHelpProps {
  open: boolean;
  onClose: () => void;
}

const SECTIONS = [
  {
    title: "NAVIGATION",
    keys: [
      { key: "j / ↓", desc: "Next task" },
      { key: "k / ↑", desc: "Previous task" },
      { key: "Enter", desc: "Open task" },
      { key: "Esc", desc: "Go back / close" },
      { key: "1-4", desc: "Switch tabs" },
    ],
  },
  {
    title: "ACTIONS",
    keys: [
      { key: "n", desc: "New task" },
      { key: "⌘ Enter", desc: "Submit form" },
      { key: "r", desc: "Run now (in detail)" },
    ],
  },
  {
    title: "FILTERS",
    keys: [
      { key: "a", desc: "Active" },
      { key: "c", desc: "Completed" },
      { key: "f", desc: "Failed" },
    ],
  },
  {
    title: "STATUS (IN DETAIL)",
    keys: [
      { key: "q", desc: "Queue" },
      { key: "d", desc: "Done" },
      { key: "x", desc: "Dismiss" },
      { key: "b", desc: "Backlog" },
    ],
  },
];

export function KeyboardHelp({ open, onClose }: KeyboardHelpProps) {
  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/60" onClick={onClose} />
      <div className="relative bg-surface w-full max-w-md mx-4">
        <div className="px-6 py-4 flex items-center justify-between">
          <h2 className="text-[11px] font-bold uppercase tracking-widest text-dim">
            Keyboard Shortcuts
          </h2>
          <button
            onClick={onClose}
            className="text-muted hover:text-dim text-lg cursor-pointer bg-transparent border-none font-mono"
          >
            ×
          </button>
        </div>
        <div className="px-6 pb-5 grid grid-cols-2 gap-4">
          {SECTIONS.map((section) => (
            <div key={section.title}>
              <p className="text-[8px] font-bold tracking-widest text-muted uppercase mb-2">
                {section.title}
              </p>
              <div className="flex flex-col gap-1.5">
                {section.keys.map((k) => (
                  <div key={k.key} className="flex items-center gap-2">
                    <kbd className="text-[10px] font-mono text-primary bg-elevated px-1.5 py-0.5 min-w-[56px] text-center">
                      {k.key}
                    </kbd>
                    <span className="text-[10px] text-dim">{k.desc}</span>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
        <div className="px-6 pb-4">
          <span className="text-[9px] text-muted">Press <kbd className="text-[9px] font-mono text-dim bg-elevated px-1 py-0.5">?</kbd> to toggle</span>
        </div>
      </div>
    </div>
  );
}
