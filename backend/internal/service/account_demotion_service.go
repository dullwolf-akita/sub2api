package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
)

const (
	accountDemotionJobName = "account_demotion"

	// Redis leader lock keys.
	accountDemotionWindowLockKey       = "account_demotion:window:leader"
	accountDemotionConsecutiveLockKey   = "account_demotion:consecutive:leader"
	accountDemotionLeaderLockTTL        = 5 * time.Minute

	// TPS upper bound to filter passthrough/resold upstream token-inflation noise.
	accountDemotionTPSUpperBound = 500.0
)

var accountDemotionCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// accountDemotionReleaseScript releases the leader lock only if we still own it.
var accountDemotionReleaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

// AccountDemotionService periodically evaluates account health metrics
// (error rate, TPS, TTFT, consecutive errors) and sends Lark notifications
// for accounts that breach thresholds. It does NOT modify account priority
// in this phase — that is a later milestone.
type AccountDemotionService struct {
	db          *sql.DB
	redisClient *redis.Client
	cfg         *config.Config
	instanceID  string
	loc         *time.Location

	stopCh    chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup

	cron *cron.Cron

	warnNoRedisOnce sync.Once
}

// NewAccountDemotionService constructs the service. It does not start anything
// until Start() is called.
func NewAccountDemotionService(
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
) *AccountDemotionService {
	loc := time.Local
	if cfg != nil && strings.TrimSpace(cfg.Timezone) != "" {
		if parsed, err := time.LoadLocation(strings.TrimSpace(cfg.Timezone)); err == nil && parsed != nil {
			loc = parsed
		}
	}
	return &AccountDemotionService{
		db:          db,
		redisClient: redisClient,
		cfg:         cfg,
		instanceID:  uuid.NewString(),
		loc:         loc,
	}
}

// Start launches the cron jobs for task A (window) and task B (consecutive errors).
func (s *AccountDemotionService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		d := s.cfg.Ops.AccountDemotion
		if !d.Enabled {
			logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] service disabled by config")
			return
		}
		s.stopCh = make(chan struct{})
		c := cron.New(cron.WithParser(accountDemotionCronParser), cron.WithLocation(s.loc))

		windowSched := strings.TrimSpace(d.WindowJobSchedule)
		if windowSched == "" {
			windowSched = "5 * * * *"
		}
		if _, err := c.AddFunc(windowSched, s.runWindowJob); err != nil {
			logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] failed to register window job: %v (schedule=%s)", err, windowSched)
			return
		}

		consecSched := strings.TrimSpace(d.ConsecutiveJobSchedule)
		if consecSched == "" {
			consecSched = "*/10 * * * *"
		}
		if _, err := c.AddFunc(consecSched, s.runConsecutiveJob); err != nil {
			logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] failed to register consecutive job: %v (schedule=%s)", err, consecSched)
			return
		}

		s.cron = c
		c.Start()
		logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] started window_schedule=%s consecutive_schedule=%s", windowSched, consecSched)
	})
}

// Stop waits for the cron to stop.
func (s *AccountDemotionService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.stopCh != nil {
			close(s.stopCh)
		}
		if s.cron != nil {
			s.cron.Stop()
		}
		s.wg.Wait()
	})
}

// acquireLeaderLock attempts a Redis SetNX leader lock so only one instance runs.
func (s *AccountDemotionService) acquireLeaderLock(ctx context.Context, key string) bool {
	if s.redisClient == nil {
		s.warnNoRedisOnce.Do(func() {
			logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] Redis not configured; running single-instance (no leader lock)")
		})
		return true
	}
	ttl := accountDemotionLeaderLockTTL
	ok, err := s.redisClient.SetNX(ctx, key, s.instanceID, ttl).Result()
	if err != nil {
		logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] leader lock SetNX failed: %v", err)
		return false
	}
	if !ok {
		return false
	}
	return true
}

