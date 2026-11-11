# Orca Remote CLI

Agents that run inside the GoClaw Docker sandbox can drive an Orca runtime on another
machine (a developer workstation, a VM, a build box) through the `orca` CLI. Worktrees,
terminals, and coding agents (Claude Code, Codex, ...) launched this way run on the Orca
machine; the GoClaw agent stays isolated in its container and only holds a revocable
pairing credential.

```
GoClaw sandbox container ── orca <cmd> (ORCA_PAIRING_CODE) ── ws://<orca-host>:6768 ── orca serve
                                                                                     └─ worktrees, terminals, agents
```

This reuses the Secure CLI credential path ([19-credentialed-exec.md](./19-credentialed-exec.md)):
the `orca` preset uses the passthrough adapter, so the pairing code is injected per call with
`docker exec -e` and scrubbed from output.

## 1. Orca machine

Run the Orca runtime where the work should happen and make the port reachable only over a
private network (Tailscale, WireGuard, LAN). Do not expose it to the public internet.

```bash
orca serve --port 6768 --pairing-address <tailscale-or-lan-ip>
```

On a desktop that already runs the Orca app, create the access link from
Settings → Remote Orca Servers instead. `serve` prints a `Pairing URL: orca://pair?code=...`.
Each client gets its own token; revoke it from Settings → Remote Orca Servers →
Shared Server Access. Do not pass `--grant-desktop-control`.

A paired client has full control of that runtime, including running commands in its
terminals. Prefer a dedicated machine or VM for agent work over a personal workstation, and
use one pairing code per agent or user so access can be revoked independently.

## 2. Images

Both images need the CLI at `/usr/local/bin/orca`: the gateway resolves the binary path
locally before running it inside the sandbox.

```bash
docker build --build-arg ENABLE_ORCA_CLI=true -t goclaw-sandbox:bookworm-slim -f Dockerfile.sandbox .
docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
  -f docker-compose.sandbox.yml -f docker-compose.orca.yml up -d --build
```

`ENABLE_ORCA_CLI` extracts only the CLI bundle (`out/cli/index.js`) from the pinned,
SHA-256-verified `orca-ide` `.deb` release and runs it on Node.js. Override
`ORCA_CLI_VERSION` and `ORCA_CLI_SHA256` together to upgrade; keep the CLI version in step
with the Orca server.

`docker-compose.orca.yml` sets `GOCLAW_SANDBOX_NETWORK=true`. To limit network access to
specific agents, leave the global default off and enable `network_enabled` in each agent's
sandbox settings instead. The sandbox container must be able to route to the Orca host
(for Tailscale, run `tailscaled` on the Docker host; the in-process `tsnet` overlay only
covers the gateway process).

## 3. Credential

1. Web UI → CLI Credentials → add from the `orca` preset (registers the binary and its deny rules).
2. Web UI → Orca Servers → add each machine: label, description, pairing URL (`orca://...`).
   Tenant owner or admin only. Max 20 servers per tenant. The pairing code is encrypted at rest and never returned by the API.
3. Grant the binary to the agents that should use it.

Alternatively, store the pairing URL as a per-user or per-agent `ORCA_PAIRING_CODE` credential
(see below). Both work side by side.

The preset blocks commands that would retarget or leak the connection or touch the Orca
machine's desktop: leading flags other than the gateway-handled `--server` selector (put the
subcommand first), `--pairing-code`,
`--environment`, `serve`, `open`, `environment`, `account` (except `account list`), `computer`, `diagnostics`,
`skills install|update|share`, and `orchestration reset`.

### Orca Servers page

Each entry is a tenant-wide machine with a status: `healthy`, `unhealthy` (runtime reachable but
not `ready`), `error` (timeout, failed or unparseable `orca status`), `cli_missing` (no `orca` on the
gateway `PATH`), or `unknown` (not checked yet). The label is fixed after creation; description and
pairing code can be edited.

A server is exposed to agents as one more `--server <label>` choice for the `orca` binary only.
Any agent granted the `orca` binary can use any server in its tenant; there is no per-server
allowlist. Use labeled credentials when a machine must be limited to one user or agent.

Label precedence for the same label: user credential > agent credential > Orca server. A
labeled credential therefore shadows a server with the same label, and the page status then does
not reflect what that user or agent actually hits.

**Migrating from labeled credentials (manual).** Existing labeled `orca` credentials keep working
and are not copied automatically. To move a machine: add it on the Orca Servers page with the
same label and pairing URL, confirm it is `healthy`, then delete the labeled credential from
CLI Credentials so the server takes over.

### Coding agents and `--agent auto`

Each Orca server has an ordered list of coding agents (Orca Servers page → **Agents**), up to 10:

| Field | Rule |
|-------|------|
| Key | Unique per server, `^[a-z0-9][a-z0-9._-]{0,63}$` |
| Agent | Orca agent id (`claude`, `codex`, …), same pattern as key |
| Model | Optional, `^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$` (e.g. `openai/gpt-5`) |
| Tier | `routine`, `hard`, or `planning` (at most one planning entry) |
| Description | Up to 300 characters; the router reads it to pick an agent |
| Enabled / Fallback | At most one fallback; it must be enabled and not `planning` |

