package costqueryrealtime

import "github.com/gin-gonic/gin"

const shareRequestKey = "cqr_share"

func isShareRequest(c *gin.Context) bool {
	v, ok := c.Get(shareRequestKey)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func isShareHiddenField(col string) bool {
	switch col {
	case "account_id", "account_name", "channel_id":
		return true
	default:
		return false
	}
}

func filterShareHidden(cols []string) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		if !isShareHiddenField(c) {
			out = append(out, c)
		}
	}
	return out
}

func shareExportColumns() []string {
	return filterShareHidden(ExportColumnOrder)
}

func shareSearchColumns() []string {
	return filterShareHidden(searchCols)
}

func shareLabels() map[string]string {
	cols := shareExportColumns()
	out := make(map[string]string, len(cols))
	for _, c := range cols {
		if zh, ok := LabelMap[c]; ok {
			out[c] = zh
		}
	}
	return out
}

func projectShareRow(row map[string]any) map[string]any {
	cols := shareExportColumns()
	out := make(map[string]any, len(cols))
	for _, c := range cols {
		if v, ok := row[c]; ok {
			out[c] = v
		}
	}
	return out
}
