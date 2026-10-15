package costqueryrealtime

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var boolCols = map[string]bool{
	"upstream_model_mismatch": true, "stream": true, "openai_ws_mode": true,
	"native_compaction_v2": true, "long_context_billing_applied": true, "cache_ttl_overridden": true,
}

func buildWhere(model, q string) (string, []any) {
	where := make([]string, 0, 2)
	args := make([]any, 0, 8)
	model = strings.TrimSpace(model)
	q = strings.TrimSpace(q)
	if model != "" {
		if model == "(空)" {
			where = append(where, `(requested_model IS NULL OR requested_model = '')`)
		} else {
			where = append(where, `requested_model = ?`)
			args = append(args, model)
		}
	}
	if q != "" {
		like := "%" + q + "%"
		parts := make([]string, 0, len(searchCols))
		for _, c := range searchCols {
			parts = append(parts, fmt.Sprintf(`CAST("%s" AS TEXT) LIKE ?`, c))
			args = append(args, like)
		}
		if len(parts) > 0 {
			where = append(where, "("+strings.Join(parts, " OR ")+")")
		}
	}
	if len(where) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

func streamExportCSV(c *gin.Context, sqlitePath, model, q string) {
	db, err := openJobSQLite(sqlitePath, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "打开结果库失败: " + err.Error()})
		return
	}
	defer db.Close()

	wheresql, args := buildWhere(model, q)
	fname := fmt.Sprintf("cost_query_%s.csv", time.Now().Format("20060102_150405"))

	c.Status(http.StatusOK)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, fname, url.QueryEscape(fname)))
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	// No Content-Length: browser download bar grows as bytes arrive.
	c.Writer.WriteHeaderNow()

	flusher, _ := c.Writer.(http.Flusher)
	w := c.Writer

	// UTF-8 BOM for Excel
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	if flusher != nil {
		flusher.Flush()
	}

	cw := csv.NewWriter(w)
	cw.UseCRLF = false
	headers := make([]string, len(ExportColumnOrder))
	for i, col := range ExportColumnOrder {
		if zh, ok := LabelMap[col]; ok {
			headers[i] = zh
		} else {
			headers[i] = col
		}
	}
	_ = cw.Write(headers)
	cw.Flush()
	if flusher != nil {
		flusher.Flush()
	}

	query := `SELECT * FROM usage` + wheresql + ` ORDER BY created_at DESC`
	rows, err := db.Query(query, args...)
	if err != nil {
		_, _ = io.WriteString(w, "\n导出失败: "+err.Error())
		return
	}
	defer rows.Close()

	colNames, err := rows.Columns()
	if err != nil {
		return
	}
	colIndex := make(map[string]int, len(colNames))
	for i, n := range colNames {
		colIndex[n] = i
	}

	raw := make([]any, len(colNames))
	ptrs := make([]any, len(colNames))
	for i := range raw {
		ptrs[i] = &raw[i]
	}

	n := 0
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			break
		}
		line := make([]string, len(ExportColumnOrder))
		for i, col := range ExportColumnOrder {
			idx, ok := colIndex[col]
			var v any
			if ok {
				v = derefSQL(raw[idx])
			}
			line[i] = csvCell(col, v)
			if len(line[i]) > 32000 {
				line[i] = line[i][:32000]
			}
		}
		if err := cw.Write(line); err != nil {
			return
		}
		n++
		if n%2000 == 0 {
			cw.Flush()
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
	cw.Flush()
	if flusher != nil {
		flusher.Flush()
	}
}

func derefSQL(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	default:
		return t
	}
}

func csvCell(col string, v any) string {
	if v == nil {
		return ""
	}
	if col == "created_at" {
		return formatZhTime(fmt.Sprint(v))
	}
	if boolCols[col] {
		switch x := v.(type) {
		case int64:
			if x == 1 {
				return "是"
			}
			if x == 0 {
				return "否"
			}
		case int:
			if x == 1 {
				return "是"
			}
			if x == 0 {
				return "否"
			}
		case bool:
			if x {
				return "是"
			}
			return "否"
		case string:
			if x == "1" || x == "true" || x == "True" {
				return "是"
			}
			if x == "0" || x == "false" || x == "False" {
				return "否"
			}
		}
	}
	return fmt.Sprint(v)
}

func formatZhTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	layouts := []string{
		time.RFC3339Nano, time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05",
	}
	var t time.Time
	var err error
	for _, layout := range layouts {
		t, err = time.Parse(layout, s)
		if err == nil {
			break
		}
	}
	if err != nil {
		return s
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	local := t.In(loc)
	return local.Format("2006年01月02日 15:04:05")
}

