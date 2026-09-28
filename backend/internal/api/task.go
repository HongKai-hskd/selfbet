package api

import (
	"fmt"
	"math/rand"
	"net/http"
	"sort"
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
// reset lazily at period boundaries.
func effectiveStatus(t *model.Task, now time.Time) string {
	if t.Repeat == "daily" || t.Repeat == "weekly" {
		// multi-round tasks stay claimable all period (each round pays)
		if !t.MultiRound && t.LastDoneKey == periodKey(t.Repeat, now) {
			return "done"
		}
		return "doing"
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
	Title            string `json:"title"`
	Points           int    `json:"points"`
	EstimatedMinutes int    `json:"estimated_minutes"`
	TagID            *uint  `json:"tag_id"`
	Repeat           string `json:"repeat"`
	BoxID            *uint  `json:"box_id"`
	BoxDropRate      int    `json:"box_drop_rate"`
	DueAt            string `json:"due_at"` // flexible format, parsed manually
	Penalty          int    `json:"penalty"`
	MultiRound       bool   `json:"multi_round"` // daily/weekly: claimable multiple times per period
}

// parseDueAt accepts RFC3339, naive datetime and plain date strings.
func (b *taskBody) parseDueAt() *time.Time {
	s := strings.TrimSpace(b.DueAt)
	if s == "" {
		return nil
	}
	layouts := []string{
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
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

	for i := range tasks {
		t := &tasks[i]
		var dueKey, note string
		eligible := false
		switch t.Repeat {
		case "daily":
			dueKey = periodKey("daily", yesterdayStart)
			eligible = t.CreatedAt.Before(todayStart) &&
				t.LastDoneKey != dueKey && t.LastPenaltyKey != dueKey
			note = "每日任务未完成罚分：" + t.Title
		case "weekly":
			dueKey = periodKey("weekly", lastWeekStart)
			eligible = t.CreatedAt.Before(thisWeekStart) &&
				t.LastDoneKey != dueKey && t.LastPenaltyKey != dueKey
			note = "每周任务未完成罚分：" + t.Title
		default: // once with deadline
			if t.PenaltySettled || t.Status == "done" || t.DueAt == nil || !t.DueAt.Before(now) {
				continue
			}
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
			return tx.Create(&model.Ledger{
				Type: "penalty", Amount: -t.Penalty, RefID: t.ID, Note: note,
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
		// append to the end of the manual display order (a zero would
		// jump above everything the user has already arranged)
		var maxSort int
		db.Model(&model.Task{}).Select("COALESCE(MAX(sort_order), 0)").Scan(&maxSort)
		task := model.Task{
			Status:    "pending",
			SortOrder: maxSort + 1,
		}
		applyBody(&task, &body)
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
		Name   string `json:"name"`
		Points int    `json:"points"`
	}
	type result struct {
		Task    model.Task  `json:"task"`
		Earned  int         `json:"earned"`
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
			if err := tx.Create(&model.Ledger{Type: "task", Amount: earned, RefID: task.ID, Note: note}).Error; err != nil {
				return err
			}
			// box drop roll
			if task.BoxID != nil && task.BoxDropRate > 0 && rand.Intn(100) < task.BoxDropRate {
				var box model.Box
				if err := tx.First(&box, *task.BoxID).Error; err == nil {
					span := box.MaxPoints - box.MinPoints
					if span < 0 {
						span = 0
					}
					pts := box.MinPoints + rand.Intn(span+1)
					if err := tx.Create(&model.Ledger{Type: "box", Amount: pts, RefID: box.ID, Note: "宝箱「" + box.Name + "」开出"}).Error; err != nil {
						return err
					}
					out.Box = &boxResult{Name: box.Name, Points: pts}
					earned += pts
				}
			}
			out.Task, out.Earned = task, earned
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
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
