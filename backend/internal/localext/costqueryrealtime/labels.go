package costqueryrealtime

// ColumnOrder is the denormalized column order stored in per-job SQLite.
// Wider than export so online filters/search still have useful fields available.
var ColumnOrder = []string{
	"id", "created_at", "user_email", "user_id",
	"api_key_name", "api_key_id", "account_name", "account_id",
	"requested_model", "model_raw", "upstream_model", "upstream_response_model",
	"upstream_model_mismatch", "model_mapping_chain", "channel_id",
	"requested_reasoning_effort", "reasoning_effort", "service_tier",
	"group_name", "group_id", "subscription_id",
	"inbound_endpoint", "upstream_endpoint",
	"request_type_label", "request_type", "stream", "openai_ws_mode", "native_compaction_v2",
	"billing_type_label", "billing_type", "billing_mode", "billing_tier",
	"input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
	"cache_creation_5m_tokens", "cache_creation_1h_tokens",
	"image_input_tokens", "image_output_tokens",
	"input_cost", "output_cost", "cache_creation_cost", "cache_read_cost",
	"image_input_cost", "image_output_cost",
	"total_cost", "actual_cost", "rate_multiplier",
	"account_rate_multiplier", "account_stats_cost", "account_billed",
	"long_context_billing_applied", "cache_ttl_overridden",
	"first_token_ms", "duration_ms", "timing_breakdown",
	"request_id", "upstream_request_id", "session_id",
	"user_agent", "ip_address",
	"image_count", "image_size", "image_input_size", "image_output_size",
	"image_size_source", "image_size_breakdown",
	"video_count", "video_resolution", "video_duration_seconds",
}

// ExportColumnOrder matches Desktop/ml/usage_2026-10-01_to_2026-10-15.xlsx header set/order.
var ExportColumnOrder = []string{
	"created_at",
	"user_email",
	"api_key_name",
	"requested_model",
	"upstream_model",
	"upstream_response_model",
	"upstream_model_mismatch",
	"requested_reasoning_effort",
	"reasoning_effort",
	"group_name",
	"inbound_endpoint",
	"upstream_endpoint",
	"request_type_label",
	"input_tokens",
	"output_tokens",
	"cache_read_tokens",
	"cache_creation_tokens",
	"input_cost",
	"output_cost",
	"cache_read_cost",
	"cache_creation_cost",
	"rate_multiplier",
	"account_rate_multiplier",
	"total_cost",
	"actual_cost",
	"account_billed",
	"first_token_ms",
	"duration_ms",
	"request_id",
	"upstream_request_id",
	"user_agent",
	"ip_address",
}

// LabelMap Chinese headers. Export labels match the reference xlsx exactly.
var LabelMap = map[string]string{
	"id":                           "记录ID",
	"created_at":                   "时间",
	"user_email":                   "用户",
	"user_id":                      "用户ID",
	"api_key_name":                 "API 密钥",
	"api_key_id":                   "API密钥ID",
	"account_name":                 "账户名称",
	"account_id":                   "账户ID",
	"requested_model":              "请求",
	"model_raw":                    "模型原始值",
	"upstream_model":               "发往上游",
	"upstream_response_model":      "上游响应",
	"upstream_model_mismatch":      "上游响应模型不一致",
	"model_mapping_chain":          "模型映射链",
	"channel_id":                   "渠道ID",
	"requested_reasoning_effort":   "请求推理强度",
	"reasoning_effort":             "推理强度",
	"service_tier":                 "服务层级",
	"group_name":                   "分组",
	"group_id":                     "分组ID",
	"subscription_id":              "订阅ID",
	"inbound_endpoint":             "入站端点",
	"upstream_endpoint":            "上游端点",
	"request_type_label":           "类型",
	"request_type":                 "请求类型代码",
	"stream":                       "是否流式",
	"openai_ws_mode":               "OpenAI WS模式",
	"native_compaction_v2":         "原生压缩V2",
	"billing_type_label":           "计费类型",
	"billing_type":                 "计费类型代码",
	"billing_mode":                 "计费模式",
	"billing_tier":                 "计费层级",
	"input_tokens":                 "输入 Token",
	"output_tokens":                "输出 Token",
	"cache_creation_tokens":        "缓存创建 Token",
	"cache_read_tokens":            "缓存读取 Token",
	"cache_creation_5m_tokens":     "缓存创建5m Token",
	"cache_creation_1h_tokens":     "缓存创建1h Token",
	"image_input_tokens":           "图片输入Token",
	"image_output_tokens":          "图片输出Token",
	"input_cost":                   "输入费用",
	"output_cost":                  "输出费用",
	"cache_creation_cost":          "缓存创建费用",
	"cache_read_cost":              "缓存读取费用",
	"image_input_cost":             "图片输入费用",
	"image_output_cost":            "图片输出费用",
	"total_cost":                   "原始",
	"actual_cost":                  "用户扣费",
	"rate_multiplier":              "倍率",
	"account_rate_multiplier":      "账号倍率",
	"account_stats_cost":           "账号统计费用",
	"account_billed":               "账号计费",
	"long_context_billing_applied": "长上下文计费",
	"cache_ttl_overridden":         "缓存TTL覆盖",
	"first_token_ms":               "首 Token",
	"duration_ms":                  "耗时",
	"timing_breakdown":             "耗时明细",
	"request_id":                   "请求ID",
	"upstream_request_id":          "上游ID",
	"session_id":                   "会话ID",
	"user_agent":                   "User-Agent",
	"ip_address":                   "IP",
	"image_count":                  "图片数量",
	"image_size":                   "图片尺寸",
	"image_input_size":             "图片输入尺寸",
	"image_output_size":            "图片输出尺寸",
	"image_size_source":            "图片尺寸来源",
	"image_size_breakdown":         "图片尺寸明细",
	"video_count":                  "视频数量",
	"video_resolution":             "视频分辨率",
	"video_duration_seconds":       "视频时长秒",
}

var searchCols = []string{
	"requested_model", "model_raw", "upstream_model", "api_key_name",
	"account_name", "group_name", "request_id", "upstream_request_id",
	"session_id", "ip_address", "user_agent", "inbound_endpoint",
	"billing_type_label", "request_type_label",
}

func requestTypeLabel(v int16, stream, ws bool) string {
	switch v {
	case 1:
		return "同步"
	case 2:
		return "流式"
	case 3:
		return "WS V2"
	case 4:
		return "Cyber拦截"
	case 5:
		return "Live"
	default:
		if ws {
			return "WS V2"
		}
		if stream {
			return "流式"
		}
		return "同步"
	}
}

func billingTypeLabel(v int16) string {
	switch v {
	case 1:
		return "订阅套餐"
	default:
		return "钱包余额"
	}
}
