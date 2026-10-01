package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
	"selfbet/backend/internal/service"
)

// ---- Cash wallet: points → yuan exchange + manual spending records ----
//
// 兑换/消费的事务在 service/cashx.go；本文件只有钱包明细查询与薄 handler。

// GetCash returns wallet balance and flows. Optional filters (mirroring
// the stats detail page): start_date/end_date (YYYY-MM-DD, local, end
// exclusive) and type = in|out. earned_cents / spent_cents sum over the
// whole filtered range (items are additionally capped at 500 rows).
// 过滤全部下推 SQL：旧实现先 Limit(500) 再内存过滤，筛选范围超出最新
// 500 条时会漏行、汇总也会错，这里一并修正。
func GetCash(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
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
		typeSel := c.Query("type") // in | out | ""
		conds := func(q *gorm.DB) *gorm.DB {
			if startStr != "" {
				q = q.Where("created_at >= ?", startTime)
			}
			if endStr != "" {
				q = q.Where("created_at < ?", endTime)
			}
			switch typeSel {
			case "in":
				q = q.Where("amount_cents > 0")
			case "out":
				q = q.Where("amount_cents < 0")
			}
			return q
		}
		items := []model.CashFlow{}
		if err := conds(db.Model(&model.CashFlow{})).
			Order("created_at DESC, id DESC").Limit(500).Find(&items).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var sums struct {
			Earned int
			Spent  int
		}
		conds(db.Model(&model.CashFlow{})).Select(
			"COALESCE(SUM(CASE WHEN amount_cents > 0 THEN amount_cents ELSE 0 END),0) AS earned",
			"COALESCE(SUM(CASE WHEN amount_cents < 0 THEN -amount_cents ELSE 0 END),0) AS spent",
		).Scan(&sums)
		c.JSON(http.StatusOK, gin.H{
			"balance_cents": service.CashBalance(db),
			"items":         items,
			"earned_cents":  sums.Earned,
			"spent_cents":   sums.Spent,
		})
	}
}

// ExchangeCash delegates to service.ExchangeCash.
func ExchangeCash(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Points int `json:"points"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		out, err := service.ExchangeCash(db, body.Points)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

// SpendCash delegates to service.SpendCash.
func SpendCash(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Cents int    `json:"cents"`
			Note  string `json:"note"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		out, err := service.SpendCash(db, body.Cents, body.Note)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	}
}
