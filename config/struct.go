package config

import "time"

type Config struct {
	// General configuration
	Env string `yaml:"env" mapstructure:"env" validate:"required"`
	Log Log    `yaml:"log" mapstructure:"log" validate:"required"`
	App App    `yaml:"app" mapstructure:"app" validate:"required"`

	// Infrastructure components
	Postgres  Postgres  `yaml:"postgres" mapstructure:"postgres" validate:"required"`
	Redis     Redis     `yaml:"redis" mapstructure:"redis" validate:"required"`
	Filestore Filestore `yaml:"filestore" mapstructure:"filestore" validate:"required"`
}

type App struct {
	Name      string `yaml:"name" mapstructure:"name" validate:"required"`
	PublicURL string `yaml:"publicUrl" mapstructure:"publicUrl" validate:"required,url"`
	JWT       JWT    `yaml:"jwt" mapstructure:"jwt" validate:"required"`
	Vnpay     Vnpay  `yaml:"vnpay" mapstructure:"vnpay" validate:"required"`
	Search    Search `yaml:"search" mapstructure:"search" validate:"required"`
}

type JWT struct {
	Secret               string `yaml:"secret" mapstructure:"secret" validate:"required"`
	AccessTokenDuration  int64  `yaml:"accessTokenDuration" mapstructure:"accessTokenDuration" validate:"required,gte=1"`
	RefreshTokenDuration int64  `yaml:"refreshTokenDuration" mapstructure:"refreshTokenDuration" validate:"required,gte=1"`
	RefreshSecret        string `yaml:"refreshSecret" mapstructure:"refreshSecret"`
}

type Vnpay struct {
	TmnCode    string `yaml:"tmnCode" mapstructure:"tmnCode" validate:"required"`
	HashSecret string `yaml:"hashSecret" mapstructure:"hashSecret" validate:"required"`
	ReturnURL  string `yaml:"returnUrl" mapstructure:"returnUrl" validate:"required,url"`
}

type Search struct {
	Url                  string  `yaml:"url" mapstructure:"url" validate:"required,url"`
	DenseWeight          float32 `yaml:"denseWeight" mapstructure:"denseWeight" validate:"required,gte=0,lte=1"`
	SparseWeight         float32 `yaml:"sparseWeight" mapstructure:"sparseWeight" validate:"required,gte=0,lte=1"`
	InteractionBatchSize int     `yaml:"interactionBatchSize" mapstructure:"interactionBatchSize" validate:"required,gte=1"`
}

type Log struct {
	Level      string `yaml:"level" mapstructure:"level" validate:"required,oneof=debug info warn error"`
	Format     string `yaml:"format" mapstructure:"format" validate:"oneof=json text"`
	AddSource  bool   `yaml:"addSource" mapstructure:"addSource" validate:"required"`
	TimeFormat string `yaml:"timeFormat" mapstructure:"timeFormat" validate:"required"`
}

type Postgres struct {
	Url                     string        `yaml:"url" mapstructure:"url"`
	Host                    string        `yaml:"host" mapstructure:"host" validate:"required_without=Url"`
	Port                    int           `yaml:"port" mapstructure:"port" validate:"required_without=Url"`
	Username                string        `yaml:"username" mapstructure:"username" validate:"required_without=Url"`
	Password                string        `yaml:"password" mapstructure:"password" validate:"required_without=Url"`
	Database                string        `yaml:"database" mapstructure:"database" validate:"required_without=Url"`
	MaxConnections          int32         `yaml:"maxConnections" mapstructure:"maxConnections" validate:"gte=1"`
	MaxIdleConnections      int32         `yaml:"maxIdleConnections" mapstructure:"maxIdleConnections" validate:"gte=0"`
	MaxConnIdleTime         time.Duration `yaml:"maxConnIdleTime" mapstructure:"maxConnIdleTime" validate:"gte=0"`
	LogQuery                bool          `yaml:"logQuery" mapstructure:"logQuery"`
	AllowNestedTransactions bool          `yaml:"allowNestedTransactions" mapstructure:"allowNestedTransactions"`
}

type Redis struct {
	Host     string `yaml:"host" mapstructure:"host" validate:"required"`
	Port     string `yaml:"port" mapstructure:"port" validate:"required"`
	Password string `yaml:"password" mapstructure:"password"`
	DB       int64  `yaml:"db" mapstructure:"db" validate:"gte=0"`
}

type Filestore struct {
	Type                string      `yaml:"type" mapstructure:"type" validate:"required,oneof=local s3"`
	PresignedDefaultTTL int64       `yaml:"presignedDefaultTTL" mapstructure:"presignedDefaultTTL" validate:"gte=1"`
	S3                  S3Filestore `yaml:"s3" mapstructure:"s3"`
}

type S3Filestore struct {
	AccessKeyID     string `yaml:"accessKeyID" mapstructure:"accessKeyID"`
	SecretAccessKey string `yaml:"secretAccessKey" mapstructure:"secretAccessKey"`
	Region          string `yaml:"region" mapstructure:"region"`
	Bucket          string `yaml:"bucket" mapstructure:"bucket"`
	CloudfrontURL   string `yaml:"cloudfrontUrl" mapstructure:"cloudfrontUrl" validate:"omitempty"`
}
