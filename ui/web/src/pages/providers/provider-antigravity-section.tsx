import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { CheckCircle, Copy, ExternalLink, Loader2 } from "lucide-react";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import { useProviderModels } from "./hooks/use-provider-models";

const POLL_MS = 3000;
const PREPARE_TIMEOUT_MS = 10 * 60 * 1000;
const CODE_TIMEOUT_MS = 2 * 60 * 1000;

type Phase = "loading" | "idle" | "preparing" | "pending" | "authenticated";

interface StartResponse {
  status: "preparing" | "pending" | "already_authenticated";
  auth_url?: string;
}

interface AntigravityLoginSectionProps {
  providerId: string;
  settings?: Record<string, unknown>;
  onClose?: () => void;
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

export function AntigravityLoginSection({ providerId, settings, onClose }: AntigravityLoginSectionProps) {
  const { t } = useTranslation("providers");
  const http = useHttp();
  const queryClient = useQueryClient();
  const [phase, setPhase] = useState<Phase>("loading");
  const [authUrl, setAuthUrl] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const epoch = useRef(0);
  const guard = () => { const e = epoch.current; return () => epoch.current === e; };
  const base = `/v1/providers/${providerId}/antigravity`;

  const fail = (err: unknown) => toast.error(t("antigravity.loginFailed"), err instanceof Error ? err.message : "");

  const markAuthenticated = useCallback(() => {
    setPhase("authenticated");
    queryClient.invalidateQueries({ queryKey: ["providers"] });
    queryClient.invalidateQueries({ queryKey: queryKeys.providers.models(providerId) });
  }, [queryClient, providerId]);

  useEffect(() => {
    const e = ++epoch.current;
    const live = () => epoch.current === e;
    setPhase("loading");
    setBusy(false);
    http.get<{ authenticated: boolean }>(`${base}/auth-status`)
      .then((res) => live() && setPhase(res.authenticated ? "authenticated" : "idle"))
      .catch(() => live() && setPhase("idle"));
    return () => { epoch.current++; };
  }, [http, base]);

  const handleStart = async () => {
    const alive = guard();
    setBusy(true);
    const deadline = Date.now() + PREPARE_TIMEOUT_MS;
    try {
      while (alive()) {
        const res = await http.post<StartResponse>(`${base}/login/start`);
        if (!alive()) return;
        if (res.status === "already_authenticated") return markAuthenticated();
        if (res.status === "pending" && res.auth_url) {
          setAuthUrl(res.auth_url);
          setCode("");
          setPhase("pending");
          window.open(res.auth_url, "_blank", "noopener,noreferrer");
          return;
        }
        if (Date.now() > deadline) throw new Error(t("antigravity.preparing"));
        setPhase("preparing");
        await sleep(POLL_MS);
      }
    } catch (err) {
      if (alive()) { setPhase("idle"); fail(err); }
    } finally {
      if (alive()) setBusy(false);
    }
  };

  const handleConnect = async () => {
    const alive = guard();
    const value = code.trim();
    if (!value) return;
    setBusy(true);
    try {
      let res = await http.post<{ status: string }>(`${base}/login/code`, { code: value });
      const deadline = Date.now() + CODE_TIMEOUT_MS;
      while (alive() && res.status !== "authenticated") {
        if (Date.now() > deadline) throw new Error(t("antigravity.codeTimeout"));
        await sleep(POLL_MS);
        const s = await http.get<{ authenticated: boolean }>(`${base}/auth-status`);
        res = { status: s.authenticated ? "authenticated" : "pending" };
      }
      if (alive()) markAuthenticated();
    } catch (err) {
      if (alive()) fail(err);
    } finally {
      if (alive()) setBusy(false);
    }
  };

  if (phase === "loading") {
    return (
      <div className="flex items-center gap-2 py-4 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" /> {t("oauth.checkingStatus")}
      </div>
    );
  }

  const connected = phase === "authenticated";
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        {connected ? (
          <Badge variant="outline" className="gap-1 border-green-500/30 text-green-700 dark:text-green-400">
            <CheckCircle className="h-3 w-3" />{t("antigravity.connected")}
          </Badge>
        ) : (
          <Badge variant="outline">{t("antigravity.notConnected")}</Badge>
        )}
        <Button size="sm" variant={connected ? "outline" : "default"} onClick={handleStart} disabled={busy} className="gap-1.5">
          {busy && phase !== "pending" ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ExternalLink className="h-3.5 w-3.5" />}
          {connected ? t("antigravity.relogin") : t("antigravity.getLink")}
        </Button>
      </div>

      {phase === "preparing" && (
        <div className="flex items-center gap-2 rounded-md border border-blue-500/30 bg-blue-500/5 px-3 py-2 text-sm text-blue-700 dark:text-blue-400">
          <Loader2 className="h-4 w-4 shrink-0 animate-spin" />
          <span>{t("antigravity.preparing")}</span>
        </div>
      )}

      {phase === "pending" && (
        <div className="space-y-2 rounded-md border bg-muted/50 p-3">
          <div className="flex items-start gap-2">
            <a href={authUrl} target="_blank" rel="noopener noreferrer" className="min-w-0 flex-1 break-all text-xs text-primary underline-offset-4 hover:underline">
              {t("antigravity.openLink")}: {authUrl}
            </a>
            <Button type="button" variant="outline" size="icon" className="size-9 shrink-0" aria-label={t("antigravity.copyLink")} onClick={() => { navigator.clipboard.writeText(authUrl).catch(() => {}); }}>
              <Copy className="h-4 w-4" />
            </Button>
          </div>
          <Label htmlFor="agy-code">{t("antigravity.codeLabel")}</Label>
          <div className="flex gap-2">
            <Input
              id="agy-code"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && !busy && handleConnect()}
              placeholder={t("antigravity.codePlaceholder")}
              className="font-mono text-base md:text-sm"
              autoComplete="off"
            />
            <Button onClick={handleConnect} disabled={busy || !code.trim()} className="shrink-0 gap-1.5">
              {busy && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {t("antigravity.connect")}
            </Button>
          </div>
        </div>
      )}

      {connected && (
        <>
          <AntigravityModelPicker providerId={providerId} settings={settings} />
          <AntigravityForceModelToggle providerId={providerId} settings={settings} />
        </>
      )}

      {onClose && connected && (
        <div className="flex justify-end">
          <Button size="sm" onClick={onClose}>{t("form.close")}</Button>
        </div>
      )}
    </div>
  );
}

