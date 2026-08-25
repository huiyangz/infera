// agent-activity 跨项目 agent 执行时序聚合（INFERA-253 冻结契约）：
// GET /api/agent-activity?hours=24&bucket_minutes=30，登录态。
package api

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/tokfinity/infera/internal/store"
)

// 参数口径（冻结契约）：hours 缺省 24、1..168；bucket_minutes 缺省 30、
// 合法值 5/10/15/30/60；非法 → 400。
const (
	agentActivityDefaultHours     = 24
	agentActivityMaxHours         = 168
	agentActivityDefaultBucketMin = 30
)

var agentActivityBucketChoices = []int{5, 10, 15, 30, 60}

// agentActivityWindow 查询窗口（to = 当前时刻，from = to - hours）。
type agentActivityWindow struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// agentActivityResponse 响应载荷（冻结契约，形状不得静默变更）：series 按
// agent_name 升序、窗口内零执行的 agent 不出现、无绑定 stage 的运行归
// "unbound" 分组（agent_id 空串）；每条曲线 points 覆盖窗口内全部桶。
type agentActivityResponse struct {
	Window        agentActivityWindow         `json:"window"`
	BucketMinutes int                         `json:"bucket_minutes"`
	Series        []store.AgentActivitySeries `json:"series"`
}

// handleAgentActivity 窗口 [now-hours, now) 按 bucket_minutes 分桶的各 agent
// 执行次数曲线。桶自窗口左端对齐铺满——hours 为整数，窗口恒为桶宽整数倍，
// 无残桶；count 口径（started_at 落桶、attempt 各计一次、不分 status）冻结
// 于 store.AgentActivity。
func (s *Server) handleAgentActivity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	hours, ok := queryInt(q, "hours", agentActivityDefaultHours)
	if !ok || hours < 1 || hours > agentActivityMaxHours {
		writeError(w, http.StatusBadRequest, "参数不合法：hours 需为 1..168 的整数")
		return
	}
	bucketMinutes, ok := queryInt(q, "bucket_minutes", agentActivityDefaultBucketMin)
	if !ok || !slices.Contains(agentActivityBucketChoices, bucketMinutes) {
		writeError(w, http.StatusBadRequest, "参数不合法：bucket_minutes 需为 5/10/15/30/60 之一")
		return
	}
	to := time.Now().UTC()
	from := to.Add(-time.Duration(hours) * time.Hour)
	series, err := s.st.AgentActivity(r.Context(), from, to, bucketMinutes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取执行时序失败")
		return
	}
	writeJSON(w, http.StatusOK, agentActivityResponse{
		Window:        agentActivityWindow{From: from, To: to},
		BucketMinutes: bucketMinutes,
		Series:        series,
	})
}

// queryInt 整型查询参数：键缺席或空串取缺省值，非整数 → ok=false。
func queryInt(q url.Values, key string, def int) (int, bool) {
	v := q.Get(key)
	if v == "" {
		return def, true
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}
