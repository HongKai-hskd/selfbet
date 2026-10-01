package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"selfbet/backend/internal/service"
)

// ---- Item guide ----
//
// 道具注册表（单一事实来源）在 internal/service/itemdef.go：
// 新增/修改道具只动那里的注册行与使用函数，本文件只暴露只读接口。

// ItemsGuideHandler serves GET /api/items/guide: one payload driving the
// guide page, the backpack cells and the box-drop configuration picker.
func ItemsGuideHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"items": service.ItemDefs})
	}
}
