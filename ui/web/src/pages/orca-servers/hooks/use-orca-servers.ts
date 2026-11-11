import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import i18n from "@/i18n";

export type OrcaServerStatus = "unknown" | "healthy" | "unhealthy" | "error" | "cli_missing";

export type OrcaAgentTier = "routine" | "hard" | "planning";
export type OrcaAgentStatus = "unknown" | "healthy" | "unhealthy";

export interface OrcaAgentConfig {
  key: string;
  agent: string;
  model?: string;
  tier: OrcaAgentTier;
  description: string;
  enabled: boolean;
  fallback?: boolean;
}

export interface OrcaAgentHealth {
  status: OrcaAgentStatus;
  error?: string;
  checked_at: string;
  last_ok_at?: string | null;
}

export interface OrcaServer {
  id: string;
  tenant_id: string;
  label: string;
  description: string;
  has_pairing_code: boolean;
  last_status: OrcaServerStatus;
  last_runtime_state: string;
  last_error: string;
  last_checked_at: string | null;
  last_healthy_at: string | null;
  created_at: string;
  updated_at: string;
  created_by: string;
  agents?: OrcaAgentConfig[];
  agent_health?: Record<string, OrcaAgentHealth>;
}

export interface OrcaServerCreateInput {
  label: string;
  description?: string;
  pairing_code: string;
}

export interface OrcaServerUpdateInput {
  description?: string;
  pairing_code?: string;
}

const QUERY_KEY = ["orcaServers"] as const;
const REFRESH_INTERVAL = 30_000;
const BASE = "/v1/orca-servers";

function errMsg(err: unknown) {
  return err instanceof Error ? err.message : "";
}

export function useOrcaServers() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data, isLoading: loading } = useQuery({
    queryKey: QUERY_KEY,
    queryFn: async () => {
      const res = await http.get<{ items: OrcaServer[] }>(BASE);
      return res.items ?? [];
    },
    refetchInterval: REFRESH_INTERVAL,
    placeholderData: (prev) => prev,
  });

  const servers = data ?? [];

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: QUERY_KEY }),
    [queryClient],
  );

  const checkServer = useCallback(
    async (id: string, silent = false) => {
      try {
        await http.post<OrcaServer>(`${BASE}/${id}/check`, {});
        await invalidate();
        if (!silent) toast.success(i18n.t("orca-servers:toast.checked"));
      } catch (err) {
        toast.error(i18n.t("orca-servers:toast.checkFailed"), errMsg(err));
      }
    },
    [http, invalidate],
  );

  const checkAll = useCallback(async () => {
    await Promise.all(servers.map((s) => checkServer(s.id, true)));
    toast.success(i18n.t("orca-servers:toast.checkedAll"));
  }, [servers, checkServer]);

  const createServer = useCallback(
    async (input: OrcaServerCreateInput) => {
      try {
        const res = await http.post<OrcaServer>(BASE, input);
        await invalidate();
        toast.success(i18n.t("orca-servers:toast.created"));
        await checkServer(res.id, true);
        return res;
      } catch (err) {
        toast.error(i18n.t("orca-servers:toast.saveFailed"), errMsg(err));
        throw err;
      }
    },
    [http, invalidate, checkServer],
  );

  const updateServer = useCallback(
    async (id: string, input: OrcaServerUpdateInput) => {
      try {
        await http.put(`${BASE}/${id}`, input);
        await invalidate();
        toast.success(i18n.t("orca-servers:toast.updated"));
        if (input.pairing_code) await checkServer(id, true);
      } catch (err) {
        toast.error(i18n.t("orca-servers:toast.saveFailed"), errMsg(err));
        throw err;
      }
    },
    [http, invalidate, checkServer],
  );

  const updateAgents = useCallback(
    async (id: string, agents: OrcaAgentConfig[]) => {
      try {
        await http.put<OrcaServer>(`${BASE}/${id}/agents`, { agents });
        await invalidate();
        toast.success(i18n.t("orca-servers:toast.agentsSaved"));
      } catch (err) {
        toast.error(i18n.t("orca-servers:toast.saveFailed"), errMsg(err));
        throw err;
      }
    },
    [http, invalidate],
  );

  const deleteServer = useCallback(
    async (id: string) => {
      try {
        await http.delete(`${BASE}/${id}`);
        await invalidate();
        toast.success(i18n.t("orca-servers:toast.deleted"));
      } catch (err) {
        toast.error(i18n.t("orca-servers:toast.deleteFailed"), errMsg(err));
        throw err;
      }
    },
    [http, invalidate],
  );

  return { servers, loading, refresh: invalidate, createServer, updateServer, updateAgents, deleteServer, checkServer, checkAll };
}
