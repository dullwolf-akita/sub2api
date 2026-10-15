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
