# Cost Query Realtime (localext)

Admin async cost query: stream usage_logs → per-job SQLite → online table + streaming CSV export.

## Temp dir / TTL

- Default: `data/cost-query-realtime/` (relative to backend process cwd)
- Override: `COST_QUERY_REALTIME_DIR`
- Job TTL: 24h cleanup
- Max concurrent jobs: 2

## Wiring (minimal existing-file hooks)

1. `routes/admin.go` → `registerCostQueryRealtimeLocal(admin, h)`
2. `server/router.go` → `routes.RegisterCostQueryRealtimePublic(v1)`
3. Frontend route `/admin/cost-query-realtime` + sidebar item

## Streaming export

`export.xlsx` / `export.csv` stream CSV (UTF-8 BOM, Chinese headers, no Content-Length).
Browser download uses one-time ticket URL (no JWT on the stream) so the download bar grows immediately.


## Share link (2h reusable)

- Admin: `POST /api/v1/admin/cost-query-realtime/jobs/:id/share` → `{ url, hashcode, expires_at }`
- Public page: `/share/cost-query-realtime?hashcode=...` (no login)
- Public APIs: `/api/v1/share/cost-query-realtime/{meta,stats,rows,export.csv}`
- Token: 32-byte crypto/rand, base64url; server stores SHA-256 only
- Job temp files kept until share expires (even past default 24h job TTL if share still valid)
- IP rate limit on public share APIs (60/min, export 10/min)
