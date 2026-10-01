package service

import (
	"strconv"

	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// ---- Cash wallet: points → yuan exchange + manual spending records ----

// ExchangeResult is the payload returned by ExchangeCash.
type ExchangeResult struct {
	BalanceCents int `json:"balance_cents"`
	Cents        int `json:"cents"`
	PointBalance int `json:"point_balance"`
}

// ExchangeCash converts points to wallet cents at model.PointsPerYuan.
// points must be a positive multiple of PointsPerYuan (whole yuan).
func ExchangeCash(db *gorm.DB, points int) (*ExchangeResult, error) {
	if points <= 0 || points%model.PointsPerYuan != 0 {
		return nil, BizErr(400, "兑换积分必须是 %d 的倍数", model.PointsPerYuan)
	}
	var out ExchangeResult
	err := db.Transaction(func(tx *gorm.DB) error {
		bal := PointBalance(tx)
		if bal < points {
			return BizErr(400, "积分不足，还差 %d 分", points-bal)
		}
		cents := points / model.PointsPerYuan * 100
		if err := tx.Create(&model.Ledger{Type: "cash", Amount: -points, Note: "兑换余额 ¥" + strconv.Itoa(cents/100)}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.CashFlow{AmountCents: cents, Note: "积分兑换（" + strconv.Itoa(points) + " 分）"}).Error; err != nil {
			return err
		}
		out = ExchangeResult{BalanceCents: CashBalance(tx), Cents: cents, PointBalance: bal - points}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SpendResult is the payload returned by SpendCash.
type SpendResult struct {
	BalanceCents int `json:"balance_cents"`
}

// SpendCash manually records a real-world purchase (deducts wallet balance).
func SpendCash(db *gorm.DB, cents int, note string) (*SpendResult, error) {
	if cents <= 0 {
		return nil, BizErr(400, "金额无效")
	}
	if note == "" {
		note = "日常消费"
	}
	var out SpendResult
	err := db.Transaction(func(tx *gorm.DB) error {
		balance := CashBalance(tx)
		if balance < cents {
			return BizErr(400, "余额不足")
		}
		if err := tx.Create(&model.CashFlow{AmountCents: -cents, Note: note}).Error; err != nil {
			return err
		}
		out.BalanceCents = balance - cents
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