// releaseLeaderLock releases the lock only if we still own it.
func (s *AccountDemotionService) releaseLeaderLock(ctx context.Context, key string) {
	if s.redisClient == nil {
		return
	}
	_, _ = accountDemotionReleaseScript.Run(ctx, s.redisClient, []string{key}, s.instanceID).Result()
}

// runWindowJob executes task A: error rate + TPS + TTFT evaluation over a 1-2h window.
func (s *AccountDemotionService) runWindowJob() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if !s.acquireLeaderLock(ctx, accountDemotionWindowLockKey) {
		return
	}
	defer s.releaseLeaderLock(context.Background(), accountDemotionWindowLockKey)

	d := s.cfg.Ops.AccountDemotion

	// --- Error rate ---
	if triggers := s.evalErrorRate(ctx, d); len(triggers) > 0 {
		s.notifyLark(ctx, "窗口判据-错误率", triggers)
	}

	// --- TPS ratio ---
	if triggers := s.evalTPSRatio(ctx, d); len(triggers) > 0 {
		s.notifyLark(ctx, "窗口判据-TPS偏低", triggers)
	}

	// --- TTFT ratio ---
	if triggers := s.evalTTFTRatio(ctx, d); len(triggers) > 0 {
		s.notifyLark(ctx, "窗口判据-TTFT偏高", triggers)
	}
}

// runConsecutiveJob executes task B: consecutive-error detection.
func (s *AccountDemotionService) runConsecutiveJob() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if !s.acquireLeaderLock(ctx, accountDemotionConsecutiveLockKey) {
		return
	}
	defer s.releaseLeaderLock(context.Background(), accountDemotionConsecutiveLockKey)

	d := s.cfg.Ops.AccountDemotion
	triggers := s.evalConsecutiveErrors(ctx, d)
	if len(triggers) > 0 {
		s.notifyLark(ctx, "连续错误判据", triggers)
	}
}

// demotionTrigger is a unified payload for one account that breached a threshold.
type demotionTrigger struct {
	AccountID   int64   `json:"account_id"`
	AccountName string  `json:"account_name"`
	Platform    string  `json:"platform"`
	Model       string  `json:"model,omitempty"`
	MetricType  string  `json:"metric_type"`
	Value       float64 `json:"value"`
	Samples     int     `json:"samples"`
	Detail      string  `json:"detail,omitempty"`
}

// ── Task A: Error Rate ─────────────────────────────────────────────────────

func (s *AccountDemotionService) evalErrorRate(ctx context.Context, d config.OpsAccountDemotionConfig) []demotionTrigger {
	minSamples := d.ErrorRateMinSamples
	if minSamples <= 0 {
		minSamples = 20
	}
	threshold := d.ErrorRateThreshold
	if threshold <= 0 {
		threshold = 0.3
	}

	q := `
WITH err AS (
  SELECT account_id, count(*) AS err_cnt
  FROM ops_error_logs
  WHERE created_at >= now() - interval '1 hour'
    AND account_id IS NOT NULL
    AND COALESCE(status_code, 0) >= 400
    AND COALESCE(error_owner, '') = 'provider'
    AND is_business_limited = false
    AND COALESCE(is_count_tokens, false) = false
  GROUP BY account_id
),
ok AS (
  SELECT account_id, count(*) AS ok_cnt
  FROM usage_logs
  WHERE created_at >= now() - interval '1 hour'
  GROUP BY account_id
)
SELECT a.id, a.name, COALESCE(a.platform, ''),
       COALESCE(err.err_cnt, 0) AS errors,
       COALESCE(ok.ok_cnt, 0) + COALESCE(err.err_cnt, 0) AS total,
       CASE
         WHEN COALESCE(ok.ok_cnt, 0) + COALESCE(err.err_cnt, 0) = 0 THEN 0
         ELSE COALESCE(err.err_cnt, 0)::numeric
              / (COALESCE(ok.ok_cnt, 0) + COALESCE(err.err_cnt, 0))
       END AS error_rate
FROM accounts a
LEFT JOIN err ON err.account_id = a.id
LEFT JOIN ok  ON ok.account_id  = a.id
WHERE a.status = 'active'
  AND COALESCE(err.err_cnt, 0) + COALESCE(ok.ok_cnt, 0) >= $1
  AND (
    CASE
      WHEN COALESCE(ok.ok_cnt, 0) + COALESCE(err.err_cnt, 0) = 0 THEN 0
      ELSE COALESCE(err.err_cnt, 0)::numeric
           / (COALESCE(ok.ok_cnt, 0) + COALESCE(err.err_cnt, 0))
    END
  ) >= $2`

	rows, err := s.db.QueryContext(ctx, q, minSamples, threshold)
	if err != nil {
		logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] error_rate query failed: %v", err)
		return nil
	}
	defer rows.Close()

	var results []demotionTrigger
	for rows.Next() {
		var t demotionTrigger
		var errors, total int64
		var rate float64
		if err := rows.Scan(&t.AccountID, &t.AccountName, &t.Platform, &errors, &total, &rate); err != nil {
			continue
		}
		t.MetricType = "error_rate"
		t.Value = rate
		t.Samples = int(total)
		t.Detail = fmt.Sprintf("errors=%d total=%d rate=%.1f%%", errors, total, rate*100)
		results = append(results, t)
	}
	return results
}

