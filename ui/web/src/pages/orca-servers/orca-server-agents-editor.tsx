import { useState } from "react";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { OrcaAgentConfig, OrcaAgentTier, OrcaServer } from "./hooks/use-orca-servers";
import { OrcaAgentHealthBadge, OrcaAgentHealthDetail } from "./orca-agent-health-badge";
import {
  ORCA_AGENT_DESCRIPTION_MAX,
  ORCA_AGENT_TIERS,
  ORCA_AGENTS_MAX,
  buildOrcaAgentsPayload,
  emptyOrcaAgentRow,
  moveOrcaAgentRow,
  setOrcaAgentFallback,
  toOrcaAgentRows,
  validateOrcaAgents,
  type OrcaAgentRow,
} from "./orca-agents-helpers";

interface OrcaServerAgentsEditorProps {
  server: OrcaServer;
  onOpenChange: (open: boolean) => void;
  onSave: (agents: OrcaAgentConfig[]) => Promise<void>;
}

const fieldClass = "h-9 text-base md:text-sm";

export function OrcaServerAgentsEditor({ server, onOpenChange, onSave }: OrcaServerAgentsEditorProps) {
  const { t } = useTranslation("orca-servers");
  const [rows, setRows] = useState<OrcaAgentRow[]>(() => toOrcaAgentRows(server.agents));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function patch(index: number, change: Partial<OrcaAgentRow>) {
    setRows((prev) => prev.map((r, i) => (i === index ? { ...r, ...change } : r)));
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const invalid = validateOrcaAgents(rows);
    if (invalid) {
      const msg = t(`agents.errors.${invalid.error}`, { max: ORCA_AGENTS_MAX, maxLen: ORCA_AGENT_DESCRIPTION_MAX });
      setError(invalid.row ? t("agents.errors.row", { row: invalid.row, message: msg }) : msg);
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await onSave(buildOrcaAgentsPayload(rows));
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("toast.saveFailed"));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog open onOpenChange={(v) => { if (!submitting) onOpenChange(v); }}>
      <DialogContent className="sm:max-w-4xl">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>{t("agents.title", { label: server.label })}</DialogTitle>
            <DialogDescription>{t("agents.description")}</DialogDescription>
          </DialogHeader>

          <div className="mt-4 max-h-[60dvh] space-y-3 overflow-y-auto overscroll-contain">
            {rows.length === 0 && <p className="text-sm text-muted-foreground">{t("agents.emptyHint")}</p>}
            {rows.map((row, i) => (
              <div key={row.id} className="space-y-2 rounded-md border p-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex min-w-0 flex-col gap-1">
                    <OrcaAgentHealthBadge
                      label={row.key || t("agents.newRow")}
                      health={server.agent_health?.[row.key]}
                      disabled={!row.enabled}
                    />
                    <OrcaAgentHealthDetail health={server.agent_health?.[row.key]} />
                  </div>
                  <div className="flex items-center gap-1">
                    <Button type="button" variant="ghost" size="icon-sm" aria-label={t("agents.moveUp")}
                      disabled={i === 0} onClick={() => setRows((p) => moveOrcaAgentRow(p, i, -1))}>
                      <ArrowUp className="h-3.5 w-3.5" />
                    </Button>
                    <Button type="button" variant="ghost" size="icon-sm" aria-label={t("agents.moveDown")}
                      disabled={i === rows.length - 1} onClick={() => setRows((p) => moveOrcaAgentRow(p, i, 1))}>
                      <ArrowDown className="h-3.5 w-3.5" />
                    </Button>
                    <Button type="button" variant="ghost" size="icon-sm" aria-label={t("agents.remove")}
                      onClick={() => setRows((p) => p.filter((_, j) => j !== i))}>
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
                <div className="grid grid-cols-1 gap-2 sm:grid-cols-4">
                  <Input aria-label={t("agents.fields.key")} placeholder={t("agents.fields.key")} value={row.key}
                    onChange={(e) => patch(i, { key: e.target.value })} className={fieldClass} autoComplete="off" />
                  <Input aria-label={t("agents.fields.agent")} placeholder={t("agents.fields.agentPlaceholder")} value={row.agent}
                    onChange={(e) => patch(i, { agent: e.target.value })} className={fieldClass} autoComplete="off" />
                  <Input aria-label={t("agents.fields.model")} placeholder={t("agents.fields.modelPlaceholder")} value={row.model}
                    onChange={(e) => patch(i, { model: e.target.value })} className={fieldClass} autoComplete="off" />
                  <select aria-label={t("agents.fields.tier")} value={row.tier}
                    onChange={(e) => patch(i, { tier: e.target.value as OrcaAgentTier })}
                    className={`${fieldClass} rounded-md border border-input bg-background px-2`}>
                    {ORCA_AGENT_TIERS.map((tier) => (
                      <option key={tier} value={tier}>{t(`agents.tiers.${tier}`)}</option>
                    ))}
                  </select>
                </div>
                <Input aria-label={t("agents.fields.description")} placeholder={t("agents.fields.descriptionPlaceholder")}
                  value={row.description} maxLength={ORCA_AGENT_DESCRIPTION_MAX}
                  onChange={(e) => patch(i, { description: e.target.value })} className={fieldClass} />
                <div className="flex flex-wrap gap-4 text-sm">
                  <label className="flex items-center gap-2">
                    <input type="checkbox" checked={row.enabled} onChange={(e) => patch(i, { enabled: e.target.checked })} />
                    {t("agents.fields.enabled")}
                  </label>
                  <label className="flex items-center gap-2">
                    <input type="radio" name="orca-agent-fallback" checked={row.fallback}
                      onClick={() => setRows((p) => setOrcaAgentFallback(p, i))} onChange={() => undefined} />
                    {t("agents.fields.fallback")}
                  </label>
                </div>
              </div>
            ))}
            <Button type="button" variant="outline" size="sm" className="gap-1" disabled={rows.length >= ORCA_AGENTS_MAX}
              onClick={() => setRows((p) => [...p, emptyOrcaAgentRow()])}>
              <Plus className="h-3.5 w-3.5" />
              {t("agents.add")}
            </Button>
          </div>
          {error && <p role="alert" className="mt-3 text-sm text-destructive">{error}</p>}

          <DialogFooter className="mt-6">
            <Button type="button" variant="outline" disabled={submitting} onClick={() => onOpenChange(false)}>
              {t("form.cancel")}
            </Button>
            <Button type="submit" disabled={submitting}>{t("form.save")}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
