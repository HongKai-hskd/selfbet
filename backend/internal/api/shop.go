package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// ---- Shop items & redemption ----

func ListShop(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		items := []model.ShopItem{}
		if err := db.Order("sort_order ASC, id DESC").Find(&items).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if len(items) > 0 {
			var tags []model.Tag
			if err := db.Find(&tags).Error; err == nil {
				tm := make(map[uint]string, len(tags))
				for _, tg := range tags {
					tm[tg.ID] = tg.Name
				}
				for i := range items {
					if items[i].TagID != nil {
						items[i].TagName = tm[*items[i].TagID]
					}
				}
			}
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	}
}

type shopBody struct {
	Name         string `json:"name"`
	Price        int    `json:"price"`
	Description  string `json:"description"`
	TagID        *uint  `json:"tag_id"`
	CooldownDays int    `json:"cooldown_days"`
}

func (b *shopBody) validate() string {
	if b.Name == "" {
		return "商品名称不能为空"
	}
	if b.Price <= 0 {
		return "价格必须大于 0"
	}
	return ""
}

func CreateShopItem(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body shopBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		if msg := body.validate(); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		item := model.ShopItem{Name: body.Name, Price: body.Price, Description: body.Description, TagID: body.TagID, CooldownDays: body.CooldownDays}
		if err := db.Create(&item).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, item)
	}
}

func UpdateShopItem(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var item model.ShopItem
		if err := db.First(&item, c.Param("id")).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "商品不存在"})
			return
		}
		var body shopBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		if msg := body.validate(); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		item.Name, item.Price, item.Description = body.Name, body.Price, body.Description
		item.TagID, item.CooldownDays = body.TagID, body.CooldownDays
		if err := db.Save(&item).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, item)
	}
}

func DeleteShopItem(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := db.Delete(&model.ShopItem{}, c.Param("id")).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// ReorderShop persists the manual display order: ids in request order get
// sort_order = 1..n.
func ReorderShop(db *gorm.DB) gin.HandlerFunc {
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
				if err := tx.Model(&model.ShopItem{}).Where("id = ?", id).Update("sort_order", i+1).Error; err != nil {
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

// RedeemShopItem spends price × quantity, records one ledger entry and
// counts the redemption. Quantity is optional (default 1).
func RedeemShopItem(db *gorm.DB) gin.HandlerFunc {
	type result struct {
		Balance  int            `json:"balance"`
		Item     model.ShopItem `json:"item"`
		Quantity int            `json:"quantity"`
		Total    int            `json:"total"`
	}
	return func(c *gin.Context) {
		var body struct {
			Quantity int `json:"quantity"`
		}
		_ = c.ShouldBindJSON(&body) // body optional
		qty := body.Quantity
		if qty < 1 {
			qty = 1
		}
		var out result
		err := db.Transaction(func(tx *gorm.DB) error {
			var item model.ShopItem
			if err := tx.First(&item, c.Param("id")).Error; err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "商品不存在"})
				return gorm.ErrRecordNotFound
			}
			var row struct{ Amount int }
			tx.Model(&model.Ledger{}).Select("COALESCE(SUM(amount),0) AS amount").Scan(&row)
			balance := row.Amount
			total := item.Price * qty
			if balance < total {
				c.JSON(http.StatusBadRequest, gin.H{"error": "积分不足，还差 " + itoa(total-balance) + " 分"})
				return gorm.ErrDuplicatedKey
			}
			// cooldown check
			if item.CooldownDays > 0 && item.LastRedeemedAt != nil {
				cd := time.Duration(item.CooldownDays) * 24 * time.Hour
				elapsed := time.Since(*item.LastRedeemedAt)
				if elapsed < cd {
					remain := cd - elapsed
					msg := "冷却中，还剩 " + itoa(int(remain.Hours())/24) + " 天 " + itoa(int(remain.Hours())%24) + " 小时"
					c.JSON(http.StatusBadRequest, gin.H{"error": msg})
					return gorm.ErrDuplicatedKey
				}
			}
			note := "兑换：" + item.Name
			if qty > 1 {
				note += " ×" + itoa(qty)
			}
			if err := tx.Create(&model.Ledger{Type: "shop", Amount: -total, RefID: item.ID, Note: note}).Error; err != nil {
				return err
			}
			if err := tx.Model(&item).UpdateColumn("redeemed_count", gorm.Expr("redeemed_count + ?", qty)).Error; err != nil {
				return err
			}
			if item.CooldownDays > 0 {
				now := time.Now()
				if err := tx.Model(&item).Update("last_redeemed_at", now).Error; err != nil {
					return err
				}
				item.LastRedeemedAt = &now
			}
			item.RedeemedCount += qty
			out = result{Balance: balance - total, Item: item, Quantity: qty, Total: total}
			return nil
		})
		if err != nil {
			return
		}
		c.JSON(http.StatusOK, out)
	}
}
