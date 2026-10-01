package model

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// PointsFloor is the lowest allowed point balance: penalties can push the
// balance negative (欠账) but never below this floor. Any deduction that
// would cross it is clamped.
const PointsFloor = -500

// Tag is a manageable group/label for tasks (replaces free-text group_name).
type Tag struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;size:191;not null" json:"name"`
	Color     string    `gorm:"default:'#1989fa'" json:"color"` // hex color for chips (e.g. #07c160)
	SortOrder int       `gorm:"index" json:"sort_order"`        // manual display order (drag & drop)
	CreatedAt time.Time `json:"created_at"`
}

// Task is a user-defined mission. Repeat: once | daily | weekly.
// For repeating tasks, LastDoneKey stores the period key (date / ISO week)
// of the last completion, so the task auto-resets each period without cron.
type Task struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	Title            string     `gorm:"not null" json:"title"`
	Points           int        `gorm:"not null" json:"points"`
	EstimatedMinutes int        `json:"estimated_minutes"`
	GroupName        string     `json:"-"` // legacy free-text group, migrated to TagID
	TagID            *uint      `gorm:"index" json:"tag_id"`
	TagName          string     `gorm:"-" json:"tag_name"` // filled by API for display
	Repeat           string     `gorm:"default:once" json:"repeat"` // once | daily | weekly
	MultiRound       bool       `gorm:"default:false" json:"multi_round"` // repeating task claimable multiple times per period
	RoundsToday      int        `gorm:"-" json:"rounds_today"`            // display only: rounds done in current period (from ledger)
	BoxID            *uint      `json:"box_id"`
	BoxDropRate      int        `json:"box_drop_rate"`
	DueAt            *time.Time `json:"due_at"` // optional deadline
	// 日程计划时间（纯展示、零结算语义；都填齐才出现在日程表）
	ExpectedStartAt   *time.Time `json:"expected_start_at"` // once：预期开始
	ExpectedEndAt     *time.Time `json:"expected_end_at"`   // once：预期结束（需晚于开始）
	ExpectedStartTime string     `json:"expected_start_time"` // daily/weekly：'HH:MM'
	ExpectedEndTime   string     `json:"expected_end_time"`   // daily/weekly：'HH:MM'
	ExpectedWeekday   int        `json:"expected_weekday"`    // weekly：1=周一..7=周日，0=未设
	Status           string     `gorm:"index;size:32;default:pending" json:"status"` // API layer overwrites with effective status for repeating tasks
	CompletedAt      *time.Time `json:"completed_at"`
	LastDoneKey      string     `json:"-"`
	SortOrder        int        `gorm:"index" json:"sort_order"` // manual display order (drag & drop)
	Penalty          int        `json:"penalty"`                 // charge when not completed in period (0 = none)
	LastPenaltyKey   string     `json:"-"`                       // period key already charged
	PenaltySettled   bool       `json:"-"`                       // one-shot deadline penalty charged
	CreatedAt        time.Time  `json:"created_at"`
}

// Box is a user-defined box type. Opening it grants a random amount of
// points between MinPoints and MaxPoints (inclusive).
// ItemDrop 是宝箱的道具掉落配置：rate% 概率开到该道具，开到时一次进背包 qty 张。
type ItemDrop struct {
	ItemType int `json:"item_type"` // 1=积分利息卡 2=余额利息卡 3=冷却重置卡
	Rate     int `json:"rate"`      // 0-100
	Qty      int `json:"qty"`       // 1-99
}

type Box struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	Name      string `gorm:"not null" json:"name"`
	MinPoints int    `json:"min_points"`
	MaxPoints int    `json:"max_points"`
	ItemDrops []ItemDrop `json:"item_drops" gorm:"serializer:json"` // 动态道具掉落配置；剩余概率=开积分
	// 已废弃：旧的三列固定道具概率，由 ItemDrops 取代（保留列兼容旧库，不再读写业务值）
	ItemRatePoints int       `json:"item_rate_points"` // % 积分利息卡
	ItemRateCash   int       `json:"item_rate_cash"`   // % 余额利息卡
	ItemRateReset  int       `json:"item_rate_reset"`  // % 冷却重置卡
	CreatedAt      time.Time `json:"created_at"`
}

