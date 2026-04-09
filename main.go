package main

import (
	"fmt"
	"os"
	"strconv"

	"com.lc.go.codepush/server/config"
	localmw "com.lc.go.codepush/server/middleware"
	"com.lc.go.codepush/server/request"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/newrelic/go-agent/v3/integrations/nrgin"
	punchhmw "github.com/punchh/go-packages/middleware"
	"github.com/sirupsen/logrus"
)

func main() {
	fmt.Println("code-push-server-go V1.0.5")
	// gin.SetMode(gin.ReleaseMode)

	logger := punchhmw.NewLogger(logrus.StandardLogger())
	configs := config.GetConfig()

	// Airbrake on logrus errors (same hook as go-email-templates / go-packages)
	airPID, airKey := configs.AirbrakeProjectID, configs.AirbrakeProjectKey
	if airPID == 0 {
		if s := os.Getenv("AIRBRAKE_PROJECT_ID"); s != "" {
			airPID, _ = strconv.ParseInt(s, 10, 64)
		}
	}
	if airKey == "" {
		airKey = os.Getenv("AIRBRAKE_PROJECT_KEY")
	}
	if airPID > 0 && airKey != "" {
		logger.AddHook(punchhmw.NewAirbrake(configs.Environment, airPID, airKey))
	}

	nrKey := configs.NewRelicLicenseKey
	if nrKey == "" {
		nrKey = os.Getenv("NEW_RELIC_LICENSE_KEY")
	}

	nr, err := punchhmw.Newnewrelic(configs.Environment, "code-push-server-go", nrKey)
	if err != nil {
		logger.Println("newrelic init:", err)
	}

	logger.Println("starting HTTP server")

	g := gin.Default()
	if nr != nil && nr.Application != nil {
		g.Use(nrgin.Middleware(nr.Application))
	}
	g.Use(gzip.Gzip(gzip.DefaultCompression))
	g.Use(localmw.Recover)

	// g.Static("/bundels", "bundels")

	g.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})

	g.GET("/v0.1/public/codepush/update_check", request.Client{}.CheckUpdate)
	g.POST("/v0.1/public/codepush/report_status/deploy", request.Client{}.ReportStatus)
	g.POST("/v0.1/public/codepush/report_status/download", request.Client{}.Download)

	apiGroup := g.Group(configs.UrlPrefix)
	{
		apiGroup.POST("/login", request.User{}.Login)
	}
	authApi := apiGroup.Use(localmw.CheckToken)
	{
		authApi.POST("/createApp", request.App{}.CreateApp)
		authApi.POST("/createDeployment", request.App{}.CreateDeployment)
		authApi.POST("/createBundle", request.App{}.CreateBundle)
		authApi.POST("/checkBundle", request.App{}.CheckBundle)
		authApi.POST("/delApp", request.App{}.DelApp)
		authApi.POST("/delDeployment", request.App{}.DelDeployment)
		authApi.POST("/lsDeployment", request.App{}.LsDeployment)
		authApi.GET("/lsApp", request.App{}.LsApp)
		authApi.POST("/uploadBundle", request.App{}.UploadBundle)
		authApi.POST("/rollback", request.App{}.Rollback)
		authApi.POST("/changePassword", request.User{}.ChangePassword)
	}

	g.Run(configs.Port)
}