// ── Task A: TPS Ratio ──────────────────────────────────────────────────────

func (s *AccountDemotionService) evalTPSRatio(ctx context.Context, d config.OpsAccountDemotionConfig) []demotionTrigger {
	minSamples := d.TPSMinSamples
	if minSamples <= 0 {
		minSamples = 50
	}
	threshold := d.TPSRatioThreshold
	if threshold <= 0 {
		threshold = 0.5
	}

	q := `
WITH per_req AS (
  SELECT l.account_id,
         COALESCE(NULLIF(g.platform,''), a.platform, '') AS platform,
         l.model,
         l.output_tokens * 1000.0 / NULLIF(l.duration_ms, 0) AS tps
  FROM usage_logs l
  JOIN accounts a ON a.id = l.account_id
  LEFT JOIN groups g ON g.id = l.group_id
  WHERE l.created_at >= now() - interval '2 hours'
    AND l.request_type = 2
    AND l.output_tokens >= 100
    AND l.duration_ms >= 1000
),
clean AS (
  SELECT * FROM per_req WHERE tps BETWEEN 1 AND $1
),
per_acc AS (
  SELECT account_id, platform, model,
         count(*) AS n,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY tps) AS p50_tps
  FROM clean
  GROUP BY account_id, platform, model
  HAVING count(*) >= $2
),
pool AS (
  SELECT platform, model,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY p50_tps) AS pool_p50
  FROM per_acc
  GROUP BY platform, model
  HAVING count(*) >= 2
)
SELECT pa.account_id, a.name, pa.platform, pa.model, pa.n,
       round(pa.p50_tps::numeric, 2) AS p50_tps,
       round(pool.pool_p50::numeric, 2) AS pool_p50,
       round((pa.p50_tps / NULLIF(pool.pool_p50, 0))::numeric, 3) AS ratio
FROM per_acc pa
JOIN pool ON pool.platform = pa.platform AND pool.model = pa.model
JOIN accounts a ON a.id = pa.account_id
WHERE pa.p50_tps / NULLIF(pool.pool_p50, 0) < $3
ORDER BY ratio ASC`

	rows, err := s.db.QueryContext(ctx, q, accountDemotionTPSUpperBound, minSamples, threshold)
	if err != nil {
		logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] tps_ratio query failed: %v", err)
		return nil
	}
	defer rows.Close()

	var results []demotionTrigger
	for rows.Next() {
		var t demotionTrigger
		var n int
		var p50, poolP50, ratio float64
		if err := rows.Scan(&t.AccountID, &t.AccountName, &t.Platform, &t.Model, &n, &p50, &poolP50, &ratio); err != nil {
			continue
		}
		t.MetricType = "tps_ratio"
		t.Value = ratio
		t.Samples = n
		t.Detail = fmt.Sprintf("p50_tps=%.1f pool_p50=%.1f ratio=%.2f", p50, poolP50, ratio)
		results = append(results, t)
	}
	return results
}

