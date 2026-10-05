package service

import (
	"encoding/json"
	"fmt"
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
}


// ---- 每日结算编排（docs/02 2026-10-05 决议）----
//
// 打开 App 时按「结算顺序」依次执行：罚分固定最前（顺序 0，不可调），
// 三个奖励类结算项按设置拖拽出的顺序跑——先结算项写入的流水会成为
// 后结算项（丰收祝福）的基数，形成连锁放大。所有行回溯昨日 23:59:59。

const (
	SettleKeyPerfectDay     = "perfect_day"     // 全勤奖
	SettleKeyCooldownReward = "cooldown_reward" // 无冷却奖励
	SettleKeyFarmBoost      = "farm_boost"      // 丰收祝福（压轴）
)

func defaultSettleOrder() []string {
	return []string{SettleKeyPerfectDay, SettleKeyCooldownReward, SettleKeyFarmBoost}
}

// SettlementOrder 读取结算顺序设置（缺省/损坏时回默认并补全缺失项）。
func SettlementOrder(db *gorm.DB) []string {
	def := defaultSettleOrder()
	var s model.Settings
	if err := db.Where("setting_key = ?", "settle_order").First(&s).Error; err != nil {
		return def
	}
	var order []string
	if err := json.Unmarshal([]byte(s.Value), &order); err != nil {
		return def
	}
	valid := map[string]bool{SettleKeyPerfectDay: true, SettleKeyCooldownReward: true, SettleKeyFarmBoost: true}
	seen := map[string]bool{}
	out := []string{}
	for _, k := range order {
		if valid[k] && !seen[k] {
			out = append(out, k)
			seen[k] = true
		}
	}
	for _, k := range def {
		if !seen[k] {
			out = append(out, k)
		}
	}
	return out
}

// SettleAll 惰性结算总入口（替代散落的 SettlePenalties 直调）。
func SettleAll(db *gorm.DB, now time.Time) {
	SettlePenalties(db, now) // 顺序 0 固定最前：昨日账的基础，其余项依赖它
	for _, key := range SettlementOrder(db) {
		switch key {
		case SettleKeyPerfectDay:
			SettlePerfectDay(db, now)
		case SettleKeyCooldownReward:
			SettleCooldownReward(db, now)
		case SettleKeyFarmBoost:
			SettleDailyBonus(db, now)
		}
	}
}

// SettlePerfectDay 全勤奖：昨日「零罚分行 + 有产出」→ 发固定金额（设置可改）。
// 判定口径：免罚金牌的 0 分审计行不算扣分；昨日至少一条 task/box 收入流水
// （防连续缺勤白拿——防 burnout 规则只补罚最近一天，中间天流水干净但不发全勤）。
func SettlePerfectDay(db *gorm.DB, now time.Time) {
	amount := PerfectDayAmount(db)
	if amount <= 0 {
		return
	}
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	yKey := yesterdayStart.Format("2006-01-02")
	db.Transaction(func(tx *gorm.DB) error {
		var s model.Settings
		if err := tx.Where("setting_key = ?", "perfect_day_date").First(&s).Error; err == nil && s.Value == yKey {
			return nil // 今天已结过昨天的全勤
		}
		var penaltyCount int64
		tx.Model(&model.Ledger{}).
			Where("type = ? AND amount <> 0 AND created_at >= ? AND created_at < ?", "penalty", yesterdayStart, todayStart).
			Count(&penaltyCount) // 0 分审计行（免罚金牌）不算扣分
		var incomeCount int64
		tx.Model(&model.Ledger{}).
			Where("type IN ? AND amount > 0 AND created_at >= ? AND created_at < ?",
				[]string{"task", "box"}, yesterdayStart, todayStart).
			Count(&incomeCount)
		if penaltyCount > 0 || incomeCount == 0 {
			// 不满足全勤：只写幂等标记
			return upsertSetting(tx, "perfect_day_date", yKey)
		}
		chargeAt := yesterdayStart.Add(24*time.Hour - time.Second)
		if err := tx.Create(&model.Ledger{
			Type: "perfect_day", Amount: amount,
			Note:      fmt.Sprintf("全勤奖：昨日零罚分（产出 %d 分流水）", incomeCount),
			CreatedAt: chargeAt,
		}).Error; err != nil {
			return err
		}
		return upsertSetting(tx, "perfect_day_date", yKey)
	})
}

