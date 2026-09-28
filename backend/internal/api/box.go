package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// ---- Box types (fully user-defined) ----

func ListBoxes(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		boxes := []model.Box{}
		if err := db.Order("created_at DESC").Find(&boxes).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": boxes})
	}
}

type boxBody struct {
	Name      string `json:"name"`
	MinPoints int    `json:"min_points"`
	MaxPoints int    `json:"max_points"`
}

func (b *boxBody) validate() string {
	if b.Name == "" {
		return "宝箱名称不能为空"
	}
	if b.MinPoints < 0 || b.MaxPoints < b.MinPoints {
		return "积分范围无效（最小值 ≥ 0 且最大值 ≥ 最小值）"
	}
	return ""
}

func CreateBox(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body boxBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		if msg := body.validate(); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		box := model.Box{Name: body.Name, MinPoints: body.MinPoints, MaxPoints: body.MaxPoints}
		if err := db.Create(&box).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, box)
	}
}

func UpdateBox(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var box model.Box
		if err := db.First(&box, c.Param("id")).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "宝箱不存在"})
			return
		}
		var body boxBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		if msg := body.validate(); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		box.Name, box.MinPoints, box.MaxPoints = body.Name, body.MinPoints, body.MaxPoints
		if err := db.Save(&box).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, box)
	}
}

func DeleteBox(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		err := db.Transaction(func(tx *gorm.DB) error {
			// detach from tasks first so tasks stay valid
			if err := tx.Model(&model.Task{}).Where("box_id = ?", id).Updates(map[string]any{"box_id": nil, "box_drop_rate": 0}).Error; err != nil {
				return err
			}
			return tx.Delete(&model.Box{}, id).Error
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