// ── Task A: TTFT Ratio ─────────────────────────────────────────────────────

func (s *AccountDemotionService) evalTTFTRatio(ctx context.Context, d config.OpsAccountDemotionConfig) []demotionTrigger {
	minSamples := d.TTFTMinSamples
	if minSamples <= 0 {
		minSamples = 30
	}
	threshold := d.TTFTRatioThreshold
	if threshold <= 0 {
		threshold = 2.0
	}

	q := `
WITH per_acc AS (
  SELECT l.account_id,
         COALESCE(NULLIF(g.platform,''), a.platform, '') AS platform,
         l.model,
         count(*) AS n,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY l.first_token_ms) AS p50_ttft
  FROM usage_logs l
  JOIN accounts a ON a.id = l.account_id
  LEFT JOIN groups g ON g.id = l.group_id
  WHERE l.created_at >= now() - interval '1 hour'
    AND l.request_type = 2
    AND l.first_token_ms IS NOT NULL
  GROUP BY l.account_id, platform, l.model
  HAVING count(*) >= $1
),
pool AS (
  SELECT platform, model,
         percentile_cont(0.5) WITHIN GROUP (ORDER BY p50_ttft) AS pool_p50
  FROM per_acc
  GROUP BY platform, model
  HAVING count(*) >= 2
)
SELECT pa.account_id, a.name, pa.platform, pa.model, pa.n,
       round(pa.p50_ttft::numeric) AS p50_ttft,
       round(pool.pool_p50::numeric) AS pool_p50,
       round((pa.p50_ttft / NULLIF(pool.pool_p50, 0))::numeric, 3) AS ratio
FROM per_acc pa
JOIN pool ON pool.platform = pa.platform AND pool.model = pa.model
JOIN accounts a ON a.id = pa.account_id
WHERE pa.p50_ttft / NULLIF(pool.pool_p50, 0) > $2
ORDER BY ratio DESC`

	rows, err := s.db.QueryContext(ctx, q, minSamples, threshold)
	if err != nil {
		logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] ttft_ratio query failed: %v", err)
		return nil
	}
	defer rows.Close()

	var results []demotionTrigger
	for rows.Next() {
		var t demotionTrigger
		var n int
		var p50, poolP50, ratio float64
		if err := rows.Scan(&t.AccountID, &t.AccountName, &t.Platform, &t.Model, &n, &p50, &poolP50, &ratio); err != nil {
			continue
		}
		t.MetricType = "ttft_ratio"
		t.Value = ratio
		t.Samples = n
		t.Detail = fmt.Sprintf("p50_ttft=%.0fms pool_p50=%.0fms ratio=%.2f", p50, poolP50, ratio)
		results = append(results, t)
	}
	return results
}

// ── Task B: Consecutive Errors ─────────────────────────────────────────────

