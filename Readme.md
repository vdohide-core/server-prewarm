# server-prewarm

Background HLS CDN cache prewarmer service for vdohide-core.  
Scans transcoded media files from MongoDB, parses HLS `.m3u8` playlists, and prewarms playlist indices, HLS segments (`.ts`), and poster images concurrently against edge CDNs to ensure fast global delivery.  
Provides a sleek real-time Web UI Dashboard displaying overall progress, hit rates, and WebSocket streams of active cache warm-up jobs.

---

## Features

### Prewarming Engine
- Concurrently resolves master and child HLS playlists.
- Triggers `HEAD` requests to warm up Cloudflare/CDN caches.
- Captures and reports CDN status (e.g. `CF-Cache-Status` HIT/MISS/EXPIRED/Failed) and POP edge locations.
- **Bi-level scheduler slots**:
  - **New videos**: Auto-scans and prewarms newly processed videos on the target POP.
  - **Old videos (Re-prewarm)**: Cycles through older media records to re-trigger cache status.

### Real-time Web Dashboard (`/`)
- Display overall status (total files, pending, hit rates, misses).
- Live active jobs cards showing active files and prewarming progress bars.
- Live logs list in History tab featuring pagination and search filter.
- Real-time updates pushed using WebSockets.

---

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `HTTP_PORT` / `PORT` | `8886` | HTTP listen port for Dashboard & API |
| `MONGODB_URI` / `MONGO_URI` | _(required)_ | MongoDB connection string (goose ODM) |
| `STORAGE_ID` | _(empty)_ | Filter media records to only prewarm from this storage ID |
| `PREWARM_POP` | `fra` | Edge location identifier for current node (e.g., `fra`, `sin`) |
| `MAX_CONCURRENT` | `5` | Maximum number of HLS playlists to prewarm concurrently |
| `PARALLEL` | `20` | Concurrent HTTP/HEAD connections per playlist job |

---

## Settings (MongoDB `settings` collection)

The engine dynamically reads the following configurations from the MongoDB `settings` collection:

| Name | Type | Description |
|---|---|---|
| `prewarm_enabled` | `boolean` / `string` | Global toggle. Set to `false` to pause prewarming background loops. |
| `domain_content` | `string` | Base CDN/storage domain (e.g., `cdn.vdohls.com`) for HLS playlist URL construction (fallback: `domain_asset`). |
| `domain_preview` | `string` | Reference player domain (e.g., `preview.fembed.co`) passed in the HTTP `Referer` header (fallback: `domain_player`). |
| `prewarm_max_concurrent` | `number` | Overrides `MAX_CONCURRENT` environment variable dynamically. |
| `prewarm_parallel` | `number` | Overrides `PARALLEL` environment variable dynamically. |

---

## API Routes

| Method | Path | Description |
|---|---|---|
| `GET` | `/` | Serves the HTML Dashboard UI |
| `GET` | `/api/status` | Current overall prewarming state (JSON) |
| `GET` | `/api/logs` | Search, filter, and paginate historical prewarm logs |
| `POST`| `/api/start` | Manually start the prewarm manager routine |
| `GET` | `/ws` | WebSocket handler for real-time dashboard events |

---

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/vdohide-core/server-prewarm/main/install.sh | sudo bash -s -- \
    --port 8886 \
    --mongodb-uri "mongodb+srv://user:pass@host/platform"
```

### Update binary only (preserve `.env`)

```bash
curl -fsSL https://raw.githubusercontent.com/vdohide-core/server-prewarm/main/install.sh | sudo bash -s -- \
    --port 8886
```

### Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/vdohide-core/server-prewarm/main/install.sh | sudo bash -s -- --uninstall
```
