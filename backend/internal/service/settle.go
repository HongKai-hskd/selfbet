package service

import (
	"strconv"
	"time"

	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// SettlePenalties lazily charges penalties for tasks not completed in their
// period: daily/weekly tasks for the previous period, one-shot tasks past
// their deadline. Idempotent via LastPenaltyKey / PenaltySettled guards.
func SettlePenalties(db *gorm.DB, now time.Time) {
	tasks := []model.Task{}
	if err := db.Where("penalty > 0").Find(&tasks).Error; err != nil {
		return
	}
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	thisWeekStart := StartOfWeek(now)
	lastWeekStart := thisWeekStart.AddDate(0, 0, -7)

	// 免罚金牌：24h 窗口内触发的结算全免（幂等标记照写，本周期不再重罚）
	var exemptPenalty int64
	db.Model(&model.PendingEffect{}).
		Where("kind = ? AND expires_at > ?", pendingExempt, now).
		Count(&exemptPenalty)

	// 「失败周期内是否完成」用两条 GROUP BY 带出全部任务的完成计数，
	// 替代逐任务一条 COUNT（远程库模式下每次结算省 N-1 次往返）。
	// 流水是事实：LastDoneKey 会被下一次完成覆盖，这里只认流水。
	dailyDone := map[uint]bool{}
	weeklyDone := map[uint]bool{}
	type refRow struct {
		RefID uint
		N     int64
	}
	var refs []refRow
	if err := db.Model(&model.Ledger{}).
		Select("ref_id, COUNT(*) AS n").
		Where("type = ? AND created_at >= ? AND created_at < ?", "task", yesterdayStart, todayStart).
		Group("ref_id").Scan(&refs).Error; err == nil {
		for _, r := range refs {
			if r.N > 0 {
				dailyDone[r.RefID] = true
			}
		}
	}
	refs = refs[:0]
	if err := db.Model(&model.Ledger{}).
		Select("ref_id, COUNT(*) AS n").
		Where("type = ? AND created_at >= ? AND created_at < ?", "task", lastWeekStart, thisWeekStart).
		Group("ref_id").Scan(&refs).Error; err == nil {
		for _, r := range refs {
			if r.N > 0 {
				weeklyDone[r.RefID] = true
			}
		}
	}

	for i := range tasks {
		t := &tasks[i]
		var dueKey, note string
		var chargeAt time.Time // 罚分追溯记到「失败的那个周期」的最后一刻，而不是扣罚时刻
		eligible := false
		switch t.Repeat {
		case "daily":
			dueKey = periodKey("daily", yesterdayStart)
			chargeAt = yesterdayStart.Add(24*time.Hour - time.Second)
			if t.LastPenaltyKey == dueKey {
				continue // 本周期已结算过（罚或免），无需再查流水
			}
			eligible = t.CreatedAt.Before(todayStart) && !dailyDone[t.ID]
			note = "每日任务未完成罚分：" + t.Title
		case "weekly":
			dueKey = periodKey("weekly", lastWeekStart)
			chargeAt = lastWeekStart.Add(7*24*time.Hour - time.Second)
			if t.LastPenaltyKey == dueKey {
				continue
			}
			eligible = t.CreatedAt.Before(thisWeekStart) && !weeklyDone[t.ID]
			note = "每周任务未完成罚分：" + t.Title
		default: // once with deadline
			if t.PenaltySettled || t.Status == "done" || t.DueAt == nil || !t.DueAt.Before(now) {
				continue
			}
			chargeAt = *t.DueAt
			eligible = true
			note = "逾期未完成罚分：" + t.Title
		}
		if !eligible {
			continue
		}

		db.Transaction(func(tx *gorm.DB) error {
			var res *gorm.DB
			if t.Repeat == "daily" || t.Repeat == "weekly" {
				res = tx.Model(&model.Task{}).
					Where("id = ? AND (last_penalty_key IS NULL OR last_penalty_key != ?)", t.ID, dueKey).
					Update("last_penalty_key", dueKey)
			} else {
				res = tx.Model(&model.Task{}).
					Where("id = ? AND penalty_settled = ?", t.ID, false).
					Update("penalty_settled", true)
			}
			if res.Error != nil || res.RowsAffected == 0 {
				return gorm.ErrDuplicatedKey // settled by a concurrent request
			}
			if exemptPenalty > 0 {
				// 免罚金牌生效：跳过扣分（幂等标记已写防重复触发），
				// 留 0 分审计行让"哪个任务哪个周期被豁免"在流水可查
				if err := tx.Create(&model.Ledger{
					Type: "penalty", Amount: 0, RefID: t.ID,
					Note: "免罚金牌豁免：" + note, CreatedAt: now,
				}).Error; err != nil {
					return err
				}
				return nil
			}
			// 余额地板：积分最多透支到 PointsFloor，超出部分减免
			// （D/E 的收益放大/对冲在每日净值结算层统一处理，见 SettleDailyBonus）
			bal := PointBalance(tx)
			pts := -t.Penalty
			noteSuffix := ""
			if bal+pts < model.PointsFloor {
				pts = model.PointsFloor - bal
				if pts >= 0 {
					return nil // 已在地板上，本周期免扣（结算标记已记）
				}
				noteSuffix = "（触及 " + strconv.Itoa(model.PointsFloor) + " 下限，减免 " + strconv.Itoa(t.Penalty+pts) + " 分）"
			}
			return tx.Create(&model.Ledger{
				Type: "penalty", Amount: pts, RefID: t.ID, Note: note + noteSuffix, CreatedAt: chargeAt,
			}).Error
		})
	}

	// D 线丰收祝福：昨日正收益加成（同为惰性、每日一次、幂等）
	SettleDailyBonus(db, now)
}
