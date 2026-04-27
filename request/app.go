package request

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"com.lc.go.codepush/server/config"
	"com.lc.go.codepush/server/db"
	"com.lc.go.codepush/server/db/redis"
	"com.lc.go.codepush/server/model"
	"com.lc.go.codepush/server/model/constants"
	"com.lc.go.codepush/server/utils"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/jlaffaye/ftp"
	"gorm.io/gorm"
)

// maxBundleUploadBytes is the maximum allowed size for a single CodePush bundle upload.
const maxBundleUploadBytes int64 = 50 << 20 // 50 MiB

type App struct{}

type createAppReq struct {
	AppName *string `json:"appName" binding:"required"`
	OS      *int    `json:"os" binding:"required"`
}

func (App) CreateApp(ctx *gin.Context) {
	createAppInfo := createAppReq{}
	if err := ctx.ShouldBindBodyWith(&createAppInfo, binding.JSON); err == nil {
		uid := ctx.MustGet(constants.GIN_USER_ID).(int)
		oldApp := model.App{}.GetAppByUidAndAppName(uid, *createAppInfo.AppName)
		if oldApp != nil {
			log.Panic("AppName " + *createAppInfo.AppName + " exist")
		}
		if *createAppInfo.OS != 1 && *createAppInfo.OS != 2 {
			log.Panic("OS error")
		}
		newApp := model.App{
			Uid:        &uid,
			AppName:    createAppInfo.AppName,
			OS:         createAppInfo.OS,
			CreateTime: utils.GetTimeNow(),
		}
		model.Create[model.App](&newApp)
		ctx.JSON(http.StatusOK, gin.H{
			"success": true,
		})
	} else {
		log.Panic(err.Error())
	}
}

type createBundleReq struct {
	AppName     *string `json:"appName" binding:"required"`
	Deployment  *string `json:"deployment" binding:"required"`
	DownloadUrl *string `json:"downloadUrl" binding:"required"`
	Description *string `json:"description"`
	Version     *string `json:"version" binding:"required"`
	Size        *int64  `json:"size" binding:"required"`
	Hash        *string `json:"hash" binding:"required"`
	Mandatory   *bool   `json:"mandatory"`
}

func (App) CreateBundle(ctx *gin.Context) {
	createBundleReq := createBundleReq{}
	if err := ctx.ShouldBindBodyWith(&createBundleReq, binding.JSON); err == nil {
		uid := ctx.MustGet(constants.GIN_USER_ID).(int)
		mandatory := false
		if createBundleReq.Mandatory != nil {
			mandatory = *createBundleReq.Mandatory
		}
		app := model.App{}.GetAppByUidAndAppName(uid, *createBundleReq.AppName)
		if app == nil {
			log.Panic("App not found")
		}
		deployment := model.Deployment{}.GetByAppidAndName(*app.Id, *createBundleReq.Deployment)
		if deployment == nil {
			log.Panic("Deployment " + *createBundleReq.Deployment + " not found")
		}
		deploymentVersion := model.DeploymentVersion{}.GetByKeyDeploymentIdAndVersion(*deployment.Id, *createBundleReq.Version)
		if deploymentVersion == nil {
			versionNum := utils.FormatVersionStr(*createBundleReq.Version)
			deploymentVersion = &model.DeploymentVersion{
				DeploymentId: deployment.Id,
				AppVersion:   createBundleReq.Version,
				VersionNum:   &versionNum,
				CreateTime:   utils.GetTimeNow(),
			}
			model.Create[model.DeploymentVersion](deploymentVersion)

			if deployment.VersionId != nil {
				deploymentVersionOld := model.GetOne[model.DeploymentVersion]("id=?", *deployment.VersionId)
				if utils.FormatVersionStr(*deploymentVersionOld.AppVersion) < utils.FormatVersionStr(*createBundleReq.Version) {
					deployment.VersionId = deploymentVersion.Id
					deployment.UpdateTime = utils.GetTimeNow()
					model.Update[model.Deployment](deployment)
				}
			} else {
				deployment.VersionId = deploymentVersion.Id
				deployment.UpdateTime = utils.GetTimeNow()
				model.Update[model.Deployment](deployment)
			}
		} else {
			nowPack := model.GetOne[model.Package]("id=?", deploymentVersion.CurrentPackage)
			if nowPack != nil && *nowPack.Hash == *createBundleReq.Hash {
				log.Panic("Upload package no modification")
			}
		}
		// uuid, _ := uuid.NewUUID()
		// hash := uuid.String()
		newPackage := model.Package{
			DeploymentId:        deployment.Id,
			DeploymentVersionId: deploymentVersion.Id,
			Size:                createBundleReq.Size,
			Hash:                createBundleReq.Hash,
			Download:            createBundleReq.DownloadUrl,
			Description:         createBundleReq.Description,
			Active:              utils.CreateInt(0),
			Installed:           utils.CreateInt(0),
			Failed:              utils.CreateInt(0),
			CreateTime:          utils.GetTimeNow(),
		}
		model.Create[model.Package](&newPackage)
		deploymentVersion.CurrentPackage = newPackage.Id
		deploymentVersion.UpdateTime = utils.GetTimeNow()
		model.Update[model.DeploymentVersion](deploymentVersion)
		redis.DelRedisObj(constants.REDIS_UPDATE_INFO + *deployment.Key + "*")
		ctx.JSON(http.StatusOK, gin.H{
			"success":   true,
			"mandatory": mandatory,
		})
	} else {
		log.Panic(err.Error())
	}
}

