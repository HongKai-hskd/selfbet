package service

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// ---- 农场小游戏（docs/07 v10 终版决议）----
//
// 经济骨架：主积分买地+升级（焚烧，写 type=farm 负流水）；
// 农场积分只进不出——唯一来源是收获，唯一出口 100:1 整数提现（正流水）。
// 每田每天最多收获 2 轮（按本地日期惰性重置），成熟永不枯死、忘收不惩罚。
// 公式：收获/轮 = 200 × (1+0.02A)(1+0.06B)；有效周期 = 8h × (1+0.08B−0.05C)。

const (
	farmPlotTotal   = 24    // 4×6 盘面
	farmPlotBase    = 50    // 第 2 块地契价（用户钦定）
	farmPlotStep    = 40    // 每块线性递增
	farmBaseYield   = 200.0 // 每轮基础农场积分
	farmBasePeriodH = 8.0   // 基础生长周期（小时）
	farmCoinsPerPt  = 100.0 // 100 农场积分 = 1 主积分
	farmMaxLevel    = 10
	farmUpgradeBase = 50.0   // A/B/C 升级首级价（主积分，三线统一）
	farmUpgradeStep = 1.3    // A/B/C 升级递增
	farmDBase       = 5000.0 // D 线首级价（农场积分，docs/07 十一）
	farmDStep       = 1.5    // D 线递增
	farmEBase       = 5000.0 // E 线首级价（农场积分）
	farmEStep       = 1.2    // E 线递增
	farmDailyMax    = 2      // 每田每日收获上限
)

// FarmYieldPerRound 每轮收获的农场积分（A、B 乘法合成）。
func FarmYieldPerRound(s *model.FarmState) float64 {
	return farmBaseYield * (1+0.02*float64(s.LevelA)) * (1+0.06*float64(s.LevelB))
}

// FarmPeriodHours 当前有效生长周期（B 副作用与 C 线加法合成，A 无副作用）。
func FarmPeriodHours(s *model.FarmState) float64 {
	h := farmBasePeriodH * (1 + 0.08*float64(s.LevelB) - 0.05*float64(s.LevelC))
	if h < 1 {
		h = 1 // 保险丝：合成参数异常时周期不为负
	}
	return h
}

// FarmUpgradePrice 当前等级升下一级的价格（level = 已有等级 0..9）。
func FarmUpgradePrice(level int) int {
	return int(math.Round(farmUpgradeBase * math.Pow(farmUpgradeStep, float64(level))))
}

// FarmDEPrice D 线（丰收祝福）升下一级价格（农场积分）。
func FarmDEPrice(level int) int {
	return int(math.Round(farmDBase * math.Pow(farmDStep, float64(level))))
}

// FarmEEPrice E 线（灾祸减免）升下一级价格（农场积分）。
func FarmEEPrice(level int) int {
	return int(math.Round(farmEBase * math.Pow(farmEStep, float64(level))))
}

// FarmPlotPrice 第 n 块地契价（n = plotIndex+1，n≥2）。
func FarmPlotPrice(plotIndex int) int {
	return farmPlotBase + farmPlotStep*(plotIndex-1) // n-2 = plotIndex-1
}

func farmToday() string { return time.Now().Format("2006-01-02") }

// getFarmState 惰性创建单行状态（幂等）。
func getFarmState(db *gorm.DB) (*model.FarmState, error) {
	var s model.FarmState
	if err := db.First(&s, 1).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return nil, err
		}
		s = model.FarmState{ID: 1}
		if err := db.Create(&s).Error; err != nil {
			return nil, err
		}
	}
	return &s, nil
}

// farmDailyLeft 当日剩余可收轮次（读取时内存判定，收获时才写回重置）。
func farmDailyLeft(p *model.FarmPlot) int {
	if p.DailyDate != farmToday() {
		return farmDailyMax
	}
	left := farmDailyMax - p.DailyCount
	if left < 0 {
		left = 0
	}
	return left
}