// BackpackItem is one unopened drop sitting in the backpack (box or item).
// Opening/using it deletes the row and writes the corresponding ledger /
// cash rows. Source links a box back to the task-completion ledger row that
// dropped it, so undoing that completion recalls the unopened box.
type BackpackItem struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Kind      string    `gorm:"index;size:16" json:"kind"` // box | item
	TypeID    uint      `gorm:"index" json:"type_id"`
	Source    uint      `gorm:"index" json:"source"`
	CreatedAt time.Time `json:"created_at"`
}

// PendingEffect is an armed one-shot item effect waiting for its trigger.
// Kind: double_task (next task completion ×2) | lucky_box (next box
// opening ×2) | boost_box (next box min/max ×2) | exempt_penalty (skip
// penalty settlement while active). ExpiresAt gates exempt_penalty (24h
// window); the other kinds have no expiry and are consumed on trigger.
type PendingEffect struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	Kind      string     `gorm:"index;size:32" json:"kind"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// ShopItem is a redeemable reward defined by the user. SortOrder is the
// manual display order set by drag-and-drop (0 = newest on top).
// CooldownDays > 0 blocks re-redemption for that many days after a purchase.
type ShopItem struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	Name           string     `gorm:"not null" json:"name"`
	Price          int        `gorm:"not null" json:"price"`
	Description    string     `json:"description"`
	RedeemedCount  int        `json:"redeemed_count"`
	TagID          *uint      `gorm:"index" json:"tag_id"`
	TagName        string     `gorm:"-" json:"tag_name"` // filled by API for display
	SortOrder      int        `gorm:"index" json:"sort_order"`
	CooldownDays   int        `json:"cooldown_days"`
	LastRedeemedAt *time.Time `json:"last_redeemed_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

// CashFlow is the real-money wallet ledger. AmountCents > 0 = exchanged in
// from points, < 0 = manually recorded spending. 100 cents = ¥1.
type CashFlow struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	AmountCents int       `json:"amount_cents"`
	Note        string    `json:"note"`
	CreatedAt   time.Time `json:"created_at"`
}

const PointsPerYuan = 10 // 10 积分 = 1 元（2026-09-28 用户拍板，原 5）

