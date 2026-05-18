package config

import (
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/go-playground/validator/v10"
)

type appConfig struct {
	DBUser          dbConfig
	Redis           redisConfig
	CodePush        codePush
	UrlPrefix       string
	Port            string
	ResourceUrl     string `json:"resource_url" validate:"required"`
	TokenExpireTime int64
	Environment     string `json:"environment" validate:"required"`
	TenantName      string `json:"tenant_name" validate:"required"`
	JWTSecret       string `json:"jwt_secret" validate:"required"`
}
type dbConfig struct {
	Write           dbConfigObj
	MaxIdleConns    uint
	MaxOpenConns    uint
	ConnMaxLifetime uint
}
type dbConfigObj struct {
	UserName string `json:"db_username" validate:"required"`
	Password string `json:"db_password" validate:"required"`
	Host     string `json:"db_host" validate:"required"`
	Port     uint   `json:"db_port" validate:"required"`
	DBname   string `json:"db_name" validate:"required"`
}
type redisConfig struct {
	Host     string `json:"redis_host" validate:"required"`
	Port     uint   `json:"redis_port" validate:"required"`
	DBIndex  uint   `json:"redis_db_index"`
	UserName string `json:"redis_username"`
	Password string `json:"redis_password"`
}
type codePush struct {
	FileLocal string `json:"build_save_location" validate:"required"`
	Local     localConfig
	Aws       awsConfig
	Ftp       ftpConfig
}
type awsConfig struct {
	Endpoint         string `json:"aws_s3_endpoint" validate:"required"`
	Region           string `json:"aws_region" validate:"required"`
	S3ForcePathStyle bool   `json:"aws_s3_force_path_style" validate:"required"`
	KeyId            string `json:"aws_access_key_id" validate:"required"`
	Secret           string `json:"aws_secret_access_key" validate:"required"`
	Bucket           string `json:"aws_s3_bucket_name" validate:"required"`
}
type ftpConfig struct {
	ServerUrl string `json:"ftp_server_url"`
	UserName  string `json:"ftp_username"`
	Password  string `ftp_password`
}
type localConfig struct {
	SavePath string `json:"local_build_save_path"`
}

var config *appConfig
var once sync.Once

func GetConfig() *appConfig {
	once.Do(func() {
		config = LoadConfig()
	})
	return config
}

func envUint(key string) uint {
	v := os.Getenv(key)
	if v == "" {
		return 0
	}
	u64, _ := strconv.ParseUint(v, 10, 32)
	return uint(u64)
}

func LoadConfig() *appConfig {
	fmt.Println("Loading config from environment variables...")

	var config appConfig

	// defaults
	config.DBUser.MaxIdleConns = 5
	config.DBUser.MaxOpenConns = 20
	config.DBUser.ConnMaxLifetime = 300
	config.Port = ":8080"
	config.UrlPrefix = "/"
	config.ResourceUrl = ""
	config.TokenExpireTime = 1

	// DB
	config.DBUser.Write = dbConfigObj{
		UserName: os.Getenv("DB_USERNAME"),
		Password: os.Getenv("DB_PASSWORD"),
		Host:     os.Getenv("DB_HOST"),
		Port:     envUint("DB_PORT"),
		DBname:   os.Getenv("DB_NAME"),
	}

	// Redis
	config.Redis = redisConfig{
		Host:     os.Getenv("REDIS_HOST"),
		Port:     envUint("REDIS_PORT"),
		DBIndex:  envUint("REDIS_DB_INDEX"),
		UserName: os.Getenv("REDIS_USERNAME"),
		Password: os.Getenv("REDIS_PASSWORD"),
	}

	// CodePush / build storage
	config.CodePush = codePush{
		FileLocal: os.Getenv("BUILD_SAVE_LOCATION"),
		Local: localConfig{
			SavePath: os.Getenv("LOCAL_BUILD_SAVE_PATH"),
		},
		Aws: awsConfig{
			Endpoint:         os.Getenv("AWS_S3_ENDPOINT"),
			Region:           os.Getenv("AWS_REGION"),
			S3ForcePathStyle: os.Getenv("AWS_S3_FORCE_PATH_STYLE") == "true",
			KeyId:            os.Getenv("AWS_ACCESS_KEY_ID"),
			Secret:           os.Getenv("AWS_SECRET_ACCESS_KEY"),
			Bucket:           os.Getenv("AWS_S3_BUCKET_NAME"),
		},
		Ftp: ftpConfig{
			ServerUrl: os.Getenv("FTP_SERVER_URL"),
			UserName:  os.Getenv("FTP_USERNAME"),
			Password:  os.Getenv("FTP_PASSWORD"),
		},
	}

	// Common
	config.ResourceUrl = os.Getenv("RESOURCE_URL")
	config.TenantName = os.Getenv("TENANT_NAME")
	config.Environment = os.Getenv("ENVIRONMENT")
	config.JWTSecret = os.Getenv("JWT_SECRET")

	// validate
	validate := validator.New()
	if err := validate.Struct(config); err != nil {
		fmt.Println("config: invalid/missing configuration", err)
		panic(err)
	}
	return &config
}