Agents run `orca --server <label> worktree create|agent launch ... --agent auto`. Before the
deny checks the gateway rewrites `--agent auto` to `--agent <id> [--model <id>]` and prepends
`routed: --agent <id> --model <id> (<reason>)` to the output:

1. Prompt starts with `/planning` → the planning entry.
2. Prompt starts with `/cook` → first enabled `hard` entry that is `healthy`, else one that is `unknown`, else the fallback.
3. Otherwise candidates are enabled, not `unhealthy`, tier `routine` or `hard`. One candidate → it. If the tenant has an enabled LLM provider named `jev-*`, Jev (OpenRouter `systemone`) picks from the descriptions; confidence below the provider's `settings.threshold` (default 0.6) → fallback. No Jev → fallback.

`--agent auto` is refused, and orca is never run, when the command is not against an Orca
server (`--server <label>`), when `--model`/`--effort` is also passed, when a flag is repeated,
or when the server has no usable agent. An explicitly named agent always runs; if it is not
configured on the server a `warning:` line is prepended.

Jev provider: create an LLM provider whose name starts with `jev-` with the OpenRouter key.
Optional `settings`: `{"model": "typesafe/jev-1.13", "threshold": 0.6}`. `api_base` defaults
to `https://openrouter.ai/api/v1`. Routing decisions are logged as `orca.route`.

**Agent health** (badges on the page):

| Source | Effect |
|--------|--------|
| Launch reply `invalid_argument` `Unknown TUI agent "<id>"` | That entry → `unhealthy` |
| `orca account list --json` (every 5 min and on Check) | Signed in → `healthy`; `systemDefault.hasAuth: false` with no accounts → `unhealthy: not signed in`; no auth data (e.g. claude) → unchanged |
| Server not healthy | All entries → `unknown` |
| Check button | `unhealthy` → `unknown` first |
| Background refresh | `unhealthy` older than 30 minutes → `unknown` |

A successful launch reply does not mark an agent healthy: Orca 1.4.243 also returns
`ok: true` for signed-out agents and unknown models, which then fail inside the session.

### Multiple Orca machines (labeled credentials)

Use one credential per machine. In the CLI Credentials dialog (agent or user credential),
set:

| Field | Rule |
|-------|------|
| Label | Lowercase letters, digits, `-`, `_`; 1-32 characters. `default` is reserved for the unlabeled credential. |
| Description | Up to 200 characters. Non-secret; agents see it to pick a machine. |

The label cannot be changed after creation; delete and recreate the credential to rename it.
Max 20 labeled credentials per user or per agent per binary.

The agent selects a machine per command with `--server <label>` as the first argument:

```bash
orca --server build-vm status --json
```

| Situation | Result |
|-----------|--------|
| `--server <label>` | User credential with that label, else agent credential, else Orca server |
| `--server <unknown>` | Error; the label is echoed truncated |
| No `--server`, default credential exists | Default credential, as before |
| No `--server`, no default, one label | That label is used |
| No `--server`, no default, several labels | Error listing each label and description; re-run with `--server <label>` |

The labels and descriptions also appear in the `Servers` block of the `Credentialed CLI Tools`
section of the agent's TOOLS.md. Credentials created from the `orca` preset before this change
keep their stored tip text; the servers block explains `--server` on its own.

## 4. Verify

Ask the agent to run `orca status --json`. `runtime.state` should be `ready` and
`target.environment` should be `pairing-code`. With several machines, run
`orca --server <label> status --json` for each label. The bundled `orca-cli` skill covers the
remaining commands.

On the Orca Servers page, **Check now** runs `orca status --json` for one server and stores the
result. The same check runs automatically for servers not checked in the last 5 minutes (first
pass 30 seconds after gateway start), with a 20 second timeout per check. It runs from the
gateway process network and `PATH`, not from the sandbox container, so a healthy status does not
prove the sandbox can route to the machine. If the gateway image lacks the `orca` CLI the status is
`cli_missing`; build the image with `ENABLE_ORCA_CLI=true`.

## 5. Getting files back

Files created on the Orca machine do not reach GoClaw on their own. The `file_inbox` tool
issues a short-lived upload URL (30 min, 10 files, 50 MB each, in-memory, bound to an
`inbox/` folder). When the calling agent belongs to a team, the folder is the team workspace
`inbox/`, so the leader and every teammate see the same files; otherwise it is the agent's own
workspace `inbox/`. The tool result names the absolute folder path. The coding agent uploads with
`curl -fsS -T <file> <url>`, and the GoClaw agent delivers the result with `send_file`.

- The Orca machine must reach the gateway. Set `gateway.public_url` (or reach the gateway
  once through its public address) so the URL is known.
- The URL is the only credential: no gateway token is placed on the Orca machine.
- Tickets live in process memory; a gateway restart or a second gateway instance invalidates them.
- Executable extensions (`.sh`, `.exe`, ...) and dotfiles are rejected.
