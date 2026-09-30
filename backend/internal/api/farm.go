package api

import (
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
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
	farmUpgradeBase = 50.0 // 升级首级价（三线统一）
	farmUpgradeStep = 1.3  // 升级递增
	farmDailyMax    = 2    // 每田每日收获上限
)

// farmYieldPerRound 每轮收获的农场积分（A、B 乘法合成）。
func farmYieldPerRound(s *model.FarmState) float64 {
	return farmBaseYield * (1+0.02*float64(s.LevelA)) * (1+0.06*float64(s.LevelB))
}

// farmPeriodHours 当前有效生长周期（B 副作用与 C 线加法合成，A 无副作用）。
func farmPeriodHours(s *model.FarmState) float64 {
	h := farmBasePeriodH * (1 + 0.08*float64(s.LevelB) - 0.05*float64(s.LevelC))
	if h < 1 {
		h = 1 // 保险丝：合成参数异常时周期不为负
	}
	return h
}

// farmUpgradePrice 当前等级升下一级的价格（level = 已有等级 0..9）。
func farmUpgradePrice(level int) int {
	return int(math.Round(farmUpgradeBase * math.Pow(farmUpgradeStep, float64(level))))
}

// farmPlotPrice 第 n 块地契价（n = plotIndex+1，n≥2）。
func farmPlotPrice(plotIndex int) int {
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

func pointBalance(db *gorm.DB) int {
	var row struct {
		Sum int
	}
	db.Model(&model.Ledger{}).Select("COALESCE(SUM(amount),0) AS sum").Scan(&row)
	return row.Sum
}

// GetFarm 全量状态：余额、等级、合成数值、24 田实时状态、下一块地价、升级价。
func GetFarm(db *gorm.DB) gin.HandlerFunc {
	type plotView struct {
		PlotIndex   int        `json:"plot_index"`
		Unlocked    bool       `json:"unlocked"`
		PlantedAt   *time.Time `json:"planted_at"`
		Mature      bool       `json:"mature"`
		Progress    float64    `json:"progress"` // 0-1，空田为 0
		DailyLeft   int        `json:"daily_left"`
		Harvestable bool       `json:"harvestable"`
	}
	return func(c *gin.Context) {
		s, err := getFarmState(db)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		plots := []model.FarmPlot{}
		if err := db.Order("plot_index ASC").Find(&plots).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		yield := farmYieldPerRound(s)
		periodH := farmPeriodHours(s)
		now := time.Now()
		views := []plotView{}
		nextIdx, nextPrice := -1, 0
		harvestNow := 0.0
		for _, p := range plots {
			v := plotView{PlotIndex: p.PlotIndex, Unlocked: p.Unlocked, PlantedAt: p.PlantedAt, DailyLeft: farmDailyLeft(&p)}
			if p.Unlocked && p.PlantedAt != nil {
				elapsed := now.Sub(*p.PlantedAt).Hours()
				v.Progress = elapsed / periodH
				if v.Progress > 1 {
					v.Progress = 1
				}
				v.Mature = elapsed >= periodH
				if v.Mature && v.DailyLeft > 0 {
					v.Harvestable = true
					harvestNow += yield
				}
			}
			if !p.Unlocked && nextIdx == -1 {
				nextIdx = p.PlotIndex
				nextPrice = farmPlotPrice(p.PlotIndex)
			}
			views = append(views, v)
		}
		upg := func(level int) gin.H {
			maxed := level >= farmMaxLevel
			price := 0
			if !maxed {
				price = farmUpgradePrice(level)
			}
			return gin.H{"level": level, "price": price, "maxed": maxed}
		}
		c.JSON(http.StatusOK, gin.H{
			"coins":            s.Coins,
			"total_harvest":    s.TotalHarvest,
			"level_a":          s.LevelA,
			"level_b":          s.LevelB,
			"level_c":          s.LevelC,
			"yield_per_round":  yield,
			"period_hours":     periodH,
			"daily_income_max": yield * float64(farmDailyMax) * math.Max(1, float64(countUnlocked(db))),
			"plots":            views,
			"next_plot_index":  nextIdx,
			"next_plot_price":  nextPrice,
			"upgrade_a":        upg(s.LevelA),
			"upgrade_b":        upg(s.LevelB),
			"upgrade_c":        upg(s.LevelC),
			"withdrawable":     math.Floor(s.Coins/farmCoinsPerPt),
			"harvest_now":      harvestNow,
			"balance":          pointBalance(db),
		})
	}
}

func countUnlocked(db *gorm.DB) int64 {
	var n int64
	db.Model(&model.FarmPlot{}).Where("unlocked = ?", true).Count(&n)
	return n
}

// PlantFarm 免费种植：plot_index 为空 = 一键全种所有空田。
func PlantFarm(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			PlotIndex *int `json:"plot_index"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		planted := 0
		err := db.Transaction(func(tx *gorm.DB) error {
			if _, err := getFarmState(tx); err != nil {
				return err
			}
			q := tx.Model(&model.FarmPlot{}).
				Where("unlocked = ? AND planted_at IS NULL", true)
			if body.PlotIndex != nil {
				q = q.Where("plot_index = ?", *body.PlotIndex)
			}
			res := q.Update("planted_at", time.Now())
			if res.Error != nil {
				return res.Error
			}
			planted = int(res.RowsAffected)
			return nil
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if planted == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "没有可种植的空田"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "planted": planted})
	}
}

// HarvestFarm 收获：all=true 一键全收，否则收指定田。
// 校验成熟 + 当日轮次 <2（按收获时刻的本地日期惰性重置）。
func HarvestFarm(db *gorm.DB) gin.HandlerFunc {
	type out struct {
		Gained    float64 `json:"gained"`
		Coins     float64 `json:"coins"`
		Harvested int     `json:"harvested"`
		DailyLeft int     `json:"daily_left"` // 一键全收后仍有余量而成熟未收的田数（提示文案用）
	}
	return func(c *gin.Context) {
		var body struct {
			All       bool `json:"all"`
			PlotIndex *int `json:"plot_index"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		var res out
		err := db.Transaction(func(tx *gorm.DB) error {
			s, err := getFarmState(tx)
			if err != nil {
				return err
			}
			plots := []model.FarmPlot{}
			q := tx.Where("unlocked = ? AND planted_at IS NOT NULL", true)
			if !body.All {
				if body.PlotIndex == nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
					return gorm.ErrDuplicatedKey
				}
				q = q.Where("plot_index = ?", *body.PlotIndex)
			}
			if err := q.Find(&plots).Error; err != nil {
				return err
			}
			periodH := farmPeriodHours(s)
			yield := farmYieldPerRound(s)
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
				c.JSON(http.StatusBadRequest, gin.H{"error": "没有可收获的作物（未成熟或今日轮次已满）"})
				return gorm.ErrDuplicatedKey
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
			return // response already written
		}
		c.JSON(http.StatusOK, res)
	}
}

