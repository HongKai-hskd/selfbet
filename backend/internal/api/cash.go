package api

import (
	"net/http"

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

// GetCash returns wallet balance and recent flows.
func GetCash(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		flows := []model.CashFlow{}
		if err := db.Order("created_at DESC, id DESC").Limit(100).Find(&flows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"balance_cents": cashBalance(db),
			"items":         flows,
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