// SettleCooldownReward 无冷却奖励：只看标记为「奖励型商品」的商城商品。
// 昨日整天该商品处于可用状态（冷却结束时刻 ≤ 昨日 23:59:59）→ 按「冷却结束
// 至昨日末的天数」查梯度表发奖（默认 0-3 天 50 / 3-7 天 80 / 7-30 天 120 / ≥30 天 150，
// 四段金额设置可改）。兑换当天 → 冷却重新开始 → 停发。归属昨日 23:59:59。
func SettleCooldownReward(db *gorm.DB, now time.Time) {
	var items []model.ShopItem
	if err := db.Where("is_reward = ?", true).Find(&items).Error; err != nil || len(items) == 0 {
		return
	}
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	yKey := yesterdayStart.Format("2006-01-02")
	var dateSetting model.Settings
	if err := db.Where("setting_key = ?", "cooldown_reward_date").First(&dateSetting).Error; err == nil && dateSetting.Value == yKey {
		return
	}
	tiers := CooldownRewardTiers(db)
	chargeAt := yesterdayStart.Add(24*time.Hour - time.Second)
	db.Transaction(func(tx *gorm.DB) error {
		var s model.Settings
		if err := tx.Where("setting_key = ?", "cooldown_reward_date").First(&s).Error; err == nil && s.Value == yKey {
			return nil
		}
		paid := false
		for _, it := range items {
			if it.LastRedeemedAt == nil {
				continue // 从未兑换：没有冷却时间线，不参与
			}
			cooldownEnd := it.LastRedeemedAt.Add(time.Duration(it.CooldownDays) * 24 * time.Hour)
			if cooldownEnd.After(chargeAt) {
				continue // 昨日末仍在冷却中：不发
			}
			days := int(chargeAt.Sub(cooldownEnd).Hours()) / 24 // 冷却结束后经过的整天数
			amount := cooldownTierAmount(tiers, days)
			if amount <= 0 {
				continue
			}
			if err := tx.Create(&model.Ledger{
				Type: "cooldown_reward", Amount: amount, RefID: it.ID,
				Note:      fmt.Sprintf("无冷却奖励：「%s」可用第 %d 天", it.Name, days+1),
				CreatedAt: chargeAt,
			}).Error; err != nil {
				return err
			}
			paid = true
		}
		_ = paid
		return upsertSetting(tx, "cooldown_reward_date", yKey)
	})
}

// upsertSetting 幂等标记的写入（存在则更新，不存在则创建）。
func upsertSetting(tx *gorm.DB, key, value string) error {
	var s model.Settings
	if err := tx.Where("setting_key = ?", key).First(&s).Error; err == nil {
		return tx.Model(&model.Settings{}).Where("setting_key = ?", key).Update("value", value).Error
	}
	return tx.Create(&model.Settings{SettingKey: key, Value: value}).Error
}

// PerfectDayAmount 全勤奖金额（settings perfect_day_amount，默认 50）。
func PerfectDayAmount(db *gorm.DB) int {
	var s model.Settings
	if err := db.Where("setting_key = ?", "perfect_day_amount").First(&s).Error; err != nil {
		return 50
	}
	n, err := strconv.Atoi(s.Value)
	if err != nil || n < 0 {
		return 50
	}
	return n
}

// CooldownTier 无冷却奖励的梯度段：Days = 段上限（0 = 无上限），Amount = 每日金额。
type CooldownTier struct {
	Days   int `json:"days"`
	Amount int `json:"amount"`
}

func defaultCooldownTiers() []CooldownTier {
	return []CooldownTier{{Days: 3, Amount: 50}, {Days: 7, Amount: 80}, {Days: 30, Amount: 120}, {Days: 0, Amount: 150}}
}

// CooldownRewardTiers 读取无冷却奖励梯度（settings cooldown_reward_tiers JSON）。
func CooldownRewardTiers(db *gorm.DB) []CooldownTier {
	def := defaultCooldownTiers()
	var s model.Settings
	if err := db.Where("setting_key = ?", "cooldown_reward_tiers").First(&s).Error; err != nil {
		return def
	}
	var tiers []CooldownTier
	if err := json.Unmarshal([]byte(s.Value), &tiers); err != nil || len(tiers) == 0 {
		return def
	}
	return tiers
}

// cooldownTierAmount 按可用天数查梯度金额（段升序，Days=0 表示无上限兜底段）。
func cooldownTierAmount(tiers []CooldownTier, days int) int {
	for _, t := range tiers {
		if t.Days == 0 || days < t.Days {
			return t.Amount
		}
	}
	if len(tiers) > 0 {
		return tiers[len(tiers)-1].Amount
	}
	return 0
}
