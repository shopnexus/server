package catalogconfig

import (
	"time"

	"shopnexus-server/config"
)

type Config struct {
	Postgres config.Postgres `mapstructure:"postgres"`
	Redis    config.Redis    `mapstructure:"redis"`
	Log      config.Log      `mapstructure:"log"`
	Restate  config.Restate  `mapstructure:"restate"`
	Search   Search          `mapstructure:"search"`
	Milvus   Milvus          `mapstructure:"milvus"`
	LLM      LLM             `mapstructure:"llm"`
}

type Search struct {
	DenseWeight           float32       `yaml:"denseWeight"           mapstructure:"denseWeight"           validate:"required,gte=0,lte=1"`
	SparseWeight          float32       `yaml:"sparseWeight"          mapstructure:"sparseWeight"          validate:"required,gte=0,lte=1"`
	InteractionBatchSize  int           `yaml:"interactionBatchSize"  mapstructure:"interactionBatchSize"  validate:"required,gte=1"`
	MetadataSyncInterval  time.Duration `yaml:"metadataSyncInterval"  mapstructure:"metadataSyncInterval"  validate:"gte=0"`
	EmbeddingSyncInterval time.Duration `yaml:"embeddingSyncInterval" mapstructure:"embeddingSyncInterval" validate:"gte=0"`
}

// Milvus + LLM: only catalog (search embeddings) needs them.
type Milvus struct {
	Address string `yaml:"address" mapstructure:"address" validate:"required"`
}

type LLM struct {
	Provider string     `yaml:"provider" mapstructure:"provider" validate:"required,oneof=python openai bedrock"`
	Python   LLMPython  `yaml:"python"   mapstructure:"python"`
	OpenAI   LLMOpenAI  `yaml:"openai"   mapstructure:"openai"`
	Bedrock  LLMBedrock `yaml:"bedrock"  mapstructure:"bedrock"`
}

type LLMPython struct {
	URL string `yaml:"url" mapstructure:"url" validate:"omitempty,url"`
}

type LLMOpenAI struct {
	APIKey     string `yaml:"apiKey"     mapstructure:"apiKey"`
	BaseURL    string `yaml:"baseURL"    mapstructure:"baseURL"    validate:"omitempty,url"`
	EmbedModel string `yaml:"embedModel" mapstructure:"embedModel"`
	ChatModel  string `yaml:"chatModel"  mapstructure:"chatModel"`
}

type LLMBedrock struct {
	Region       string `yaml:"region"       mapstructure:"region"`
	EmbedModelID string `yaml:"embedModelId" mapstructure:"embedModelId"`
	ChatModelID  string `yaml:"chatModelId"  mapstructure:"chatModelId"`
}

func NewConfig() (*Config, error) {
	var cfg Config
	return &cfg, config.LoadModule("catalog", &cfg)
}
