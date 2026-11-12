import { useState } from "react";
import { Plus, RefreshCw, Server } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useOrcaServers, type OrcaServer } from "./hooks/use-orca-servers";
import { OrcaServerFormDialog } from "./orca-server-form-dialog";
import { OrcaServerRow } from "./orca-server-row";
import { OrcaServerAgentsEditor } from "./orca-server-agents-editor";
import { buildOrcaServerCreatePayload, buildOrcaServerUpdatePayload } from "./orca-server-form-helpers";

export function OrcaServersPage() {
  const { t } = useTranslation("orca-servers");
  const { servers, loading, createServer, updateServer, updateAgents, deleteServer, checkServer, checkAll } = useOrcaServers();

  const isEmpty = servers.length === 0;
  const showSkeleton = useDeferredLoading(loading && isEmpty);

  const [formOpen, setFormOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<OrcaServer | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<OrcaServer | null>(null);
  const [agentsTarget, setAgentsTarget] = useState<OrcaServer | null>(null);
  const [checkingIds, setCheckingIds] = useState<Set<string>>(new Set());
  const [checkingAll, setCheckingAll] = useState(false);
  const checkAllSpinning = useMinLoading(checkingAll);

  function openCreate() {
    setEditTarget(null);
    setFormOpen(true);
  }

  function openEdit(server: OrcaServer) {
    setEditTarget(server);
    setFormOpen(true);
  }

  async function runCheck(id: string) {
    setCheckingIds((prev) => new Set(prev).add(id));
    try {
      await checkServer(id);
    } finally {
      setCheckingIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }
  }

  async function runCheckAll() {
    setCheckingAll(true);
    try {
      await checkAll();
    } finally {
      setCheckingAll(false);
    }
  }

  const headers = ["label", "description", "status", "agents", "runtime", "lastChecked", "lastHealthy", "lastError"] as const;

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={runCheckAll}
              disabled={checkingAll || isEmpty}
              className="gap-1"
            >
              <RefreshCw className={"h-3.5 w-3.5" + (checkAllSpinning ? " animate-spin" : "")} />
              {t("checkAll")}
            </Button>
            <Button size="sm" onClick={openCreate} className="gap-1">
              <Plus className="h-3.5 w-3.5" />
              {t("addServer")}
            </Button>
          </div>
        }
      />

      <p className="mt-3 text-xs text-muted-foreground">{t("note")}</p>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={4} />
        ) : isEmpty ? (
          <EmptyState icon={Server} title={t("emptyTitle")} description={t("emptyDescription")} />
        ) : (
          <div className="rounded-md border overflow-x-auto">
            <table className="w-full min-w-[600px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  {headers.map((h) => (
                    <th key={h} className="px-4 py-3 text-left font-medium">{t(`columns.${h}`)}</th>
                  ))}
                  <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {servers.map((server) => (
                  <OrcaServerRow
                    key={server.id}
                    server={server}
                    checking={checkingIds.has(server.id) || checkingAll}
                    onCheck={() => runCheck(server.id)}
                    onEdit={() => openEdit(server)}
                    onEditAgents={() => setAgentsTarget(server)}
                    onDelete={() => setDeleteTarget(server)}
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {formOpen && (
        <OrcaServerFormDialog
          key={editTarget?.id ?? "create"}
          open
          server={editTarget}
          onOpenChange={setFormOpen}
          onSubmit={async (form) => {
            if (editTarget) {
              await updateServer(editTarget.id, buildOrcaServerUpdatePayload(form));
            } else {
              await createServer(buildOrcaServerCreatePayload(form));
            }
          }}
        />
      )}

      {agentsTarget && (
        <OrcaServerAgentsEditor
          key={agentsTarget.id}
          server={agentsTarget}
          onOpenChange={(v) => { if (!v) setAgentsTarget(null); }}
          onSave={(agents) => updateAgents(agentsTarget.id, agents)}
        />
      )}

      {deleteTarget && (
        <ConfirmDialog
          open
          onOpenChange={() => setDeleteTarget(null)}
          title={t("deleteDialog.title")}
          description={t("deleteDialog.description", { label: deleteTarget.label })}
          confirmLabel={t("deleteDialog.confirmLabel")}
          variant="destructive"
          onConfirm={async () => {
            try {
              await deleteServer(deleteTarget.id);
            } finally {
              setDeleteTarget(null);
            }
          }}
        />
      )}
    </div>
  );
}
