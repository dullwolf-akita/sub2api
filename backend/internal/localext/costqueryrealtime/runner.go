package costqueryrealtime

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"
)

const ingestBatch = 2000

const pgSelectSQL = `
SELECT
  ul.id,
  ul.created_at,
  u.email AS user_email,
  ul.user_id,
  ak.name AS api_key_name,
  ul.api_key_id,
  a.name AS account_name,
  ul.account_id,
  COALESCE(NULLIF(ul.requested_model, ''), ul.model) AS requested_model,
  ul.model AS model_raw,
  ul.upstream_model,
  ul.upstream_response_model,
  ul.upstream_model_mismatch,
  ul.model_mapping_chain,
  ul.channel_id,
  ul.requested_reasoning_effort,
  ul.reasoning_effort,
  ul.service_tier,
  g.name AS group_name,
  ul.group_id,
  ul.subscription_id,
  ul.inbound_endpoint,
  ul.upstream_endpoint,
  ul.request_type,
  ul.stream,
  ul.openai_ws_mode,
  ul.native_compaction_v2,
  ul.billing_type,
  ul.billing_mode,
  ul.billing_tier,
  ul.input_tokens,
  ul.output_tokens,
  ul.cache_creation_tokens,
  ul.cache_read_tokens,
  ul.cache_creation_5m_tokens,
  ul.cache_creation_1h_tokens,
  ul.image_input_tokens,
  ul.image_output_tokens,
  ul.input_cost,
  ul.output_cost,
  ul.cache_creation_cost,
  ul.cache_read_cost,
  ul.image_input_cost,
  ul.image_output_cost,
  ul.total_cost,
  ul.actual_cost,
  ul.rate_multiplier,
  ul.account_rate_multiplier,
  ul.account_stats_cost,
  ul.long_context_billing_applied,
  ul.cache_ttl_overridden,
  ul.first_token_ms,
  ul.duration_ms,
  ul.timing_breakdown,
  ul.request_id,
  ul.upstream_request_id,
  ul.session_id,
  ul.user_agent,
  ul.ip_address,
  ul.image_count,
  ul.image_size,
  ul.image_input_size,
  ul.image_output_size,
  ul.image_size_source,
  ul.image_size_breakdown,
  ul.video_count,
  ul.video_resolution,
  ul.video_duration_seconds
FROM usage_logs ul
LEFT JOIN users u ON u.id = ul.user_id
LEFT JOIN api_keys ak ON ak.id = ul.api_key_id
LEFT JOIN accounts a ON a.id = ul.account_id
LEFT JOIN groups g ON g.id = ul.group_id
WHERE ul.user_id = $1 AND ul.created_at >= $2 AND ul.created_at < $3
ORDER BY ul.id ASC
`

func findUserIDByEmail(ctx context.Context, db *sql.DB, email string) (int64, string, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return 0, "", fmt.Errorf("email required")
	}
	var id int64
	var got string
	// Prefer active user; fall back to soft-deleted.
	err := db.QueryRowContext(ctx, `
		SELECT id, email FROM users
		WHERE lower(email) = $1
		ORDER BY (deleted_at IS NULL) DESC, id ASC
		LIMIT 1
	`, email).Scan(&id, &got)
	if err == sql.ErrNoRows {
		return 0, "", fmt.Errorf("user not found: %s", email)
	}
	if err != nil {
		return 0, "", err
	}
	return id, got, nil
}

