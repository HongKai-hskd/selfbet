package service

import (
	"testing"
	"time"

	"selfbet/backend/internal/model"
)

func TestCompleteTaskPaysAndConsumesDoubleCard(t *testing.T) {
	db := newTestDB(t)
	task := model.Task{Title: "背单词", Points: 10, Status: "pending"}
	db.Create(&task)
	db.Create(&model.PendingEffect{Kind: pendingDouble})

	res, err := CompleteTask(db, task.ID, time.Now())
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !res.Doubled || res.Earned != 20 {
		t.Fatalf("earned=%d doubled=%v, want 20/true", res.Earned, res.Doubled)
	}
	if got := PointBalance(db); got != 20 {
		t.Fatalf("balance=%d, want 20", got)
	}
	var n int64
	db.Model(&model.PendingEffect{}).Where("kind = ?", pendingDouble).Count(&n)
	if n != 0 {
		t.Fatal("double card should be consumed")
	}
	// 重复完成被拒
	if _, err := CompleteTask(db, task.ID, time.Now()); err == nil {
		t.Fatal("second complete should be rejected")
	}
}

func TestCompleteTaskDailyOncePerPeriod(t *testing.T) {
	db := newTestDB(t)
	task := model.Task{Title: "早睡", Points: 5, Repeat: "daily", Status: "pending"}
	db.Create(&task)
	if _, err := CompleteTask(db, task.ID, time.Now()); err != nil {
		t.Fatalf("first complete: %v", err)
	}
	if _, err := CompleteTask(db, task.ID, time.Now()); err == nil {
		t.Fatal("same-period second complete should be rejected")
	}
	// multi-round：同周期可重复领取
	mr := model.Task{Title: "喝水", Points: 2, Repeat: "daily", MultiRound: true, Status: "pending"}
	db.Create(&mr)
	for i := 0; i < 3; i++ {
		if _, err := CompleteTask(db, mr.ID, time.Now()); err != nil {
			t.Fatalf("multi-round round %d: %v", i+1, err)
		}
	}
	if got := PointBalance(db); got != 5+2*3 {
		t.Fatalf("balance=%d, want %d", got, 5+2*3)
	}
}

func TestSettlePenaltiesDailyAndIdempotent(t *testing.T) {
	db := newTestDB(t)
	// 昨天创建的 daily 任务，昨天没完成 → 罚 8
	yesterday := time.Now().AddDate(0, 0, -1)
	db.Create(&model.Task{
		Title: "喝水", Points: 1, Repeat: "daily", Penalty: 8, CreatedAt: yesterday,
	})

	SettlePenalties(db, time.Now())

	var rows []model.Ledger
	db.Where("type = ?", "penalty").Find(&rows)
	if len(rows) != 1 || rows[0].Amount != -8 {
		t.Fatalf("penalty rows=%+v, want one -8", rows)
	}
	if rows[0].CreatedAt.Day() != yesterday.Day() {
		t.Fatalf("chargeAt should fall on the failed period's last moment: %v", rows[0].CreatedAt)
	}
	// 幂等：再跑一次不重复罚
	SettlePenalties(db, time.Now())
	db.Where("type = ?", "penalty").Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("idempotency broken: %d rows", len(rows))
	}
	// 免罚金牌窗口内：豁免且留 0 分审计行
	db.Create(&model.Task{
		Title: "早睡", Points: 1, Repeat: "daily", Penalty: 5, CreatedAt: yesterday,
	})
	exp := time.Now().Add(time.Hour)
	db.Create(&model.PendingEffect{Kind: pendingExempt, ExpiresAt: &exp})
	SettlePenalties(db, time.Now())
	var exemptRows []model.Ledger
	db.Where("type = ? AND amount = 0", "penalty").Find(&exemptRows)
	if len(exemptRows) != 1 {
		t.Fatalf("exempt audit row missing: %+v", exemptRows)
	}
}

func TestUndoLedgerRecallsBackpack(t *testing.T) {
	db := newTestDB(t)
	// 掉率 100% 的关联宝箱：完成必掉一个未开箱进背包
	box := model.Box{Name: "铜箱", MinPoints: 1, MaxPoints: 2, ItemDrops: []model.ItemDrop{}}
	db.Create(&box)
	boxID := box.ID
	task := model.Task{Title: "刷题", Points: 10, Status: "pending", BoxID: &boxID, BoxDropRate: 100}
	db.Create(&task)
	res, err := CompleteTask(db, task.ID, time.Now())
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Box == nil || res.Box.Item {
		t.Fatalf("box drop expected: %+v", res.Box)
	}
	var bpBefore int64
	db.Model(&model.BackpackItem{}).Where("kind = ?", "box").Count(&bpBefore)
	if bpBefore != 1 {
		t.Fatalf("backpack should hold 1 unopened box, got %d", bpBefore)
	}

	// 撤回该完成：流水删除 + 连带收回未开宝箱
	var lrow model.Ledger
	db.Where("type = ? AND ref_id = ?", "task", task.ID).First(&lrow)
	if lrow.ID == 0 {
		t.Fatal("task ledger row should exist")
	}
	if _, err := UndoLedgerRow(db, lrow.ID, time.Now()); err != nil {
		t.Fatalf("undo: %v", err)
	}
	var bpAfter int64
	db.Model(&model.BackpackItem{}).Where("source = ?", lrow.ID).Count(&bpAfter)
	if bpAfter != 0 {
		t.Fatalf("backpack recall failed: %d rows left", bpAfter)
	}
	if got := PointBalance(db); got != 0 {
		t.Fatalf("balance after undo = %d, want 0", got)
	}
	// 状态恢复：once 任务（无截止日/已逾期语义）回到 doing
	var after model.Task
	db.First(&after, task.ID)
	if after.Status != "doing" {
		t.Fatalf("status after undo = %q, want doing", after.Status)
	}
	// 撤回后可以重新完成（宝箱再掉一次）
	res2, err := CompleteTask(db, task.ID, time.Now())
	if err != nil {
		t.Fatalf("re-complete after undo: %v", err)
	}
	if res2.Box == nil {
		t.Fatal("re-complete should drop the box again")
	}
}
