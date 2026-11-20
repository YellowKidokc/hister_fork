# Nogon on a Synology NAS with Portainer

This sets up three containers as one Portainer stack:

| Container           | What it is                                         | Address                   |
| ------------------- | -------------------------------------------------- | ------------------------- |
| `Nogon`             | The search engine, homepage groups, `/research`    | http://192.168.1.177:4433 |
| `Nogon-Research`    | The Theophysics research control center            | http://192.168.1.177:4444 |
| `Nogon-Research-DB` | PostgreSQL for the research center (not reachable) | internal only             |

Written for a DS923+ with the stack on **Volume 2**. Change `/volume2` everywhere if yours differs.

## 1. Make the folders (File Station)

Inside the shared folder `docker` on Volume 2, make a folder `nogon`, and inside it three folders:

```
/volume2/docker/nogon/data
/volume2/docker/nogon/research
/volume2/docker/nogon/postgres
```

`data` holds the search index and Nogon's database, `research` holds uploaded sources and receipts, `postgres` holds the research database.

## 2. Bring over your existing install (skip if starting fresh)

Your current search index stays exactly as it is. Nothing is converted.

1. In Portainer (or Container Manager), open your current Hister/Nogon container and note the folder mapped to `/hister/data` (Volumes section).
2. **Stop** the old container. Do not delete it yet.
3. In File Station, **copy** (not move) everything inside that folder into `/volume2/docker/nogon/data`. Copying makes the files belong to your user, which the new container needs. The old folder stays as a backup.
4. If the old container used port 4433, leave it stopped. Delete it once the new one works.

## 3. Find your user ID (once)

The containers run as your Synology user so they can write to the folders. The first admin account is usually `1026` with group `100`, which are the defaults. To check: Control Panel → Task Scheduler → Create → Scheduled Task → User-defined script, user `root`, script `id YOUR_USERNAME`, and tick "Send run details by email". Run it once. The email shows `uid=1026(...) gid=100(users)`. Put those numbers in `PUID` and `PGID` below if they differ.

## 4. Create the stack (Portainer)

**Stacks → Add stack**, name it `nogon`, choose **Repository**:

| Field                | Value                                         |
| -------------------- | --------------------------------------------- |
| Repository URL       | `https://github.com/YellowKidokc/hister_fork` |
| Repository reference | `refs/heads/master`                           |
| Compose path         | `deploy/synology/compose.yaml`                |

Under **Environment variables**, add:

| Name             | Value                                                         |
| ---------------- | ------------------------------------------------------------- |
| `DB_PASSWORD`    | a long random password, letters and numbers only              |
| `RESEARCH_TOKEN` | a long random login token for the research center (24+ chars) |
| `PUID`           | `1026` (from step 3)                                          |
| `PGID`           | `100` (from step 3)                                           |
| `NOGON_URL`      | `http://192.168.1.177:4433`                                   |
| `NOGON_TOKEN`    | optional login token for Nogon; empty means no login          |
| `TZ`             | `America/Chicago`                                             |

Optional: `URL_HOSTS` (sites the research center may download from) and, for local AI that only runs when you ask, `AI_BASE_URL` (for Ollama: `http://192.168.1.177:11434/v1`) and `AI_MODEL`.

For random values, open any terminal and run `openssl rand -hex 24`, or use a password manager.

Click **Deploy the stack**. The first deploy builds both images from the code on GitHub. On a DS923+ that takes roughly 10–20 minutes. Later updates are faster.

## 5. Check it

- http://192.168.1.177:4433 should show the Nogon homepage, with your **Groups** if you copied old data.
- http://192.168.1.177:4433/research should show the research page with **Import links**.
- http://192.168.1.177:4444 should show the research control center (log in with `RESEARCH_TOKEN`).
- Point the browser extension at `http://192.168.1.177:4433` (and enter `NOGON_TOKEN` if you set one).

## Updating later

When new code is pushed to `master`: Portainer → Stacks → `nogon` → **Pull and redeploy**. Your data folders are not touched. Portainer can also do this automatically (GitOps updates → polling).

## If something goes wrong

- **Container stops right away, log says "permission denied"**: `PUID`/`PGID` don't match the owner of the folders. Redo step 3, or copy the data in File Station again so your user owns it.
- **Port already in use**: the old container is still running on 4433. Stop it.
- **Research center won't start**: `DB_PASSWORD` was changed after the first start. PostgreSQL keeps the first password; either set it back or empty the `postgres` folder (this deletes research jobs).

## Notes

- Nogon keeps its own database in `data` as SQLite. Moving that to PostgreSQL, and moving old pages off fast storage after a while, are not built yet.
- `deploy/theophysics/` is the earlier version of this deployment. Use this folder instead.