type createDeploymentInfo struct {
	AppName        *string `json:"appName" binding:"required"`
	DeploymentName *string `json:"deploymentName" binding:"required"`
	Key            *string `json:"key"`
}

func (App) CreateDeployment(ctx *gin.Context) {
	createDeploymentInfo := createDeploymentInfo{}
	if err := ctx.ShouldBindBodyWith(&createDeploymentInfo, binding.JSON); err == nil {
		uid := ctx.MustGet(constants.GIN_USER_ID).(int)
		app := model.App{}.GetAppByUidAndAppName(uid, *createDeploymentInfo.AppName)
		if app == nil {
			log.Panic("App not found")
		}
		deployment := model.Deployment{}.GetByAppidAndName(*app.Id, *createDeploymentInfo.DeploymentName)
		if deployment != nil {
			log.Panic("Deployment name " + *createDeploymentInfo.DeploymentName + " exist")
		}
		if createDeploymentInfo.Key == nil || *createDeploymentInfo.Key == "" {
			uuid, _ := uuid.NewUUID()
			uuidStr := uuid.String()
			createDeploymentInfo.Key = &uuidStr
		}
		newDeployment := model.Deployment{
			AppId:      app.Id,
			Name:       createDeploymentInfo.DeploymentName,
			Key:        createDeploymentInfo.Key,
			CreateTime: utils.GetTimeNow(),
		}
		err := model.Create[model.Deployment](&newDeployment)
		if err != nil {
			log.Panic(err.Error())
		}
		ctx.JSON(http.StatusOK, gin.H{
			"name": createDeploymentInfo.DeploymentName,
			"key":  *createDeploymentInfo.Key,
		})
	} else {
		log.Panic(err.Error())
	}
}
func (App) UploadBundle(ctx *gin.Context) {
	_, headers, err := ctx.Request.FormFile("file")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"msg":     "file is required",
		})
		return
	}
	if headers.Size > maxBundleUploadBytes {
		ctx.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"success": false,
			"msg":     "bundle size must be at most 50 MB",
		})
		return
	}

	file, err := headers.Open()
	if err != nil {
		log.Panic(err.Error())
	}
	defer file.Close()

	// Read at most max+1 bytes so streams larger than the cap fail without loading the whole file.
	data, err := io.ReadAll(io.LimitReader(file, maxBundleUploadBytes+1))
	if err != nil {
		log.Panic(err.Error())
	}
	if int64(len(data)) > maxBundleUploadBytes {
		ctx.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"success": false,
			"msg":     "bundle size must be at most 50 MB",
		})
		return
	}

	cfg := config.GetConfig()
	key := headers.Filename
	body := bytes.NewReader(data)

	switch cfg.CodePush.FileLocal {
	case "local":
		exist := utils.Exists(cfg.CodePush.Local.SavePath)
		if !exist {
			err := os.MkdirAll(cfg.CodePush.Local.SavePath, 0777)
			if err != nil {
				log.Panic(err.Error())
			}
		}
		if err := os.WriteFile(path.Clean(cfg.CodePush.Local.SavePath+"/"+key), data, 0777); err != nil {
			log.Panic(err.Error())
		}
	case "aws":
		s3Config := &aws.Config{
			Credentials:      credentials.NewStaticCredentials(cfg.CodePush.Aws.KeyId, cfg.CodePush.Aws.Secret, ""),
			Endpoint:         aws.String(cfg.CodePush.Aws.Endpoint),
			Region:           aws.String(cfg.CodePush.Aws.Region),
			S3ForcePathStyle: aws.Bool(cfg.CodePush.Aws.S3ForcePathStyle),
		}
		newSession, _ := session.NewSession(s3Config)

		s3Client := s3.New(newSession)

		_, err = s3Client.PutObject(&s3.PutObjectInput{
			Body:   body,
			Bucket: aws.String(cfg.CodePush.Aws.Bucket),
			Key:    &key,
		})
		if err != nil {
			log.Panic(err.Error())
		}
	case "ftp":
		f, err := ftp.Dial(cfg.CodePush.Ftp.ServerUrl)
		if err != nil {
			log.Panic(err.Error())
		}
		err = f.Login(cfg.CodePush.Ftp.UserName, cfg.CodePush.Ftp.Password)
		if err != nil {
			log.Panic(err.Error())
		}

		err = f.Stor(key, body)
		if err != nil {
			log.Panic(err.Error())
		}
		if err := f.Quit(); err != nil {
			log.Panic(err.Error())
		}

	}

	fileName := path.Base(strings.TrimSpace(key))
	if fileName == "" || fileName == "." {
		fileName = key
	}

	downloadURL := ""
	if ru := strings.TrimSpace(cfg.ResourceUrl); ru != "" {
		downloadURL = strings.TrimSuffix(ru, "/") + "/" + url.PathEscape(fileName)
	} else {
		switch cfg.CodePush.FileLocal {
		case "aws":
			a := cfg.CodePush.Aws
			ep := strings.TrimSuffix(strings.TrimSpace(a.Endpoint), "/")
			if a.S3ForcePathStyle || strings.HasPrefix(ep, "http://") || strings.HasPrefix(ep, "https://") {
				downloadURL = fmt.Sprintf("%s/%s/%s", ep, a.Bucket, url.PathEscape(fileName))
			} else {
				downloadURL = fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", a.Bucket, a.Region, url.PathEscape(fileName))
			}
		case "local":
			downloadURL = filepath.ToSlash(filepath.Join(cfg.CodePush.Local.SavePath, fileName))
		case "ftp":
			base := strings.TrimSuffix(strings.TrimSpace(cfg.CodePush.Ftp.ServerUrl), "/")
			downloadURL = base + "/" + url.PathEscape(fileName)
		}
	}

	ctx.JSON(http.StatusOK, gin.H{
		"success":     true,
		"fileName":    fileName,
		"downloadUrl": downloadURL,
	})
}

