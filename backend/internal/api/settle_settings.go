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
	Order            []string             `json:"order"`
	PerfectDayAmount int                  `json:"perfect_day_amount"`
	CooldownTiers    []service.CooldownTier `json:"cooldown_tiers"`
}

func GetSettleSettings(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, settleSettingsOut{
			Order:            service.SettlementOrder(db),
			PerfectDayAmount: service.PerfectDayAmount(db),
			CooldownTiers:    service.CooldownRewardTiers(db),
		})
	}
}

func UpdateSettleSettings(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Order            []string        `json:"order"`
			PerfectDayAmount int             `json:"perfect_day_amount"`
			CooldownTiers    []service.CooldownTier `json:"cooldown_tiers"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		// 校验 order：恰好包含三个合法 key
		valid := map[string]bool{service.SettleKeyPerfectDay: true, service.SettleKeyCooldownReward: true, service.SettleKeyFarmBoost: true}
		seen := map[string]bool{}
		for _, k := range body.Order {
			if !valid[k] || seen[k] {
				c.JSON(http.StatusBadRequest, gin.H{"error": "结算顺序不合法"})
				return
			}
			seen[k] = true
		}
		if len(body.Order) != 3 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "结算顺序不完整"})
			return
		}
		if body.PerfectDayAmount < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "全勤奖金额不能为负"})
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