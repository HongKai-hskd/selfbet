package service

import (
	"math/rand"
	"strconv"
	"time"

	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// ---- Backpack: unopened boxes and items ----
//
// 道具池定义/图标/交互元数据在 itemdef.go 的注册表里，本文件负责
// 开箱结算与使用分发（业务事务）。

type OpenBoxResult struct {
	Points int    `json:"points"`
	Item   string `json:"item,omitempty"` // 命中道具时非空
	Icon   string `json:"icon,omitempty"`
	Qty    int    `json:"qty,omitempty"`
	Bonus  string `json:"bonus,omitempty"` // 本箱吃到的加成：幸运符/运势卡
}

// OpenBoxes opens `count` unopened boxes of one type. Each box credits a
// random amount inside its min~max range as a box ledger row.
func OpenBoxes(db *gorm.DB, typeID uint, count int) ([]OpenBoxResult, int, error) {
	var box model.Box
	if err := db.First(&box, typeID).Error; err != nil {
		return nil, 0, BizErr(404, "宝箱类型不存在")
	}
	var rows []model.BackpackItem
	db.Where("kind = ? AND type_id = ?", "box", typeID).
		Order("created_at ASC, id ASC").Limit(count).Find(&rows)
	if len(rows) == 0 {
		return nil, 0, BizErr(400, "背包里没有这种宝箱")
	}
	span := box.MaxPoints - box.MinPoints
	if span < 0 {
		span = 0
	}
	results := []OpenBoxResult{}
	total := 0
	err := db.Transaction(func(tx *gorm.DB) error {
		// 幸运符/运势卡只作用于本次批量开启的第一个箱子（并行制下每箱必有积分）
		var luckyEff, boostEff model.PendingEffect
		hasLucky := tx.Where("kind = ?", pendingLucky).Order("id ASC").First(&luckyEff).Error == nil
		hasBoost := tx.Where("kind = ?", pendingBoost).Order("id ASC").First(&boostEff).Error == nil
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
			// 道具判定：累积概率落点。道具是「附加掉落」——开到照发进背包，
			// 积分并行结算不互斥（2026-10-01 用户拍板，此前道具命中顶掉积分）
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
			res := OpenBoxResult{}
			if itemType > 0 {
				for i := 0; i < qty; i++ {
					if err := tx.Create(&model.BackpackItem{Kind: "item", TypeID: uint(itemType), Source: r.ID}).Error; err != nil {
						return err
					}
				}
				res.Item = ItemName(uint(itemType))
				res.Icon = itemDefByID[uint(itemType)].Icon
				res.Qty = qty
			}
			// 积分结算（必得）：运势卡（区间×2）与幸运符（积分×2）只吃
			// 第一个开出的箱子（含附道具的箱子），可叠加
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
			if res.Item != "" {
				note += "（附「" + res.Item + "」×" + strconv.Itoa(res.Qty) + "）"
			}
			if bonus != "" {
				note += "（" + bonus + "）"
			}
			if err := tx.Create(&model.Ledger{Type: "box", Amount: pts, RefID: box.ID, Note: note}).Error; err != nil {
				return err
			}
			res.Points = pts
			res.Bonus = bonus
			results = append(results, res)
			total += pts
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

// UseItem consumes one backpack item:
// 1 = 积分利息卡 (+3% of point balance, floor)
// 2 = 余额利息卡 (+3% of cash balance, floor to cent)
// 3 = 冷却重置卡 (target_id = shop item, clears its cooldown)
// 4 = 免罚金牌 (arms a 24h window during which penalty settlement is skipped)
// 5 = 幸运符 (next box opened pays ×2)
// 6 = 双倍卡 (next task completion pays ×2)
// 7 = 运势卡 (next box opened rolls in a min/max ×2 range)
func UseItem(db *gorm.DB, typeID, targetID uint) (map[string]any, error) {
	use, ok := itemUses[typeID]
	if !ok {
		return nil, BizErr(400, "未知道具")
	}
	var row model.BackpackItem
	if err := db.Where("kind = ? AND type_id = ?", "item", typeID).
		Order("created_at ASC, id ASC").First(&row).Error; err != nil {
		return nil, BizErr(400, "背包里没有这种道具")
	}

	var out map[string]any
	err := db.Transaction(func(tx *gorm.DB) error {
		// 先删后用 + RowsAffected 防双花：并发请求只有一方拿到行；
		// use 返回错误时事务回滚，背包行原样保留
		del := tx.Where("id = ?", row.ID).Delete(&model.BackpackItem{})
		if del.Error != nil {
			return del.Error
		}
		if del.RowsAffected == 0 {
			return BizErr(400, "道具已被使用")
		}
		var err error
		if out, err = use(tx, row, targetID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ---- per-item use handlers (registered in itemdef.go) ----

// useInterestPoints: +3% of point balance, floored; negative counts as 0.
func useInterestPoints(tx *gorm.DB, row model.BackpackItem, _ uint) (map[string]any, error) {
	bal := PointBalance(tx)
	pts := bal * 3 / 100
	if pts < 0 {
		pts = 0 // 负余额不计负利息
	}
	if err := tx.Create(&model.Ledger{Type: "item", Amount: pts, RefID: row.ID, Note: "道具「" + ItemName(row.TypeID) + "」利息入账"}).Error; err != nil {
		return nil, err
	}
	return map[string]any{"points": pts}, nil
}

// useInterestCash: +3% of cash balance, floored to the cent.
func useInterestCash(tx *gorm.DB, row model.BackpackItem, _ uint) (map[string]any, error) {
	cents := CashBalance(tx)
	i := cents * 3 / 100
	if err := tx.Create(&model.CashFlow{AmountCents: i, Note: "道具「" + ItemName(row.TypeID) + "」利息入账"}).Error; err != nil {
		return nil, err
	}
	return map[string]any{"cents": i}, nil
}

// useResetCooldown: target shop item's cooldown is cleared (LastRedeemedAt nil).
func useResetCooldown(tx *gorm.DB, row model.BackpackItem, targetID uint) (map[string]any, error) {
	if targetID == 0 {
		return nil, BizErr(400, "请选择要重置冷却的商品")
	}
	var shop model.ShopItem
	if err := tx.First(&shop, targetID).Error; err != nil {
		return nil, BizErr(404, "商品不存在")
	}
	inCooldown := shop.LastRedeemedAt != nil && shop.CooldownDays > 0 &&
		time.Now().Before(shop.LastRedeemedAt.Add(time.Duration(shop.CooldownDays)*24*time.Hour))
	if !inCooldown {
		return nil, BizErr(400, "该商品没有进行中的冷却")
	}
	if err := tx.Model(&model.ShopItem{}).Where("id = ?", shop.ID).Update("last_redeemed_at", nil).Error; err != nil {
		return nil, err
	}
	if err := tx.Create(&model.Ledger{Type: "item", Amount: 0, RefID: shop.ID, Note: "道具「" + ItemName(row.TypeID) + "」重置「" + shop.Name + "」冷却"}).Error; err != nil {
		return nil, err
	}
	return map[string]any{"message": "已重置「" + shop.Name + "」冷却"}, nil
}