type lsDeploymentReq struct {
	ShowKey *bool   `json:"k" binding:"required"`
	AppName *string `json:"appName" binding:"required"`
}

type lsDeploymentInfo struct {
	AppName     *string           `json:"appName"`
	Deployments *[]deploymentInfo `json:"deployments"`
}

type deploymentInfo struct {
	DeploymentName *string `json:"deploymentName"`
	AppVersion     *string `json:"appVersion"`
	Active         *int    `json:"active"`
	Failed         *int    `json:"failed"`
	Installed      *int    `json:"installed"`
	DeploymentKey  *string `json:"deploymentKey"`
}

func (App) LsDeployment(ctx *gin.Context) {
	lsAppReq := lsDeploymentReq{}
	if err := ctx.ShouldBindBodyWith(&lsAppReq, binding.JSON); err == nil {
		uid := ctx.MustGet(constants.GIN_USER_ID).(int)
		app := model.App{}.GetAppByUidAndAppName(uid, *lsAppReq.AppName)
		if app == nil {
			log.Panic("App not found")
		}
		var deploymentInfos []deploymentInfo
		deployment := model.Deployment{}.GetByAppids(*app.Id)

		for _, v := range *deployment {
			var key *string
			if *lsAppReq.ShowKey {
				key = v.Key
			}
			deploymentInfo := deploymentInfo{
				DeploymentName: v.Name,
				DeploymentKey:  key,
			}
			if v.VersionId != nil {
				deploymentVersion := model.GetOne[model.DeploymentVersion]("id=?", v.VersionId)
				deploymentInfo.AppVersion = deploymentVersion.AppVersion
				if deploymentVersion.CurrentPackage != nil {
					pack := model.GetOne[model.Package]("id=?", deploymentVersion.CurrentPackage)
					deploymentInfo.Active = pack.Active
					deploymentInfo.Failed = pack.Failed
					deploymentInfo.Installed = pack.Installed
				}
			}

			deploymentInfos = append(deploymentInfos, deploymentInfo)
		}
		lsAppInfo := lsDeploymentInfo{
			AppName:     app.AppName,
			Deployments: &deploymentInfos,
		}
		ctx.JSON(http.StatusOK, lsAppInfo)
	} else {
		log.Panic(err.Error())
	}
}