// Ledger records every point movement. Amount > 0 = earned, < 0 = spent.
// Type: task | box | shop | farm. RefID points to task/box/shop item respectively.
type Ledger struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Type      string    `gorm:"index;size:32" json:"type"`
	Amount    int       `json:"amount"`
	RefID     uint      `json:"ref_id"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

// FarmState 是农场小游戏的全局状态（单行，ID=1）。docs/07 v10 决议：
// 一切消费（买地/升级）直接扣主积分；Coins（农场积分）只进不出——
// 唯一来源是收获，唯一出口是 100:1 整数提现，纯小数缓冲零钱包。
type FarmState struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Coins        float64   `json:"coins"`         // 农场积分余额（允许小数，展示 1 位）
	LevelA       int       `json:"level_a"`       // 产量A：收获 +2%/级，无副作用
	LevelB       int       `json:"level_b"`       // 产量B：收获 +6%/级，周期 +8%/级
	LevelC       int       `json:"level_c"`       // 周期C：周期 −5%/级
	TotalHarvest int       `json:"total_harvest"` // 累计收获轮次（里程碑展示）
	UpdatedAt    time.Time `json:"updated_at"`
}

// FarmPlot 是一块田（4×6 = 24 块，plot_index 0..23）。种植免费；
// 每田每天最多收获 2 轮，按本地日期惰性重置；成熟永不枯死、忘收不惩罚。
type FarmPlot struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	PlotIndex  int        `gorm:"uniqueIndex" json:"plot_index"` // 0..23，顺序解锁
	Unlocked   bool       `json:"unlocked"`
	PlantedAt  *time.Time `json:"planted_at"` // nil = 空田
	DailyCount int        `json:"daily_count"` // 当日已收获轮次（0-2）
	DailyDate  string     `json:"daily_date"`  // 轮次计数归属的本地日期 YYYY-MM-DD
}

// Settings 是键值配置表：登录密码等运行时设置存这里（替代旧 config.json 的密码字段）。
type Settings struct {
	SettingKey string `gorm:"column:setting_key;primaryKey;size:64" json:"key"`
	Value      string `json:"value"`
}

// AuthPasswordKey 是登录密码在 settings 表里的键。
const AuthPasswordKey = "auth_password"

// DefaultPassword 是 settings 表无记录时的兜底密码（首次启动 seed 用）。
const DefaultPassword = "kaytodo"

// Open opens (and migrates) the database: driver = "mysql"（用 dsn）或 "sqlite"（用 dataDir/selfbet.db）。
func Open(driver, dsn, dataDir string) (*gorm.DB, error) {
	var db *gorm.DB
	var err error
	switch driver {
	case "mysql":
		if dsn == "" {
			return nil, fmt.Errorf("db_driver=mysql 但 db_dsn 为空")
		}
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	default: // sqlite
		_ = os.MkdirAll(dataDir, 0o755) // config.Load 不再创建 data 目录，sqlite 模式自行确保
		db, err = gorm.Open(sqlite.Open(filepath.Join(dataDir, "selfbet.db")), &gorm.Config{})
	}
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Task{}, &Box{}, &ShopItem{}, &Ledger{}, &Tag{}, &CashFlow{}, &BackpackItem{}, &PendingEffect{}, &FarmState{}, &FarmPlot{}, &Settings{}); err != nil {
		return nil, err
	}
	// settings 种子：登录密码（已存在则不动）
	var sCount int64
	db.Model(&Settings{}).Where("setting_key = ?", AuthPasswordKey).Count(&sCount)
	if sCount == 0 {
		db.Create(&Settings{SettingKey: AuthPasswordKey, Value: DefaultPassword})
	}
	// 连接池上限：防止连接数膨胀吃满 max_connections（远程共享库时尤其重要）
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(4)
		sqlDB.SetMaxIdleConns(2)
		sqlDB.SetConnMaxLifetime(5 * time.Minute)
	}
	// 农场初始化（幂等）：单行状态 + 24 块田（仅第 1 块解锁）
	var fsCount int64
	db.Model(&FarmState{}).Count(&fsCount)
	if fsCount == 0 {
		db.Create(&FarmState{ID: 1})
	}
	var fpCount int64
	db.Model(&FarmPlot{}).Count(&fpCount)
	if fpCount < 24 {
		for i := 0; i < 24; i++ {
			var p FarmPlot
			if err := db.Where("plot_index = ?", i).First(&p).Error; err != nil {
				db.Create(&FarmPlot{PlotIndex: i, Unlocked: i == 0})
			}
		}
	}
	// 一次性迁移：旧三列固定道具概率 → item_drops JSON（0% 不迁）
	var legacyBoxes []Box
	if err := db.Where("item_drops IS NULL OR item_drops = ''").Find(&legacyBoxes).Error; err == nil {
		for _, b := range legacyBoxes {
			drops := []ItemDrop{}
			for _, d := range []struct {
				t int
				r int
			}{{1, b.ItemRatePoints}, {2, b.ItemRateCash}, {3, b.ItemRateReset}} {
				if d.r > 0 {
					drops = append(drops, ItemDrop{ItemType: d.t, Rate: d.r, Qty: 1})
				}
			}
			b.ItemDrops = drops
			db.Save(&b)
		}
	}
	// one-time lazy migration: legacy free-text group_name → Tag rows
	var legacy []Task
	if err := db.Where("group_name != '' AND tag_id IS NULL").Find(&legacy).Error; err == nil {
		for _, t := range legacy {
			var tag Tag
			if err := db.Where("name = ?", t.GroupName).First(&tag).Error; err != nil {
				tag = Tag{Name: t.GroupName}
				if err := db.Create(&tag).Error; err != nil {
					continue
				}
			}
			db.Model(&Task{}).Where("id = ?", t.ID).Update("tag_id", tag.ID)
		}
	}
	return db, nil
}
