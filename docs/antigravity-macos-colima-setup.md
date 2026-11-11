# Antigravity CLI on macOS/Colima — Setup & Troubleshooting

Covers the chain of issues that broke the Antigravity CLI (`agy`) login flow
("Get login link" button) on a macOS host running Docker via **Colima**
(no Docker Desktop, no `docker buildx` by default), and the fixes applied.

## Symptom

Clicking "Get login link" on an `antigravity_cli` provider returned:

```
503 Antigravity runtime is not configured: antigravity runtime not configured
```

and, after partial fixes, a sequence of different underlying errors (below).
All four root causes are specific to running goclaw's Docker-outside-of-Docker
(DooD) antigravity orchestration on a Mac/Colima setup — none of this applies
on a Linux host with Docker Desktop/Docker Engine and `buildx` pre-installed.

## Root causes (in the order they were hit)

### 1. `GOCLAW_AGY_BINARY_HOST_PATH` unset

`internal/providers/antigravity/runtime.go` `Ready()` requires this env var to
point at an absolute path on the host, plus a working `docker` CLI. Unset →
immediate `ErrNotConfigured`. Fixed by setting it in `.env` and recreating the
`goclaw` container so the new env var is loaded (`env_file` is only read at
container creation, not on `docker restart`).

### 2. `dockerfile_inline` silently ignored → wrong image built

`docker-compose.agy.yml` (manual per-account `agy` container helper) defines
its image with `build.dockerfile_inline` (a BuildKit-only Compose feature).
This machine had no `docker buildx` plugin — Docker Desktop was uninstalled,
leaving a dead symlink at `~/.docker/cli-plugins/docker-buildx`. Without
buildx, Compose falls back to its legacy "classic" builder, which does not
understand `dockerfile_inline` and silently builds `context + "Dockerfile"`
instead — i.e. **the main goclaw gateway Dockerfile** sitting in the repo
root, since the compose file's build context defaults to `.`.

Symptom: the `agy-acc1` container ran the full 300MB goclaw image
(`ENTRYPOINT ["/app/docker-entrypoint.sh"]`, `CMD ["serve"]`) instead of the
tiny `debian:bookworm-slim` image. `command: sleep infinity` from the compose
file was passed as an argument to the goclaw CLI → `Error: unknown command
"sleep" for "goclaw"` → crash loop (`restart: unless-stopped`).

**Fix:** installed `docker-buildx` via Homebrew and re-pointed
`~/.docker/cli-plugins/docker-buildx` at it (the old symlink pointed into a
no-longer-installed Docker Desktop). Confirmed with `docker buildx ls` that
the existing `colima` builder came up healthy. Rebuilding after this
correctly honored `dockerfile_inline`.

### 3. `agy` binary is macOS, container is Linux

`GOCLAW_AGY_BINARY_HOST_PATH` pointed at `~/.local/bin/agy`, the **macOS**
build of the Antigravity CLI (`file` confirmed `Mach-O 64-bit executable
arm64`). That binary gets bind-mounted read-only into the Linux `agy`
container and invoked there — a Linux container can never execute a macOS
binary, mount or copy makes no difference (`exec format error`).

**Fix:** ran the official installer (`curl -fsSL
https://antigravity.google/cli/install.sh | bash`) *inside* the Linux
container to fetch the matching `linux_arm64` build, then `docker cp`'d it out
to a new host path `~/.local/bin/agy-linux-arm64` (kept separate from the
macOS CLI at `~/.local/bin/agy`). Updated `GOCLAW_AGY_BINARY_HOST_PATH` in
`.env` and the volume source in `docker-compose.agy.yml` to point at the
Linux binary.

### 4. `goclaw` container had no `docker` CLI / socket access

The *actual* per-provider flow (`internal/providers/antigravity/container.go`,
`image.go`) runs `docker` from inside the `goclaw` gateway container itself
(DooD) to build `goclaw-agy:bookworm-slim` and manage per-provider containers
named `goclaw-agy-<provider-uuid>`. The published image has no `docker` CLI
and the container had no access to the host's Docker socket at all.

**Fix:** added `docker-compose.antigravity.yml`, a dedicated overlay:
- `build.args.ENABLE_SANDBOX: "true"` — this is the existing Dockerfile flag
  that installs `docker-cli` (`Dockerfile` apk step); it does **not** enable
  the unrelated sandbox code-execution feature by itself (that's gated by the
  `GOCLAW_SANDBOX_MODE` runtime env var, left unset here).
