import { useCallback, useEffect, useRef, useState } from "react";
import { Events } from "@/api/protocol";
import { useWsEvent } from "@/hooks/use-ws-event";
import { useAuthStore } from "@/stores/use-auth-store";
import { type TeamEventEntry, useTeamEventStore } from "@/stores/use-team-event-store";
import { useTeams } from "@/pages/teams/hooks/use-teams";
import { applyEvent, initState, type SimulationState } from "../lib/simulation-reducer";

const FLUSH_MS = 250;

const emptyState = () => initState([], "");

function sessionEvents(): TeamEventEntry[] {
  return useTeamEventStore.getState().events.filter((e) => e.timestamp >= performance.timeOrigin);
}

export function useCompanySimulation() {
  const { teams, loading, load, getTeam } = useTeams();
  const connected = useAuthStore((s) => s.connected);
  const [selectedTeamId, setTeamId] = useState<string | null>(null);
  const [state, setState] = useState<SimulationState>(emptyState);
  const queue = useRef<unknown[]>([]);
  const request = useRef(0);
  const pendingFresh = useRef(false);

  const teamId =
    (selectedTeamId && teams.some((t) => t.id === selectedTeamId)
      ? selectedTeamId
      : (teams.find((t) => t.status === "active") ?? teams[0])?.id) ?? null;

  useEffect(() => {
    load();
  }, [load]);

  const reload = useCallback(
    async (fresh: boolean) => {
      const id = ++request.current;
      pendingFresh.current ||= fresh;
      if (!teamId) {
        pendingFresh.current = false;
        setState(emptyState());
        return;
      }
      try {
        const res = await getTeam(teamId);
        if (id !== request.current) return;
        const members = res.members ?? [];
        const lead = res.team.lead_agent_id;
        if (pendingFresh.current) {
          pendingFresh.current = false;
          const events = sessionEvents();
          queue.current = [];
          setState((prev) => ({
            ...events.reduce(
              (s, e) => applyEvent(s, e.event, e.payload, e.timestamp),
              { ...initState(members, lead), nextTripSeq: prev.nextTripSeq },
            ),
            trips: [],
          }));
        } else {
          setState((prev) => initState(members, lead, prev));
        }
      } catch {
        if (id !== request.current || !pendingFresh.current) return;
        pendingFresh.current = false;
        setState(emptyState());
      }
    },
    [teamId, getTeam],
  );

  useEffect(() => {
    if (connected) reload(true);
  }, [connected, reload]);

  useWsEvent(Events.AGENT, (payload) => {
    queue.current.push(payload);
  });

  useEffect(() => {
    const timer = setInterval(() => {
      if (queue.current.length === 0) return;
      const batch = queue.current;
      queue.current = [];
      setState((s) => batch.reduce<SimulationState>((acc, p) => applyEvent(acc, Events.AGENT, p), s));
    }, FLUSH_MS);
    return () => clearInterval(timer);
  }, []);

  const onTeamChanged = (payload: unknown) => {
    const changed = (payload as { team_id?: string } | null)?.team_id;
    if (changed === teamId) reload(false);
  };
  useWsEvent(Events.TEAM_MEMBER_ADDED, onTeamChanged);
  useWsEvent(Events.TEAM_MEMBER_REMOVED, onTeamChanged);
  useWsEvent(Events.TEAM_UPDATED, onTeamChanged);
  useWsEvent(Events.TEAM_DELETED, load);
  useWsEvent(Events.TEAM_CREATED, load);

  return { teams, teamsLoading: loading, teamId, setTeamId, state };
}
