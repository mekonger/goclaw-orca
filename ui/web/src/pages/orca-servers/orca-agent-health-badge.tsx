import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { formatRelativeTime } from "@/lib/format";
import type { OrcaAgentHealth, OrcaAgentStatus } from "./hooks/use-orca-servers";

const VARIANTS = {
  healthy: "success",
  unhealthy: "warning",
  unknown: "outline",
} as const;

interface OrcaAgentHealthBadgeProps {
  label: string;
  health?: OrcaAgentHealth;
  disabled?: boolean;
}

export function OrcaAgentHealthBadge({ label, health, disabled }: OrcaAgentHealthBadgeProps) {
  const { t } = useTranslation("orca-servers");
  const status: OrcaAgentStatus = health?.status ?? "unknown";
  const title = [
    `${t(`agentStatus.${status}`, status)}`,
    health?.error ? health.error : "",
    health?.last_ok_at ? t("agents.lastOk", { time: formatRelativeTime(health.last_ok_at) }) : "",
    disabled ? t("agents.disabled") : "",
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <Badge variant={disabled ? "secondary" : VARIANTS[status] ?? "outline"} title={title} className="font-mono">
      {label}
    </Badge>
  );
}

export function OrcaAgentHealthDetail({ health }: { health?: OrcaAgentHealth }) {
  const { t } = useTranslation("orca-servers");
  if (!health?.error && !health?.last_ok_at) return null;
  return (
    <span className="text-xs text-muted-foreground break-words">
      {health.error && <span className="text-destructive">{health.error}</span>}
      {health.error && health.last_ok_at ? " · " : ""}
      {health.last_ok_at ? t("agents.lastOk", { time: formatRelativeTime(health.last_ok_at) }) : ""}
    </span>
  );
}
