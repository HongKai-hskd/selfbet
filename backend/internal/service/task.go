package service

import (
	"math/rand"
	"strconv"
	"time"

	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// EffectiveStatus computes the display status. Repeating tasks are
// automatically "in progress" each period (no manual start needed) and
// reset lazily at period boundaries. Once tasks with a deadline auto-start
// on the deadline day (and stay doing if overdue) — same no-manual-start
// spirit as repeating tasks.
func EffectiveStatus(t *model.Task, now time.Time) string {
	if t.Repeat == "daily" || t.Repeat == "weekly" {
		// multi-round tasks stay claimable all period (each round pays)
		if !t.MultiRound && t.LastDoneKey == periodKey(t.Repeat, now) {
			return "done"
		}
		return "doing"
	}
	if t.Repeat == "once" && t.Status == "pending" && t.DueAt != nil {
		dueDayStart := time.Date(t.DueAt.Year(), t.DueAt.Month(), t.DueAt.Day(), 0, 0, 0, 0, t.DueAt.Location())
		if !now.Before(dueDayStart) {
			return "doing" // 截止日当天（或逾期后）自动开始
		}
	}
	return t.Status
}

// BoxResult is the drop roll outcome attached to a task completion.
type BoxResult struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Item  bool   `json:"item"` // true = 开出的是道具而非待开宝箱
	Qty   int    `json:"qty,omitempty"`
}

// CompleteResult is the payload returned by CompleteTask.
type CompleteResult struct {
	Task    model.Task  `json:"task"`
	Earned  int         `json:"earned"`
	Doubled bool        `json:"doubled,omitempty"` // 双倍卡生效
	Box     *BoxResult  `json:"box"`
	Balance int         `json:"balance"`
}