- mounts `/var/run/docker.sock:/var/run/docker.sock`
- `group_add: ${DOCKER_GID:-999}`

This mirrors the existing `docker-compose.sandbox.yml` pattern but stays
scoped to just the docker-client capability antigravity needs.

### 5. `su-exec` wipes the `--group-add` grant

Even with the socket mounted and `group_add: 991` applied, the gateway
process still got `permission denied while trying to connect to the docker
API`. `docker-entrypoint.sh` starts as root, then calls `su-exec goclaw
/app/goclaw` to drop privileges. `su-exec` calls `initgroups()` for the
target user, which **replaces** the process's supplementary group list — it
does not merge with whatever `--group-add`/`group_add` set on the container's
root process. So the `goclaw`-user process that actually shells out to
`docker` never had group `991`, even though a bare `docker exec` (which
defaults to root, no privilege drop) worked fine and gave false confidence
during diagnosis.

**Fix:** patched `docker-entrypoint.sh` to detect the bind-mounted socket's
real gid at startup (works regardless of what `DOCKER_GID` was set to, or
whether it was set at all) and add `goclaw` to a group with that gid, before
the `su-exec` privilege drop:

```sh
if [ "$(id -u)" = "0" ] && [ -S /var/run/docker.sock ]; then
  sock_gid="$(stat -c '%g' /var/run/docker.sock 2>/dev/null || true)"
  if [ -n "$sock_gid" ]; then
    grp_name="$(awk -F: -v g="$sock_gid" '$3==g{print $1; exit}' /etc/group)"
    if [ -z "$grp_name" ]; then
      addgroup -g "$sock_gid" dockerhost 2>/dev/null || true
      grp_name=dockerhost
    fi
    addgroup goclaw "$grp_name" 2>/dev/null || true
  fi
fi
```

Verified: `docker exec -u goclaw goclaw-orca-goclaw-1 docker ps` now lists all
sibling containers; the `docker build`/`docker exec` calls inside
`internal/providers/antigravity/*.go` stopped failing with permission denied.

## Files changed

| File | Change |
|---|---|
| `docker-entrypoint.sh` | Add dynamic docker-socket group grant before privilege drop (fix #5) |
| `docker-compose.agy.yml` | Volume source `agy` → `agy-linux-arm64` (fix #3) |
| `docker-compose.antigravity.yml` | New overlay: docker-cli + socket + group for the `goclaw` service (fix #4) |
| `~/.docker/cli-plugins/docker-buildx` (host, not tracked) | Re-pointed to Homebrew's buildx (fix #2) |
| `.env` (host, not tracked, gitignored) | `GOCLAW_AGY_BINARY_HOST_PATH` → Linux binary path (fixes #1, #3) |

## Setup checklist for a new macOS/Colima environment

1. `brew install docker-buildx`; ensure `~/.docker/cli-plugins/docker-buildx`
   is a valid symlink (`docker buildx version` should succeed, not
   "unknown command").
2. Get a **Linux** build of `agy` matching the container's arch (run the
   official installer inside any Linux container, then `docker cp` it out —
   see fix #3). Do not point `GOCLAW_AGY_BINARY_HOST_PATH` at the macOS CLI.
3. Set `GOCLAW_AGY_BINARY_HOST_PATH` in `.env` to that Linux binary's host
   path.
4. Start/rebuild `goclaw` with the antigravity overlay:
   ```bash
   docker compose -f docker-compose.yml -f docker-compose.postgres.yml \
     -f docker-compose.antigravity.yml up -d --build
   ```
5. Confirm `docker exec -u goclaw <goclaw-container> docker ps` succeeds
   before testing the login flow in the UI.

## Open question (unresolved)

`docker-compose.agy.yml` creates a **fixed**, persistent container named
`agy-acc1`. The actual code path behind "Get login link"
(`antigravity.ContainerName(id)`) creates a **separate**, per-provider
container named `goclaw-agy-<provider-uuid>` on demand — these two mechanisms
are independent. `docker-compose.agy.yml` is effectively unused by the live
HTTP flow; it may be intended for a different (manual, persistent-login)
workflow. Not yet clarified whether `docker-compose.agy.yml`/`agy-acc1`
should be kept, documented as a separate manual flow, or removed.