// BuyFarmPlot 开垦下一块田（顺序解锁），价格扣主积分、写 type=farm 负流水。
func BuyFarmPlot(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var out struct {
			Balance     int `json:"balance"`
			UnlockedIdx int `json:"unlocked_index"`
			Price       int `json:"price"`
			TotalPlots  int `json:"total_plots"`
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			var next model.FarmPlot
			if err := tx.Where("unlocked = ?", false).Order("plot_index ASC").First(&next).Error; err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "农场已全部开垦完毕"})
				return gorm.ErrDuplicatedKey
			}
			price := farmPlotPrice(next.PlotIndex)
			balance := pointBalance(tx)
			if balance < price {
				c.JSON(http.StatusBadRequest, gin.H{"error": "积分不足，还差 " + itoa(price-balance) + " 分"})
				return gorm.ErrDuplicatedKey
			}
			n := next.PlotIndex + 1 // 第几块
			if err := tx.Create(&model.Ledger{Type: "farm", Amount: -price, Note: "开垦第 " + itoa(n) + " 块田"}).Error; err != nil {
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
			return
		}
		out.TotalPlots = farmPlotTotal
		c.JSON(http.StatusOK, out)
	}
}

// UpgradeFarm 升级 A/B/C 三线之一，扣主积分、写 type=farm 负流水。
func UpgradeFarm(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Line string `json:"line"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || (body.Line != "A" && body.Line != "B" && body.Line != "C") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		var out struct {
			Line    string `json:"line"`
			Level   int    `json:"level"`
			Balance int    `json:"balance"`
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			s, err := getFarmState(tx)
			if err != nil {
				return err
			}
			level := map[string]*int{"A": &s.LevelA, "B": &s.LevelB, "C": &s.LevelC}[body.Line]
			if *level >= farmMaxLevel {
				c.JSON(http.StatusBadRequest, gin.H{"error": "该线已升到满级"})
				return gorm.ErrDuplicatedKey
			}
			price := farmUpgradePrice(*level)
			balance := pointBalance(tx)
			if balance < price {
				c.JSON(http.StatusBadRequest, gin.H{"error": "积分不足，还差 " + itoa(price-balance) + " 分"})
				return gorm.ErrDuplicatedKey
			}
			names := map[string]string{"A": "产量A", "B": "产量B", "C": "周期C"}
			if err := tx.Create(&model.Ledger{Type: "farm", Amount: -price, Note: "农场升级 " + names[body.Line] + " Lv" + itoa(*level+1)}).Error; err != nil {
				return err
			}
			*level++
			if err := tx.Model(&model.FarmState{}).Where("id = ?", s.ID).
				Updates(map[string]any{"level_a": s.LevelA, "level_b": s.LevelB, "level_c": s.LevelC}).Error; err != nil {
				return err
			}
			out.Line, out.Level, out.Balance = body.Line, *level, balance-price
			return nil
		})
		if err != nil {
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

// WithdrawFarm 农场积分 → 主积分，100:1 只提整数（小数留存），写 type=farm 正流水。
func WithdrawFarm(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var out struct {
			Points  int     `json:"points"`
			Coins   float64 `json:"coins"`
			Balance int     `json:"balance"`
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			s, err := getFarmState(tx)
			if err != nil {
				return err
			}
			whole := math.Floor(s.Coins / farmCoinsPerPt)
			if whole < 1 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "农场积分不足 100，还无法提现"})
				return gorm.ErrDuplicatedKey
			}
			points := int(whole)
			if err := tx.Create(&model.Ledger{Type: "farm", Amount: points, Note: "农场提现（" + itoa(points) + " 分）"}).Error; err != nil {
				return err
			}
			s.Coins -= float64(whole) * farmCoinsPerPt
			if err := tx.Model(&model.FarmState{}).Where("id = ?", s.ID).Update("coins", s.Coins).Error; err != nil {
				return err
			}
			out.Points, out.Coins, out.Balance = points, s.Coins, pointBalance(tx)
			return nil
		})
		if err != nil {
			return
		}
		c.JSON(http.StatusOK, out)
	}
}
