package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
	"selfbet/backend/internal/service"
)

// ---- Tasks ----
//
// 任务领域的写路径事务（完成/撤回/结算）在 internal/service；
// 本文件保留 HTTP 输入校验、列表组装与薄 handler。

// taskBody is the create/update payload.
type taskBody struct {
	Title             string `json:"title"`
	Points            int    `json:"points"`
	EstimatedMinutes  int    `json:"estimated_minutes"`
	TagID             *uint  `json:"tag_id"`
	Repeat            string `json:"repeat"`
	BoxID             *uint  `json:"box_id"`
	BoxDropRate       int    `json:"box_drop_rate"`
	DueAt             string `json:"due_at"` // flexible format, parsed manually
	Penalty           int    `json:"penalty"`
	MultiRound        bool   `json:"multi_round"` // daily/weekly: claimable multiple times per period
	ExpectedStartAt   string `json:"expected_start_at"` // once：日程计划开始
	ExpectedEndAt     string `json:"expected_end_at"`   // once：日程计划结束
	ExpectedStartTime string `json:"expected_start_time"` // daily/weekly：'HH:MM'
	ExpectedEndTime   string `json:"expected_end_time"`   // daily/weekly：'HH:MM'
	ExpectedWeekday   int    `json:"expected_weekday"`    // weekly：1=周一..7=周日
}

// parseFlexibleTime accepts RFC3339, naive datetime and plain date strings.
func (b *taskBody) parseDueAt() *time.Time {
	return parseFlexibleTime(b.DueAt)
}

// parseFlexibleTime accepts RFC3339, naive datetime and plain date strings.
func parseFlexibleTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	layouts := []string{
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return &t
		}
	}
	return nil
}

func (b *taskBody) validate(db *gorm.DB) string {
	if b.Title == "" {
		return "任务名称不能为空"
	}
	if b.Points < 0 {
		return "积分不能为负"
	}
	if b.Repeat == "" {
		b.Repeat = "once"
	}
	if b.Repeat != "once" && b.Repeat != "daily" && b.Repeat != "weekly" {
		return "重复类型无效（once/daily/weekly）"
	}
	if b.Repeat == "once" {
		b.MultiRound = false // multi-round only makes sense for repeating tasks
	}
	if b.BoxID != nil && (b.BoxDropRate < 1 || b.BoxDropRate > 100) {
		return "宝箱掉率需在 1-100 之间"
	}
	if b.BoxID == nil {
		b.BoxDropRate = 0
	}
	if b.TagID != nil {
		var count int64
		db.Model(&model.Tag{}).Where("id = ?", *b.TagID).Count(&count)
		if count == 0 {
			return "分组不存在"
		}
	}
	// 日程时间字段：按任务类型取不同形状，不适用类型一律清空
	switch b.Repeat {
	case "once":
		b.ExpectedStartTime, b.ExpectedEndTime, b.ExpectedWeekday = "", "", 0
		if (strings.TrimSpace(b.ExpectedStartAt) == "") != (strings.TrimSpace(b.ExpectedEndAt) == "") {
			return "预期开始/结束时间需成对填写"
		}
		if strings.TrimSpace(b.ExpectedStartAt) != "" {
			s, e := parseFlexibleTime(b.ExpectedStartAt), parseFlexibleTime(b.ExpectedEndAt)
			if s == nil || e == nil {
				return "预期时间格式无效"
			}
			if !e.After(*s) {
				return "预期结束时间需晚于开始时间"
			}
		}
	case "daily":
		b.ExpectedStartAt, b.ExpectedEndAt, b.ExpectedWeekday = "", "", 0
		if msg := validateTimeRange(b.ExpectedStartTime, b.ExpectedEndTime); msg != "" {
			return msg
		}
	case "weekly":
		b.ExpectedStartAt, b.ExpectedEndAt = "", ""
		if b.ExpectedWeekday < 0 || b.ExpectedWeekday > 7 {
			return "周几无效（1-7）"
		}
		if msg := validateTimeRange(b.ExpectedStartTime, b.ExpectedEndTime); msg != "" {
			return msg
		}
	}
	return ""
}

// validateTimeRange：成对填写、HH:MM 格式、结束晚于开始（允许都为空）。
func validateTimeRange(start, end string) string {
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	if (start == "") != (end == "") {
		return "计划开始/结束时间需成对填写"
	}
	if start == "" {
		return ""
	}
	valid := func(s string) bool {
		parts := strings.Split(s, ":")
		if len(parts) != 2 {
			return false
		}
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		return err1 == nil && err2 == nil && h >= 0 && h <= 23 && m >= 0 && m <= 59 && len(parts[0]) == 2 && len(parts[1]) == 2
	}
	if !valid(start) || !valid(end) {
		return "时间格式无效（HH:MM）"
	}
	if end <= start {
		return "计划结束时间需晚于开始时间"
	}
	return ""
}

