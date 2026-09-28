package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// Stats provides the dashboard aggregates for the stats page.
// All aggregation happens in memory over the (small) ledger table, keyed by
// local dates — no SQLite date-function dialects involved.
type period struct {
	Earned int `json:"earned"`
	Spent  int `json:"spent"`
}

type heatDay struct {
	Date   string `json:"date"` // 2006-01-02
	Weekday int   `json:"weekday"` // 0=Mon .. 6=Sun
	Weeks  int    `json:"weeks"`   // column index from start
	Earned int    `json:"earned"`
}

func dayKey(t time.Time) string {
	return t.Format("2006-01-02")
}

func startOfWeek(t time.Time) time.Time {
	// Monday-based week start
	offset := (int(t.Weekday()) + 6) % 7
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return d.AddDate(0, 0, -offset)
}

func GetStats(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		SettlePenalties(db, time.Now())
		var rows []model.Ledger
		if err := db.Select("amount", "created_at").Find(&rows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		now := time.Now()
		todayKey, yKey := dayKey(now), dayKey(now.AddDate(0, 0, -1))
		thisWeekStart := startOfWeek(now)
		lastWeekStart := thisWeekStart.AddDate(0, 0, -7)
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		lastMonthStart := monthStart.AddDate(0, -1, 0)

		var (
			today, yesterday, thisWeek, lastWeek, thisMonth, lastMonth period
			heat                                                        = map[string]int{}
			yesterdayItems                                              []model.Ledger
		)
		for _, r := range rows {
			k := dayKey(r.CreatedAt)
			heat[k] += r.Amount // 净值口径：含罚分等负数行，与四格卡/明细一致
			switch {
			case k == todayKey:
				applyPeriod(&today, r.Amount)
			case k == yKey:
				applyPeriod(&yesterday, r.Amount)
				if r.Amount > 0 {
					yesterdayItems = append(yesterdayItems, r)
				}
			}
			if !r.CreatedAt.Before(thisWeekStart) {
				applyPeriod(&thisWeek, r.Amount)
			} else if !r.CreatedAt.Before(lastWeekStart) {
				applyPeriod(&lastWeek, r.Amount)
			}
			if !r.CreatedAt.Before(monthStart) {
				applyPeriod(&thisMonth, r.Amount)
			} else if !r.CreatedAt.Before(lastMonthStart) {
				applyPeriod(&lastMonth, r.Amount)
			}
		}

		// heatmap: weeks param (26 default, up to 53 ≈ one year), columns aligned to Monday
		weeks := 26
		if v := atoi(c.Query("weeks")); v >= 4 && v <= 53 {
			weeks = v
		}
		start := startOfWeek(now).AddDate(0, 0, -(weeks-1)*7)
		heatList := []heatDay{}
		for d := start; !d.After(now); d = d.AddDate(0, 0, 1) {
			k := dayKey(d)
			wd := (int(d.Weekday()) + 6) % 7
			weekIdx := int(d.Sub(start).Hours()) / 24 / 7
			heatList = append(heatList, heatDay{Date: k, Weekday: wd, Weeks: weekIdx, Earned: heat[k]})
		}
		sort.Slice(yesterdayItems, func(i, j int) bool {
			return yesterdayItems[i].CreatedAt.After(yesterdayItems[j].CreatedAt)
		})

		c.JSON(http.StatusOK, gin.H{
			"today": today, "yesterday": yesterday,
			"this_week": thisWeek, "last_week": lastWeek,
			"this_month": thisMonth, "last_month": lastMonth,
			"yesterday_items": yesterdayItems,
			"heatmap":         heatList,
		})
	}
}

func applyPeriod(p *period, amount int) {
	if amount > 0 {
		p.Earned += amount
	} else {
		p.Spent += -amount
	}
}
