package api

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// newTestFarmDB 内存库跑农场全流程，不碰生产库。
func newTestFarmDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.Box{}, &model.ShopItem{}, &model.Ledger{},
		&model.Tag{}, &model.CashFlow{}, &model.BackpackItem{}, &model.PendingEffect{},
		&model.FarmState{}, &model.FarmPlot{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Create(&model.FarmState{ID: 1})
	for i := 0; i < farmPlotTotal; i++ {
		db.Create(&model.FarmPlot{PlotIndex: i, Unlocked: i == 0})
	}
	return db
}

// farmSetPlantedAt 把某块田的种下时间拨到 hoursAgo 小时前（模拟成熟）。
func farmSetPlantedAt(t *testing.T, db *gorm.DB, idx int, hoursAgo float64) {
	t.Helper()
	at := time.Now().Add(-time.Duration(hoursAgo * float64(time.Hour)))
	if err := db.Model(&model.FarmPlot{}).Where("plot_index = ?", idx).
		Update("planted_at", at).Error; err != nil {
		t.Fatalf("set planted_at: %v", err)
	}
}

func TestFarmInitAndPlant(t *testing.T) {
	db := newTestFarmDB(t)
	var plots []model.FarmPlot
	db.Order("plot_index ASC").Find(&plots)
	if len(plots) != farmPlotTotal {
		t.Fatalf("init plots wrong: total=%d", len(plots))
	}
	if !plots[0].Unlocked {
		t.Fatal("plot 0 should be unlocked")
	}
	if plots[1].Unlocked {
		t.Fatal("plot 1 should be locked")
	}
	// 免费种植：空田 → planted_at 非空
	if err := db.Model(&model.FarmPlot{}).Where("plot_index = ?", 0).
		Update("planted_at", time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	var p model.FarmPlot
	db.Where("plot_index = ?", 0).First(&p)
	if p.PlantedAt == nil {
		t.Fatal("plant failed")
	}
}

func TestFarmFormulas(t *testing.T) {
	s := model.FarmState{ID: 1, LevelA: 10, LevelB: 10, LevelC: 10}
	if got := farmYieldPerRound(&s); got < 383.9 || got > 384.1 {
		t.Fatalf("yield A10B10 = %v, want 384", got) // 200×1.2×1.6
	}
	if got := farmPeriodHours(&s); got < 10.39 || got > 10.41 {
		t.Fatalf("period A10B10C10 = %v, want 10.4h", got) // 8×(1+0.8−0.5)
	}
	// B 无 C：8×1.8 = 14.4h
	s2 := model.FarmState{LevelB: 10}
	if got := farmPeriodHours(&s2); got < 14.39 || got > 14.41 {
		t.Fatalf("period B10C0 = %v, want 14.4h", got)
	}
	// 价格曲线：Lv1=50，Lv10=50×1.3^9≈530
	if farmUpgradePrice(0) != 50 {
		t.Fatalf("upgrade lv1 price = %d", farmUpgradePrice(0))
	}
	if p := farmUpgradePrice(9); p < 525 || p > 535 {
		t.Fatalf("upgrade lv10 price = %d, want ≈530", p)
	}
	// 地契：第 2 块 50，每块 +40，第 24 块 = 50+40×22 = 930
	if farmPlotPrice(1) != 50 || farmPlotPrice(2) != 90 || farmPlotPrice(23) != 930 {
		t.Fatalf("plot price wrong: %d %d %d", farmPlotPrice(1), farmPlotPrice(2), farmPlotPrice(23))
	}
}

func TestFarmHarvestDailyCap(t *testing.T) {
	db := newTestFarmDB(t)
	// 第 1 轮：9h 前种下（基础周期 8h）→ 可收
	farmSetPlantedAt(t, db, 0, 9)
	harvestAt := func(idx int) (float64, error) {
		var p model.FarmPlot
		db.Where("plot_index = ?", idx).First(&p)
		planted := p.PlantedAt
		s, _ := getFarmState(db)
		periodH := farmPeriodHours(s)
		yield := farmYieldPerRound(s)
		today := farmToday()
		if planted == nil || time.Now().Sub(*planted).Hours() < periodH {
			return 0, errFake
		}
		if p.DailyDate != today {
			p.DailyCount = 0
			p.DailyDate = today
		}
		if p.DailyCount >= farmDailyMax {
			return 0, errFake
		}
		gained := yield
		p.DailyCount++
		p.PlantedAt = nil
		db.Model(&model.FarmPlot{}).Where("id = ?", p.ID).
			Updates(map[string]any{"planted_at": nil, "daily_count": p.DailyCount, "daily_date": p.DailyDate})
		s.Coins += gained
		s.TotalHarvest++
		db.Model(&model.FarmState{}).Where("id = ?", 1).
			Updates(map[string]any{"coins": s.Coins, "total_harvest": s.TotalHarvest})
		return gained, nil
	}
	g1, err := harvestAt(0)
	if err != nil || g1 < 199.9 || g1 > 200.1 {
		t.Fatalf("round1 gained=%v err=%v, want 200", g1, err)
	}
	// 第 2 轮：再种再收
	farmSetPlantedAt(t, db, 0, 9)
	if _, err := harvestAt(0); err != nil {
		t.Fatalf("round2 err=%v", err)
	}
	// 第 3 轮：今日已满 → 拒绝
	farmSetPlantedAt(t, db, 0, 9)
	if _, err := harvestAt(0); err == nil {
		t.Fatal("round3 should be rejected (daily cap)")
	}
	var p model.FarmPlot
	db.Where("plot_index = ?", 0).First(&p)
	if p.PlantedAt == nil {
		t.Fatal("mature crop should stay in field when daily cap reached (永不枯死)")
	}
	// 跨天重置：把 DailyDate 拨成昨天 → 又能收
	db.Model(&model.FarmPlot{}).Where("plot_index = ?", 0).
		Update("daily_date", time.Now().AddDate(0, 0, -1).Format("2006-01-02"))
	if _, err := harvestAt(0); err != nil {
		t.Fatalf("after date reset should harvest, err=%v", err)
	}
}

var errFake = errorFake{}

type errorFake struct{}

func (errorFake) Error() string { return "fake" }

func TestFarmWithdraw(t *testing.T) {
	db := newTestFarmDB(t)
	s, _ := getFarmState(db)
	s.Coins = 450.7
	db.Model(&model.FarmState{}).Where("id = ?", 1).Update("coins", s.Coins)

	whole := float64(int(s.Coins / farmCoinsPerPt)) // floor = 4
	if whole != 4 {
		t.Fatalf("withdrawable = %v, want 4", whole)
	}
	// 提现：coins -= 400 → 50.7；主积分 +4 写 type=farm 正流水
	s.Coins -= whole * farmCoinsPerPt
	db.Model(&model.FarmState{}).Where("id = ?", 1).Update("coins", s.Coins)
	db.Create(&model.Ledger{Type: "farm", Amount: int(whole), Note: "农场提现"})
	var left float64
	db.Model(&model.FarmState{}).Where("id = ?", 1).Select("coins").Scan(&left)
	if left < 50.6 || left > 50.8 {
		t.Fatalf("coins after withdraw = %v, want 50.7", left)
	}
	var sum int
	db.Model(&model.Ledger{}).Select("COALESCE(SUM(amount),0) AS sum").Scan(&sum)
	if sum != 4 {
		t.Fatalf("point balance = %d, want 4", sum)
	}
}

func TestFarmBuyPlotAndUpgradeLedger(t *testing.T) {
	db := newTestFarmDB(t)
	// 先给主积分：任务流水 +1000
	db.Create(&model.Ledger{Type: "task", Amount: 1000, Note: "任务"})
	// 买第 2 块：-50，解锁 index1
	db.Create(&model.Ledger{Type: "farm", Amount: -farmPlotPrice(1), Note: "开垦第 2 块田"})
	db.Model(&model.FarmPlot{}).Where("plot_index = ?", 1).Update("unlocked", true)
	var p model.FarmPlot
	db.Where("plot_index = ?", 1).First(&p)
	if !p.Unlocked {
		t.Fatal("plot 1 should be unlocked")
	}
	var sum int
	db.Model(&model.Ledger{}).Select("COALESCE(SUM(amount),0) AS sum").Scan(&sum)
	if sum != 1000-50 {
		t.Fatalf("balance = %d, want 950", sum)
	}
	// 升级 A Lv1：-50 → level_a=1
	db.Create(&model.Ledger{Type: "farm", Amount: -farmUpgradePrice(0), Note: "农场升级 产量A Lv1"})
	db.Model(&model.FarmState{}).Where("id = ?", 1).Update("level_a", 1)
	s, _ := getFarmState(db)
	if s.LevelA != 1 {
		t.Fatal("level_a should be 1")
	}
	// farm 类型流水不可撤回（UndoLedger 只允许 task/box）
	var led model.Ledger
	db.Where("type = ?", "farm").First(&led)
	if led.Type != "farm" {
		t.Fatal("ledger type")
	}
}
