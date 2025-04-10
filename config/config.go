package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
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

func LoadConfig() *appConfig {
	fmt.Println("Fetching config from AWS secret manager...")
	keys := []string{
		"global",  // Global secrets
		"tenant",  // Tendancy punchh-server secrets
		"service", // Email template secrets
		"db",      // DB secrets
	}

	var config appConfig

	var dbObj dbConfigObj
	var redis redisConfig
	var buildSaveLocation codePush
	var aws awsConfig
	var ftp ftpConfig

	// default values
	config.DBUser.MaxIdleConns = 5
	config.DBUser.MaxOpenConns = 20
	config.DBUser.ConnMaxLifetime = 300

	config.Port = ":8080"
	config.UrlPrefix = "/"
	config.ResourceUrl = ""
	config.TokenExpireTime = 1 //in days

	for _, key := range keys {
		key = key + "_secrets"

		data, ok := os.LookupEnv(key)
		if !ok {
			fmt.Println("config: no secrets found for - ", key)
			continue
		}

		secrets := make(map[string]interface{})
		if err := json.Unmarshal([]byte(data), &secrets); err != nil {
			fmt.Println("config: error unmarshalling secrets for - ", key)
			panic(err)
		}

		for k, v := range secrets {
			k = strings.ToLower(k)
			fmt.Println(k)
			// DB
			if k == "db_username" {
				dbObj.UserName = v.(string)
			}
			if k == "db_password" {
				dbObj.Password = v.(string)
			}
			if k == "db_host" {
				dbObj.Host = v.(string)
			}
			if k == "db_port" {
				u64, _ := strconv.ParseUint(v.(string), 10, 32)
				dbObj.Port = uint(u64)
			}
			if k == "db_name" {
				dbObj.DBname = v.(string)
			}

			// Redis
			if k == "redis_host" {
				redis.Host = v.(string)
			}
			if k == "redis_port" {
				u64, _ := strconv.ParseUint(v.(string), 10, 32)
				redis.Port = uint(u64)
			}
			if k == "redis_db_index" {
				u64, _ := strconv.ParseUint(v.(string), 10, 32)
				redis.DBIndex = uint(u64)
			}
			if k == "redis_username" {
				redis.UserName = v.(string)
			}
			if k == "redis_password" {
				redis.Password = v.(string)
			}

			// local bundle save location
			if k == "build_save_location" {
				buildSaveLocation.FileLocal = v.(string)
			}

			// AWS
			if k == "aws_s3_endpoint" {
				aws.Endpoint = v.(string)
			}
			if k == "aws_region" {
				aws.Region = v.(string)
			}
			if k == "aws_s3_force_path_style" {
				aws.S3ForcePathStyle = true
			}
			if k == "aws_access_key_id" {
				aws.KeyId = v.(string)
			}
			if k == "aws_secret_access_key" {
				aws.Secret = v.(string)
			}
			if k == "aws_s3_bucket_name" {
				aws.Bucket = v.(string)
			}

			// ftp
			if k == "ftp_server_url" {
				ftp.ServerUrl = v.(string)
			}
			if k == "ftp_username" {
				ftp.UserName = v.(string)
			}
			if k == "ftp_password" {
				ftp.Password = v.(string)
			}
			// common

			// if build_save_location is set to `local` then resource URL should the self server URL
			// if build_save_location is set to `aws` then resource URL should the AWS S3 bucket URL
			if k == "resource_url" {
				config.ResourceUrl = v.(string)
			}
			if k == "tenant_name" {
				config.TenantName = v.(string)
			}

			if k == "environment" {
				config.Environment = v.(string)
			}
		}
	}
	config.DBUser.Write = dbObj
	config.Redis = redis
	config.CodePush = buildSaveLocation
	config.CodePush.Aws = aws
	config.CodePush.Ftp = ftp

	// validate the config
	validate := validator.New()
	if err := validate.Struct(config); err != nil {
		fmt.Println("config: invalid/missing configuration", err)
		panic(err)
	}
	return &config
}