// CompleteTask marks the task done, credits its points instantly and rolls
// for the linked box drop. Everything happens in one transaction.
func CompleteTask(db *gorm.DB, taskID uint, now time.Time, count int) (*CompleteResult, error) {
	if count < 1 {
		count = 1
	}
	if count > 50 {
		count = 50 // 单次批量上限，防误传大数
	}
	var out CompleteResult
	err := db.Transaction(func(tx *gorm.DB) error {
		var task model.Task
		if err := tx.First(&task, taskID).Error; err != nil {
			return BizErr(404, "任务不存在")
		}
		multi := task.MultiRound && (task.Repeat == "daily" || task.Repeat == "weekly")
		if !multi && count > 1 {
			return BizErr(400, "该任务不支持批量完成")
		}
		if task.Repeat == "daily" || task.Repeat == "weekly" {
			// repeating task: single-round credits once per period (resets
			// lazily); multi-round pays every completion
			key := periodKey(task.Repeat, now)
			if task.LastDoneKey == key && !task.MultiRound {
				return BizErr(400, "本周期已完成，下个周期再来")
			}
			task.LastDoneKey = key
			task.CompletedAt = &now
			if err := tx.Save(&task).Error; err != nil {
				return err
			}
		} else {
			if task.Status == "done" {
				return BizErr(400, "任务已完成，不要重复领取")
			}
			task.Status, task.CompletedAt = "done", &now
			if err := tx.Save(&task).Error; err != nil {
				return err
			}
		}
		// 双倍卡：下一个完成的任务积分 ×2（事务内先删后用，RowsAffected 防双耗）。
		// 批量完成时一张卡只作用于第 1 轮
		doubled := false
		var de model.PendingEffect
		if err := tx.Where("kind = ?", pendingDouble).Order("id ASC").First(&de).Error; err == nil {
			del := tx.Where("id = ?", de.ID).Delete(&model.PendingEffect{})
			if del.Error != nil {
				return del.Error
			}
			if del.RowsAffected > 0 {
				doubled = true
			}
		}
		// 轮次基数 = 本周期已有的 task 流水数（一次查询，批量内递增）
		roundBase := 0
		if multi {
			start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			if task.Repeat == "weekly" {
				start = StartOfWeek(now)
			}
			var prior []model.Ledger
			tx.Where("type = ? AND amount > 0 AND ref_id = ?", "task", task.ID).Find(&prior)
			for _, l := range prior {
				if !l.CreatedAt.Before(start) {
					roundBase++
				}
			}
		}
		totalEarned := 0
		boxCount := 0
		var lastBox *BoxResult
		for i := 0; i < count; i++ {
			earned := task.Points
			note := "完成任务：" + task.Title
			if i == 0 && doubled {
				earned = task.Points * 2
				note += "（双倍卡×2）"
			}
			if multi {
				note += "（第 " + strconv.Itoa(roundBase+i+1) + " 轮）"
			}
			lrow := model.Ledger{Type: "task", Amount: earned, RefID: task.ID, Note: note}
			if err := tx.Create(&lrow).Error; err != nil {
				return err
			}
			totalEarned += earned
			// box drop roll: the box goes to the backpack unopened — what it
			// contains is decided when the user opens it. Item rates roll via
			// the box's ItemDrops (cumulative hit, remainder = plain points
			// box), same rule as backpack opening. 批量完成时每轮独立掉落。
			if task.BoxID != nil && task.BoxDropRate > 0 && rand.Intn(100) < task.BoxDropRate {
				var box model.Box
				if err := tx.First(&box, *task.BoxID).Error; err == nil {
					roll := rand.Intn(100)
					itemType, qty := 0, 1
					acc := 0
					for _, d := range box.ItemDrops {
						if d.Rate <= 0 {
							continue
						}
						acc += d.Rate
						if roll < acc {
							itemType, qty = d.ItemType, d.Qty
							break
						}
					}
					kind, typeID, name := "box", box.ID, box.Name
					if itemType > 0 {
						kind, typeID, name = "item", uint(itemType), ItemName(uint(itemType))
					}
					n := 1
					if itemType > 0 {
						n = qty
					}
					for i := 0; i < n; i++ {
						if err := tx.Create(&model.BackpackItem{Kind: kind, TypeID: typeID, Source: lrow.ID}).Error; err != nil {
							return err
						}
					}
					boxCount++
					lastBox = &BoxResult{Name: name, Count: 1, Item: itemType > 0, Qty: qty}
				}
			}
		}
		out.Task, out.Doubled = task, doubled
		out.Earned = totalEarned
		if boxCount > 0 {
			lastBox.Count = boxCount
			out.Box = lastBox
		}
		out.Balance = PointBalance(tx)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UndoLedgerRow removes a same-day task-completion or box-drop ledger row.
// For task rows the task status is restored: once → doing; repeating task's
// LastDoneKey cleared when no completion remains in the period (multi-round
// rounds re-count from ledger automatically). Penalty/shop rows are not
// undoable (penalty would be re-charged by lazy settle; shop involves
// cooldown rollback). Returns the undone amount.
func UndoLedgerRow(db *gorm.DB, ledgerID uint, now time.Time) (int, error) {
	var row model.Ledger
	if err := db.First(&row, ledgerID).Error; err != nil {
		return 0, BizErr(404, "记录不存在")
	}
	if row.Type != "task" && row.Type != "box" {
		return 0, BizErr(400, "只有任务完成和宝箱开出的记录可以撤回")
	}
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if row.CreatedAt.Before(todayStart) {
		return 0, BizErr(400, "只能撤回今天获得的记录")
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if row.Type == "box" {
			// box rows are standalone: delete and done
			return tx.Delete(&row).Error
		}
		var task model.Task
		if err := tx.First(&task, row.RefID).Error; err != nil {
			return BizErr(404, "任务不存在或已删除")
		}
		if err := tx.Delete(&row).Error; err != nil {
			return err
		}
		// 撤回任务完成 = 整次作废：该次掉落进背包的未开宝箱/道具一并收回
		if row.Type == "task" {
			if err := tx.Where("source = ?", row.ID).Delete(&model.BackpackItem{}).Error; err != nil {
				return err
			}
		}
		// remaining completions of this task in the current period
		// (in-memory date filtering, same convention as the stats page)
		var rows []model.Ledger
		tx.Where("type = ? AND ref_id = ?", "task", task.ID).Find(&rows)
		start := todayStart
		if task.Repeat == "weekly" {
			start = StartOfWeek(now)
		}
		n := 0
		for _, l := range rows {
			if l.ID != row.ID && !l.CreatedAt.Before(start) {
				n++
			}
		}
		// restore the task status when the period has no completion left
		if task.Repeat == "once" {
			if task.Status == "done" && n == 0 {
				task.CompletedAt = nil
				if task.DueAt != nil && task.DueAt.After(now) {
					task.Status = "pending" // 截止日未到，回待办（到期当天会自动开始）
				} else {
					task.Status = "doing" // 截止日当天/已逾期，自动进行中
				}
				if err := tx.Save(&task).Error; err != nil {
					return err
				}
			}
		} else if task.LastDoneKey == periodKey(task.Repeat, now) && n == 0 {
			// single-round daily/weekly becomes claimable again; for
			// multi-round tasks this is a no-op safeguard
			if err := tx.Model(&task).Updates(map[string]any{"last_done_key": "", "completed_at": nil}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return row.Amount, nil
}
