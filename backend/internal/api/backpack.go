package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
	"selfbet/backend/internal/service"
)

// ---- Backpack: unopened boxes and items ----
//
// 道具池定义/图标/交互元数据在 service/itemdef.go 的注册表里；
// 开箱与使用的事务在 service/backpack.go，本文件只有聚合查询与薄 handler。

type backpackCell struct {
	TypeID    uint   `json:"type_id"`
	Name      string `json:"name"`
	Icon      string `json:"icon,omitempty"`
	Count     int64  `json:"count"`
	Min       int    `json:"min_points"`
	Max       int    `json:"max_points"`
	UseMode   string `json:"use_mode,omitempty"`
	UsePrompt string `json:"use_prompt,omitempty"`
}

// ListBackpack returns the unopened backpack aggregated by kind + type.
func ListBackpack(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		boxes := []backpackCell{}
		if err := db.Model(&model.BackpackItem{}).
			Select("type_id, COUNT(*) AS count").
			Where("kind = ?", "box").
			Group("type_id").Order("type_id ASC").Scan(&boxes).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for i := range boxes {
			var box model.Box
			if err := db.First(&box, boxes[i].TypeID).Error; err == nil {
				boxes[i].Name = box.Name
				boxes[i].Min, boxes[i].Max = box.MinPoints, box.MaxPoints
			} else {
				boxes[i].Name = "未知宝箱"
			}
		}
		items := []backpackCell{}
		if err := db.Model(&model.BackpackItem{}).
			Select("type_id, COUNT(*) AS count").
			Where("kind = ?", "item").
			Group("type_id").Order("type_id ASC").Scan(&items).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for i := range items {
			if d, ok := service.LookupItem(items[i].TypeID); ok {
				items[i].Name = d.Name
				items[i].Icon = d.Icon
				items[i].UseMode = d.UseMode
				items[i].UsePrompt = d.UsePrompt
			} else {
				items[i].Name = "未知道具"
			}
		}
		c.JSON(http.StatusOK, gin.H{"boxes": boxes, "items": items})
	}
}

// OpenBoxes delegates to service.OpenBoxes (batch opening, luck boosters and
// item drop rolls happen in one service transaction).
func OpenBoxes(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			TypeID uint `json:"type_id"`
			Count  int  `json:"count"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || body.Count <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		results, total, err := service.OpenBoxes(db, body.TypeID, body.Count)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"results": results, "total": total})
	}
}

// UseItem delegates to service.UseItem (per-item handlers registered in
// service/itemdef.go; consumption and effect happen in one transaction).
func UseItem(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			TypeID   uint `json:"type_id"`
			TargetID uint `json:"target_id"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		out, err := service.UseItem(db, body.TypeID, body.TargetID)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	}
}
