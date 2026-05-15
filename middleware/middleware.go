package middleware

import (
	"fmt"
	"log"
	"net/http"
	"reflect"
	"strings"

	"com.lc.go.codepush/server/config"
	"com.lc.go.codepush/server/model"
	"com.lc.go.codepush/server/model/constants"
	"com.lc.go.codepush/server/utils"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

// CheckToken authenticates via the "token" cookie or header.
// It first attempts to parse the value as a JWT (from app-center-server).
// If JWT validation fails, it falls back to the existing DB token lookup.
func CheckToken(ctx *gin.Context) {
	var token, _ = ctx.Cookie("token")
	if token == "" {
		token = ctx.GetHeader("token")
	}

	if token == "" {
		ctx.JSON(http.StatusUnauthorized, gin.H{
			"code": 1100,
			"msg":  "Token required",
		})
		ctx.Abort()
		return
	}

	// 1. Try JWT validation
	if uid, ok := validateJWT(token); ok {
		ctx.Set(constants.GIN_USER_ID, uid)
		return
	}

	// 2. Fall back to existing DB token lookup (UUID from code-push login)
	tokenNow := model.GetOne[model.Token]("token=?", token)
	if tokenNow == nil || tokenNow.ExpireTime == nil || tokenNow.Del == nil || tokenNow.Uid == nil {
		ctx.JSON(http.StatusUnauthorized, gin.H{"code": 1100, "msg": "Invalid token"})
		ctx.Abort()
		return
	}

	if *utils.GetTimeNow() > *tokenNow.ExpireTime || *tokenNow.Del {
		ctx.JSON(http.StatusUnauthorized, gin.H{"code": 1100, "msg": "Token expired"})
		ctx.Abort()
		return
	}

	ctx.Set(constants.GIN_USER_ID, *tokenNow.Uid)
}

// validateJWT tries to parse tokenStr as an HS256 JWT signed with JWT_SECRET.
// Returns (uid, true) on success. Returns (0, false) silently on any failure
// so the caller can fall back to DB token lookup.
// validateJWT validates the token against the shared JWT_SECRET.
// If the signature and claims are valid, authentication passes — no DB lookup needed.
// Returns (default admin uid, true) on success so downstream handlers have a valid uid.
func validateJWT(tokenStr string) (int, bool) {
	secret := config.GetConfig().JWTSecret
	if secret == "" {
		return 0, false
	}

	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return 0, false
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, false
	}

	email := ""
	if email, _ := claims["email"].(string); email == "" {
		return 0, false
	}
	if strings.HasSuffix(email, "@punchh.com") || strings.HasSuffix(email, "@partech.com") {
		return 1, true
	}

	return 0, false
}

// 異常處理
func Recover(c *gin.Context) {
	c.Writer.Header().Add("Access-Control-Allow-Origin", "*")
	c.Writer.Header().Add("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	c.Writer.Header().Add("Access-Control-Allow-Headers", "*")
	lang := c.GetHeader("Accept-Language")
	c.Set(constants.GIN_LANG, lang)
	// 加载defer异常处理
	defer func() {
		if err := recover(); err != nil {
			c.Writer.WriteHeader(http.StatusInternalServerError)
			log.Printf("Error:%s", err)
			// 返回统一的Json风格
			var msgStr string
			if fmt.Sprint(reflect.TypeOf(err)) == "string" {
				msgStr = fmt.Sprint(err)
			} else {
				msgStr = "system error"
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    500,
				"msg":     msgStr,
				"success": false,
			})
			//终止后续操作
			c.Abort()
		}
	}()
	if c.Request.Method == "OPTIONS" {
		c.Writer.WriteHeader(http.StatusNoContent)
		c.Abort()
		// return
	}
	//继续操作
	c.Next()
}
