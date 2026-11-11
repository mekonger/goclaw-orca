import type { OrcaAgentConfig, OrcaAgentTier } from "./hooks/use-orca-servers";

export interface OrcaAgentRow {
  id: string;
  key: string;
  agent: string;
  model: string;
  tier: OrcaAgentTier;
  description: string;
  enabled: boolean;
  fallback: boolean;
}

export type OrcaAgentsFormError =
  | "tooMany"
  | "keyInvalid"
  | "agentInvalid"
  | "modelInvalid"
  | "duplicateKey"
  | "fallbackRule"
  | "planningLimit"
  | "descriptionTooLong";

export interface OrcaAgentsValidation {
  error: OrcaAgentsFormError;
  row?: number;
}

export const ORCA_AGENTS_MAX = 10;
export const ORCA_AGENT_DESCRIPTION_MAX = 300;
export const ORCA_AGENT_TIERS: OrcaAgentTier[] = ["routine", "hard", "planning"];

const ID_RE = /^[a-z0-9][a-z0-9._-]{0,63}$/;
const MODEL_RE = /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/;

let rowSeq = 0;
const nextRowId = () => `row-${++rowSeq}`;

export function toOrcaAgentRows(agents: OrcaAgentConfig[] | undefined): OrcaAgentRow[] {
  return (agents ?? []).map((a) => ({
    id: nextRowId(),
    key: a.key,
    agent: a.agent,
    model: a.model ?? "",
    tier: a.tier,
    description: a.description ?? "",
    enabled: a.enabled,
    fallback: !!a.fallback,
  }));
}

export function emptyOrcaAgentRow(): OrcaAgentRow {
  return { id: nextRowId(), key: "", agent: "", model: "", tier: "routine", description: "", enabled: true, fallback: false };
}

export function setOrcaAgentFallback(rows: OrcaAgentRow[], index: number): OrcaAgentRow[] {
  return rows.map((r, i) => ({ ...r, fallback: i === index ? !r.fallback : false }));
}

export function moveOrcaAgentRow(rows: OrcaAgentRow[], index: number, delta: -1 | 1): OrcaAgentRow[] {
  const target = index + delta;
  if (target < 0 || target >= rows.length) return rows;
  const next = [...rows];
  [next[index], next[target]] = [next[target]!, next[index]!];
  return next;
}

export function validateOrcaAgents(rows: OrcaAgentRow[]): OrcaAgentsValidation | null {
  if (rows.length > ORCA_AGENTS_MAX) return { error: "tooMany" };
  const seen = new Set<string>();
  let fallbacks = 0;
  let planning = 0;
  for (const [i, r] of rows.entries()) {
    const row = i + 1;
    if (!ID_RE.test(r.key.trim())) return { error: "keyInvalid", row };
    if (!ID_RE.test(r.agent.trim())) return { error: "agentInvalid", row };
    if (r.model.trim() && !MODEL_RE.test(r.model.trim())) return { error: "modelInvalid", row };
    if (seen.has(r.key.trim())) return { error: "duplicateKey", row };
    seen.add(r.key.trim());
    if (r.description.length > ORCA_AGENT_DESCRIPTION_MAX) return { error: "descriptionTooLong", row };
    if (r.tier === "planning") planning++;
    if (r.fallback) {
      fallbacks++;
      if (!r.enabled || r.tier === "planning") return { error: "fallbackRule", row };
    }
  }
  if (fallbacks > 1) return { error: "fallbackRule" };
  if (planning > 1) return { error: "planningLimit" };
  return null;
}

export function buildOrcaAgentsPayload(rows: OrcaAgentRow[]): OrcaAgentConfig[] {
  return rows.map((r) => {
    const model = r.model.trim();
    return {
      key: r.key.trim(),
      agent: r.agent.trim(),
      ...(model ? { model } : {}),
      tier: r.tier,
      description: r.description.trim(),
      enabled: r.enabled,
      ...(r.fallback ? { fallback: true } : {}),
    };
  });
}
