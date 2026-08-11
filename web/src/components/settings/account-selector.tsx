"use client";

import { useEffect, useState } from "react";
import { useConfigStore } from "@/stores/config-store";
import { Card, CardBody, Badge, Button, Spinner } from "@/components/ui";

function credentialStatus(account: {
  has_sandbox_keychain: boolean;
  has_credentials_file: boolean;
}): { label: string; variant: "running" | "queued" | "paused" } {
  if (account.has_sandbox_keychain) {
    return { label: "sandbox keychain", variant: "running" };
  }
  if (account.has_credentials_file) {
    return { label: "credentials file", variant: "queued" };
  }
  return { label: "no credentials", variant: "paused" };
}

export function AccountSelector() {
  const { accounts, fetchAccounts, selectAccount } = useConfigStore();
  const [switching, setSwitching] = useState<string | null>(null);

  useEffect(() => {
    fetchAccounts();
  }, [fetchAccounts]);

  async function handleSelect(configDir: string) {
    setSwitching(configDir);
    try {
      await selectAccount(configDir);
    } finally {
      setSwitching(null);
    }
  }

  if (accounts.length === 0) {
    return (
      <Card>
        <CardBody className="flex items-center justify-center py-8">
          <Spinner size="md" />
        </CardBody>
      </Card>
    );
  }

  return (
    <Card>
      <CardBody className="flex flex-col gap-0.5 py-3 px-4">
        <p className="text-[9px] font-bold uppercase tracking-widest text-muted font-mono mb-1">
          Accounts
        </p>
        {accounts.map((account) => {
          const cred = credentialStatus(account);
          const isActive = account.active;
          const isSwitching = switching === account.config_dir;

          return (
            <div
              key={account.config_dir}
              className={`flex items-center justify-between px-3 py-2 ${
                isActive ? "bg-elevated" : "bg-raised"
              }`}
            >
              <div className="flex flex-col gap-0.5 min-w-0">
                <div className="flex items-center gap-0.5">
                  <span className="text-[11px] font-mono text-primary truncate">
                    {account.label}
                  </span>
                  {isActive && (
                    <Badge variant="done">active</Badge>
                  )}
                </div>
                <div className="flex items-center gap-0.5">
                  <span className="text-[10px] font-mono text-muted truncate">
                    {account.keychain_suffix}
                  </span>
                  <Badge variant={cred.variant}>{cred.label}</Badge>
                </div>
              </div>
              {!isActive && (
                <Button
                  variant="primary"
                  size="sm"
                  disabled={isSwitching}
                  onClick={() => handleSelect(account.config_dir)}
                >
                  {isSwitching ? "Switching..." : "Use"}
                </Button>
              )}
            </div>
          );
        })}
      </CardBody>
    </Card>
  );
}
