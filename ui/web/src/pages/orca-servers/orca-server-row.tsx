import { Bot, Pencil, Play, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { formatRelativeTime } from "@/lib/format";
import type { OrcaServer } from "./hooks/use-orca-servers";
import { OrcaServerStatusBadge } from "./orca-server-status-badge";
import { OrcaAgentHealthBadge } from "./orca-agent-health-badge";

interface OrcaServerRowProps {
  server: OrcaServer;
  checking: boolean;
  onCheck: () => void;
  onEdit: () => void;
  onEditAgents: () => void;
  onDelete: () => void;
}

export function OrcaServerRow({ server, checking, onCheck, onEdit, onEditAgents, onDelete }: OrcaServerRowProps) {
  const { t } = useTranslation("orca-servers");
  const time = (v: string | null) => (v ? formatRelativeTime(v) : t("never"));

  return (
    <tr className="border-b last:border-0 hover:bg-muted/30 align-top">
      <td className="px-4 py-3 font-mono text-xs font-medium">{server.label}</td>
      <td className="px-4 py-3 text-muted-foreground">{server.description || "-"}</td>
      <td className="px-4 py-3">
        <div className="flex flex-col gap-1">
          <OrcaServerStatusBadge status={server.last_status} />
          {server.last_status === "cli_missing" && (
            <span className="max-w-[220px] text-xs text-muted-foreground">{t("cliMissingHint")}</span>
          )}
        </div>
      </td>
      <td className="px-4 py-3">
        {server.agents?.length ? (
          <div className="flex max-w-[260px] flex-wrap gap-1">
            {server.agents.map((a) => (
              <OrcaAgentHealthBadge key={a.key} label={a.key} health={server.agent_health?.[a.key]} disabled={!a.enabled} />
            ))}
          </div>
        ) : (
          <span className="block max-w-[200px] text-xs text-muted-foreground">{t("agents.emptyHint")}</span>
        )}
      </td>
      <td className="px-4 py-3 text-muted-foreground">{server.last_runtime_state || "-"}</td>
      <td className="px-4 py-3 text-muted-foreground">{time(server.last_checked_at)}</td>
      <td className="px-4 py-3 text-muted-foreground">{time(server.last_healthy_at)}</td>
      <td className="px-4 py-3 max-w-[260px]">
        {server.last_error ? (
          <span className="block truncate text-xs text-destructive" title={server.last_error}>
            {server.last_error}
          </span>
        ) : (
          "-"
        )}
      </td>
      <td className="px-4 py-3 text-right whitespace-nowrap">
        <Button variant="ghost" size="sm" onClick={onCheck} disabled={checking} className="gap-1">
          <Play className="h-3.5 w-3.5" />
          {t("checkNow")}
        </Button>
        <Button variant="ghost" size="sm" onClick={onEditAgents} className="gap-1">
          <Bot className="h-3.5 w-3.5" />
          {t("actions.agents")}
        </Button>
        <Button variant="ghost" size="sm" onClick={onEdit} className="gap-1">
          <Pencil className="h-3.5 w-3.5" />
          {t("actions.edit")}
        </Button>
        <Button variant="ghost" size="sm" onClick={onDelete} className="gap-1">
          <Trash2 className="h-3.5 w-3.5" />
          {t("actions.delete")}
        </Button>
      </td>
    </tr>
  );
}
