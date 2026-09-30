package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// ---- Item registry: the single source of truth for the item pool ----
//
// 新增 / 修改 / 下线道具只动这里（注册行 + 使用函数）和 docs/05。
// 图鉴页、背包道具格、宝箱掉落配置全部由 GET /api/items/guide 驱动，
// confirm 型新道具前端零改动；只有全新交互模式才需要动前端。

// pending_effects.kind 常量：生效站点（OpenBoxes / CompleteTask /
// SettlePenalties）与注册表共用同一来源。
const (
	pendingLucky    = "lucky_box"      // 幸运符：下一个宝箱积分 ×2
	pendingBoost    = "boost_box"      // 运势卡：下一个宝箱区间 ×2
	pendingDouble   = "double_task"    // 双倍卡：下一个任务 ×2
	pendingExempt   = "exempt_penalty" // 免罚金牌：24h 罚分豁免
)

// ItemDef describes one item in the pool.
type ItemDef struct {
	ID          uint          `json:"id"`
	Name        string        `json:"name"`
	Icon        string        `json:"icon"`
	Desc        string        `json:"desc"`       // 图鉴效果描述
	UsePrompt   string        `json:"use_prompt"` // 使用确认弹窗文案（confirm 模式）
	UseMode     string        `json:"use_mode"`   // confirm | shop_cooldown
	PendingKind string        `json:"-"`          // 触发型道具挂 pending_effects 的 kind（空 = 即时结算）
	PendingTTL  time.Duration `json:"-"`          // 触发型的有效期（0 = 不过期，触发即消耗）
}

var itemDefs = []ItemDef{
	{
		ID: 1, Name: "积分利息卡", Icon: "🪙", UseMode: "confirm",
		Desc:      "使用后立刻获得「当前积分 × 3%」的积分（向下取整）。积分为负时利息按 0 计。",
		UsePrompt: "使用后立刻获得当前积分的 3%，一次性结算。",
	},
	{
		ID: 2, Name: "余额利息卡", Icon: "💴", UseMode: "confirm",
		Desc:      "使用后立刻获得「当前现金余额 × 3%」的现金（取整到分）。",
		UsePrompt: "使用后立刻获得当前余额的 3%，一次性结算。",
	},
	{
		ID: 3, Name: "冷却重置卡", Icon: "🔑", UseMode: "shop_cooldown",
		Desc: "选择一个冷却中的商城商品，清零其冷却时间，立即可再次兑换。",
	},
	{
		ID: 4, Name: "免罚金牌", Icon: "🛡️", UseMode: "confirm",
		PendingKind: pendingExempt, PendingTTL: 24 * time.Hour,
		Desc:      "挂上后 24 小时内，打开 App 时触发的所有补罚全部免除（昨日每日任务、上周每周任务、逾期任务）。已自动结算的罚分不返还，建议睡前或预感要崩的当天挂上。",
		UsePrompt: "挂上后 24 小时内，打开 App 时触发的所有补罚全免（昨日每日任务、上周每周任务、逾期任务）。\n\n已自动结算的罚分不返还，建议睡前或预感要崩的当天挂上。",
	},
	{
		ID: 5, Name: "幸运符", Icon: "🍀", UseMode: "confirm",
		PendingKind: pendingLucky,
		Desc:      "激活后，下一个开启的宝箱积分 ×2。批量开启时只有第一个宝箱吃到；开出道具时不消耗，继续等待。",
		UsePrompt: "激活后，下一个开启的宝箱积分 ×2。\n\n批量开启时只有第一个宝箱吃到；开出道具时不消耗，继续等待。",
	},
	{
		ID: 6, Name: "双倍卡", Icon: "⚡", UseMode: "confirm",
		PendingKind: pendingDouble,
		Desc:      "激活后，下一个完成的任务积分 ×2。多张可排队：连续完成多个任务时逐张消耗。",
		UsePrompt: "激活后，下一个完成的任务积分 ×2。\n\n多张可排队：连续完成多个任务时逐张消耗。",
	},
	{
		ID: 7, Name: "运势卡", Icon: "🔮", UseMode: "confirm",
		PendingKind: pendingBoost,
		Desc:      "激活后，下一个开启的宝箱上限、下限都 ×2（如 10~100 → 20~200）。可与幸运符叠加：区间 ×2 后再整体翻倍。",
		UsePrompt: "激活后，下一个开启的宝箱上限、下限都 ×2（如 10~100 → 20~200）。\n\n可与幸运符叠加：区间×2 后再整体翻倍。",
	},
}

var itemDefByID = func() map[uint]ItemDef {
	m := make(map[uint]ItemDef, len(itemDefs))
	for _, d := range itemDefs {
		m[d.ID] = d
	}
	return m
}()

func itemDef(id uint) (ItemDef, bool) {
	d, ok := itemDefByID[id]
	return d, ok
}

// itemName remains the lookup used across drop/open code paths.
func itemName(t uint) string {
	return itemDefByID[t].Name
}

// ItemsGuideHandler serves GET /api/items/guide: one payload driving the
// guide page, the backpack cells and the box-drop configuration picker.
func ItemsGuideHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"items": itemDefs})
	}
}

// ---- Use handlers: one per item, dispatched from UseItem ----

// itemUse runs inside the UseItem transaction. Writing the response early
// (c.JSON + sentinel error) aborts and rolls back, keeping the backpack row.
type itemUse func(tx *gorm.DB, c *gin.Context, row model.BackpackItem, targetID uint) (gin.H, error)

// armPending is the shared implementation for trigger-type items: insert a
// pending_effects row (optionally expiring) plus a 0-point audit ledger row.
// armedMsg is the toast shown after activation.
func armPending(def ItemDef, armedMsg string, extra func(tx *gorm.DB) gin.H) itemUse {
	return func(tx *gorm.DB, c *gin.Context, row model.BackpackItem, _ uint) (gin.H, error) {
		eff := model.PendingEffect{Kind: def.PendingKind}
		if def.PendingTTL > 0 {
			exp := time.Now().Add(def.PendingTTL)
			eff.ExpiresAt = &exp
		}
		if err := tx.Create(&eff).Error; err != nil {
			return nil, err
		}
		if err := tx.Create(&model.Ledger{Type: "item", Amount: 0, RefID: row.ID, Note: "道具「" + def.Name + "」激活"}).Error; err != nil {
			return nil, err
		}
		out := gin.H{"message": armedMsg}
		if extra != nil {
			for k, v := range extra(tx) {
				out[k] = v
			}
		}
		return out, nil
	}
}

var itemUses = map[uint]itemUse{
	1: useInterestPoints,
	2: useInterestCash,
	3: useResetCooldown,
	4: armPending(itemDefByID[4], "金牌已挂上：24 小时内触发的补罚全免", func(tx *gorm.DB) gin.H {
		now := time.Now()
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		var settledToday int64
		tx.Model(&model.Ledger{}).Where("type = ? AND amount < 0 AND created_at >= ?", "penalty", todayStart).Count(&settledToday)
		return gin.H{"settled_today": settledToday}
	}),
	5: armPending(itemDefByID[5], "幸运符已激活：下一个开启的宝箱积分 ×2", nil),
	6: armPending(itemDefByID[6], "双倍卡已激活：下一个完成的任务积分 ×2", nil),
	7: armPending(itemDefByID[7], "运势卡已激活：下一个宝箱区间 ×2（如 10~100 → 20~200）", nil),
}
