package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"selfbet/backend/internal/config"
)

const tokenKey = "selfbet-token-key-v1"

// TokenFor derives a deterministic token from the password. Changing the
// password in config invalidates old tokens automatically.
func TokenFor(password string) string {
	mac := hmac.New(sha256.New, []byte(tokenKey))
	mac.Write([]byte(password))
	return hex.EncodeToString(mac.Sum(nil))
}

// Login checks the password and returns the bearer token.
func Login(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
			return
		}
		if subtle.ConstantTimeCompare([]byte(body.Password), []byte(cfg.Password)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "密码错误"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": TokenFor(cfg.Password)})
	}
}

// Auth is the middleware guarding every /api route except /api/auth/login.
func Auth(cfg *config.Config) gin.HandlerFunc {
	expect := TokenFor(cfg.Password)
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		token := strings.TrimPrefix(h, "Bearer ")
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(expect)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录或登录已过期"})
			return
		}
		c.Next()
	}
}
