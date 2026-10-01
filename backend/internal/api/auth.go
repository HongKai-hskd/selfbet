package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

const tokenKey = "selfbet-token-key-v1"

// TokenFor derives a deterministic token from the password. Changing the
// password (settings 表) invalidates old tokens automatically.
func TokenFor(password string) string {
	mac := hmac.New(sha256.New, []byte(tokenKey))
	mac.Write([]byte(password))
	return hex.EncodeToString(mac.Sum(nil))
}

// authPassword reads the login password from the settings table (fallback: default).
func authPassword(db *gorm.DB) string {
	var s model.Settings
	if err := db.Where("setting_key = ?", model.AuthPasswordKey).First(&s).Error; err == nil && s.Value != "" {
		return s.Value
	}
	return model.DefaultPassword
}

// Login checks the password (settings 表) and returns the bearer token.
func Login(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		expected := authPassword(db)
		if subtle.ConstantTimeCompare([]byte(body.Password), []byte(expected)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "密码错误"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": TokenFor(expected)})
	}
}

// Auth is the middleware guarding every /api route except /api/auth/login.
func Auth(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		expect := TokenFor(authPassword(db))
		h := c.GetHeader("Authorization")
		token := strings.TrimPrefix(h, "Bearer ")
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(expect)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录或登录已过期"})
			return
		}
		c.Next()
	}
}