func (r *Registry) runJob(job *Job) {
	defer r.releaseSlot()
	ctx := context.Background()
	job.setProgress(0, nil, StatusRunning, "")

	pg, err := postgresDB()
	if err != nil {
		job.setProgress(0, nil, StatusFailed, "数据库连接失败: "+err.Error())
		return
	}

	var total int64
	err = pg.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM usage_logs WHERE user_id = $1 AND created_at >= $2 AND created_at < $3`,
		job.UserID, job.StartTime, job.EndTime,
	).Scan(&total)
	if err != nil {
		job.setProgress(0, nil, StatusFailed, "统计行数失败: "+err.Error())
		return
	}
	if total > maxJobRows {
		job.setProgress(0, &total, StatusFailed, fmt.Sprintf("行数过多(%d)，超过上限 %d", total, maxJobRows))
		return
	}
	job.setProgress(0, &total, StatusRunning, "")

	tmpPath := job.SQLitePath + ".tmp"
	_ = removeSQLite(tmpPath)
	sdb, err := openJobSQLite(tmpPath, false)
	if err != nil {
		job.setProgress(0, &total, StatusFailed, "创建临时库失败: "+err.Error())
		return
	}
	defer func() { _ = sdb.Close() }()

	if err := createUsageTable(sdb); err != nil {
		job.setProgress(0, &total, StatusFailed, "建表失败: "+err.Error())
		return
	}

	rows, err := pg.QueryContext(ctx, pgSelectSQL, job.UserID, job.StartTime, job.EndTime)
	if err != nil {
		job.setProgress(0, &total, StatusFailed, "查询 usage_logs 失败: "+err.Error())
		return
	}
	defer rows.Close()

	colDefs := quoteIdentList(ColumnOrder)
	placeholders := strings.Repeat("?,", len(ColumnOrder))
	placeholders = placeholders[:len(placeholders)-1]
	insertSQL := fmt.Sprintf("INSERT INTO usage (%s) VALUES (%s)", colDefs, placeholders)

	tx, err := sdb.Begin()
	if err != nil {
		job.setProgress(0, &total, StatusFailed, err.Error())
		return
	}
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		_ = tx.Rollback()
		job.setProgress(0, &total, StatusFailed, err.Error())
		return
	}

	var done int64
	batch := 0
	for rows.Next() {
		vals, scanErr := scanPGRow(rows)
		if scanErr != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			job.setProgress(done, &total, StatusFailed, "扫描行失败: "+scanErr.Error())
			return
		}
		if _, err := stmt.Exec(vals...); err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			job.setProgress(done, &total, StatusFailed, "写入临时库失败: "+err.Error())
			return
		}
		done++
		batch++
		if batch >= ingestBatch {
			if err := tx.Commit(); err != nil {
				_ = stmt.Close()
				job.setProgress(done, &total, StatusFailed, err.Error())
				return
			}
			tx, err = sdb.Begin()
			if err != nil {
				_ = stmt.Close()
				job.setProgress(done, &total, StatusFailed, err.Error())
				return
			}
			_ = stmt.Close()
			stmt, err = tx.Prepare(insertSQL)
			if err != nil {
				_ = tx.Rollback()
				job.setProgress(done, &total, StatusFailed, err.Error())
				return
			}
			batch = 0
			job.setProgress(done, &total, StatusRunning, "")
			if done%50000 == 0 {
				log.Printf("[cost-query-realtime] job=%s rows=%d/%d", job.ID[:8], done, total)
			}
		}
	}
	_ = stmt.Close()
	if err := rows.Err(); err != nil {
		_ = tx.Rollback()
		job.setProgress(done, &total, StatusFailed, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		job.setProgress(done, &total, StatusFailed, err.Error())
		return
	}

	// indexes + rollups
	_, _ = sdb.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_model ON usage(requested_model)`)
	_, _ = sdb.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_created ON usage(created_at)`)
	_, _ = sdb.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_actual ON usage(actual_cost)`)
	_, _ = sdb.Exec(`DROP TABLE IF EXISTS model_stats`)
	_, _ = sdb.Exec(`
		CREATE TABLE model_stats AS
		SELECT
		  COALESCE(requested_model, '(空)') AS model,
		  COUNT(*) AS cnt,
		  COALESCE(SUM(actual_cost), 0) AS sum_actual,
		  COALESCE(SUM(total_cost), 0) AS sum_total,
		  COALESCE(SUM(input_tokens), 0) AS sum_in,
		  COALESCE(SUM(output_tokens), 0) AS sum_out
		FROM usage
		GROUP BY COALESCE(requested_model, '(空)')
	`)
	_, _ = sdb.Exec(`DROP TABLE IF EXISTS overall_stats`)
	_, _ = sdb.Exec(`
		CREATE TABLE overall_stats AS
		SELECT
		  COUNT(*) AS cnt,
		  COALESCE(SUM(actual_cost), 0) AS sum_actual,
		  COALESCE(SUM(total_cost), 0) AS sum_total,
		  COALESCE(SUM(input_tokens), 0) AS sum_in,
		  COALESCE(SUM(output_tokens), 0) AS sum_out,
		  COUNT(DISTINCT COALESCE(requested_model, '(空)')) AS model_count
		FROM usage
	`)
	_ = metaSet(sdb, "columns", columnsJSON())
	_ = metaSet(sdb, "row_count", fmt.Sprintf("%d", done))
	_ = metaSet(sdb, "email", job.Email)
	_ = metaSet(sdb, "user_id", fmt.Sprintf("%d", job.UserID))
	_ = metaSet(sdb, "start_date", job.StartDate)
	_ = metaSet(sdb, "end_date", job.EndDate)
	_, _ = sdb.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	_ = sdb.Close()

	_ = removeSQLite(job.SQLitePath)
	if err := renameSQLite(tmpPath, job.SQLitePath); err != nil {
		job.setProgress(done, &total, StatusFailed, "落盘失败: "+err.Error())
		return
	}
	job.setProgress(done, &total, StatusDone, "")
	log.Printf("[cost-query-realtime] job=%s done rows=%d", job.ID[:8], done)
}

func createUsageTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)`)
	if err != nil {
		return err
	}
	parts := make([]string, 0, len(ColumnOrder))
	for _, c := range ColumnOrder {
		parts = append(parts, fmt.Sprintf(`"%s" %s`, c, sqliteType(c)))
	}
	_, err = db.Exec(`DROP TABLE IF EXISTS usage`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE TABLE usage (` + strings.Join(parts, ",") + `)`)
	return err
}

func sqliteType(col string) string {
	switch col {
	case "input_cost", "output_cost", "cache_creation_cost", "cache_read_cost",
		"image_input_cost", "image_output_cost", "total_cost", "actual_cost",
		"rate_multiplier", "account_rate_multiplier", "account_stats_cost", "account_billed",
		"video_duration_seconds":
		return "REAL"
	case "id", "user_id", "api_key_id", "account_id", "channel_id", "group_id", "subscription_id",
		"request_type", "billing_type",
		"input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
		"cache_creation_5m_tokens", "cache_creation_1h_tokens",
		"image_input_tokens", "image_output_tokens",
		"first_token_ms", "duration_ms", "image_count", "video_count",
		"upstream_model_mismatch", "stream", "openai_ws_mode", "native_compaction_v2",
		"long_context_billing_applied", "cache_ttl_overridden":
		return "INTEGER"
	default:
		return "TEXT"
	}
}

func quoteIdentList(cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = `"` + c + `"`
	}
	return strings.Join(out, ",")
}

