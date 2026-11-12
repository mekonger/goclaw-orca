import { describe, expect, it } from "vitest";
import type { TeamMemberData } from "@/types/team";
import { applyEvent, initState, type SimulationState } from "./simulation-reducer";

const member = (key: string, role: TeamMemberData["role"] = "member"): TeamMemberData => ({
  team_id: "t1",
  agent_id: `uuid-${key}`,
  agent_key: key,
  display_name: key.toUpperCase(),
  role,
});

const members = [member("lead", "lead"), member("a"), member("b"), member("c")];
const fresh = () => initState(members, "uuid-lead");

const run = (state: SimulationState, events: Record<string, unknown>[]) =>
  events.reduce((s, e) => applyEvent(s, "agent", e, 1), state);

const ev = (type: string, agentId: string, runId: string, extra: Record<string, unknown> = {}) => ({
  type,
  agentId,
  runId,
  ...extra,
});

describe("simulation reducer", () => {
  it("wakes on run.started and sleeps on run.completed", () => {
    const working = run(fresh(), [ev("run.started", "a", "r1")]);
    expect(working.employees.a?.status).toBe("working");
    const done = run(working, [ev("run.completed", "a", "r1")]);
    expect(done.employees.a?.status).toBe("sleeping");
    expect(done.employees.a?.bubble).toBeNull();
  });

  it("stays working until every overlapping run ends", () => {
    let s = run(fresh(), [ev("run.started", "a", "r1"), ev("run.started", "a", "r2"), ev("run.failed", "a", "r1")]);
    expect(s.employees.a?.status).toBe("working");
    s = run(s, [ev("run.cancelled", "a", "r2")]);
    expect(s.employees.a?.status).toBe("sleeping");
  });

  it("counts concurrent runs that share one runId", () => {
    const started = [ev("run.started", "b", "teammate-a-b"), ev("run.started", "b", "teammate-a-b")];
    let s = run(fresh(), [...started, ev("run.completed", "b", "teammate-a-b")]);
    expect(s.employees.b?.status).toBe("working");
    s = run(s, [ev("run.completed", "b", "teammate-a-b")]);
    expect(s.employees.b?.status).toBe("sleeping");
  });

  it("creates one delegation and one trip for a delegate run, and drops the link when it ends", () => {
    const started = run(fresh(), [ev("run.started", "b", "d1", { parentAgentId: "a" })]);
    expect(started.delegations).toEqual([{ runId: "d1", from: "a", to: "b" }]);
    expect(started.trips).toEqual([{ seq: 1, from: "a", to: "b" }]);
    for (const end of ["run.completed", "run.failed"]) {
      const done = run(started, [ev(end, "b", "d1")]);
      expect(done.delegations).toEqual([]);
      expect(done.trips).toHaveLength(1);
    }
  });

  it("queues one trip per parallel delegation with increasing seq", () => {
    const s = run(
      fresh(),
      ["a", "b", "c"].map((k) => ev("run.started", k, `d-${k}`, { parentAgentId: "lead" })),
    );
    expect(s.delegations.map((d) => d.to)).toEqual(["a", "b", "c"]);
    expect(s.trips.map((t) => t.seq)).toEqual([1, 2, 3]);
    expect(s.trips.every((t) => t.from === "lead")).toBe(true);
  });

  it("treats the lead as delegator of team task runs, except its own", () => {
    const s = run(fresh(), [
      ev("run.started", "a", "t1", { teamTaskId: "task-1", teamId: "t1" }),
      ev("run.started", "lead", "t2", { teamTaskId: "task-2" }),
    ]);
    expect(s.delegations).toEqual([{ runId: "t1", from: "lead", to: "a" }]);
  });

  it("ignores team task runs that belong to another team", () => {
    const s = run(fresh(), [ev("run.started", "a", "t1", { teamTaskId: "task-1", teamId: "t2" })]);
    expect(s.delegations).toEqual([]);
    expect(s.employees.a?.status).toBe("working");
  });

  it("ignores a parent that is not a team member", () => {
    const s = run(fresh(), [ev("run.started", "a", "r1", { parentAgentId: "outsider" })]);
    expect(s.delegations).toEqual([]);
    expect(s.employees.a?.status).toBe("working");
  });

  it("resolves agentId given as UUID", () => {
    const s = run(fresh(), [ev("run.started", "uuid-a", "r1", { parentAgentId: "uuid-lead" })]);
    expect(s.employees.a?.status).toBe("working");
    expect(s.delegations).toEqual([{ runId: "r1", from: "lead", to: "a" }]);
  });

  it("returns the same state for non-members and non-agent events", () => {
    const s = fresh();
    expect(applyEvent(s, "agent", ev("run.started", "stranger", "r1"))).toBe(s);
    expect(applyEvent(s, "team.updated", { team_id: "t1" })).toBe(s);
    expect(applyEvent(s, "agent", ev("chunk", "a", "r1"))).toBe(s);
  });

  it("appends and truncates thinking, and merges it into one log entry", () => {
    const s = run(fresh(), [
      ev("thinking", "a", "r1", { payload: { content: "x".repeat(150) } }),
      ev("thinking", "a", "r1", { payload: { content: "y".repeat(50) } }),
    ]);
    expect(s.employees.a?.bubble?.kind).toBe("thinking");
    expect(s.employees.a?.bubble?.text).toHaveLength(160);
    expect(s.employees.a?.bubble?.text.endsWith("y".repeat(50))).toBe(true);
    expect(s.employees.a?.log).toHaveLength(1);
  });

  it("shows a tool bubble and clears it on tool.result", () => {
    let s = run(fresh(), [
      ev("tool.call", "a", "r1", { payload: { name: "use_skill", arguments: { name: "pdf" } } }),
    ]);
    expect(s.employees.a?.bubble).toEqual({ kind: "tool", text: "use_skill: pdf" });
    s = run(s, [ev("tool.result", "a", "r1", { payload: { name: "use_skill", is_error: true } })]);
    expect(s.employees.a?.bubble).toBeNull();
    expect(s.employees.a?.log.slice(-1)[0]?.text).toBe("use_skill ✗");
  });

  it("keeps employee state across a member reload and drops links to removed members", () => {
    const s = run(fresh(), [ev("run.started", "b", "d1", { parentAgentId: "a" })]);
    const grown = initState([...members, member("d")], "uuid-lead", s);
    expect(grown.employees.b?.status).toBe("working");
    expect(grown.employees.d?.status).toBe("sleeping");
    expect(grown.delegations).toHaveLength(1);
    const shrunk = initState(members.filter((m) => m.agent_key !== "a"), "uuid-lead", s);
    expect(shrunk.delegations).toEqual([]);
  });
});
