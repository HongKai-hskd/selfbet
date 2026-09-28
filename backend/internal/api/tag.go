package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// ---- Tag (task group) management ----

func ListTags(db *gorm.DB) gin.HandlerFunc {
	type tagWithCount struct {
		ID        uint   `json:"id"`
		Name      string `json:"name"`
		Color     string `json:"color"`
		TaskCount int64  `json:"task_count"`
		ShopCount int64  `json:"shop_count"`
	}
	return func(c *gin.Context) {
		list := []tagWithCount{}
		if err := db.Table("tags").
			Select("tags.id, tags.name, tags.color, "+
				"(SELECT COUNT(*) FROM tasks WHERE tasks.tag_id = tags.id) AS task_count, "+
				"(SELECT COUNT(*) FROM shop_items WHERE shop_items.tag_id = tags.id) AS shop_count").
			Group("tags.id").
			Order("tags.sort_order ASC, tags.created_at ASC").
			Scan(&list).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": list})
	}
}

type tagBody struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// normalizeColor lowercases and validates a hex color; empty falls back to
// the default blue.
func normalizeColor(s string) (string, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return "#1989fa", true
	}
	if len(s) != 4 && len(s) != 7 {
		return "", false
	}
	if s[0] != '#' {
		return "", false
	}
	for _, r := range s[1:] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return "", false
		}
	}
	return s, true
}

func CreateTag(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body tagBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		if body.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "名称不能为空"})
			return
		}
		var count int64
		db.Model(&model.Tag{}).Where("name = ?", body.Name).Count(&count)
		if count > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "同名分组已存在"})
			return
		}
		color, ok := normalizeColor(body.Color)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "颜色格式无效（#RRGGBB）"})
			return
		}
		tag := model.Tag{Name: body.Name, Color: color}
		if err := db.Create(&tag).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, tag)
	}
}

func UpdateTag(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var tag model.Tag
		if err := db.First(&tag, c.Param("id")).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "分组不存在"})
			return
		}
		var body tagBody
		if err := c.ShouldBindJSON(&body); err != nil || body.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "名称不能为空"})
			return
		}
		var count int64
		db.Model(&model.Tag{}).Where("name = ? AND id != ?", body.Name, tag.ID).Count(&count)
		if count > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "同名分组已存在"})
			return
		}
		// tasks reference the tag by id, so renaming propagates automatically;
		// empty color keeps the current one
		tag.Name = body.Name
		if body.Color != "" {
			color, ok := normalizeColor(body.Color)
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{"error": "颜色格式无效（#RRGGBB）"})
				return
			}
			tag.Color = color
		}
		if err := db.Save(&tag).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, tag)
	}
}

// ReorderTags persists manual display order: ids in request order get
// sort_order = 1..n.
func ReorderTags(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			IDs []uint `json:"ids"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || len(body.IDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			for i, id := range body.IDs {
				if err := tx.Model(&model.Tag{}).Where("id = ?", id).Update("sort_order", i+1).Error; err != nil {
					return err
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

func DeleteTag(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		err := db.Transaction(func(tx *gorm.DB) error {
			// detach tasks first; they fall back to "未分组"
			if err := tx.Model(&model.Task{}).Where("tag_id = ?", id).Update("tag_id", nil).Error; err != nil {
				return err
			}
			return tx.Delete(&model.Tag{}, id).Error
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}
