package api

import (
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// ---- Backpack: unopened boxes and items ----

// Fixed item pool (drop sources: boxes). IDs are stable and used as
// backpack_items.type_id for kind = "item".
var itemNames = map[uint]string{
	1: "积分利息卡",
	2: "余额利息卡",
	3: "冷却重置卡",
}

func itemName(t uint) string {
	return itemNames[t]
}

type backpackCell struct {
	TypeID uint   `json:"type_id"`
	Name   string `json:"name"`
	Count  int64  `json:"count"`
	Min    int    `json:"min_points"`
	Max    int    `json:"max_points"`
}

// ListBackpack returns the unopened backpack aggregated by kind + type.
func ListBackpack(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		boxes := []backpackCell{}
		if err := db.Model(&model.BackpackItem{}).
			Select("type_id, COUNT(*) AS count").
			Where("kind = ?", "box").
			Group("type_id").Order("type_id ASC").Scan(&boxes).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for i := range boxes {
			var box model.Box
			if err := db.First(&box, boxes[i].TypeID).Error; err == nil {
				boxes[i].Name = box.Name
				boxes[i].Min, boxes[i].Max = box.MinPoints, box.MaxPoints
			} else {
				boxes[i].Name = "未知宝箱"
			}
		}
		items := []backpackCell{}
		if err := db.Model(&model.BackpackItem{}).
			Select("type_id, COUNT(*) AS count").
			Where("kind = ?", "item").
			Group("type_id").Order("type_id ASC").Scan(&items).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for i := range items {
			items[i].Name = itemName(items[i].TypeID)
		}
		c.JSON(http.StatusOK, gin.H{"boxes": boxes, "items": items})
	}
}

// OpenBoxes opens `count` unopened boxes of one type. Each box credits a
// random amount inside its min~max range as a box ledger row.
func OpenBoxes(db *gorm.DB) gin.HandlerFunc {
	type openResult struct {
		Points int `json:"points"`
	}
	return func(c *gin.Context) {
		var body struct {
			TypeID uint `json:"type_id"`
			Count  int  `json:"count"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.Count <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		var box model.Box
		if err := db.First(&box, body.TypeID).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "宝箱类型不存在"})
			return
		}
		var rows []model.BackpackItem
		db.Where("kind = ? AND type_id = ?", "box", body.TypeID).
			Order("created_at ASC, id ASC").Limit(body.Count).Find(&rows)
		if len(rows) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "背包里没有这种宝箱"})
			return
		}
		span := box.MaxPoints - box.MinPoints
		if span < 0 {
			span = 0
		}
		results := []openResult{}
		total := 0
		err := db.Transaction(func(tx *gorm.DB) error {
			for _, r := range rows {
				pts := box.MinPoints + rand.Intn(span+1)
				// RowsAffected=0 说明该箱已被并发请求消费，跳过防双开
				del := tx.Where("id = ?", r.ID).Delete(&model.BackpackItem{})
				if del.Error != nil {
					return del.Error
				}
				if del.RowsAffected == 0 {
					continue
				}
				if err := tx.Create(&model.Ledger{Type: "box", Amount: pts, RefID: box.ID, Note: "宝箱「" + box.Name + "」开出"}).Error; err != nil {
					return err
				}
				results = append(results, openResult{Points: pts})
				total += pts
			}
			return nil
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"results": results, "total": total})
	}
}

// UseItem consumes one backpack item:
// 1 = 积分利息卡 (+3% of point balance, floor)
// 2 = 余额利息卡 (+3% of cash balance, floor to cent)
// 3 = 冷却重置卡 (target_id = shop item, clears its cooldown)
func UseItem(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			TypeID   uint `json:"type_id"`
			TargetID uint `json:"target_id"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		if body.TypeID < 1 || body.TypeID > 3 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "未知道具"})
			return
		}
		var row model.BackpackItem
		if err := db.Where("kind = ? AND type_id = ?", "item", body.TypeID).
			Order("created_at ASC, id ASC").First(&row).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "背包里没有这种道具"})
			return
		}

		var out gin.H
		err := db.Transaction(func(tx *gorm.DB) error {
			switch body.TypeID {
			case 1: // 积分利息卡
				var bal int
				tx.Model(&model.Ledger{}).Select("COALESCE(SUM(amount),0) AS amount").Scan(&bal)
				pts := bal * 3 / 100
				if err := tx.Create(&model.Ledger{Type: "item", Amount: pts, RefID: row.ID, Note: "道具「积分利息卡」利息入账"}).Error; err != nil {
					return err
				}
				out = gin.H{"points": pts}
			case 2: // 余额利息卡
				cents := cashBalance(tx)
				i := cents * 3 / 100
				if err := tx.Create(&model.CashFlow{AmountCents: i, Note: "道具「余额利息卡」利息入账"}).Error; err != nil {
					return err
				}
				out = gin.H{"cents": i}
			case 3: // 冷却重置卡
				if body.TargetID == 0 {
					c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要重置冷却的商品"})
					return gorm.ErrDuplicatedKey
				}
				var shop model.ShopItem
				if err := tx.First(&shop, body.TargetID).Error; err != nil {
					c.JSON(http.StatusNotFound, gin.H{"error": "商品不存在"})
					return err
				}
				inCooldown := shop.LastRedeemedAt != nil && shop.CooldownDays > 0 &&
					time.Now().Before(shop.LastRedeemedAt.Add(time.Duration(shop.CooldownDays)*24*time.Hour))
				if !inCooldown {
					c.JSON(http.StatusBadRequest, gin.H{"error": "该商品没有进行中的冷却"})
					return gorm.ErrDuplicatedKey
				}
				if err := tx.Model(&model.ShopItem{}).Where("id = ?", shop.ID).Update("last_redeemed_at", nil).Error; err != nil {
					return err
				}
				if err := tx.Create(&model.Ledger{Type: "item", Amount: 0, RefID: shop.ID, Note: "道具「冷却重置卡」重置「" + shop.Name + "」冷却"}).Error; err != nil {
					return err
				}
				out = gin.H{"message": "已重置「" + shop.Name + "」冷却"}
			}
			return tx.Delete(&row).Error
		})
		if err != nil {
			return
		}
		c.JSON(http.StatusOK, out)
	}
}