func applyBody(t *model.Task, b *taskBody) {
	t.Title, t.Points = b.Title, b.Points
	t.EstimatedMinutes = b.EstimatedMinutes
	t.TagID, t.Repeat = b.TagID, b.Repeat
	t.BoxID, t.BoxDropRate = b.BoxID, b.BoxDropRate
	t.Penalty = b.Penalty
	t.MultiRound = b.MultiRound
	if b.DueAt == "" {
		t.DueAt = nil
	} else {
		t.DueAt = b.parseDueAt()
	}
	// 日程计划字段：按类型落位，不适用类型一律清空（纯展示，零结算语义）
	switch b.Repeat {
	case "once":
		t.ExpectedStartAt = parseFlexibleTime(b.ExpectedStartAt)
		t.ExpectedEndAt = parseFlexibleTime(b.ExpectedEndAt)
		t.ExpectedStartTime, t.ExpectedEndTime, t.ExpectedWeekday = "", "", 0
	case "daily":
		t.ExpectedStartAt, t.ExpectedEndAt = nil, nil
		t.ExpectedStartTime = strings.TrimSpace(b.ExpectedStartTime)
		t.ExpectedEndTime = strings.TrimSpace(b.ExpectedEndTime)
		t.ExpectedWeekday = 0
	case "weekly":
		t.ExpectedStartAt, t.ExpectedEndAt = nil, nil
		t.ExpectedStartTime = strings.TrimSpace(b.ExpectedStartTime)
		t.ExpectedEndTime = strings.TrimSpace(b.ExpectedEndTime)
		t.ExpectedWeekday = b.ExpectedWeekday
	}
}

func ListTasks(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		service.SettleAll(db, time.Now())
		tasks := []model.Task{}
		q := db.Order("created_at DESC")
		if s := c.Query("status"); s != "" {
			q = q.Where("status = ?", s)
		}
		if err := q.Find(&tasks).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		now := time.Now()
		// fill effective status + per-period round counts for multi-round tasks
		// (rounds counted from task-type ledger rows in memory, following the
		// stats page convention of avoiding SQLite date dialects)
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		weekStart := service.StartOfWeek(now)
		var earns []model.Ledger
		for i := range tasks {
			tasks[i].Status = service.EffectiveStatus(&tasks[i], now)
			if tasks[i].MultiRound && (tasks[i].Repeat == "daily" || tasks[i].Repeat == "weekly") {
				if earns == nil {
					db.Where("type = ? AND amount > 0", "task").Find(&earns)
				}
				start := todayStart
				if tasks[i].Repeat == "weekly" {
					start = weekStart
				}
				n := 0
				for _, l := range earns {
					if l.RefID == tasks[i].ID && !l.CreatedAt.Before(start) {
						n++
					}
				}
				tasks[i].RoundsToday = n
			}
		}
		// display order: done tasks last (by completion time desc); the rest
		// grouped — ungrouped on top, then tags by their own order; within a
		// group by manual sort order then newest first
		var tags []model.Tag
		db.Order("sort_order ASC, created_at ASC").Find(&tags)
		tm := make(map[uint]string, len(tags))
		rank := make(map[uint]int, len(tags))
		for i, tg := range tags {
			tm[tg.ID] = tg.Name
			rank[tg.ID] = i + 1
		}
		for i := range tasks {
			if tasks[i].TagID != nil {
				tasks[i].TagName = tm[*tasks[i].TagID]
			}
		}
		sort.SliceStable(tasks, func(i, j int) bool {
			a, b := &tasks[i], &tasks[j]
			if (a.Status == "done") != (b.Status == "done") {
				return b.Status == "done"
			}
			if a.Status == "done" {
				ac, bc := a.CompletedAt, b.CompletedAt
				if ac == nil || bc == nil {
					return ac != nil
				}
				return ac.After(*bc)
			}
			ar, br := 0, 0 // ungrouped rank 0 → top
			if a.TagID != nil {
				ar = rank[*a.TagID]
			}
			if b.TagID != nil {
				br = rank[*b.TagID]
			}
			if ar != br {
				return ar < br
			}
			if a.SortOrder != b.SortOrder {
				return a.SortOrder < b.SortOrder
			}
			return a.ID > b.ID
		})
		c.JSON(http.StatusOK, gin.H{"items": tasks})
	}
}

func CreateTask(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body taskBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		if msg := body.validate(db); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		// 新任务排到所属分区（同分组/未分组）的最前面：
		// 分区内最小 sort_order - 1（空分区为 -1），新加的当前关注点一眼可见
		task := model.Task{Status: "pending"}
		applyBody(&task, &body)
		var minSort int
		q := db.Model(&model.Task{})
		if task.TagID == nil {
			q = q.Where("tag_id IS NULL")
		} else {
			q = q.Where("tag_id = ?", *task.TagID)
		}
		q.Select("COALESCE(MIN(sort_order), 0)").Scan(&minSort)
		task.SortOrder = minSort - 1
		if err := db.Create(&task).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, task)
	}
}

func UpdateTask(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var task model.Task
		if err := db.First(&task, c.Param("id")).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		var body taskBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		if msg := body.validate(db); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		applyBody(&task, &body)
		if err := db.Save(&task).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, task)
	}
}

func DeleteTask(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := db.Delete(&model.Task{}, c.Param("id")).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

func StartTask(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var task model.Task
		if err := db.First(&task, c.Param("id")).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		if service.EffectiveStatus(&task, time.Now()) == "done" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "任务已完成"})
			return
		}
		task.Status = "doing"
		if err := db.Save(&task).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, task)
	}
}

// CompleteTask delegates to service.CompleteTask (points credit, double-card
// consumption and box drop roll all happen in one service transaction).
func CompleteTask(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id64, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			writeErr(c, service.BizErr(404, "任务不存在"))
			return
		}
		var body struct {
			Count int `json:"count"`
		}
		_ = c.ShouldBindJSON(&body) // count 可选；空 body 时 0 → service 内钳为 1
		out, err := service.CompleteTask(db, uint(id64), time.Now(), body.Count)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

// ReorderTasks persists manual display order for non-done tasks:
// ids in request order get sort_order = 1..n.
func ReorderTasks(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			IDs []uint `json:"ids"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || len(body.IDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			for i, id := range body.IDs {
				if err := tx.Model(&model.Task{}).Where("id = ?", id).Update("sort_order", i+1).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
