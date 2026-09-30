package api

import (
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// ---- Tasks ----

// periodKey returns the completion key for repeating tasks:
// daily → "2026-09-27", weekly → "2026-W39".
func periodKey(repeat string, now time.Time) string {
	if repeat == "weekly" {
		y, w := now.ISOWeek()
		return fmt.Sprintf("%d-W%02d", y, w)
	}
	return now.Format("2006-01-02")
}

// effectiveStatus computes the display status. Repeating tasks are
// automatically "in progress" each period (no manual start needed) and
// reset lazily at period boundaries. Once tasks with a deadline auto-start
// on the deadline day (and stay doing if overdue) — same no-manual-start
// spirit as repeating tasks.
func effectiveStatus(t *model.Task, now time.Time) string {
	if t.Repeat == "daily" || t.Repeat == "weekly" {
		// multi-round tasks stay claimable all period (each round pays)
		if !t.MultiRound && t.LastDoneKey == periodKey(t.Repeat, now) {
			return "done"
		}
		return "doing"
	}
	if t.Repeat == "once" && t.Status == "pending" && t.DueAt != nil {
		dueDayStart := time.Date(t.DueAt.Year(), t.DueAt.Month(), t.DueAt.Day(), 0, 0, 0, 0, t.DueAt.Location())
		if !now.Before(dueDayStart) {
			return "doing" // 截止日当天（或逾期后）自动开始
		}
	}
	return t.Status
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func ListTasks(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		SettlePenalties(db, time.Now())
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
		weekStart := startOfWeek(now)
		var earns []model.Ledger
		for i := range tasks {
			tasks[i].Status = effectiveStatus(&tasks[i], now)
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

// parseDueAt accepts RFC3339, naive datetime and plain date strings.
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

// SettlePenalties lazily charges penalties for tasks not completed in their
// period: daily/weekly tasks for the previous period, one-shot tasks past
// their deadline. Idempotent via LastPenaltyKey / PenaltySettled guards.
func SettlePenalties(db *gorm.DB, now time.Time) {
	tasks := []model.Task{}
	if err := db.Where("penalty > 0").Find(&tasks).Error; err != nil {
		return
	}
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	thisWeekStart := startOfWeek(now)
	lastWeekStart := thisWeekStart.AddDate(0, 0, -7)

	// 免罚金牌：24h 窗口内触发的结算全免（幂等标记照写，本周期不再重罚）
	var exemptPenalty int64
	db.Model(&model.PendingEffect{}).
		Where("kind = ? AND expires_at > ?", "exempt_penalty", now).
		Count(&exemptPenalty)

	for i := range tasks {
		t := &tasks[i]
		var dueKey, note string
		var chargeAt time.Time // 罚分追溯记到「失败的那个周期」的最后一刻，而不是扣罚时刻
		eligible := false
		switch t.Repeat {
		case "daily":
			dueKey = periodKey("daily", yesterdayStart)
			chargeAt = yesterdayStart.Add(24*time.Hour - time.Second)
			if t.LastPenaltyKey == dueKey {
				continue // 本周期已结算过（罚或免），无需再查流水
			}
			// 用流水判定「失败周期内是否完成」——LastDoneKey 会被下一次
			// 完成覆盖（今天完成早睡会把昨天完成的证据冲掉），流水才是事实
			var done int64
			db.Model(&model.Ledger{}).
				Where("type = ? AND ref_id = ? AND created_at >= ? AND created_at < ?",
					"task", t.ID, yesterdayStart, todayStart).
				Count(&done)
			eligible = t.CreatedAt.Before(todayStart) && done == 0
			note = "每日任务未完成罚分：" + t.Title
		case "weekly":
			dueKey = periodKey("weekly", lastWeekStart)
			chargeAt = lastWeekStart.Add(7*24*time.Hour - time.Second)
			if t.LastPenaltyKey == dueKey {
				continue
			}
			var done int64
			db.Model(&model.Ledger{}).
				Where("type = ? AND ref_id = ? AND created_at >= ? AND created_at < ?",
					"task", t.ID, lastWeekStart, thisWeekStart).
				Count(&done)
			eligible = t.CreatedAt.Before(thisWeekStart) && done == 0
			note = "每周任务未完成罚分：" + t.Title
		default: // once with deadline
			if t.PenaltySettled || t.Status == "done" || t.DueAt == nil || !t.DueAt.Before(now) {
				continue
			}
			chargeAt = *t.DueAt
			eligible = true
			note = "逾期未完成罚分：" + t.Title
		}
		if !eligible {
			continue
		}

		db.Transaction(func(tx *gorm.DB) error {
			var res *gorm.DB
			if t.Repeat == "daily" || t.Repeat == "weekly" {
				res = tx.Model(&model.Task{}).
					Where("id = ? AND (last_penalty_key IS NULL OR last_penalty_key != ?)", t.ID, dueKey).
					Update("last_penalty_key", dueKey)
			} else {
				res = tx.Model(&model.Task{}).
					Where("id = ? AND penalty_settled = ?", t.ID, false).
					Update("penalty_settled", true)
			}
			if res.Error != nil || res.RowsAffected == 0 {
				return gorm.ErrDuplicatedKey // settled by a concurrent request
			}
			if exemptPenalty > 0 {
				// 免罚金牌生效：跳过扣分（幂等标记已写防重复触发），
				// 留 0 分审计行让"哪个任务哪个周期被豁免"在流水可查
				if err := tx.Create(&model.Ledger{
					Type: "penalty", Amount: 0, RefID: t.ID,
					Note: "免罚金牌豁免：" + note, CreatedAt: now,
				}).Error; err != nil {
					return err
				}
				return nil
			}
			// 余额地板：积分最多透支到 PointsFloor，超出部分减免
			var bal int
			tx.Model(&model.Ledger{}).Select("COALESCE(SUM(amount),0) AS amount").Scan(&bal)
			pts := -t.Penalty
			noteSuffix := ""
			if bal+pts < model.PointsFloor {
				pts = model.PointsFloor - bal
				if pts >= 0 {
					return nil // 已在地板上，本周期免扣（结算标记已记）
				}
				noteSuffix = "（触及 " + itoa(model.PointsFloor) + " 下限，减免 " + itoa(t.Penalty+pts) + " 分）"
			}
			return tx.Create(&model.Ledger{
				Type: "penalty", Amount: pts, RefID: t.ID, Note: note + noteSuffix, CreatedAt: chargeAt,
			}).Error
		})
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
		if effectiveStatus(&task, time.Now()) == "done" {
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

// CompleteTask marks the task done, credits its points instantly and rolls
// for the linked box drop. Everything happens in one transaction.
func CompleteTask(db *gorm.DB) gin.HandlerFunc {
	type boxResult struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
		Item  bool   `json:"item"` // true = 开出的是道具而非待开宝箱
		Qty   int    `json:"qty,omitempty"`
	}
	type result struct {
		Task    model.Task  `json:"task"`
		Earned  int         `json:"earned"`
		Doubled bool        `json:"doubled,omitempty"` // 双倍卡生效
		Box     *boxResult  `json:"box"`
		Balance int         `json:"balance"`
	}
	return func(c *gin.Context) {
		var out result
		err := db.Transaction(func(tx *gorm.DB) error {
			var task model.Task
			if err := tx.First(&task, c.Param("id")).Error; err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
				return err
			}
			now := time.Now()
			if task.Repeat == "daily" || task.Repeat == "weekly" {
				// repeating task: single-round credits once per period (resets
				// lazily); multi-round pays every completion
				key := periodKey(task.Repeat, now)
				if task.LastDoneKey == key && !task.MultiRound {
					c.JSON(http.StatusBadRequest, gin.H{"error": "本周期已完成，下个周期再来"})
					return gorm.ErrDuplicatedKey
				}
				task.LastDoneKey = key
				task.CompletedAt = &now
				if err := tx.Save(&task).Error; err != nil {
					return err
				}
			} else {
				if task.Status == "done" {
					c.JSON(http.StatusBadRequest, gin.H{"error": "任务已完成，不要重复领取"})
					return gorm.ErrDuplicatedKey
				}
				task.Status, task.CompletedAt = "done", &now
				if err := tx.Save(&task).Error; err != nil {
					return err
				}
			}
			earned := task.Points
			note := "完成任务：" + task.Title
			doubled := false
			// 双倍卡：下一个完成的任务积分 ×2（事务内先删后用，RowsAffected 防双耗）
			var de model.PendingEffect
			if err := tx.Where("kind = ?", "double_task").Order("id ASC").First(&de).Error; err == nil {
				del := tx.Where("id = ?", de.ID).Delete(&model.PendingEffect{})
				if del.Error != nil {
					return del.Error
				}
				if del.RowsAffected > 0 {
					earned = task.Points * 2
					doubled = true
					note += "（双倍卡×2）"
				}
			}
			if task.MultiRound && (task.Repeat == "daily" || task.Repeat == "weekly") {
				// round number = prior task-ledger rows this period + 1
				start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
				if task.Repeat == "weekly" {
					start = startOfWeek(now)
				}
				var prior []model.Ledger
				tx.Where("type = ? AND amount > 0 AND ref_id = ?", "task", task.ID).Find(&prior)
				n := 1
				for _, l := range prior {
					if !l.CreatedAt.Before(start) {
						n++
					}
				}
				note += "（第 " + itoa(n) + " 轮）"
			}
			lrow := model.Ledger{Type: "task", Amount: earned, RefID: task.ID, Note: note}
			if err := tx.Create(&lrow).Error; err != nil {
				return err
			}
			// box drop roll: the box goes to the backpack unopened — what it
			// contains is decided when the user opens it. Item rates roll via
			// the box's ItemDrops (cumulative hit, remainder = plain points
			// box), same rule as backpack opening.
			if task.BoxID != nil && task.BoxDropRate > 0 && rand.Intn(100) < task.BoxDropRate {
				var box model.Box
				if err := tx.First(&box, *task.BoxID).Error; err == nil {
					roll := rand.Intn(100)
					itemType, qty := 0, 1
					acc := 0
					for _, d := range box.ItemDrops {
						if d.Rate <= 0 {
							continue
						}
						acc += d.Rate
						if roll < acc {
							itemType, qty = d.ItemType, d.Qty
							break
						}
					}
					kind, typeID, name := "box", box.ID, box.Name
					if itemType > 0 {
						kind, typeID, name = "item", uint(itemType), itemName(uint(itemType))
					}
					n := 1
					if itemType > 0 {
						n = qty
					}
					for i := 0; i < n; i++ {
						if err := tx.Create(&model.BackpackItem{Kind: kind, TypeID: typeID, Source: lrow.ID}).Error; err != nil {
							return err
						}
					}
					out.Box = &boxResult{Name: name, Count: 1, Item: itemType > 0, Qty: qty}
				}
			}
			out.Task, out.Earned, out.Doubled = task, earned, doubled
			var row struct{ Amount int }
			tx.Model(&model.Ledger{}).Select("COALESCE(SUM(amount),0) AS amount").Scan(&row)
			out.Balance = row.Amount
			return nil
		})
		if err != nil {
			return // response already written inside the transaction
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

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	if neg {
		digits = "-" + digits
	}
	return digits
}
