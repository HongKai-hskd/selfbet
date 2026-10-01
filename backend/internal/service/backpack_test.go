package service

import (
	"strings"
	"testing"

	"selfbet/backend/internal/model"
)

// 道具与积分并行（2026-10-01 拍板）：开到道具积分照得，不互斥。
func TestOpenBoxesItemAndPointsParallel(t *testing.T) {
	db := newTestDB(t)
	// 掉率 100% 的积分利息卡（×2 张），积分 10~20
	box := model.Box{
		Name: "紫箱", MinPoints: 10, MaxPoints: 20,
		ItemDrops: []model.ItemDrop{{ItemType: 1, Rate: 100, Qty: 2}},
	}
	db.Create(&box)
	for i := 0; i < 2; i++ {
		db.Create(&model.BackpackItem{Kind: "box", TypeID: box.ID})
	}

	results, total, err := OpenBoxes(db, box.ID, 2)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results=%d, want 2", len(results))
	}
	for i, r := range results {
		if r.Item != "积分利息卡" || r.Qty != 2 {
			t.Fatalf("box %d item=%q qty=%d, want 积分利息卡/2", i, r.Item, r.Qty)
		}
		if r.Points < 10 || r.Points > 20 {
			t.Fatalf("box %d points=%d, want 10~20 (parallel broken)", i, r.Points)
		}
	}
	if got := PointBalance(db); got != total {
		t.Fatalf("balance=%d, want %d", got, total)
	}
	var items int64
	db.Model(&model.BackpackItem{}).Where("kind = ?", "item").Count(&items)
	if items != 4 { // 2 箱 × 2 张
		t.Fatalf("item rows=%d, want 4", items)
	}
	// 流水 note 应注明附带道具
	var led model.Ledger
	db.Where("type = ?", "box").First(&led)
	if led.Note == "" || !strings.Contains(led.Note, "附「积分利息卡」") {
		t.Fatalf("note should mention the attached item: %q", led.Note)
	}
}

// 幸运符吃第一个开出的箱子（并行制下含附道具的箱子）。
func TestOpenBoxesLuckyHitsFirstBoxWithItem(t *testing.T) {
	db := newTestDB(t)
	box := model.Box{
		Name: "铜箱", MinPoints: 10, MaxPoints: 10, // 固定 10 分便于断言
		ItemDrops: []model.ItemDrop{{ItemType: 5, Rate: 100, Qty: 1}},
	}
	db.Create(&box)
	for i := 0; i < 2; i++ {
		db.Create(&model.BackpackItem{Kind: "box", TypeID: box.ID})
	}
	db.Create(&model.PendingEffect{Kind: pendingLucky})

	results, _, err := OpenBoxes(db, box.ID, 2)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// 第一箱：积分 10×2=20 且附幸运符道具；第二箱：无加成 10 分
	if results[0].Points != 20 || results[0].Bonus == "" {
		t.Fatalf("first box should be doubled: %+v", results[0])
	}
	if results[1].Points != 10 || results[1].Bonus != "" {
		t.Fatalf("second box should be plain: %+v", results[1])
	}
}
