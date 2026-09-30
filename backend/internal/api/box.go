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
	Name      string            `json:"name"`
	MinPoints int               `json:"min_points"`
	MaxPoints int               `json:"max_points"`
	ItemDrops []model.ItemDrop `json:"item_drops"` // 动态道具掉落配置；剩余概率=开积分
}

func (b *boxBody) validate() string {
	if b.Name == "" {
		return "宝箱名称不能为空"
	}
	if b.MinPoints < 0 || b.MaxPoints < b.MinPoints {
		return "积分范围无效（最小值 ≥ 0 且最大值 ≥ 最小值）"
	}
	sum := 0
	seen := map[int]bool{}
	for _, d := range b.ItemDrops {
		// 道具池校验走注册表（itemdef.go 单一来源），新增道具自动可用
		if _, ok := itemDef(uint(d.ItemType)); !ok {
			return "道具类型无效"
		}
		if seen[d.ItemType] {
			return "同一道具不能配置多行"
		}
		seen[d.ItemType] = true
		if d.Rate < 0 || d.Rate > 100 {
			return "道具概率需在 0-100 之间"
		}
		if d.Qty < 1 || d.Qty > 99 {
			return "道具数量需在 1-99 之间"
		}
		sum += d.Rate
	}
	if sum > 100 {
		return "道具概率之和不能超过 100（剩余概率开积分）"
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
		box := model.Box{
			Name: body.Name, MinPoints: body.MinPoints, MaxPoints: body.MaxPoints,
			ItemDrops:        body.ItemDrops,
			ItemRatePoints:   0, ItemRateCash: 0, ItemRateReset: 0, // 旧列停用
		}
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
		box.ItemDrops = body.ItemDrops
		box.ItemRatePoints, box.ItemRateCash, box.ItemRateReset = 0, 0, 0 // 旧列停用
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
			// 背包里该类型的未开宝箱一并清除（类型没了开不出来）
			if err := tx.Where("kind = ? AND type_id = ?", "box", id).Delete(&model.BackpackItem{}).Error; err != nil {
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
