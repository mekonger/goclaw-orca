import { ArrowLeft } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { orderEmployees } from "./lib/office-layout";
import type { EmployeeState, LogEntry, SimulationState } from "./lib/simulation-reducer";

interface EmployeeActivityPanelProps {
  state: SimulationState;
  selected: string | null;
  onSelect: (agentKey: string | null) => void;
}

const LOG_ICON: Record<LogEntry["kind"], string> = { thinking: "💭", tool: "🔧", run: "▶" };

export function EmployeeActivityPanel({ state, selected, onSelect }: EmployeeActivityPanelProps) {
  const { t } = useTranslation("company-simulator");
  const nameOf = (key: string) => state.employees[key]?.displayName ?? key;
  const current = selected ? state.employees[selected] : undefined;

  const summary = (emp: EmployeeState) => (
    <>
      <span className="flex items-center gap-2">
        <span aria-hidden>{emp.emoji || "🙂"}</span>
        <span className="min-w-0 flex-1 truncate text-sm font-medium">{emp.displayName}</span>
        <Badge variant={emp.status === "working" ? "default" : "secondary"}>{t(`status.${emp.status}`)}</Badge>
      </span>
      <span className="mt-1 flex flex-wrap gap-1 empty:hidden">
        {state.delegations.map((d, i) =>
          d.from === emp.agentKey ? (
            <Badge key={i} variant="outline">{t("delegation.to", { name: nameOf(d.to) })}</Badge>
          ) : d.to === emp.agentKey ? (
            <Badge key={i} variant="outline">{t("delegation.from", { name: nameOf(d.from) })}</Badge>
          ) : null,
        )}
      </span>
      {emp.bubble && (
        <span className="mt-1 line-clamp-2 block text-xs text-muted-foreground">
          {LOG_ICON[emp.bubble.kind]} {emp.bubble.text}
        </span>
      )}
    </>
  );

  return (
    <section
      aria-label={t("panel.title")}
      className="max-h-[60dvh] overflow-y-auto overscroll-contain rounded-lg border bg-card p-2 lg:h-full lg:max-h-none"
    >
      {current ? (
        <div>
          <Button autoFocus variant="ghost" size="sm" className="mb-1 gap-1" onClick={() => onSelect(null)}>
            <ArrowLeft className="h-3.5 w-3.5" />
            {t("panel.back")}
          </Button>
          <div className="px-2 pb-2">{summary(current)}</div>
          {current.log.length === 0 ? (
            <p className="px-2 py-4 text-sm text-muted-foreground">{t("panel.noActivity")}</p>
          ) : (
            <ol className="space-y-1 px-2">
              {[...current.log].reverse().map((entry, i) => (
                <li key={`${entry.at}-${i}`} className="rounded-md bg-muted/50 px-2 py-1 text-xs">
                  <time className="mr-1 text-muted-foreground">{new Date(entry.at).toLocaleTimeString()}</time>
                  <span className="whitespace-pre-wrap break-words">
                    {LOG_ICON[entry.kind]} {entry.kind === "run" ? t(entry.text) : entry.text}
                  </span>
                </li>
              ))}
            </ol>
          )}
        </div>
      ) : (
        <>
          <h2 className="px-2 py-1 text-sm font-semibold">{t("panel.title")}</h2>
          {state.keys.size === 0 && (
            <p className="px-2 py-4 text-sm text-muted-foreground">{t("empty.noMembers")}</p>
          )}
          <ul className="space-y-1">
            {orderEmployees(Object.values(state.employees), state.leadKey).map((key) => {
              const emp = state.employees[key];
              if (!emp) return null;
              return (
                <li key={key}>
                  <button
                    type="button"
                    onClick={() => onSelect(key)}
                    className="block w-full rounded-md px-2 py-2 text-left hover:bg-accent focus-visible:outline-2"
                  >
                    {summary(emp)}
                  </button>
                </li>
              );
            })}
          </ul>
        </>
      )}
    </section>
  );
}
