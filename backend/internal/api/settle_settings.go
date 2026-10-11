package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
	"selfbet/backend/internal/service"
)

// ---- 结算设置：结算顺序（拖拽）+ 全勤奖金额 + 无冷却梯度 ----

type settleSettingsOut struct {
	Order                 []string             `json:"order"`
	PerfectDayAmount      int                  `json:"perfect_day_amount"`
	CooldownTiers         []service.CooldownTier `json:"cooldown_tiers"`
	AdvanceLimit          int                  `json:"advance_limit"`           // 可预支金额（正数，余额最低 -advance_limit）
	OverdraftDailyRate    int                  `json:"overdraft_daily_rate"`    // 透支日利率（%）
	CooldownPremiumPercent int                 `json:"cooldown_premium_percent"` // 冷却期购买溢价（%）
}

func GetSettleSettings(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, settleSettingsOut{
			Order:                  service.SettlementOrder(db),
			PerfectDayAmount:       service.PerfectDayAmount(db),
			CooldownTiers:          service.CooldownRewardTiers(db),
			AdvanceLimit:           -service.PointFloorValue(db),
			OverdraftDailyRate:     service.OverdraftDailyRate(db),
			CooldownPremiumPercent: service.CooldownPremiumPercent(db),
		})
	}
}

func UpdateSettleSettings(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Order                  []string               `json:"order"`
			PerfectDayAmount       int                    `json:"perfect_day_amount"`
			CooldownTiers          []service.CooldownTier `json:"cooldown_tiers"`
			AdvanceLimit           int                    `json:"advance_limit"`
			OverdraftDailyRate     int                    `json:"overdraft_daily_rate"`
			CooldownPremiumPercent int                    `json:"cooldown_premium_percent"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		// 校验 order：恰好包含四个合法 key
		valid := map[string]bool{service.SettleKeyOverdraftInterest: true, service.SettleKeyPerfectDay: true, service.SettleKeyCooldownReward: true, service.SettleKeyFarmBoost: true}
		seen := map[string]bool{}
		for _, k := range body.Order {
			if !valid[k] || seen[k] {
				c.JSON(http.StatusBadRequest, gin.H{"error": "结算顺序不合法"})
				return
			}
			seen[k] = true
		}
		if len(body.Order) != 4 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "结算顺序不完整"})
			return
		}
		if body.PerfectDayAmount < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "全勤奖金额不能为负"})
			return
		}
		if body.AdvanceLimit < 0 || body.AdvanceLimit > 1000000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "可预支金额不合法"})
			return
		}
		if body.OverdraftDailyRate < 0 || body.OverdraftDailyRate > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "透支日利率需在 0~100 之间"})
			return
		}
		if body.CooldownPremiumPercent < 100 || body.CooldownPremiumPercent > 1000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "冷却溢价需在 100~1000 之间"})
			return
		}
		for _, t := range body.CooldownTiers {
			if t.Amount < 0 || t.Days < 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "梯度配置不能为负"})
				return
			}
		}
		orderJSON, _ := json.Marshal(body.Order)
		tierJSON, _ := json.Marshal(body.CooldownTiers)
		err := db.Transaction(func(tx *gorm.DB) error {
			for _, pair := range [][2]string{
				{"settle_order", string(orderJSON)},
				{"perfect_day_amount", strconv.Itoa(body.PerfectDayAmount)},
				{"cooldown_reward_tiers", string(tierJSON)},
				{"advance_limit", strconv.Itoa(body.AdvanceLimit)},
				{"overdraft_daily_rate", strconv.Itoa(body.OverdraftDailyRate)},
				{"cooldown_premium_percent", strconv.Itoa(body.CooldownPremiumPercent)},
			} {
				var s model.Settings
				if err := tx.Where("setting_key = ?", pair[0]).First(&s).Error; err == nil {
					if err := tx.Model(&model.Settings{}).Where("setting_key = ?", pair[0]).Update("value", pair[1]).Error; err != nil {
						return err
					}
				} else {
					if err := tx.Create(&model.Settings{SettingKey: pair[0], Value: pair[1]}).Error; err != nil {
						return err
					}
				}
			}
			return nil
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}