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
//
// 道具池定义/图标/交互元数据统一在 itemdef.go 的注册表里，本文件只负责
// 背包聚合、开箱与使用分发。

type backpackCell struct {
	TypeID    uint   `json:"type_id"`
	Name      string `json:"name"`
	Icon      string `json:"icon,omitempty"`
	Count     int64  `json:"count"`
	Min       int    `json:"min_points"`
	Max       int    `json:"max_points"`
	UseMode   string `json:"use_mode,omitempty"`
	UsePrompt string `json:"use_prompt,omitempty"`
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
		if d, ok := itemDef(items[i].TypeID); ok {
			items[i].Name = d.Name
			items[i].Icon = d.Icon
			items[i].UseMode = d.UseMode
			items[i].UsePrompt = d.UsePrompt
		} else {
			items[i].Name = "未知道具"
		}
	}
		c.JSON(http.StatusOK, gin.H{"boxes": boxes, "items": items})
	}
}

// OpenBoxes opens `count` unopened boxes of one type. Each box credits a
// random amount inside its min~max range as a box ledger row.
func OpenBoxes(db *gorm.DB) gin.HandlerFunc {
type openResult struct {
	Points int    `json:"points"`
	Item   string `json:"item,omitempty"` // 命中道具时非空
	Icon   string `json:"icon,omitempty"`
	Qty    int    `json:"qty,omitempty"`
	Bonus  string `json:"bonus,omitempty"` // 本箱吃到的加成：幸运符/运势卡
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
			// 幸运符/运势卡只作用于本次批量开启的第一个积分箱（开出道具不消耗）
			var luckyEff, boostEff model.PendingEffect
			hasLucky := tx.Where("kind = ?", "lucky_box").Order("id ASC").First(&luckyEff).Error == nil
			hasBoost := tx.Where("kind = ?", "boost_box").Order("id ASC").First(&boostEff).Error == nil
			firstBoxOpened := false
			for _, r := range rows {
				// RowsAffected=0 说明该箱已被并发请求消费，跳过防双开
				del := tx.Where("id = ?", r.ID).Delete(&model.BackpackItem{})
				if del.Error != nil {
					return del.Error
				}
				if del.RowsAffected == 0 {
					continue
				}
				// 道具判定：累积概率落点（剩余概率=积分）。道具概率从未在旧逻辑生效，本次为首次实现
				roll := rand.Intn(100) // 0..99
				acc := 0
				itemType, qty := 0, 0
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
				if itemType > 0 {
					for i := 0; i < qty; i++ {
						if err := tx.Create(&model.BackpackItem{Kind: "item", TypeID: uint(itemType), Source: r.ID}).Error; err != nil {
							return err
						}
					}
					results = append(results, openResult{Item: itemName(uint(itemType)), Icon: itemDefByID[uint(itemType)].Icon, Qty: qty})
					continue
				}
				// 积分结算：运势卡（区间×2）与幸运符（积分×2）只吃第一个积分箱，可叠加
				pts := box.MinPoints + rand.Intn(span+1)
				bonus := ""
				if !firstBoxOpened {
					if hasBoost {
						del := tx.Where("id = ?", boostEff.ID).Delete(&model.PendingEffect{})
						if del.Error == nil && del.RowsAffected > 0 {
							pts = box.MinPoints*2 + rand.Intn(span*2+1)
							bonus = "运势卡强化"
						}
					}
					if hasLucky {
						del := tx.Where("id = ?", luckyEff.ID).Delete(&model.PendingEffect{})
						if del.Error == nil && del.RowsAffected > 0 {
							pts *= 2
							if bonus != "" {
								bonus += "·"
							}
							bonus += "幸运符×2"
						}
					}
					firstBoxOpened = true
				}
				note := "宝箱「" + box.Name + "」开出"
				if bonus != "" {
					note += "（" + bonus + "）"
				}
				if err := tx.Create(&model.Ledger{Type: "box", Amount: pts, RefID: box.ID, Note: note}).Error; err != nil {
					return err
				}
				results = append(results, openResult{Points: pts, Bonus: bonus})
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
// 4 = 免罚金牌 (arms a 24h window during which penalty settlement is skipped)
// 5 = 幸运符 (next box opened pays ×2)
// 6 = 双倍卡 (next task completion pays ×2)
// 7 = 运势卡 (next box opened rolls in a min/max ×2 range)
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
		use, ok := itemUses[body.TypeID]
		if !ok {
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
			var err error
			if out, err = use(tx, c, row, body.TargetID); err != nil {
				return err
			}
			return tx.Delete(&row).Error
		})
		if err != nil {
			return // response already written inside the transaction
		}
		c.JSON(http.StatusOK, out)
	}
}

// ---- per-item use handlers (registered in itemdef.go) ----

// useInterestPoints: +3% of point balance, floored; negative counts as 0.
func useInterestPoints(tx *gorm.DB, c *gin.Context, row model.BackpackItem, _ uint) (gin.H, error) {
	var bal int
	tx.Model(&model.Ledger{}).Select("COALESCE(SUM(amount),0) AS amount").Scan(&bal)
	pts := bal * 3 / 100
	if pts < 0 {
		pts = 0 // 负余额不计负利息
	}
	if err := tx.Create(&model.Ledger{Type: "item", Amount: pts, RefID: row.ID, Note: "道具「" + itemName(row.TypeID) + "」利息入账"}).Error; err != nil {
		return nil, err
	}
	return gin.H{"points": pts}, nil
}

// useInterestCash: +3% of cash balance, floored to the cent.
func useInterestCash(tx *gorm.DB, c *gin.Context, row model.BackpackItem, _ uint) (gin.H, error) {
	cents := cashBalance(tx)
	i := cents * 3 / 100
	if err := tx.Create(&model.CashFlow{AmountCents: i, Note: "道具「" + itemName(row.TypeID) + "」利息入账"}).Error; err != nil {
		return nil, err
	}
	return gin.H{"cents": i}, nil
}

// useResetCooldown: target shop item's cooldown is cleared (LastRedeemedAt nil).
func useResetCooldown(tx *gorm.DB, c *gin.Context, row model.BackpackItem, targetID uint) (gin.H, error) {
	if targetID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要重置冷却的商品"})
		return nil, gorm.ErrDuplicatedKey
	}
	var shop model.ShopItem
	if err := tx.First(&shop, targetID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "商品不存在"})
		return nil, err
	}
	inCooldown := shop.LastRedeemedAt != nil && shop.CooldownDays > 0 &&
		time.Now().Before(shop.LastRedeemedAt.Add(time.Duration(shop.CooldownDays)*24*time.Hour))
	if !inCooldown {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该商品没有进行中的冷却"})
		return nil, gorm.ErrDuplicatedKey
	}
	if err := tx.Model(&model.ShopItem{}).Where("id = ?", shop.ID).Update("last_redeemed_at", nil).Error; err != nil {
		return nil, err
	}
	if err := tx.Create(&model.Ledger{Type: "item", Amount: 0, RefID: shop.ID, Note: "道具「" + itemName(row.TypeID) + "」重置「" + shop.Name + "」冷却"}).Error; err != nil {
		return nil, err
	}
	return gin.H{"message": "已重置「" + shop.Name + "」冷却"}, nil
}
