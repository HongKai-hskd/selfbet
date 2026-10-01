// Package service 承载业务规则：所有写路径的事务逻辑都在这里，
// gin handler 只负责参数绑定与响应。业务拒绝用 *BizError 表达，
// api 层统一 writeErr 落到 JSON（格式与旧实现一致，前端零感知）。
package service

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// BizError 业务拒绝：Code 是回给前端的 HTTP 状态码，Msg 是用户可读文案。
// 与基础设施错误（连接失败、真·约束冲突）严格区分，不再借
// gorm.ErrDuplicatedKey 当哨兵。
type BizError struct {
	Code int
	Msg  string
}

func (e *BizError) Error() string { return e.Msg }

// BizErr 构造业务错误；code 传 0 视为 400。
func BizErr(code int, format string, args ...any) *BizError {
	if code == 0 {
		code = 400
	}
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	return &BizError{Code: code, Msg: msg}
}

// PointBalance 流水净额 = 当前积分余额。全项目唯一口径，
// 各处共用（原先 7 处手写 SUM 已收敛到这里）。
func PointBalance(db *gorm.DB) int {
	var row struct {
		Sum int
	}
	db.Model(&model.Ledger{}).Select("COALESCE(SUM(amount),0) AS sum").Scan(&row)
	return row.Sum
}

// CashBalance 现金流净额（分）。
func CashBalance(db *gorm.DB) int {
	var row struct {
		Sum int
	}
	db.Model(&model.CashFlow{}).Select("COALESCE(SUM(amount_cents),0) AS sum").Scan(&row)
	return row.Sum
}

// periodKey returns the completion key for repeating tasks:
// daily → "2026-09-27", weekly → "2026-W39".
func periodKey(repeat string, now time.Time) string {
	if repeat == "weekly" {
		y, w := now.ISOWeek()
		return fmt.Sprintf("%d-W%02d", y, w)
	}
	return now.Format("2006-01-02")
}

// StartOfWeek Monday-based week start (周一为一周起点)。
func StartOfWeek(t time.Time) time.Time {
	offset := (int(t.Weekday()) + 6) % 7
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return d.AddDate(0, 0, -offset)
}
