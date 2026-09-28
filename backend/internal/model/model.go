package model

import (
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// Tag is a manageable group/label for tasks (replaces free-text group_name).
type Tag struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;not null" json:"name"`
	SortOrder int       `gorm:"index" json:"sort_order"` // manual display order (drag & drop)
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
	Status           string     `gorm:"index;default:pending" json:"status"` // API layer overwrites with effective status for repeating tasks
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
type Box struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"not null" json:"name"`
	MinPoints int       `json:"min_points"`
	MaxPoints int       `json:"max_points"`
	CreatedAt time.Time `json:"created_at"`
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
// Type: task | box | shop. RefID points to task/box/shop item respectively.
type Ledger struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Type      string    `gorm:"index" json:"type"`
	Amount    int       `json:"amount"`
	RefID     uint      `json:"ref_id"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

// Open opens (and migrates) the SQLite database stored in dataDir.
func Open(dataDir string) (*gorm.DB, error) {
	dsn := filepath.Join(dataDir, "selfbet.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&Task{}, &Box{}, &ShopItem{}, &Ledger{}, &Tag{}, &CashFlow{}); err != nil {
		return nil, err
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
