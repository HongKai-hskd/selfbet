package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
	"selfbet/backend/internal/service"
)

// ---- Shop items & redemption ----

func ListShop(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		items := []model.ShopItem{}
		if err := db.Order("sort_order ASC, id DESC").Find(&items).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if len(items) > 0 {
			var tags []model.Tag
			if err := db.Find(&tags).Error; err == nil {
				tm := make(map[uint]string, len(tags))
				for _, tg := range tags {
					tm[tg.ID] = tg.Name
				}
				for i := range items {
					if items[i].TagID != nil {
						items[i].TagName = tm[*items[i].TagID]
					}
				}
			}
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	}
}

type shopBody struct {
	Name         string `json:"name"`
	Price        int    `json:"price"`
	Description  string `json:"description"`
	TagID        *uint  `json:"tag_id"`
	CooldownDays int    `json:"cooldown_days"`
	IsReward     bool   `json:"is_reward"`
}

func (b *shopBody) validate() string {
	if b.Name == "" {
		return "商品名称不能为空"
	}
	if b.Price <= 0 {
		return "价格必须大于 0"
	}
	return ""
}

func CreateShopItem(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body shopBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		if msg := body.validate(); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		item := model.ShopItem{Name: body.Name, Price: body.Price, Description: body.Description, TagID: body.TagID, CooldownDays: body.CooldownDays, IsReward: body.IsReward}
		if err := db.Create(&item).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, item)
	}
}

func UpdateShopItem(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var item model.ShopItem
		if err := db.First(&item, c.Param("id")).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "商品不存在"})
			return
		}
		var body shopBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		if msg := body.validate(); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		item.Name, item.Price, item.Description = body.Name, body.Price, body.Description
		item.TagID, item.CooldownDays, item.IsReward = body.TagID, body.CooldownDays, body.IsReward
		if err := db.Save(&item).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, item)
	}
}

func DeleteShopItem(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := db.Delete(&model.ShopItem{}, c.Param("id")).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	}
}

// ReorderShop persists the manual display order: ids in request order get
// sort_order = 1..n.
func ReorderShop(db *gorm.DB) gin.HandlerFunc {
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
				if err := tx.Model(&model.ShopItem{}).Where("id = ?", id).Update("sort_order", i+1).Error; err != nil {
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

// RedeemShopItem delegates to service.RedeemShopItem (overdraft floor,
// cooldown check and ledger write happen in one service transaction).
func RedeemShopItem(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Quantity int `json:"quantity"`
		}
		_ = c.ShouldBindJSON(&body) // body optional
		id64, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil {
			writeErr(c, service.BizErr(404, "商品不存在"))
			return
		}
		out, err := service.RedeemShopItem(db, uint(id64), body.Quantity)
		if err != nil {
			writeErr(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	}
}
