package service

import (
	"math"
	"strconv"
	"time"

	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// ---- Shop redemption ----

// RedeemResult is the payload returned by RedeemShopItem.
type RedeemResult struct {
	Balance  int            `json:"balance"`
	Item     model.ShopItem `json:"item"`
	Quantity int            `json:"quantity"`
	Total    int            `json:"total"`
}

// RedeemShopItem spends price × quantity, records one ledger entry and
// counts the redemption. Quantity below 1 is treated as 1.
func RedeemShopItem(db *gorm.DB, itemID uint, qty int) (*RedeemResult, error) {
	if qty < 1 {
		qty = 1
	}
	var out RedeemResult
	err := db.Transaction(func(tx *gorm.DB) error {
		var item model.ShopItem
		if err := tx.First(&item, itemID).Error; err != nil {
			return BizErr(404, "商品不存在")
		}
		balance := PointBalance(tx)
		unit := item.Price
		inCooldown := false
		newStreak := 0
		// 冷却中可购买（2026-10-11 拍板），价格 × 溢价^连续次数（2026-10-13 拍板：
		// 冷却期内每再买一次，价格继续翻倍：base→×2→×4→×8…；冷却自然结束则归零）。
		// 购买后冷却重新计时
		if item.CooldownDays > 0 && item.LastRedeemedAt != nil {
			cd := time.Duration(item.CooldownDays) * 24 * time.Hour
			if time.Since(*item.LastRedeemedAt) < cd {
				inCooldown = true
				newStreak = item.PremiumStreak + 1
				mult := math.Pow(float64(CooldownPremiumPercent(tx))/100, float64(newStreak))
				unit = int(math.Round(float64(item.Price) * mult))
			}
		}
		total := unit * qty
		// 透支额度：兑换后余额最低到「可预支金额」地板（与罚分共用）
		floor := PointFloorValue(tx)
		if balance-total < floor {
			return BizErr(400, "超出透支额度，还差 %d 分", total-balance+floor)
		}
		note := "兑换：" + item.Name
		if qty > 1 {
			note += " ×" + strconv.Itoa(qty)
		}
		if inCooldown {
			mult := math.Pow(float64(CooldownPremiumPercent(tx))/100, float64(newStreak))
			note += "（冷却期第 " + strconv.Itoa(newStreak) + " 次溢价 ×" + strconv.FormatFloat(mult, 'f', -1, 64) + "）"
		}
		if err := tx.Create(&model.Ledger{Type: "shop", Amount: -total, RefID: item.ID, Note: note}).Error; err != nil {
			return err
		}
		if err := tx.Model(&item).UpdateColumn("redeemed_count", gorm.Expr("redeemed_count + ?", qty)).Error; err != nil {
			return err
		}
		if item.CooldownDays > 0 {
			now := time.Now()
			if err := tx.Model(&item).Updates(map[string]any{
				"last_redeemed_at": now,
				"premium_streak":   newStreak, // 冷却中递增、正常购买归零（冷却自然结束 → 下次购买归零）
			}).Error; err != nil {
				return err
			}
			item.LastRedeemedAt = &now
		}
		item.PremiumStreak = newStreak
		item.RedeemedCount += qty
		out = RedeemResult{Balance: balance - total, Item: item, Quantity: qty, Total: total}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