func (App) LsApp(ctx *gin.Context) {
	uid := ctx.MustGet(constants.GIN_USER_ID).(int)
	apps := model.GetList[model.App]("uid=?", uid)
	if len(*apps) <= 0 {
		log.Panic("No app")
	}
	var appsRep []createAppReq

	for _, v := range *apps {
		appsRep = append(appsRep, createAppReq{
			AppName: v.AppName,
			OS:      v.OS,
		})
	}
	ctx.JSON(http.StatusOK, appsRep)
}

type checkBundleReq struct {
	AppName    *string `json:"appName" binding:"required"`
	Deployment *string `json:"deployment" binding:"required"`
	Version    *string `json:"version" binding:"required"`
}

type listBundlesReq struct {
	AppName    *string `json:"appName" binding:"required"`
	Deployment *string `json:"deployment" binding:"required"`
}

// LsBundle returns the three most recently updated deployment_version rows for the app/deployment,
// each with the current package (bundle) when current_package is set.
func (App) LsBundle(ctx *gin.Context) {
	req := listBundlesReq{}
	if err := ctx.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		log.Panic(err.Error())
	}
	uid := ctx.MustGet(constants.GIN_USER_ID).(int)
	app := model.App{}.GetAppByUidAndAppName(uid, *req.AppName)
	if app == nil {
		log.Panic("App not found")
	}
	deployment := model.Deployment{}.GetByAppidAndName(*app.Id, *req.Deployment)
	if deployment == nil {
		log.Panic("Deployment " + *req.Deployment + " not found")
	}
	const listBundlesLimit = 3
	versions := model.DeploymentVersion{}.ListLatestForDeployment(*deployment.Id, listBundlesLimit)
	bundles := make([]gin.H, 0, len(versions))
	for i := range versions {
		dv := &versions[i]
		item := gin.H{
			"deploymentVersionId": dv.Id,
			"appVersion":          dv.AppVersion,
			"versionNum":          dv.VersionNum,
			"updateTime":          dv.UpdateTime,
			"createTime":          dv.CreateTime,
		}
		if dv.CurrentPackage != nil {
			if p := model.GetOne[model.Package]("id=?", *dv.CurrentPackage); p != nil {
				item["package"] = p
			}
		}
		bundles = append(bundles, item)
	}
	ctx.JSON(http.StatusOK, gin.H{
		"success": true,
		"bundles": bundles,
	})
}

func (App) CheckBundle(ctx *gin.Context) {
	checkBundleReq := checkBundleReq{}
	if err := ctx.ShouldBindBodyWith(&checkBundleReq, binding.JSON); err == nil {
		uid := ctx.MustGet(constants.GIN_USER_ID).(int)

		app := model.App{}.GetAppByUidAndAppName(uid, *checkBundleReq.AppName)
		if app == nil {
			log.Panic("App not found")
		}
		deployment := model.Deployment{}.GetByAppidAndName(*app.Id, *checkBundleReq.Deployment)
		if deployment == nil {
			log.Panic("Deployment " + *checkBundleReq.Deployment + " not found")
		}
		var hash *string
		if deployment.VersionId != nil {
			deployment := model.DeploymentVersion{}.GetByKeyDeploymentIdAndVersion(*deployment.Id, *checkBundleReq.Version)
			if deployment != nil && deployment.CurrentPackage != nil {
				pack := model.GetOne[model.Package]("id", deployment.CurrentPackage)
				hash = pack.Hash
			}
		}

		ctx.JSON(http.StatusOK, gin.H{
			"appName": app.AppName,
			"os":      app.OS,
			"hash":    hash,
		})
	} else {
		log.Panic(err.Error())
	}
}

type delAppInfo struct {
	AppName *string `json:"appName" binding:"required"`
}

func (App) DelApp(ctx *gin.Context) {
	delAppInfo := delAppInfo{}
	if err := ctx.ShouldBindBodyWith(&delAppInfo, binding.JSON); err == nil {
		uid := ctx.MustGet(constants.GIN_USER_ID).(int)

		app := model.App{}.GetAppByUidAndAppName(uid, *delAppInfo.AppName)
		if app == nil {
			log.Panic("App not found")
		}
		deployment := model.Deployment{}.GetByAppids(*app.Id)
		if deployment != nil && len(*deployment) > 0 {
			log.Panic("App exist deployment,Delete the deployment first and then delete the app ")
		}
		model.Delete[model.App](model.App{Id: app.Id})
		ctx.JSON(http.StatusOK, gin.H{
			"success": true,
		})
	} else {
		log.Panic(err.Error())
	}
}

