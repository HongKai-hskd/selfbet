package service

import (
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
		total := item.Price * qty
		// 透支额度：兑换后余额最低到 PointsFloor（与罚分共用 -500）
		if balance-total < model.PointsFloor {
			return BizErr(400, "超出透支额度，还差 %d 分", total-balance+model.PointsFloor)
		}
		// cooldown check
		if item.CooldownDays > 0 && item.LastRedeemedAt != nil {
			cd := time.Duration(item.CooldownDays) * 24 * time.Hour
			elapsed := time.Since(*item.LastRedeemedAt)
			if elapsed < cd {
				remain := cd - elapsed
				return BizErr(400, "冷却中，还剩 %d 天 %d 小时", int(remain.Hours())/24, int(remain.Hours())%24)
			}
		}
		note := "兑换：" + item.Name
		if qty > 1 {
			note += " ×" + strconv.Itoa(qty)
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
		out = RedeemResult{Balance: balance - total, Item: item, Quantity: qty, Total: total}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
