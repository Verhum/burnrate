import { create } from "zustand";
import { client } from "@/lib/api/client";
import { useUsageStore } from "./usage-store";
import type { Config, Account } from "@/lib/api/types";

interface ConfigState {
  config: Config | null;
  accounts: Account[];

  fetchConfig: () => Promise<void>;
  updateConfig: (config: Config) => Promise<void>;
  /** Persists a single setting without touching the rest of the config. */
  setConfigValue: (
    key: string,
    value: string | number | boolean
  ) => Promise<void>;
  fetchAccounts: () => Promise<void>;
  selectAccount: (configDir: string) => Promise<void>;
}

export const useConfigStore = create<ConfigState>((set) => ({
  config: null,
  accounts: [],

  fetchConfig: async () => {
    try {
      const config = await client.getConfig();
      set({ config });
    } catch {
      // ignore
    }
  },

  updateConfig: async (config) => {
    await client.saveConfig(config);
    set({ config });
  },

  setConfigValue: async (key, value) => {
    await client.saveConfig({ [key]: value });
    set((s) => (s.config ? { config: { ...s.config, [key]: value } } : {}));
  },

  fetchAccounts: async () => {
    try {
      const resp = await client.getAccounts();
      set({ accounts: resp.accounts });
    } catch {
      // ignore
    }
  },

  selectAccount: async (configDir) => {
    useUsageStore.getState().clear();
    await client.selectAccount({ config_dir: configDir });
    const resp = await client.getAccounts();
    set({ accounts: resp.accounts });
    useUsageStore.getState().fetchUsage();
    useUsageStore.getState().fetchStatus();
  },
}));