func countUnlocked(db *gorm.DB) int64 {
	var n int64
	db.Model(&model.FarmPlot{}).Where("unlocked = ?", true).Count(&n)
	return n
}

// GetFarm 全量状态：余额、等级、合成数值、24 田实时状态、下一块地价、升级价。
func GetFarmView(db *gorm.DB) (map[string]any, error) {
	s, err := getFarmState(db)
	if err != nil {
		return nil, err
	}
	plots := []model.FarmPlot{}
	if err := db.Order("plot_index ASC").Find(&plots).Error; err != nil {
		return nil, err
	}
	yield := FarmYieldPerRound(s)
	periodH := FarmPeriodHours(s)
	now := time.Now()
	views := []map[string]any{}
	nextIdx, nextPrice := -1, 0
	harvestNow := 0.0
	for _, p := range plots {
		v := map[string]any{
			"plot_index":   p.PlotIndex,
			"unlocked":     p.Unlocked,
			"planted_at":   p.PlantedAt,
			"daily_left":   farmDailyLeft(&p),
			"progress":     0.0,
			"mature":       false, // 空田分支也落键，保持与旧 struct 版相同的 JSON 契约
			"harvestable":  false,
		}
		if p.Unlocked && p.PlantedAt != nil {
			elapsed := now.Sub(*p.PlantedAt).Hours()
			progress := elapsed / periodH
			if progress > 1 {
				progress = 1
			}
			v["progress"] = progress
			v["mature"] = elapsed >= periodH
			harvestable := v["mature"].(bool) && v["daily_left"].(int) > 0
			v["harvestable"] = harvestable
			if harvestable {
				harvestNow += yield
			}
		}
		if !p.Unlocked && nextIdx == -1 {
			nextIdx = p.PlotIndex
			nextPrice = FarmPlotPrice(p.PlotIndex)
		}
		views = append(views, v)
	}
	upg := func(level int, priceFn func(int) int) map[string]any {
		maxed := level >= farmMaxLevel
		price := 0
		if !maxed {
			price = priceFn(level)
		}
		return map[string]any{"level": level, "price": price, "maxed": maxed}
	}
	return map[string]any{
		"coins":            s.Coins,
		"total_harvest":    s.TotalHarvest,
		"level_a":          s.LevelA,
		"level_b":          s.LevelB,
		"level_c":          s.LevelC,
		"level_d":          s.LevelD,
		"level_e":          s.LevelE,
		"yield_per_round":  yield,
		"period_hours":     periodH,
		"daily_income_max": yield * float64(farmDailyMax) * math.Max(1, float64(countUnlocked(db))),
		"plots":            views,
		"next_plot_index":  nextIdx,
		"next_plot_price":  nextPrice,
		"upgrade_a":        upg(s.LevelA, FarmUpgradePrice),
		"upgrade_b":        upg(s.LevelB, FarmUpgradePrice),
		"upgrade_c":        upg(s.LevelC, FarmUpgradePrice),
		"upgrade_d":        upg(s.LevelD, FarmDEPrice),
		"upgrade_e":        upg(s.LevelE, FarmEEPrice),
		"withdrawable":     math.Floor(s.Coins/farmCoinsPerPt),
		"harvest_now":      harvestNow,
		"balance":          PointBalance(db),
	}, nil
}

// PlantFarm 免费种植：plotIndex 为空 = 一键全种所有空田。返回种下块数。
func PlantFarm(db *gorm.DB, plotIndex *int) (int, error) {
	planted := 0
	err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := getFarmState(tx); err != nil {
			return err
		}
		q := tx.Model(&model.FarmPlot{}).
			Where("unlocked = ? AND planted_at IS NULL", true)
		if plotIndex != nil {
			q = q.Where("plot_index = ?", *plotIndex)
		}
		res := q.Update("planted_at", time.Now())
		if res.Error != nil {
			return res.Error
		}
		planted = int(res.RowsAffected)
		return nil
	})
	if err != nil {
		return 0, err
	}
	if planted == 0 {
		return 0, BizErr(400, "没有可种植的空田")
	}
	return planted, nil
}