func (s *AccountDemotionService) evalConsecutiveErrors(ctx context.Context, d config.OpsAccountDemotionConfig) []demotionTrigger {
	nth := d.ConsecutiveErrorThreshold
	if nth <= 0 {
		nth = 5
	}

	q := `
WITH recent_error_accounts AS (
  SELECT DISTINCT account_id
  FROM ops_error_logs
  WHERE created_at >= now() - interval '10 minutes'
    AND account_id IS NOT NULL
    AND COALESCE(status_code, 0) >= 400
    AND COALESCE(error_owner, '') = 'provider'
    AND is_business_limited = false
    AND COALESCE(is_count_tokens, false) = false
),
nth_error AS (
  SELECT account_id, created_at AS nth_error_at
  FROM (
    SELECT account_id, created_at,
           ROW_NUMBER() OVER (
             PARTITION BY account_id ORDER BY created_at DESC
           ) AS rn
    FROM ops_error_logs
    WHERE account_id IN (SELECT account_id FROM recent_error_accounts)
      AND COALESCE(status_code, 0) >= 400
      AND COALESCE(error_owner, '') = 'provider'
      AND is_business_limited = false
      AND COALESCE(is_count_tokens, false) = false
  ) ranked
  WHERE rn = $1
),
success_after AS (
  SELECT fe.account_id, fe.nth_error_at, COUNT(ul.id) AS success_count
  FROM nth_error fe
  LEFT JOIN usage_logs ul
    ON ul.account_id = fe.account_id
    AND ul.created_at > fe.nth_error_at
  GROUP BY fe.account_id, fe.nth_error_at
)
SELECT sa.account_id, a.name, COALESCE(a.platform,''), sa.nth_error_at
FROM success_after sa
JOIN accounts a ON a.id = sa.account_id
WHERE sa.success_count = 0
  AND a.status = 'active'`

	rows, err := s.db.QueryContext(ctx, q, nth)
	if err != nil {
		logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] consecutive_errors query failed: %v", err)
		return nil
	}
	defer rows.Close()

	var results []demotionTrigger
	for rows.Next() {
		var t demotionTrigger
		var nthErrorAt time.Time
		if err := rows.Scan(&t.AccountID, &t.AccountName, &t.Platform, &nthErrorAt); err != nil {
			continue
		}
		t.MetricType = "consecutive_errors"
		t.Value = float64(nth)
		t.Samples = nth
		t.Detail = fmt.Sprintf("第%d条错误时间=%s 之后无成功请求", nth, nthErrorAt.Format("15:04:05"))
		results = append(results, t)
	}
	return results
}

// ── Lark notification ──────────────────────────────────────────────────────

func (s *AccountDemotionService) notifyLark(ctx context.Context, jobType string, triggers []demotionTrigger) {
	if s == nil || s.cfg == nil {
		return
	}
	webhookURL := strings.TrimSpace(s.cfg.Ops.AccountDemotion.LarkWebhookURL)
	if webhookURL == "" {
		logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] notify skip: no lark_webhook_url configured (job=%s count=%d)", jobType, len(triggers))
		return
	}

	text := buildLarkText(jobType, triggers)
	payload := map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": text},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] notify marshal failed: %v", err)
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] notify build request failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] notify send failed: %v", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	if resp.StatusCode != http.StatusOK {
		logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] notify unexpected status %d body=%s", resp.StatusCode, string(respBody))
		return
	}
	logger.LegacyPrintf("service.account_demotion", "[AccountDemotion] notify sent job=%s count=%d", jobType, len(triggers))
}

func buildLarkText(jobType string, triggers []demotionTrigger) string {
	now := time.Now().Format("2006-01-02 15:04:05")
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("【账号降权预警】%s\n", now))
	sb.WriteString(fmt.Sprintf("判据类型：%s\n", jobType))
	sb.WriteString(fmt.Sprintf("触发账号数：%d\n\n", len(triggers)))
	for i, t := range triggers {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s (id=%s)\n",
			i+1, t.Platform, t.AccountName, strconv.FormatInt(t.AccountID, 10)))
		if t.Model != "" {
			sb.WriteString(fmt.Sprintf("   模型: %s\n", t.Model))
		}
		sb.WriteString(fmt.Sprintf("   指标: %s  样本: %d\n", t.MetricType, t.Samples))
		if t.Detail != "" {
			sb.WriteString(fmt.Sprintf("   详情: %s\n", t.Detail))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("⚠️ 当前为观察模式，不会自动降低优先级。")
	return sb.String()
}
