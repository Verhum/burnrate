"use client";

import { useState, useEffect, useRef, useCallback } from "react";
import type { Task, ModelInfo } from "@/lib/api/types";
import { Button, Input, Textarea, Select, Modal } from "@/components/ui";
import { useTaskStore } from "@/stores/task-store";
import { client } from "@/lib/api/client";

interface PendingFile {
  file: File;
  previewUrl: string;
}

const DRAFT_KEY = "burnrate:task-form-draft";

interface Draft {
  title: string;
  prompt: string;
  model: string;
  backlog: boolean;
}

function loadDraft(): Draft | null {
  try {
    const raw = sessionStorage.getItem(DRAFT_KEY);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}

function saveDraft(draft: Draft) {
  try {
    sessionStorage.setItem(DRAFT_KEY, JSON.stringify(draft));
  } catch {}
}

function clearDraft() {
  try {
    sessionStorage.removeItem(DRAFT_KEY);
  } catch {}
}

interface TaskFormProps {
  open: boolean;
  onClose: () => void;
  editTask?: Task | null;
}

export function TaskForm({ open, onClose, editTask }: TaskFormProps) {
  const { createTask, updateTask } = useTaskStore();
  const [title, setTitle] = useState("");
  const [prompt, setPrompt] = useState("");
  const [model, setModel] = useState("");
  const [models, setModels] = useState<ModelInfo[]>([]);
  const [backlog, setBacklog] = useState(false);
  const [saving, setSaving] = useState(false);
  const isCreate = open && !editTask;
  const draftReady = useRef(false);
  const [pendingFiles, setPendingFiles] = useState<PendingFile[]>([]);
  const [dragOver, setDragOver] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    client.getModels().then(setModels).catch(() => {});
  }, []);

  useEffect(() => {
    if (open) {
      /* eslint-disable react-hooks/set-state-in-effect -- reset form state when dialog opens */
      if (editTask) {
        draftReady.current = false;
        setTitle(editTask.title);
        setPrompt(editTask.prompt || "");
        setModel(editTask.model || "");
        setBacklog(editTask.status === "backlog");
      } else {
        const draft = loadDraft();
        setTitle(draft?.title ?? "");
        setPrompt(draft?.prompt ?? "");
        setModel(draft?.model ?? "");
        setBacklog(draft?.backlog ?? false);
        draftReady.current = true;
      }
      setPendingFiles((prev) => {
        prev.forEach((p) => URL.revokeObjectURL(p.previewUrl));
        return [];
      });
      /* eslint-enable react-hooks/set-state-in-effect */
    } else {
      draftReady.current = false;
    }
  }, [open, editTask]);

  useEffect(() => {
    if (isCreate && draftReady.current) {
      saveDraft({ title, prompt, model, backlog });
    }
    return () => {
      pendingFiles.forEach((p) => URL.revokeObjectURL(p.previewUrl));
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- cleanup only on unmount
  }, [isCreate, title, prompt, model, backlog]);

  const addFiles = useCallback((files: File[]) => {
    const imageFiles = files.filter((f) => f.type.startsWith("image/"));
    if (imageFiles.length === 0) return;
    setPendingFiles((prev) => [
      ...prev,
      ...imageFiles.map((file) => ({
        file,
        previewUrl: URL.createObjectURL(file),
      })),
    ]);
  }, []);

  const removeFile = useCallback((index: number) => {
    setPendingFiles((prev) => {
      const removed = prev[index];
      if (removed) URL.revokeObjectURL(removed.previewUrl);
      return prev.filter((_, i) => i !== index);
    });
  }, []);

  const handlePaste = useCallback(
    (e: React.ClipboardEvent) => {
      const items = e.clipboardData?.items;
      if (!items) return;
      const files: File[] = [];
      for (const item of items) {
        if (item.type.startsWith("image/")) {
          const file = item.getAsFile();
          if (file) files.push(file);
        }
      }
      if (files.length > 0) {
        e.preventDefault();
        addFiles(files);
      }
    },
    [addFiles],
  );

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setDragOver(false);
      const files: File[] = [];
      if (e.dataTransfer.files) {
        for (const file of e.dataTransfer.files) {
          files.push(file);
        }
      }
      addFiles(files);
    },
    [addFiles],
  );

  const handleFileSelect = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = e.target.files;
    if (files) {
      addFiles(Array.from(files));
    }
    e.target.value = "";
  };

  const handleSave = async () => {
    if (!title.trim()) return;
    setSaving(true);
    try {
      if (editTask) {
        await updateTask(editTask.id, {
          title: title.trim(),
          prompt,
          model: model || undefined,
        });
        if (pendingFiles.length > 0) {
          await Promise.all(
            pendingFiles.map((p) =>
              client.uploadAttachment(editTask.id, p.file),
            ),
          );
        }
      } else {
        const task = await createTask({
          title: title.trim(),
          prompt,
          model: model || undefined,
          status: backlog ? "backlog" : undefined,
        });
        clearDraft();
        if (pendingFiles.length > 0) {
          await Promise.all(
            pendingFiles.map((p) => client.uploadAttachment(task.id, p.file)),
          );
        }
      }
      onClose();
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={
        editTask
          ? `Edit ${editTask.display_id || "BR" + editTask.id}`
          : "Add Task"
      }
      actions={
        <>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            variant="primary"
            onClick={handleSave} data-keyboard-submit
            disabled={saving || !title.trim()}
          >
            {saving ? "Saving..." : editTask ? "Save" : "Create"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <label className="text-[9px] font-bold uppercase tracking-wider text-dim">Title</label>
          <Input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="Task title"
          />
        </div>
        <div className="flex flex-col gap-1">
          <label className="text-[9px] font-bold uppercase tracking-wider text-dim">Prompt</label>
          <Textarea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            onPaste={handlePaste}
            placeholder="Instructions for the agent — name the repo(s) to work in... (paste images here)"
            rows={6}
          />
        </div>
        <p className="text-[10px] text-muted -mt-1">
          Effort defaults to level 3 — implement and verify. Add{" "}
          <span className="font-mono text-dim">LOE: 1</span> (investigate),{" "}
          <span className="font-mono text-dim">2</span> (write the code), or{" "}
          <span className="font-mono text-dim">4</span> (validate end to end)
          anywhere above to pin it.
        </p>

        <Select
          label="Model"
          value={model}
          onChange={(e) => setModel(e.target.value)}
          options={[
            { value: "", label: "Default (from config)" },
            ...models.map((m) => ({ value: m.id, label: m.name })),
          ]}
        />

        <div>
          <label className="block text-[9px] font-bold uppercase tracking-widest text-dim mb-1">
            Attachments
          </label>
          <div
            className={`p-3 transition-colors ${
              dragOver
                ? "bg-elevated"
                : "bg-surface"
            }`}
            onDragOver={(e) => {
              e.preventDefault();
              setDragOver(true);
            }}
            onDragLeave={(e) => {
              e.preventDefault();
              setDragOver(false);
            }}
            onDrop={handleDrop}
          >
            {pendingFiles.length > 0 && (
              <div className="grid grid-cols-4 gap-0.5 mb-3">
                {pendingFiles.map((p, i) => (
                  <div
                    key={p.previewUrl}
                    className="relative group bg-elevated overflow-hidden"
                  >
                    <div className="aspect-square bg-surface flex items-center justify-center">
                      {/* eslint-disable-next-line @next/next/no-img-element */}
                      <img
                        src={p.previewUrl}
                        alt={p.file.name}
                        className="w-full h-full object-cover"
                      />
                    </div>
                    <div className="px-2 py-1">
                      <p
                        className="text-[9px] text-dim truncate"
                        title={p.file.name}
                      >
                        {p.file.name}
                      </p>
                    </div>
                    <button
                      type="button"
                      onClick={() => removeFile(i)}
                      className="absolute top-1 right-1 w-5 h-5 bg-surface text-dim text-xs flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity cursor-pointer border-none hover:bg-raised"
                      aria-label={`Remove ${p.file.name}`}
                    >
                      &times;
                    </button>
                  </div>
                ))}
              </div>
            )}

            <div className="flex items-center justify-between">
              <p className="text-[9px] text-muted uppercase tracking-wider">
                {pendingFiles.length === 0
                  ? "Drop images here, paste into the prompt, or click upload"
                  : `${pendingFiles.length} file${pendingFiles.length === 1 ? "" : "s"} selected`}
              </p>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={() => fileInputRef.current?.click()}
              >
                Upload image
              </Button>
            </div>
          </div>
          <input
            ref={fileInputRef}
            type="file"
            accept="image/*"
            multiple
            className="hidden"
            onChange={handleFileSelect}
          />
        </div>

        {!editTask && (
          <label className="flex items-center gap-2 text-[13px] text-dim cursor-pointer">
            <input
              type="checkbox"
              checked={backlog}
              onChange={(e) => setBacklog(e.target.checked)}
              className="accent-amber"
            />
            Add to backlog
          </label>
        )}
      </div>
    </Modal>
  );
}