// HarvestResult is the payload returned by HarvestFarm.
type HarvestResult struct {
	Gained    float64 `json:"gained"`
	Coins     float64 `json:"coins"`
	Harvested int     `json:"harvested"`
}

// HarvestFarm 收获：all=true 一键全收，否则收指定田。
// 校验成熟 + 当日轮次 <2（按收获时刻的本地日期惰性重置）。
func HarvestFarm(db *gorm.DB, all bool, plotIndex *int) (*HarvestResult, error) {
	var res HarvestResult
	err := db.Transaction(func(tx *gorm.DB) error {
		s, err := getFarmState(tx)
		if err != nil {
			return err
		}
		plots := []model.FarmPlot{}
		q := tx.Where("unlocked = ? AND planted_at IS NOT NULL", true)
		if !all {
			if plotIndex == nil {
				return BizErr(400, "参数错误")
			}
			q = q.Where("plot_index = ?", *plotIndex)
		}
		if err := q.Find(&plots).Error; err != nil {
			return err
		}
		periodH := FarmPeriodHours(s)
		yield := FarmYieldPerRound(s)
		now := time.Now()
		today := farmToday()
		hits := []model.FarmPlot{}
		for _, p := range plots {
			if now.Sub(*p.PlantedAt).Hours() < periodH {
				continue // 未成熟
			}
			if p.DailyDate != today {
				p.DailyCount = 0
				p.DailyDate = today
			}
			if p.DailyCount >= farmDailyMax {
				continue // 今日已满 2 轮：熟了挂着过夜，不惩罚
			}
			hits = append(hits, p)
		}
		if len(hits) == 0 {
			return BizErr(400, "没有可收获的作物（未成熟或今日轮次已满）")
		}
		for _, p := range hits {
			res.Gained += yield
			res.Harvested++
			p.DailyCount++
			p.PlantedAt = nil
			if err := tx.Model(&model.FarmPlot{}).Where("id = ?", p.ID).
				Updates(map[string]any{"planted_at": nil, "daily_count": p.DailyCount, "daily_date": p.DailyDate}).Error; err != nil {
				return err
			}
		}
		s.Coins += res.Gained
		s.TotalHarvest += res.Harvested
		if err := tx.Model(&model.FarmState{}).Where("id = ?", s.ID).
			Updates(map[string]any{"coins": s.Coins, "total_harvest": s.TotalHarvest}).Error; err != nil {
			return err
		}
		res.Coins = s.Coins
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// BuyPlotResult is the payload returned by BuyFarmPlot.
type BuyPlotResult struct {
	Balance     int `json:"balance"`
	UnlockedIdx int `json:"unlocked_index"`
	Price       int `json:"price"`
	TotalPlots  int `json:"total_plots"`
}

// BuyFarmPlot 开垦下一块田（顺序解锁），价格扣主积分、写 type=farm 负流水。
func BuyFarmPlot(db *gorm.DB) (*BuyPlotResult, error) {
	var out BuyPlotResult
	err := db.Transaction(func(tx *gorm.DB) error {
		var next model.FarmPlot
		if err := tx.Where("unlocked = ?", false).Order("plot_index ASC").First(&next).Error; err != nil {
			return BizErr(400, "农场已全部开垦完毕")
		}
		price := FarmPlotPrice(next.PlotIndex)
		balance := PointBalance(tx)
		if balance < price {
			return BizErr(400, "积分不足，还差 %d 分", price-balance)
		}
		n := next.PlotIndex + 1 // 第几块
		if err := tx.Create(&model.Ledger{Type: "farm", Amount: -price, Note: "开垦第 " + strconv.Itoa(n) + " 块田"}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.FarmPlot{}).Where("id = ?", next.ID).Update("unlocked", true).Error; err != nil {
			return err
		}
		out.Balance = balance - price
		out.UnlockedIdx = next.PlotIndex
		out.Price = price
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.TotalPlots = farmPlotTotal
	return &out, nil
}

// UpgradeResult is the payload returned by UpgradeFarm.
type UpgradeResult struct {
	Line    string  `json:"line"`
	Level   int     `json:"level"`
	Balance int     `json:"balance"`
	Coins   float64 `json:"coins"` // D/E 线：升级后的农场积分余额
}

// UpgradeFarm 升级线：A/B/C 扣主积分（写 type=farm 负流水）；
// D/E 扣农场积分（Coins 内部扣减，不写主积分流水——docs/07 十一）。
func UpgradeFarm(db *gorm.DB, line string) (*UpgradeResult, error) {
	if line != "A" && line != "B" && line != "C" && line != "D" && line != "E" {
		return nil, BizErr(400, "参数错误")
	}
	var out UpgradeResult
	err := db.Transaction(func(tx *gorm.DB) error {
		s, err := getFarmState(tx)
		if err != nil {
			return err
		}
		level := map[string]*int{"A": &s.LevelA, "B": &s.LevelB, "C": &s.LevelC, "D": &s.LevelD, "E": &s.LevelE}[line]
		if *level >= farmMaxLevel {
			return BizErr(400, "该线已升到满级")
		}
		names := map[string]string{"A": "产量A", "B": "产量B", "C": "周期C", "D": "丰收祝福", "E": "灾祸减免"}
		if line == "D" || line == "E" {
			// D/E：扣农场积分（Coins 内部扣减，不产生主积分流水）
			priceCoins := float64(FarmEEPrice(*level))
			if line == "D" {
				priceCoins = float64(FarmDEPrice(*level))
			}
			if s.Coins < priceCoins {
				return BizErr(400, "农场积分不足，还差 %.0f", priceCoins-s.Coins)
			}
			s.Coins -= priceCoins
			if line == "D" {
				s.LevelD++
			} else {
				s.LevelE++
			}
			if err := tx.Model(&model.FarmState{}).Where("id = ?", s.ID).
				Updates(map[string]any{"coins": s.Coins, "level_d": s.LevelD, "level_e": s.LevelE}).Error; err != nil {
				return err
			}
			out.Line, out.Level, out.Coins = line, *level, s.Coins
			out.Balance = PointBalance(tx)
			return nil
		}
		price := FarmUpgradePrice(*level)
		balance := PointBalance(tx)
		if balance < price {
			return BizErr(400, "积分不足，还差 %d 分", price-balance)
		}
		if err := tx.Create(&model.Ledger{Type: "farm", Amount: -price, Note: "农场升级 " + names[line] + " Lv" + strconv.Itoa(*level+1)}).Error; err != nil {
			return err
		}
		*level++
		if err := tx.Model(&model.FarmState{}).Where("id = ?", s.ID).
			Updates(map[string]any{"level_a": s.LevelA, "level_b": s.LevelB, "level_c": s.LevelC}).Error; err != nil {
			return err
		}
		out.Line, out.Level, out.Balance = line, *level, balance-price
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// WithdrawResult is the payload returned by WithdrawFarm.
type WithdrawResult struct {
	Points  int     `json:"points"`
	Coins   float64 `json:"coins"`
	Balance int     `json:"balance"`
}

// WithdrawFarm 农场积分 → 主积分，100:1 只提整数（小数留存），写 type=farm 正流水。
func WithdrawFarm(db *gorm.DB) (*WithdrawResult, error) {
	var out WithdrawResult
	err := db.Transaction(func(tx *gorm.DB) error {
		s, err := getFarmState(tx)
		if err != nil {
			return err
		}
		whole := math.Floor(s.Coins / farmCoinsPerPt)
		if whole < 1 {
			return BizErr(400, "农场积分不足 100，还无法提现")
		}
		points := int(whole)
		if err := tx.Create(&model.Ledger{Type: "farm", Amount: points, Note: "农场提现（" + strconv.Itoa(points) + " 分）"}).Error; err != nil {
			return err
		}
		s.Coins -= whole * farmCoinsPerPt
		if err := tx.Model(&model.FarmState{}).Where("id = ?", s.ID).Update("coins", s.Coins).Error; err != nil {
			return err
		}
		out.Points, out.Coins, out.Balance = points, s.Coins, PointBalance(tx)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SettleDailyBonus D 线丰收祝福（docs/07 十一 v3 净值带符号模型）：
// 昨日净值 N = task+box 收入 − penalty 罚分（提现/商城/道具不计，与统计净值口径一致）。
//   - N > 0：奖励 = N × 10%×D_Lv（type=farm_bonus 正行）——净赚才发
//   - N < 0：放大 = N × 10%×max(0, D−E)（type=farm_penalty 负行）——净亏放大亏损，E 对冲
//   - N = 0 或无需发放：只写幂等标记
//
// 每天一次（last_bonus_date 幂等键）；行 created_at 回溯昨日 23:59:59（归属昨天，
// 与罚分行的追溯口径一致）。挂在 SettlePenalties 末尾同一触发点。
func SettleDailyBonus(db *gorm.DB, now time.Time) {
	var fs model.FarmState
	if err := db.Take(&fs).Error; err != nil || (fs.LevelD <= 0 && fs.LevelE <= 0) {
		return
	}
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	yKey := yesterdayStart.Format("2006-01-02")
	if fs.LastBonusDate == yKey {
		return // 今天已结过昨天的奖励
	}
	chargeAt := yesterdayStart.Add(24*time.Hour - time.Second) // 归属昨天最后一刻
	db.Transaction(func(tx *gorm.DB) error {
		var fresh model.FarmState
		if err := tx.Take(&fresh).Error; err != nil {
			return nil // 无状态行：无事可做
		}
		if fresh.LastBonusDate == yKey {
			return nil // 并发下已被结算
		}
		mark := func() error {
			return tx.Model(&model.FarmState{}).Where("id = ?", fresh.ID).
				Update("last_bonus_date", yKey).Error
		}
		var net int
		tx.Model(&model.Ledger{}).
			Where("type IN ? AND created_at >= ? AND created_at < ?",
				[]string{"task", "box", "penalty"}, yesterdayStart, todayStart).
			Select("COALESCE(SUM(amount),0) AS amount").Scan(&net)
		if net == 0 {
			return mark()
		}
		if net > 0 {
			// 净赚：发奖励（E 不参与——它只对冲亏损放大）
			if fresh.LevelD <= 0 {
				return mark()
			}
			amount := int(math.Round(float64(net) * 0.1 * float64(fresh.LevelD)))
			if amount <= 0 {
				return mark()
			}
			if err := tx.Create(&model.Ledger{
				Type: "farm_bonus", Amount: amount,
				Note:      fmt.Sprintf("丰收祝福 Lv%d 昨日净收益加成（%d×%d%%）", fresh.LevelD, net, 10*fresh.LevelD),
				CreatedAt: chargeAt,
			}).Error; err != nil {
				return err
			}
			return mark()
		}
		// 净亏：放大亏损（E 对冲后仍有放大才发）
		gap := fresh.LevelD - fresh.LevelE
		if gap <= 0 {
			return mark()
		}
		amount := int(math.Round(float64(net) * 0.1 * float64(gap)))
		if amount >= 0 {
			return mark()
		}
		if err := tx.Create(&model.Ledger{
			Type: "farm_penalty", Amount: amount,
			Note:      fmt.Sprintf("丰收祝福 Lv%d 昨日净亏损放大（净 %d × −%d%%）", fresh.LevelD, net, 10*gap),
			CreatedAt: chargeAt,
		}).Error; err != nil {
			return err
		}
		return mark()
	})
}
