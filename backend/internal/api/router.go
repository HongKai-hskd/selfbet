package api

import (
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/config"
	"selfbet/backend/internal/model"
	"selfbet/backend/web"
)

// NewRouter wires up every route. Static files come from the embedded dist.
func NewRouter(cfg *config.Config, db *gorm.DB) *gin.Engine {
	r := gin.Default()

	apiGroup := r.Group("/api")
	apiGroup.POST("/auth/login", Login(cfg))

	authed := apiGroup.Group("", Auth(cfg))
	{
		authed.GET("/me", Me(db))
		authed.GET("/ledger", ListLedger(db))
		authed.POST("/ledger/:id/undo", UndoLedger(db))

		authed.GET("/backpack", ListBackpack(db))
		authed.POST("/backpack/open", OpenBoxes(db))
		authed.POST("/backpack/use", UseItem(db))

		authed.GET("/tasks", ListTasks(db))
		authed.POST("/tasks", CreateTask(db))
		authed.PUT("/tasks/:id", UpdateTask(db))
		authed.DELETE("/tasks/:id", DeleteTask(db))
		authed.POST("/tasks/reorder", ReorderTasks(db))
		authed.POST("/tasks/:id/start", StartTask(db))
		authed.POST("/tasks/:id/complete", CompleteTask(db))

		authed.GET("/boxes", ListBoxes(db))
		authed.POST("/boxes", CreateBox(db))
		authed.PUT("/boxes/:id", UpdateBox(db))
		authed.DELETE("/boxes/:id", DeleteBox(db))

		authed.GET("/tags", ListTags(db))
		authed.POST("/tags", CreateTag(db))
		authed.PUT("/tags/:id", UpdateTag(db))
		authed.DELETE("/tags/:id", DeleteTag(db))
		authed.POST("/tags/reorder", ReorderTags(db))

		authed.GET("/cash", GetCash(db))
		authed.POST("/cash/exchange", ExchangeCash(db))
		authed.POST("/cash/spend", SpendCash(db))

		authed.GET("/stats", GetStats(db))

		authed.GET("/shop", ListShop(db))
		authed.POST("/shop", CreateShopItem(db))
		authed.PUT("/shop/:id", UpdateShopItem(db))
		authed.DELETE("/shop/:id", DeleteShopItem(db))
		authed.POST("/shop/reorder", ReorderShop(db))
		authed.POST("/shop/:id/redeem", RedeemShopItem(db))
	}

	// SPA static from embedded dist
	sub, err := fs.Sub(web.Dist, "dist")
	if err == nil {
		fileServer := http.FileServer(http.FS(sub))
		r.NoRoute(func(c *gin.Context) {
			p := strings.TrimPrefix(c.Request.URL.Path, "/")
			if p == "" {
				p = "index.html"
			}
			if _, statErr := fs.Stat(sub, p); statErr != nil {
				c.Request.URL.Path = "/" // SPA fallback → index.html
			}
			fileServer.ServeHTTP(c.Writer, c.Request)
		})
	}
	return r
}

// Me returns the point summary for the header/dashboard.
func Me(db *gorm.DB) gin.HandlerFunc {
	type summary struct {
		Balance       int   `json:"balance"`
		TotalEarned   int   `json:"total_earned"`
		TotalSpent    int   `json:"total_spent"`
		TasksDone     int64 `json:"tasks_done"`
		BackpackCount int64 `json:"backpack_count"` // 未开宝箱数（背包入口角标）
	}
	return func(c *gin.Context) {
		var sums struct {
			Amount      int
			TotalEarned int
			Spent       int
		}
		db.Model(&model.Ledger{}).Select(
			"COALESCE(SUM(amount),0) AS amount",
			"COALESCE(SUM(CASE WHEN amount > 0 THEN amount ELSE 0 END),0) AS total_earned",
			"COALESCE(-SUM(CASE WHEN amount < 0 THEN amount ELSE 0 END),0) AS spent",
		).Scan(&sums)
		var done int64
		// completion count from ledger: daily/weekly completions only flip
		// last_done_key (DB status stays pending), so counting task rows is
		// the truthful "how many times did I complete something"
		db.Model(&model.Ledger{}).Where("type = ?", "task").Count(&done)
		var bcount int64
		db.Model(&model.BackpackItem{}).Where("kind = ?", "box").Count(&bcount)
		c.JSON(http.StatusOK, summary{
			Balance:       sums.Amount,
			TotalEarned:   sums.TotalEarned,
			TotalSpent:    sums.Spent,
			TasksDone:     done,
			BackpackCount: bcount,
		})
	}
}

