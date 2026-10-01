package service

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// newTestDB 内存库跑全流程，不碰生产库。
func newTestDB(t *testing.T) *gorm.DB {
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

func intPtr(i int) *int { return &i }

// plantAt 把某块田的种下时间拨到 hoursAgo 小时前（模拟成熟）。
func plantAt(t *testing.T, db *gorm.DB, idx int, hoursAgo float64) {
	t.Helper()
	at := time.Now().Add(-time.Duration(hoursAgo * float64(time.Hour)))
	if err := db.Model(&model.FarmPlot{}).Where("plot_index = ?", idx).
		Update("planted_at", at).Error; err != nil {
		t.Fatalf("set planted_at: %v", err)
	}
}

func TestFarmInitAndPlant(t *testing.T) {
	db := newTestDB(t)
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
	// 免费种植走真函数：空田 → planted_at 非空
	n, err := PlantFarm(db, intPtr(0))
	if err != nil || n != 1 {
		t.Fatalf("plant n=%d err=%v, want 1/nil", n, err)
	}
	var p model.FarmPlot
	db.Where("plot_index = ?", 0).First(&p)
	if p.PlantedAt == nil {
		t.Fatal("plant failed")
	}
	// 已种的地再种 → 拒绝
	if _, err := PlantFarm(db, intPtr(0)); err == nil {
		t.Fatal("plant on planted plot should be rejected")
	}
}

func TestFarmFormulas(t *testing.T) {
	s := model.FarmState{ID: 1, LevelA: 10, LevelB: 10, LevelC: 10}
	if got := FarmYieldPerRound(&s); got < 383.9 || got > 384.1 {
		t.Fatalf("yield A10B10 = %v, want 384", got) // 200×1.2×1.6
	}
	if got := FarmPeriodHours(&s); got < 10.39 || got > 10.41 {
		t.Fatalf("period A10B10C10 = %v, want 10.4h", got) // 8×(1+0.8−0.5)
	}
	// B 无 C：8×1.8 = 14.4h
	s2 := model.FarmState{LevelB: 10}
	if got := FarmPeriodHours(&s2); got < 14.39 || got > 14.41 {
		t.Fatalf("period B10C0 = %v, want 14.4h", got)
	}
	// 价格曲线：Lv1=50，Lv10=50×1.3^9≈530
	if FarmUpgradePrice(0) != 50 {
		t.Fatalf("upgrade lv1 price = %d", FarmUpgradePrice(0))
	}
	if p := FarmUpgradePrice(9); p < 525 || p > 535 {
		t.Fatalf("upgrade lv10 price = %d, want ≈530", p)
	}
	// 地契：第 2 块 50，每块 +40，第 24 块 = 50+40×22 = 930
	if FarmPlotPrice(1) != 50 || FarmPlotPrice(2) != 90 || FarmPlotPrice(23) != 930 {
		t.Fatalf("plot price wrong: %d %d %d", FarmPlotPrice(1), FarmPlotPrice(2), FarmPlotPrice(23))
	}
}

func TestFarmHarvestDailyCap(t *testing.T) {
	db := newTestDB(t)
	// 第 1 轮：9h 前种下（基础周期 8h）→ 可收
	plantAt(t, db, 0, 9)
	res, err := HarvestFarm(db, false, intPtr(0))
	if err != nil || res.Gained < 199.9 || res.Gained > 200.1 || res.Harvested != 1 {
		t.Fatalf("round1 res=%+v err=%v, want gained 200", res, err)
	}
	// 第 2 轮：再种再收
	plantAt(t, db, 0, 9)
	if _, err := HarvestFarm(db, false, intPtr(0)); err != nil {
		t.Fatalf("round2 err=%v", err)
	}
	// 第 3 轮：今日已满 → 拒绝
	plantAt(t, db, 0, 9)
	if _, err := HarvestFarm(db, false, intPtr(0)); err == nil {
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
	if _, err := HarvestFarm(db, false, intPtr(0)); err != nil {
		t.Fatalf("after date reset should harvest, err=%v", err)
	}
}

func TestFarmWithdraw(t *testing.T) {
	db := newTestDB(t)
	db.Model(&model.FarmState{}).Where("id = ?", 1).Update("coins", 450.7)
	res, err := WithdrawFarm(db)
	if err != nil {
		t.Fatalf("withdraw err=%v", err)
	}
	if res.Points != 4 {
		t.Fatalf("points = %d, want 4", res.Points)
	}
	if res.Coins < 50.6 || res.Coins > 50.8 {
		t.Fatalf("coins after withdraw = %v, want 50.7", res.Coins)
	}
	if got := PointBalance(db); got != 4 {
		t.Fatalf("point balance = %d, want 4", got)
	}
	// 小数留存：再提 → 不足 100 拒绝
	if _, err := WithdrawFarm(db); err == nil {
		t.Fatal("second withdraw should be rejected (50.7 < 100)")
	}
}

func TestFarmBuyPlotAndUpgradeLedger(t *testing.T) {
	db := newTestDB(t)
	// 先给主积分：任务流水 +1000
	db.Create(&model.Ledger{Type: "task", Amount: 1000, Note: "任务"})
	// 买第 2 块：-50，解锁 index1
	res, err := BuyFarmPlot(db)
	if err != nil {
		t.Fatalf("buy err=%v", err)
	}
	if res.UnlockedIdx != 1 || res.Price != FarmPlotPrice(1) || res.TotalPlots != farmPlotTotal {
		t.Fatalf("buy res=%+v", res)
	}
	var p model.FarmPlot
	db.Where("plot_index = ?", 1).First(&p)
	if !p.Unlocked {
		t.Fatal("plot 1 should be unlocked")
	}
	if got := PointBalance(db); got != 1000-FarmPlotPrice(1) {
		t.Fatalf("balance = %d, want %d", got, 1000-FarmPlotPrice(1))
	}
	// 升级 A Lv1：-50 → level_a=1
	up, err := UpgradeFarm(db, "A")
	if err != nil {
		t.Fatalf("upgrade err=%v", err)
	}
	if up.Level != 1 || up.Line != "A" {
		t.Fatalf("upgrade res=%+v", up)
	}
	var s model.FarmState
	db.First(&s, 1)
	if s.LevelA != 1 {
		t.Fatal("level_a should be 1")
	}
	// 非法线名 → 业务拒绝
	if _, err := UpgradeFarm(db, "D"); err == nil {
		t.Fatal("invalid line should be rejected")
	}
	// farm 类型流水不可撤回（UndoLedgerRow 只允许 task/box）
	var led model.Ledger
	db.Where("type = ?", "farm").First(&led)
	if led.Type != "farm" {
		t.Fatal("ledger type")
	}
	if _, err := UndoLedgerRow(db, led.ID, time.Now()); err == nil {
		t.Fatal("farm ledger row should not be undoable")
	}
}

func TestFarmPenaltyCoef(t *testing.T) {
	cases := []struct {
		d, e int
		want float64
	}{
		{0, 0, 1}, {10, 0, 2}, {10, 10, 1}, {10, 5, 1.5}, {0, 10, 1}, {5, 10, 1},
	}
	for _, c := range cases {
		if got := FarmPenaltyCoef(c.d, c.e); got != c.want {
			t.Errorf("FarmPenaltyCoef(%d,%d) = %v, want %v", c.d, c.e, got, c.want)
		}
	}
}
