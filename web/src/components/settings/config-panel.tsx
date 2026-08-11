"use client";

import { useEffect, useState } from "react";
import { useConfigStore } from "@/stores/config-store";
import { useOnboardingStore } from "@/stores/onboarding-store";
import { Card, CardBody, Button, Spinner } from "@/components/ui";
import { apiErrorMessage } from "@/lib/api/errors";
import { toast } from "@/lib/toast";

/** Settings with a dedicated control below, kept out of the raw key list. */
const HIDDEN_KEYS = new Set(["onboarding_completed"]);

/**
 * Keys GET /api/config reports that PUT rejects — see readOnlyConfigKeys in
 * internal/server/handlers_config.go. The handler refuses the whole request on
 * the first one, so sending them made Save fail with nothing persisted at all.
 */
const READONLY_KEYS = new Set(["base_code_dir", "port", "usage_url"]);

export function ConfigPanel() {
  const { config, fetchConfig, updateConfig } = useConfigStore();
  const startTour = useOnboardingStore((s) => s.start);
  const [edits, setEdits] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    fetchConfig();
  }, [fetchConfig]);

  useEffect(() => {
    if (config) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- sync prop to local state
      setEdits(Object.fromEntries(
        Object.entries(config)
          .filter(([k]) => !HIDDEN_KEYS.has(k))
          .map(([k, v]) => [k, String(v)])
      ));
    }
  }, [config]);

  if (!config) {
    return (
      <Card>
        <CardBody className="flex items-center justify-center py-8">
          <Spinner size="md" />
        </CardBody>
      </Card>
    );
  }

  const sortedKeys = Object.keys(edits).sort((a, b) =>
    a.localeCompare(b)
  );

  function handleChange(key: string, value: string) {
    setEdits((prev) => ({ ...prev, [key]: value }));
  }

  // Only what actually changed. handlePutConfig applies every key it receives
  // and rejects the whole request on the first read-only one, so a full-map PUT
  // failed with nothing persisted at all.
  const changed = Object.fromEntries(
    sortedKeys
      .filter((k) => !READONLY_KEYS.has(k) && edits[k] !== String(config[k]))
      .map((k) => [k, edits[k]])
  );
  const hasChanges = Object.keys(changed).length > 0;

  async function handleSave() {
    setSaving(true);
    try {
      await updateConfig(changed);
      toast.success("Config saved");
    } catch (err) {
      toast.error("Couldn't save config", apiErrorMessage(err));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card>
      <CardBody className="flex flex-col gap-0.5 py-3 px-4">
        <p className="text-[9px] font-bold uppercase tracking-widest text-muted font-mono mb-1">
          Configuration
        </p>

        {sortedKeys.length === 0 && (
          <p className="text-[10px] font-mono text-muted">No configuration keys found.</p>
        )}

        {sortedKeys.map((key) => (
          <div key={key} className="flex items-center gap-0.5">
            <span className="text-[9px] font-bold uppercase tracking-widest text-muted font-mono min-w-[200px]">
              {key}
            </span>
            <div className="flex-1">
              <input
                id={`config-${key}`}
                value={edits[key] ?? ""}
                readOnly={READONLY_KEYS.has(key)}
                title={
                  READONLY_KEYS.has(key)
                    ? "Takes effect only at startup — set it via env var or the settings table"
                    : undefined
                }
                onChange={(e) => handleChange(key, e.target.value)}
                className={`w-full px-3 py-1.5 text-[10px] font-mono border-none outline-none placeholder:text-muted transition-colors ${
                  READONLY_KEYS.has(key)
                    ? "bg-surface text-muted cursor-not-allowed"
                    : "bg-elevated text-dim focus:bg-raised"
                }`}
              />
            </div>
          </div>
        ))}

        <div className="flex items-center gap-0.5 mt-2">
          <span className="text-[9px] font-bold uppercase tracking-widest text-muted font-mono min-w-[200px]">
            tutorial
          </span>
          <div className="flex-1 flex items-center gap-3">
            <Button variant="secondary" size="sm" onClick={() => startTour("config")}>
              Replay tutorial
            </Button>
            <span className="text-[10px] font-mono text-muted">
              Walks through the queue, runs, usage, and these settings.
            </span>
          </div>
        </div>

        <div className="flex items-center justify-end mt-2">
          <Button
            variant="primary"
            size="sm"
            disabled={saving || !hasChanges}
            onClick={handleSave}
          >
            {saving ? "Saving..." : "Save"}
          </Button>
        </div>
      </CardBody>
    </Card>
  );
}