// ListLedger returns recent point movements (newest first).
// Optional start_date / end_date (YYYY-MM-DD, local) filter in memory.
func ListLedger(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := 100
		if v := atoi(c.Query("limit")); v > 0 && v <= 2000 {
			limit = v
		}
		all := []model.Ledger{}
		q := db.Order("created_at DESC, id DESC")
		if t := c.Query("type"); t != "" {
			q = q.Where("type = ?", t)
		}
		if v := atoi(c.Query("ref_id")); v > 0 {
			q = q.Where("ref_id = ?", v)
		}
		if err := q.Find(&all).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		startStr, endStr := c.Query("start_date"), c.Query("end_date")
		var startTime, endTime time.Time
		hasRange := false
		if startStr != "" {
			if t, err := time.ParseInLocation("2006-01-02", startStr, time.Local); err == nil {
				startTime, hasRange = t, true
			}
		}
		if endStr != "" {
			if t, err := time.ParseInLocation("2006-01-02", endStr, time.Local); err == nil {
				endTime, hasRange = t.AddDate(0, 0, 1), true // exclusive
			}
		}
		items := []model.Ledger{}
		rangeSum := 0 // 范围内净额（含罚分等负数行）
		for _, r := range all {
			if hasRange {
				if startStr != "" && r.CreatedAt.Before(startTime) {
					continue
				}
				if endStr != "" && !r.CreatedAt.Before(endTime) {
					continue
				}
			}
			rangeSum += r.Amount
			items = append(items, r)
		}
		if len(items) > limit {
			items = items[:limit]
		}
		var row model.Ledger
		db.Model(&model.Ledger{}).Select("COALESCE(SUM(amount),0) AS amount").Scan(&row)
		c.JSON(http.StatusOK, gin.H{"items": items, "balance": int(row.Amount), "range_sum": rangeSum})
	}
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// UndoLedger removes a same-day task-completion or box-drop ledger row.
// For task rows the task status is restored: once → doing; repeating task's
// LastDoneKey cleared when no completion remains in the period (multi-round
// rounds re-count from ledger automatically). Penalty/shop rows are not
// undoable (penalty would be re-charged by lazy settle; shop involves
// cooldown rollback).
func UndoLedger(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var row model.Ledger
		if err := db.First(&row, c.Param("id")).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "记录不存在"})
			return
		}
		if row.Type != "task" && row.Type != "box" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "只有任务完成和宝箱开出的记录可以撤回"})
			return
		}
		now := time.Now()
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		if row.CreatedAt.Before(todayStart) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "只能撤回今天获得的记录"})
			return
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			if row.Type == "box" {
				// box rows are standalone: delete and done
				return tx.Delete(&row).Error
			}
			var task model.Task
			if err := tx.First(&task, row.RefID).Error; err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在或已删除"})
				return err
			}
			if err := tx.Delete(&row).Error; err != nil {
				return err
			}
			// 撤回任务完成 = 整次作废：该次掉落进背包的未开宝箱/道具一并收回
			if row.Type == "task" {
				if err := tx.Where("source = ?", row.ID).Delete(&model.BackpackItem{}).Error; err != nil {
					return err
				}
			}
			// remaining completions of this task in the current period
			// (in-memory date filtering, same convention as the stats page)
			var rows []model.Ledger
			tx.Where("type = ? AND ref_id = ?", "task", task.ID).Find(&rows)
			start := todayStart
			if task.Repeat == "weekly" {
				start = startOfWeek(now)
			}
			n := 0
			for _, l := range rows {
				if l.ID != row.ID && !l.CreatedAt.Before(start) {
					n++
				}
			}
			// restore the task status when the period has no completion left
			if task.Repeat == "once" {
				if task.Status == "done" && n == 0 {
					task.Status, task.CompletedAt = "doing", nil
					if err := tx.Save(&task).Error; err != nil {
						return err
					}
				}
			} else if task.LastDoneKey == periodKey(task.Repeat, now) && n == 0 {
				// single-round daily/weekly becomes claimable again; for
				// multi-round tasks this is a no-op safeguard
				if err := tx.Model(&task).Updates(map[string]any{"last_done_key": "", "completed_at": nil}).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return // response already written inside the transaction
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "undone": row.Amount})
	}
}
