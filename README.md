# UniFi Time-Machine

Automatic timelapse videos from your UniFi Protect cameras, with a clean web interface to watch them.

## Screenshots

**Daily View**
<img width="1663" height="1813" alt="Daily view" src="https://github.com/user-attachments/assets/f8a84e85-a78e-41dd-857c-320df74924cc" />

**Gallery**
<img width="1651" height="996" alt="Gallery" src="https://github.com/user-attachments/assets/e730967b-b32d-449b-ae96-f258d2a907e6" />

**Admin Panel**
<img width="1238" height="1038" alt="Admin panel" src="https://github.com/user-attachments/assets/17a73cef-4e40-44f3-93d3-e71541fb9322" />

---

## Features

- Captures hourly snapshots and builds **daily, weekly, monthly, and yearly** timelapses automatically
- **24-hour gallery** — browse any day's images, sort and filter by date
- **Share links** — generate a time-limited public link to any timelapse
- **Daylight filtering** — weekly and monthly lapses skip night images automatically
- **HLS adaptive streaming** — smooth playback on any connection
- All settings configured in the **Admin → Settings** panel — no restarts needed
- Multi-arch Docker image (amd64 + ARM64)

---

## Quick Start

### Option A — Docker Compose (recommended)

1. Copy `.env` and fill in the four required values:

   ```
   UFP_HOST=192.168.1.1
   UFP_API_KEY=<your api key>
   TARGET_CAMERA_ID=<your camera id>
   APP_KEY=<run: head -c 32 /dev/urandom | base64>
   ADMIN_PASSWORD=<choose a password>
   ```

2. Start:

   ```bash
   docker compose up -d
   ```

3. Open `http://localhost:8000` and log in with `admin` / your `ADMIN_PASSWORD`.

### Option B — start.sh

Edit the values at the top of `start.sh`, then:

```bash
bash start.sh
```

`UFP_API_KEY` and `APP_KEY` can also be exported in your shell before running rather than written into the script.

---

## Finding your values

**`UFP_API_KEY`** — In your UniFi OS console go to **Integrations → New API Key**.

**`TARGET_CAMERA_ID`** — Open the camera in the Protect web UI. The ID is in the URL:
```
https://192.168.1.1/protect/dashboard/all/sidepanel/device/<YOUR_CAMERA_ID>/manage
```

**`APP_KEY`** — A random secret used to sign sessions. Generate one:
```bash
head -c 32 /dev/urandom | base64
```

---

## Configuration

Only these values need to be set in the environment. Everything else (snapshot interval, video quality, retention, formats, timezones, etc.) is configured in the **Admin → Settings** panel after first launch.

| Variable | Required | Description |
|---|---|---|
| `UFP_HOST` | Yes | IP or hostname of your UniFi Protect controller |
| `UFP_API_KEY` | Yes | API key from UniFi OS → Integrations |
| `TARGET_CAMERA_ID` | Yes | Camera ID from the Protect URL |
| `APP_KEY` | Yes | Base64 secret for session signing |
| `ADMIN_PASSWORD` | Yes | Initial password for the `admin` account |
| `TZ` | No | Container timezone (e.g. `Australia/Sydney`) |
| `GIN_MODE` | No | Set to `release` for production (default) |

---

## Docker image tags

| Tag | Description |
|---|---|
| `v1.2.3` | Specific release — recommended for production |
| `latest` | Latest build from `main` |
| `dev` | Latest build from development branches |

Pull from Docker Hub: `mbern/unifi-time-machine`

---

## Permissions

The container runs as `appuser` (UID/GID `1000`). If you're using a bind-mounted data directory, set ownership on the host:

```bash
chown -R 1000:1000 ./data
```

---

## Contributing

Issues and PRs are welcome. See [DEVGUIDE.md](DEVGUIDE.md) for build instructions and developer notes.

## Roadmap

- GPU encoding support
- Multi-camera support
- Cloud / tiered storage for edge deployments
- AI video summaries
