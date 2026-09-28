package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// ---- Cash wallet: points → yuan exchange + manual spending records ----

func cashBalance(db *gorm.DB) int {
	var row struct {
		Sum int
	}
	db.Model(&model.CashFlow{}).Select("COALESCE(SUM(amount_cents),0) AS sum").Scan(&row)
	return row.Sum
}

// GetCash returns wallet balance and flows. Optional filters (mirroring
// the stats detail page): start_date/end_date (YYYY-MM-DD, local, end
// exclusive) and type = in|out. earned_cents / spent_cents sum within the
// selected range.
func GetCash(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		flows := []model.CashFlow{}
		if err := db.Order("created_at DESC, id DESC").Limit(500).Find(&flows).Error; err != nil {
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
				endTime, hasRange = t.AddDate(0, 0, 1), true
			}
		}
		typeSel := c.Query("type") // in | out | ""
		items := []model.CashFlow{}
		earned, spent := 0, 0
		for _, f := range flows {
			if hasRange {
				if startStr != "" && f.CreatedAt.Before(startTime) {
					continue
				}
				if endStr != "" && !f.CreatedAt.Before(endTime) {
					continue
				}
			}
			if typeSel == "in" && f.AmountCents < 0 {
				continue
			}
			if typeSel == "out" && f.AmountCents > 0 {
				continue
			}
			if f.AmountCents > 0 {
				earned += f.AmountCents
			} else {
				spent += -f.AmountCents
			}
			items = append(items, f)
		}
		c.JSON(http.StatusOK, gin.H{
			"balance_cents": cashBalance(db),
			"items":         items,
			"earned_cents":  earned,
			"spent_cents":   spent,
		})
	}
}

// ExchangeCash converts points to wallet cents at model.PointsPerYuan.
// points must be a positive multiple of 5 (whole yuan).
func ExchangeCash(db *gorm.DB) gin.HandlerFunc {
	type result struct {
		BalanceCents int   `json:"balance_cents"`
		Cents        int   `json:"cents"`
		PointBalance int   `json:"point_balance"`
	}
	return func(c *gin.Context) {
		var body struct {
			Points int `json:"points"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		if body.Points <= 0 || body.Points%model.PointsPerYuan != 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "兑换积分必须是 " + itoa(model.PointsPerYuan) + " 的倍数"})
			return
		}
		var out result
		err := db.Transaction(func(tx *gorm.DB) error {
			var prow struct{ Amount int }
			tx.Model(&model.Ledger{}).Select("COALESCE(SUM(amount),0) AS amount").Scan(&prow)
			if prow.Amount < body.Points {
				c.JSON(http.StatusBadRequest, gin.H{"error": "积分不足，还差 " + itoa(body.Points-prow.Amount) + " 分"})
				return gorm.ErrDuplicatedKey
			}
			cents := body.Points / model.PointsPerYuan * 100
			if err := tx.Create(&model.Ledger{Type: "cash", Amount: -body.Points, Note: "兑换余额 ¥" + itoa(cents/100)}).Error; err != nil {
				return err
			}
			if err := tx.Create(&model.CashFlow{AmountCents: cents, Note: "积分兑换（" + itoa(body.Points) + " 分）"}).Error; err != nil {
				return err
			}
			out = result{BalanceCents: cashBalance(tx), Cents: cents, PointBalance: prow.Amount - body.Points}
			return nil
		})
		if err != nil {
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

// SpendCash manually records a real-world purchase (deducts wallet balance).
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
		if body.Cents <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "金额无效"})
			return
		}
		if body.Note == "" {
			body.Note = "日常消费"
		}
		var out struct {
			BalanceCents int `json:"balance_cents"`
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			balance := cashBalance(tx)
			if balance < body.Cents {
				c.JSON(http.StatusBadRequest, gin.H{"error": "余额不足"})
				return gorm.ErrDuplicatedKey
			}
			if err := tx.Create(&model.CashFlow{AmountCents: -body.Cents, Note: body.Note}).Error; err != nil {
				return err
			}
			out.BalanceCents = balance - body.Cents
			return nil
		})
		if err != nil {
			return
		}
		c.JSON(http.StatusOK, out)
	}
}
