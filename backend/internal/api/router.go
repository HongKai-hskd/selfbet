package api

import (
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/config"
	"selfbet/backend/internal/model"
	"selfbet/backend/internal/service"
	"selfbet/backend/web"
)

// writeErr 统一错误出口：*service.BizError → 其状态码与文案；
// 其余按基础设施错误 500。与旧实现的响应格式完全一致（{error: msg}）。
func writeErr(c *gin.Context, err error) {
	var be *service.BizError
	if errors.As(err, &be) {
		c.JSON(be.Code, gin.H{"error": be.Msg})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}

// NewRouter wires up every route. Static files come from the embedded dist.
func NewRouter(cfg *config.Config, db *gorm.DB) *gin.Engine {
	r := gin.Default()

	apiGroup := r.Group("/api")
	apiGroup.POST("/auth/login", Login(db))

	authed := apiGroup.Group("", Auth(db))
	{
		authed.GET("/me", Me(db))
		authed.GET("/ledger", ListLedger(db))
		authed.POST("/ledger/:id/undo", UndoLedger(db))

		authed.GET("/backpack", ListBackpack(db))
		authed.POST("/backpack/open", OpenBoxes(db))
		authed.POST("/backpack/use", UseItem(db))

		authed.GET("/items/guide", ItemsGuideHandler())

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
		authed.GET("/settle-settings", GetSettleSettings(db))
		authed.PUT("/settle-settings", UpdateSettleSettings(db))
		authed.POST("/shop/:id/redeem", RedeemShopItem(db))

		authed.GET("/farm", GetFarm(db))
		authed.POST("/farm/plant", PlantFarm(db))
		authed.POST("/farm/harvest", HarvestFarm(db))
		authed.POST("/farm/buy-plot", BuyFarmPlot(db))
		authed.POST("/farm/upgrade", UpgradeFarm(db))
		authed.POST("/farm/withdraw", WithdrawFarm(db))
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
			isIndex := p == "index.html"
			if _, statErr := fs.Stat(sub, p); statErr != nil {
				c.Request.URL.Path = "/" // SPA fallback → index.html
				p = "index.html"
				isIndex = true
			}
			// 入口页禁缓存（部署后刷新即最新版）；带哈希的静态资源长缓存
			if isIndex {
				c.Header("Cache-Control", "no-cache")
			} else if strings.HasPrefix(p, "assets/") {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
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
		db.Model(&model.BackpackItem{}).Count(&bcount) // 背包入口角标：宝箱+道具全部待处理件数
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
// Optional type / ref_id / start_date / end_date (YYYY-MM-DD, local) filters
// are pushed down to SQL; range_sum is the net amount over the whole filtered
// range (items are additionally capped by limit).
func ListLedger(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := 100
		if v, err := strconv.Atoi(c.Query("limit")); err == nil && v > 0 && v <= 2000 {
			limit = v
		}
		typeFilter := c.Query("type")
		refID, _ := strconv.Atoi(c.Query("ref_id"))
		startStr, endStr := c.Query("start_date"), c.Query("end_date")
		var startTime, endTime time.Time
		if startStr != "" {
			if t, err := time.ParseInLocation("2006-01-02", startStr, time.Local); err == nil {
				startTime = t
			} else {
				startStr = ""
			}
		}
		if endStr != "" {
			if t, err := time.ParseInLocation("2006-01-02", endStr, time.Local); err == nil {
				endTime = t.AddDate(0, 0, 1) // exclusive
			} else {
				endStr = ""
			}
		}
		conds := func(q *gorm.DB) *gorm.DB {
			if typeFilter != "" {
				q = q.Where("type = ?", typeFilter)
			}
			if refID > 0 {
				q = q.Where("ref_id = ?", refID)
			}
			if startStr != "" {
				q = q.Where("created_at >= ?", startTime)
			}
			if endStr != "" {
				q = q.Where("created_at < ?", endTime)
			}
			return q
		}
		items := []model.Ledger{}
		if err := conds(db.Model(&model.Ledger{})).Order("created_at DESC, id DESC").Limit(limit).Find(&items).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var rangeRow struct {
			Sum int
		}
		conds(db.Model(&model.Ledger{})).Select("COALESCE(SUM(amount),0) AS sum").Scan(&rangeRow)
		c.JSON(http.StatusOK, gin.H{
			"items":     items,
			"balance":   service.PointBalance(db),
			"range_sum": rangeRow.Sum,
		})
	}
}

// UndoLedger removes a same-day task-completion or box-drop ledger row via
// service.UndoLedgerRow (business rules live there).
func UndoLedger(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id64, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			writeErr(c, service.BizErr(404, "记录不存在"))
			return
		}
		undone, err := service.UndoLedgerRow(db, uint(id64), time.Now())
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "undone": undone})
	}
}
