package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/service"
)

// ---- 农场 ----
//
// 公式、状态与全部事务在 service/farm.go；本文件只有参数绑定与响应。

func GetFarm(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		out, err := service.GetFarmView(db)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

func PlantFarm(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			PlotIndex *int `json:"plot_index"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		planted, err := service.PlantFarm(db, body.PlotIndex)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true, "planted": planted})
	}
}

func HarvestFarm(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			All       bool `json:"all"`
			PlotIndex *int `json:"plot_index"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		res, err := service.HarvestFarm(db, body.All, body.PlotIndex)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, res)
	}
}

func BuyFarmPlot(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		out, err := service.BuyFarmPlot(db)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

func UpgradeFarm(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Line string `json:"line"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		out, err := service.UpgradeFarm(db, body.Line)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

func WithdrawFarm(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		out, err := service.WithdrawFarm(db)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	}
}
