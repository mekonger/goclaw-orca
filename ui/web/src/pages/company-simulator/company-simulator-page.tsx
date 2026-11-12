import { useState } from "react";
import { Building2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { EmptyState } from "@/components/shared/empty-state";
import { PageHeader } from "@/components/shared/page-header";
import { ROUTES } from "@/lib/constants";
import { EmployeeActivityPanel } from "./employee-activity-panel";
import { useCompanySimulation } from "./hooks/use-company-simulation";
import { OfficeCanvas } from "./office-canvas";

export function CompanySimulatorPage() {
  const { t } = useTranslation("company-simulator");
  const { teams, teamsLoading, teamId, setTeamId, state } = useCompanySimulation();
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const selected = selectedKey && state.keys.has(selectedKey) ? selectedKey : null;

  return (
    <div className="p-4 pb-10 sm:p-6">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          teams.length > 0 && (
            <Select value={teamId ?? ""} onValueChange={setTeamId}>
              <SelectTrigger aria-label={t("selectTeam")} className="w-56 text-base md:text-sm">
                <SelectValue placeholder={t("selectTeam")} />
              </SelectTrigger>
              <SelectContent>
                {teams.map((team) => (
                  <SelectItem key={team.id} value={team.id}>{team.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          )
        }
      />

      {teams.length === 0 ? (
        !teamsLoading && (
          <EmptyState
            icon={Building2}
            title={t("empty.noTeams")}
            description={t("empty.noTeamsDescription")}
            action={
              <Button asChild size="sm">
                <Link to={ROUTES.TEAMS}>{t("empty.goToTeams")}</Link>
              </Button>
            }
          />
        )
      ) : (
        <div className="mt-4 grid grid-cols-1 gap-4 lg:h-[calc(100dvh-13rem)] lg:grid-cols-[1fr_320px]">
          <OfficeCanvas state={state} selected={selected} onSelect={setSelectedKey} />
          <EmployeeActivityPanel state={state} selected={selected} onSelect={setSelectedKey} />
        </div>
      )}
    </div>
  );
}
