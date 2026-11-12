import { describe, expect, it } from "vitest";
import {
  buildOrcaAgentsPayload,
  emptyOrcaAgentRow,
  moveOrcaAgentRow,
  setOrcaAgentFallback,
  toOrcaAgentRows,
  validateOrcaAgents,
  type OrcaAgentRow,
} from "../orca-agents-helpers";

function row(overrides: Partial<OrcaAgentRow> = {}): OrcaAgentRow {
  return { ...emptyOrcaAgentRow(), key: "codex-fast", agent: "codex", model: "gpt-5", ...overrides };
}

const sample = (): OrcaAgentRow[] => [
  row({ fallback: true }),
  row({ key: "claude-hard", agent: "claude", model: "sonnet", tier: "hard" }),
  row({ key: "claude-plan", agent: "claude", model: "opus", tier: "planning" }),
  row({ key: "oc", agent: "opencode", model: "openai/gpt-5", enabled: false }),
];

describe("validateOrcaAgents", () => {
  it("accepts the sample list and an empty list", () => {
    expect(validateOrcaAgents(sample())).toBeNull();
    expect(validateOrcaAgents([])).toBeNull();
  });

  it.each([
    ["keyInvalid", { key: "Bad Key" }],
    ["agentInvalid", { agent: "-x" }],
    ["modelInvalid", { model: "--effort" }],
    ["descriptionTooLong", { description: "x".repeat(301) }],
  ] as const)("reports %s with the row number", (error, change) => {
    const rows = sample();
    rows[1] = { ...rows[1]!, ...change };
    expect(validateOrcaAgents(rows)).toEqual({ error, row: 2 });
  });

  it("rejects duplicate keys, bad fallback and two planning rows", () => {
    expect(validateOrcaAgents([row(), row()])?.error).toBe("duplicateKey");
    expect(validateOrcaAgents([row({ fallback: true, enabled: false })])?.error).toBe("fallbackRule");
    expect(validateOrcaAgents([row({ fallback: true, tier: "planning" })])?.error).toBe("fallbackRule");
    expect(validateOrcaAgents([row({ key: "a", fallback: true }), row({ key: "b", fallback: true })])?.error).toBe("fallbackRule");
    expect(validateOrcaAgents([row({ key: "a", tier: "planning" }), row({ key: "b", tier: "planning" })])?.error).toBe("planningLimit");
    expect(validateOrcaAgents(Array.from({ length: 11 }, (_, i) => row({ key: `k${i}` })))?.error).toBe("tooMany");
  });
});

describe("row helpers", () => {
  it("keeps at most one fallback and toggles it off", () => {
    const rows = setOrcaAgentFallback(sample(), 1);
    expect(rows.map((r) => r.fallback)).toEqual([false, true, false, false]);
    expect(setOrcaAgentFallback(rows, 1).some((r) => r.fallback)).toBe(false);
  });

  it("moves rows within bounds", () => {
    const rows = sample();
    expect(moveOrcaAgentRow(rows, 0, 1).map((r) => r.key).slice(0, 2)).toEqual(["claude-hard", "codex-fast"]);
    expect(moveOrcaAgentRow(rows, 0, -1)).toBe(rows);
  });

  it("round-trips the API shape and drops empty model and false fallback", () => {
    const payload = buildOrcaAgentsPayload([row({ key: " k ", model: " ", description: " d " }), row({ key: "f", fallback: true })]);
    expect(payload[0]).toEqual({ key: "k", agent: "codex", tier: "routine", description: "d", enabled: true });
    expect(payload[1]?.fallback).toBe(true);
    expect(payload[0]).not.toHaveProperty("id");
    expect(toOrcaAgentRows(payload)[0]).toMatchObject({ key: "k", agent: "codex", model: "", tier: "routine", description: "d", enabled: true, fallback: false });
  });
});
