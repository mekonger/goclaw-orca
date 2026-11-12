import type { TeamMemberData } from "@/types/team";
import type { EnrichedAgentEventPayload } from "@/types/team-events";

const BUBBLE_MAX = 160;
const LOG_MAX = 20;
const LOG_TEXT_MAX = 400;
const TRIPS_MAX = 50;

export type EmployeeStatus = "sleeping" | "working";

export interface Bubble {
  kind: "thinking" | "tool";
  text: string;
}

export interface LogEntry {
  at: number;
  kind: "thinking" | "tool" | "run";
  text: string;
}

export interface EmployeeState {
  agentId: string;
  agentKey: string;
  displayName: string;
  emoji?: string;
  role: TeamMemberData["role"];
  joinedAt?: string;
  status: EmployeeStatus;
  activeRuns: Record<string, number>;
  bubble: Bubble | null;
  log: LogEntry[];
}

export interface Delegation {
  runId: string;
  from: string;
  to: string;
}

export interface HandoffTrip {
  seq: number;
  from: string;
  to: string;
}

export interface SimulationState {
  employees: Record<string, EmployeeState>;
  keys: Map<string, string>;
  teamId: string | null;
  leadKey: string | null;
  delegations: Delegation[];
  trips: HandoffTrip[];
  nextTripSeq: number;
}

export function initState(
  members: TeamMemberData[],
  leadAgentId: string,
  prev?: SimulationState,
): SimulationState {
  const employees: Record<string, EmployeeState> = {};
  const keys = new Map<string, string>();
  let leadKey: string | null = null;
  for (const m of members) {
    const key = m.agent_key || m.agent_id;
    keys.set(m.agent_id, key);
    keys.set(key, key);
    if (m.agent_id === leadAgentId || (!leadKey && m.role === "lead")) leadKey = key;
    const old = prev?.keys.has(key) ? prev.employees[key] : undefined;
    employees[key] = {
      agentId: m.agent_id,
      agentKey: key,
      displayName: m.display_name || key,
      emoji: m.emoji,
      role: m.role,
      joinedAt: m.joined_at,
      status: old?.status ?? "sleeping",
      activeRuns: old?.activeRuns ?? {},
      bubble: old?.bubble ?? null,
      log: old?.log ?? [],
    };
  }
  return {
    employees,
    keys,
    teamId: members[0]?.team_id ?? null,
    leadKey,
    delegations: (prev?.delegations ?? []).filter((d) => keys.has(d.from) && keys.has(d.to)),
    trips: prev?.trips ?? [],
    nextTripSeq: prev?.nextTripSeq ?? 1,
  };
}

function pushLog(log: LogEntry[], entry: LogEntry): LogEntry[] {
  return [...log, entry].slice(-LOG_MAX);
}

function appendThinking(log: LogEntry[], text: string, at: number): LogEntry[] {
  const last = log[log.length - 1];
  if (last?.kind !== "thinking") return pushLog(log, { at, kind: "thinking", text: text.slice(-LOG_TEXT_MAX) });
  const merged = { ...last, text: (last.text + text).slice(-LOG_TEXT_MAX) };
  return [...log.slice(0, -1), merged];
}

function withEmployee(state: SimulationState, emp: EmployeeState): SimulationState {
  return { ...state, employees: { ...state.employees, [emp.agentKey]: emp } };
}

function resolveParent(state: SimulationState, ev: EnrichedAgentEventPayload, self: string): string | null {
  const parent = ev.parentAgentId
    ? state.keys.get(ev.parentAgentId)
    : ev.teamTaskId && (!ev.teamId || ev.teamId === state.teamId)
      ? state.leadKey
      : null;
  return parent && parent !== self ? parent : null;
}

function toolLabel(ev: EnrichedAgentEventPayload): string {
  const name = ev.payload?.name ?? "tool";
  const skill = name === "use_skill" ? ev.payload?.arguments?.name : undefined;
  return typeof skill === "string" && skill ? `${name}: ${skill}` : name;
}

export function applyEvent(
  state: SimulationState,
  eventName: string,
  payload: unknown,
  at: number = Date.now(),
): SimulationState {
  if (eventName !== "agent" || !payload || typeof payload !== "object") return state;
  const ev = payload as EnrichedAgentEventPayload;
  const key = state.keys.get(ev.agentId);
  if (!key) return state;
  const emp = state.employees[key];
  if (!emp) return state;
  const runId = ev.runId ?? "";

  switch (ev.type) {
    case "run.started": {
      const next = withEmployee(state, {
        ...emp,
        status: "working",
        activeRuns: { ...emp.activeRuns, [runId]: (emp.activeRuns[runId] ?? 0) + 1 },
        log: pushLog(emp.log, { at, kind: "run", text: ev.type }),
      });
      const parent = resolveParent(state, ev, key);
      if (!parent) return next;
      return {
        ...next,
        delegations: [...state.delegations, { runId, from: parent, to: key }],
        trips: [...state.trips, { seq: state.nextTripSeq, from: parent, to: key }].slice(-TRIPS_MAX),
        nextTripSeq: state.nextTripSeq + 1,
      };
    }
    case "thinking": {
      const content = ev.payload?.content;
      if (!content) return state;
      const prior = emp.bubble?.kind === "thinking" ? emp.bubble.text : "";
      return withEmployee(state, {
        ...emp,
        bubble: { kind: "thinking", text: (prior + content).slice(-BUBBLE_MAX) },
        log: appendThinking(emp.log, content, at),
      });
    }
    case "tool.call": {
      const text = toolLabel(ev);
      return withEmployee(state, {
        ...emp,
        bubble: { kind: "tool", text },
        log: pushLog(emp.log, { at, kind: "tool", text }),
      });
    }
    case "tool.result": {
      const failed = ev.payload?.is_error;
      if (emp.bubble?.kind !== "tool" && !failed) return state;
      return withEmployee(state, {
        ...emp,
        bubble: emp.bubble?.kind === "tool" ? null : emp.bubble,
        log: failed
          ? pushLog(emp.log, { at, kind: "tool", text: `${ev.payload?.name ?? "tool"} ✗` })
          : emp.log,
      });
    }
    case "run.completed":
    case "run.failed":
    case "run.cancelled": {
      const activeRuns = { ...emp.activeRuns };
      const left = (activeRuns[runId] ?? 0) - 1;
      if (left > 0) activeRuns[runId] = left;
      else delete activeRuns[runId];
      const idle = Object.keys(activeRuns).length === 0;
      const idx = state.delegations.findIndex((d) => d.runId === runId && d.to === key);
      const next = withEmployee(state, {
        ...emp,
        activeRuns,
        status: idle ? "sleeping" : "working",
        bubble: idle ? null : emp.bubble,
        log: pushLog(emp.log, { at, kind: "run", text: ev.type }),
      });
      return idx < 0 ? next : { ...next, delegations: state.delegations.filter((_, i) => i !== idx) };
    }
    default:
      return state;
  }
}
