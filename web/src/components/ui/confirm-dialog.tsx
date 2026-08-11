"use client";

import { useEffect, useRef } from "react";
import { Modal } from "./modal";
import { Button } from "./button";

interface ConfirmDialogProps {
  open: boolean;
  title: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  /** Styles the confirm button as destructive (delete, cancel a live run). */
  destructive?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * In-app confirmation. Replaces `window.confirm`, which returns `false`
 * synchronously in the Tauri webview — wry's `WKUIDelegate` never implements
 * `runJavaScriptConfirmPanel`, so WebKit shows nothing and answers "no".
 *
 * The same `web/out` bundle is also served by the Go binary at
 * localhost:9112, so this is deliberately not a native dialog: one path that
 * behaves identically in both hosts, rather than two to keep in sync.
 */
export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  destructive,
  onConfirm,
  onCancel,
}: ConfirmDialogProps) {
  const confirmRef = useRef<HTMLButtonElement>(null);

  // Modal has no focus trap, so without this the buttons are not
  // keyboard-reachable. Focusing confirm also makes Enter confirm, with
  // Escape cancelling via Modal's own handler.
  useEffect(() => {
    if (open) confirmRef.current?.focus();
  }, [open]);

  return (
    <Modal
      open={open}
      onClose={onCancel}
      title={title}
      size="sm"
      actions={
        <>
          <Button variant="secondary" onClick={onCancel}>
            {cancelLabel}
          </Button>
          <Button
            ref={confirmRef}
            variant={destructive ? "danger" : "primary"}
            onClick={onConfirm}
          >
            {confirmLabel}
          </Button>
        </>
      }
    >
      <p className="text-xs text-dim font-mono">{message}</p>
    </Modal>
  );
}