type delDeploymentInfo struct {
	AppName    *string `json:"appName" binding:"required"`
	Deployment *string `json:"deployment" binding:"required"`
}

func (App) DelDeployment(ctx *gin.Context) {
	delDeploymentInfo := delDeploymentInfo{}
	if err := ctx.ShouldBindBodyWith(&delDeploymentInfo, binding.JSON); err == nil {
		uid := ctx.MustGet(constants.GIN_USER_ID).(int)

		app := model.App{}.GetAppByUidAndAppName(uid, *delDeploymentInfo.AppName)
		if app == nil {
			log.Panic("App not found")
		}
		deployment := model.Deployment{}.GetByAppidAndName(*app.Id, *delDeploymentInfo.Deployment)
		if deployment == nil {
			log.Panic("Deployment " + *delDeploymentInfo.Deployment + " not found")
		}
		userDb, _ := db.GetUserDB()
		err := userDb.Transaction(func(tx *gorm.DB) error {
			if err := tx.Delete(model.Deployment{Id: deployment.Id}).Error; err != nil {
				panic("DeleteError:" + err.Error())
			}
			if err := tx.Where("deployment_id", *deployment.Id).Delete(model.DeploymentVersion{}).Error; err != nil {
				panic("DeleteError:" + err.Error())
			}
			if err := tx.Where("deployment_id", *deployment.Id).Delete(model.Package{}).Error; err != nil {
				panic("DeleteError:" + err.Error())
			}
			return nil
		})
		if err != nil {
			panic("DeleteError:" + err.Error())
		}

		ctx.JSON(http.StatusOK, gin.H{
			"success": true,
		})
	} else {
		log.Panic(err.Error())
	}
}

type rollbackReq struct {
	AppName    *string `json:"appName" binding:"required"`
	Deployment *string `json:"deployment" binding:"required"`
	Version    *string `json:"version"`
}

func (App) Rollback(ctx *gin.Context) {
	rollbackReq := rollbackReq{}
	if err := ctx.ShouldBindBodyWith(&rollbackReq, binding.JSON); err == nil {
		uid := ctx.MustGet(constants.GIN_USER_ID).(int)

		app := model.App{}.GetAppByUidAndAppName(uid, *rollbackReq.AppName)
		if app == nil {
			log.Panic("App not found")
		}
		deployment := model.Deployment{}.GetByAppidAndName(*app.Id, *rollbackReq.Deployment)
		if deployment == nil {
			log.Panic("Deployment " + *rollbackReq.Deployment + " not found")
		}

		var deploymentVersion *model.DeploymentVersion
		if deployment.VersionId != nil {
			deploymentVersion = model.DeploymentVersion{}.GetByKeyDeploymentIdAndVersion(*deployment.Id, *rollbackReq.Version)
		}
		if deploymentVersion == nil {
			log.Panic("Version not found")
		}
		if deploymentVersion.CurrentPackage == nil {
			log.Panic("There is no upload package for the current version")
		}
		newPackage := model.Package{}.GetRollbackPack(*deployment.Id, *deploymentVersion.CurrentPackage, *deploymentVersion.Id)

		userDb, _ := db.GetUserDB()
		err := userDb.Transaction(func(tx *gorm.DB) error {
			if newPackage == nil {
				model.DeploymentVersion{}.UpdateCurrentPackage(*deploymentVersion.Id, nil)
			} else {
				model.DeploymentVersion{}.UpdateCurrentPackage(*deploymentVersion.Id, newPackage.Id)
			}
			return nil
		})
		if err != nil {
			panic("RollbackError:" + err.Error())
		}

		if newPackage != nil {
			ctx.JSON(http.StatusOK, gin.H{
				"Success":    true,
				"Version":    *deploymentVersion.AppVersion,
				"PackId":     *newPackage.Id,
				"Size":       *newPackage.Size,
				"Hash":       *newPackage.Hash,
				"CreateTime": *newPackage.CreateTime,
			})
		} else {
			ctx.JSON(http.StatusOK, gin.H{
				"Success": true,
				"Version": *deploymentVersion.AppVersion,
			})
		}
		redis.DelRedisObj(constants.REDIS_UPDATE_INFO + *deployment.Key + "*")
	} else {
		log.Panic(err.Error())
	}
}
