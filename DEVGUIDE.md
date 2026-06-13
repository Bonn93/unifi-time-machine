# Developer Guide

Notes for building, running locally, and contributing to UniFi Time-Machine.

---

## Tech stack

- **Go 1.26+** — single binary, CGO enabled for SQLite
- **SQLite** — embedded database via `modernc.org/sqlite` (no system dependency)
- **Gin** — HTTP framework
- **HTMX + Bootstrap** — frontend, vanilla JS, no build step
- **FFmpeg** — video encoding (injected at runtime via the Docker image)

The app follows 12-factor principles: config at the boundary (env vars for bootstrap, DB for everything else), stateless process, data in a mounted volume.

---

## Project structure

```
cmd/server/         main entrypoint
pkg/config/         bootstrap config (env vars only)
pkg/services/       business logic — snapshots, timelapse, settings, auth
pkg/handlers/       HTTP handlers
pkg/database/       SQLite helpers and migrations
web/                HTML templates, static assets
```

---

## Running locally

You'll need Go 1.26+ and FFmpeg installed.

```bash
go mod tidy
export UFP_API_KEY="..."
export TARGET_CAMERA_ID="..."
export APP_KEY="$(head -c 32 /dev/urandom | base64)"
export ADMIN_PASSWORD="dev"
export GIN_MODE=debug
go run ./cmd/server
```

The web UI will be at `http://localhost:8080`.

> The app expects a `web/` directory relative to the working directory. Run from the repo root or set `DATA_DIR` explicitly.

---

## Building the Docker image

`build.sh` produces a multi-arch image for `linux/amd64` and `linux/arm64` and pushes it to Docker Hub.

```bash
bash build.sh [tag]
```

Tag defaults to `latest`. The script also tags with the current date (`YYYYMMDD`).

Requires `docker buildx` and a configured builder instance. The script creates one named `mybuilder` if it doesn't exist.

### Dockerfiles

| File | Base | Notes |
|---|---|---|
| `Dockerfile` | `debian:bookworm-slim` | Standard image, recommended |
| `Dockerfile_chainguard` | Chainguard | Minimal/hardened alternative |

The build pipeline runs tests (`go test -v ./...`) in a separate stage before compiling, so a failing test will abort the image build.

---

## Releasing

1. Merge to `main`
2. Tag the commit: `git tag v1.2.3 && git push origin v1.2.3`
3. Run `bash build.sh v1.2.3` to build and push the versioned + dated tags

---

## Settings architecture

Bootstrap config (things the app needs before the DB is open) lives in env vars — see `pkg/config/config.go`.

All operational settings (snapshot interval, video quality, retention counts, daylight hours, etc.) are seeded into SQLite on first launch and managed at runtime via the Admin → Settings UI. The seed table is in `pkg/services/settings/settings.go` (`KnownSettings`). Env vars listed there only take effect on the very first run; after that the DB value wins.

---

## Tests

```bash
go test ./...
```

All packages should have a corresponding `_test.go`. New features should ship with tests.

---

## Timelapse file naming

Current naming convention (calendar-based):

| Type | Filename pattern |
|---|---|
| Daily (24 h) | `timelapse_24_hour_YYYY-MM-DD.webm` |
| Weekly | `timelapse_week_YYYY-MM-DD.webm` (Monday date) |
| Monthly | `timelapse_month_YYYY-MM.webm` |
| Yearly | `timelapse_year_YYYY.webm` |

Older installs may have rolling-window files named `timelapse_1_week.webm`, `timelapse_1_month.webm`, `timelapse_1_year.webm`. These are no longer generated and can be safely deleted.

---

## Style notes

- UK/Australian English spelling
- No third-party packages without discussion — prefer stdlib
- Dates/times in UTC internally; display timezone applied in the UI layer