func scanPGRow(rows *sql.Rows) ([]any, error) {
	var (
		id                                                   int64
		createdAt                                            time.Time
		userEmail                                            sql.NullString
		userID, apiKeyID, accountID                          int64
		apiKeyName, accountName                              sql.NullString
		requestedModel, modelRaw                             sql.NullString
		upstreamModel, upstreamRespModel                     sql.NullString
		upstreamMismatch                                     sql.NullBool
		modelMappingChain                                    sql.NullString
		channelID                                            sql.NullInt64
		reqReasoning, reasoning, serviceTier                 sql.NullString
		groupName                                            sql.NullString
		groupID, subscriptionID                              sql.NullInt64
		inboundEP, upstreamEP                                sql.NullString
		requestType                                          sql.NullInt64
		stream, openaiWS, nativeCompaction                   sql.NullBool
		billingType                                          sql.NullInt64
		billingMode, billingTier                            sql.NullString
		inputTokens, outputTokens                            sql.NullInt64
		cacheCreate, cacheRead                               sql.NullInt64
		cache5m, cache1h                                     sql.NullInt64
		imgInTok, imgOutTok                                  sql.NullInt64
		inputCost, outputCost, cacheCreateCost, cacheReadCost sql.NullFloat64
		imgInCost, imgOutCost                                sql.NullFloat64
		totalCost, actualCost, rateMult                      sql.NullFloat64
		acctRateMult, acctStatsCost                          sql.NullFloat64
		longCtx, cacheTTL                                    sql.NullBool
		firstTokenMs, durationMs                             sql.NullInt64
		timingBreakdown                                      []byte
		requestID, upstreamReqID, sessionID                  sql.NullString
		userAgent, ipAddress                                 sql.NullString
		imageCount                                           sql.NullInt64
		imageSize, imageInSize, imageOutSize                 sql.NullString
		imageSizeSource                                      sql.NullString
		imageSizeBreakdown                                   []byte
		videoCount                                           sql.NullInt64
		videoResolution                                      sql.NullString
		videoDuration                                        sql.NullInt64
	)

	err := rows.Scan(
		&id, &createdAt, &userEmail, &userID,
		&apiKeyName, &apiKeyID, &accountName, &accountID,
		&requestedModel, &modelRaw, &upstreamModel, &upstreamRespModel,
		&upstreamMismatch, &modelMappingChain, &channelID,
		&reqReasoning, &reasoning, &serviceTier,
		&groupName, &groupID, &subscriptionID,
		&inboundEP, &upstreamEP,
		&requestType, &stream, &openaiWS, &nativeCompaction,
		&billingType, &billingMode, &billingTier,
		&inputTokens, &outputTokens, &cacheCreate, &cacheRead,
		&cache5m, &cache1h, &imgInTok, &imgOutTok,
		&inputCost, &outputCost, &cacheCreateCost, &cacheReadCost,
		&imgInCost, &imgOutCost,
		&totalCost, &actualCost, &rateMult,
		&acctRateMult, &acctStatsCost,
		&longCtx, &cacheTTL,
		&firstTokenMs, &durationMs, &timingBreakdown,
		&requestID, &upstreamReqID, &sessionID,
		&userAgent, &ipAddress,
		&imageCount, &imageSize, &imageInSize, &imageOutSize,
		&imageSizeSource, &imageSizeBreakdown,
		&videoCount, &videoResolution, &videoDuration,
	)
	if err != nil {
		return nil, err
	}

	rt := int16(0)
	if requestType.Valid {
		rt = int16(requestType.Int64)
	}
	st := stream.Bool
	ws := openaiWS.Bool
	bt := int16(0)
	if billingType.Valid {
		bt = int16(billingType.Int64)
	}

	accountBilled := any(nil)
	if acctStatsCost.Valid {
		accountBilled = acctStatsCost.Float64
	} else if totalCost.Valid {
		mult := 1.0
		if acctRateMult.Valid {
			mult = acctRateMult.Float64
		}
		accountBilled = totalCost.Float64 * mult
	}

	timingStr := any(nil)
	if len(timingBreakdown) > 0 {
		timingStr = string(timingBreakdown)
	}
	imgBreakStr := any(nil)
	if len(imageSizeBreakdown) > 0 {
		imgBreakStr = string(imageSizeBreakdown)
	}

	m := map[string]any{
		"id": id,
		"created_at": createdAt.UTC().Format(time.RFC3339Nano),
		"user_email": nullStr(userEmail),
		"user_id": userID,
		"api_key_name": nullStr(apiKeyName),
		"api_key_id": apiKeyID,
		"account_name": nullStr(accountName),
		"account_id": accountID,
		"requested_model": nullStr(requestedModel),
		"model_raw": nullStr(modelRaw),
		"upstream_model": nullStr(upstreamModel),
		"upstream_response_model": nullStr(upstreamRespModel),
		"upstream_model_mismatch": boolInt(upstreamMismatch),
		"model_mapping_chain": nullStr(modelMappingChain),
		"channel_id": nullInt(channelID),
		"requested_reasoning_effort": nullStr(reqReasoning),
		"reasoning_effort": nullStr(reasoning),
		"service_tier": nullStr(serviceTier),
		"group_name": nullStr(groupName),
		"group_id": nullInt(groupID),
		"subscription_id": nullInt(subscriptionID),
		"inbound_endpoint": nullStr(inboundEP),
		"upstream_endpoint": nullStr(upstreamEP),
		"request_type_label": requestTypeLabel(rt, st, ws),
		"request_type": rt,
		"stream": boolInt(stream),
		"openai_ws_mode": boolInt(openaiWS),
		"native_compaction_v2": boolInt(nativeCompaction),
		"billing_type_label": billingTypeLabel(bt),
		"billing_type": bt,
		"billing_mode": nullStr(billingMode),
		"billing_tier": nullStr(billingTier),
		"input_tokens": nullInt(inputTokens),
		"output_tokens": nullInt(outputTokens),
		"cache_creation_tokens": nullInt(cacheCreate),
		"cache_read_tokens": nullInt(cacheRead),
		"cache_creation_5m_tokens": nullInt(cache5m),
		"cache_creation_1h_tokens": nullInt(cache1h),
		"image_input_tokens": nullInt(imgInTok),
		"image_output_tokens": nullInt(imgOutTok),
		"input_cost": nullFloat(inputCost),
		"output_cost": nullFloat(outputCost),
		"cache_creation_cost": nullFloat(cacheCreateCost),
		"cache_read_cost": nullFloat(cacheReadCost),
		"image_input_cost": nullFloat(imgInCost),
		"image_output_cost": nullFloat(imgOutCost),
		"total_cost": nullFloat(totalCost),
		"actual_cost": nullFloat(actualCost),
		"rate_multiplier": nullFloat(rateMult),
		"account_rate_multiplier": nullFloat(acctRateMult),
		"account_stats_cost": nullFloat(acctStatsCost),
		"account_billed": accountBilled,
		"long_context_billing_applied": boolInt(longCtx),
		"cache_ttl_overridden": boolInt(cacheTTL),
		"first_token_ms": nullInt(firstTokenMs),
		"duration_ms": nullInt(durationMs),
		"timing_breakdown": timingStr,
		"request_id": nullStr(requestID),
		"upstream_request_id": nullStr(upstreamReqID),
		"session_id": nullStr(sessionID),
		"user_agent": nullStr(userAgent),
		"ip_address": nullStr(ipAddress),
		"image_count": nullInt(imageCount),
		"image_size": nullStr(imageSize),
		"image_input_size": nullStr(imageInSize),
		"image_output_size": nullStr(imageOutSize),
		"image_size_source": nullStr(imageSizeSource),
		"image_size_breakdown": imgBreakStr,
		"video_count": nullInt(videoCount),
		"video_resolution": nullStr(videoResolution),
		"video_duration_seconds": nullInt(videoDuration),
	}

	out := make([]any, len(ColumnOrder))
	for i, c := range ColumnOrder {
		out[i] = m[c]
	}
	return out, nil
}

func nullStr(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}
func nullInt(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}
func nullFloat(v sql.NullFloat64) any {
	if !v.Valid {
		return nil
	}
	return v.Float64
}
func boolInt(v sql.NullBool) any {
	if !v.Valid {
		return nil
	}
	if v.Bool {
		return 1
	}
	return 0
}

func removeSQLite(path string) error {
	_ = removeFile(path)
	_ = removeFile(path + "-wal")
	_ = removeFile(path + "-shm")
	return nil
}

func removeFile(p string) error {
	return removePath(p)
}