function AntigravityModelPicker({ providerId, settings }: { providerId: string; settings?: Record<string, unknown> }) {
  const { t } = useTranslation("providers");
  const http = useHttp();
  const queryClient = useQueryClient();
  const { models, loading } = useProviderModels(providerId);
  const saved = typeof settings?.model === "string" ? settings.model : "";
  const [current, setCurrent] = useState(saved);
  const [saving, setSaving] = useState(false);

  useEffect(() => { setCurrent(saved); }, [saved, providerId]);

  const handleChange = async (model: string) => {
    setSaving(true);
    try {
      const latest = await http.get<{ settings?: Record<string, unknown> }>(`/v1/providers/${providerId}`);
      await http.put(`/v1/providers/${providerId}`, { settings: { ...latest.settings, model } });
      setCurrent(model);
      queryClient.invalidateQueries({ queryKey: ["providers"] });
      toast.success(t("antigravity.modelSaved"));
    } catch (err) {
      toast.error(t("antigravity.modelSaveFailed"), err instanceof Error ? err.message : "");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-2">
      <Label>{t("antigravity.model")}</Label>
      {loading ? (
        <div className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" /></div>
      ) : (
        <Select value={current} onValueChange={handleChange} disabled={saving || models.length === 0}>
          <SelectTrigger className="text-base md:text-sm"><SelectValue placeholder={t("antigravity.model")} /></SelectTrigger>
          <SelectContent>
            {models.map((m) => <SelectItem key={m.id} value={m.id}>{m.name || m.id}</SelectItem>)}
          </SelectContent>
        </Select>
      )}
    </div>
  );
}

function AntigravityForceModelToggle({ providerId, settings }: { providerId: string; settings?: Record<string, unknown> }) {
  const { t } = useTranslation("providers");
  const http = useHttp();
  const queryClient = useQueryClient();
  const saved = settings?.force_model === true;
  const [checked, setChecked] = useState(saved);
  const [saving, setSaving] = useState(false);

  useEffect(() => { setChecked(saved); }, [saved, providerId]);

  const handleChange = async (force: boolean) => {
    setChecked(force);
    setSaving(true);
    try {
      const latest = await http.get<{ settings?: Record<string, unknown> }>(`/v1/providers/${providerId}`);
      await http.put(`/v1/providers/${providerId}`, { settings: { ...latest.settings, force_model: force } });
      queryClient.invalidateQueries({ queryKey: ["providers"] });
      toast.success(t("antigravity.forceModelSaved"));
    } catch (err) {
      setChecked(!force);
      toast.error(t("antigravity.modelSaveFailed"), err instanceof Error ? err.message : "");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex items-center justify-between gap-4 pt-1">
      <div className="space-y-0.5">
        <Label htmlFor="agy-force-model" className="text-sm font-normal cursor-pointer">{t("antigravity.forceModel")}</Label>
        <p className="text-xs text-muted-foreground">{t("antigravity.forceModelHint")}</p>
      </div>
      <Switch id="agy-force-model" checked={checked} onCheckedChange={handleChange} disabled={saving} />
    </div>
  );
}
