package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

// Loader reads + validates a config tree from a single directory's YAML pair
// (config.default.yml + config.<env>.yml). There is intentionally no
// "root config" — every module (and internal/app) owns its own dir.
type Loader struct {
	v        *viper.Viper
	validate *validator.Validate
	env      string
	dir      string
}

// NewDirLoader builds a Loader for any dir holding config.{default,<env>}.yml.
// Module code uses LoadModule/LoadDir helpers below; this constructor is for
// callers that want to keep a Loader handle (e.g. iterate Unmarshal calls).
func NewDirLoader(dir string) (*Loader, error) {
	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.SetEnvPrefix("APP")

	if err := loadDefaultConfig(v, dir); err != nil {
		slog.Warn("Could not load default config",
			slog.String("dir", dir), slog.Any("error", err))
	}
	if err := loadEnvConfig(v, dir); err != nil {
		slog.Warn("Could not load env config",
			slog.String("dir", dir), slog.Any("error", err))
	}

	env := v.GetString("env")
	if env == "" {
		if e := os.Getenv("APP_ENV"); e != "" {
			env = e
		} else {
			env = "development"
		}
	}

	return &Loader{v: v, validate: validator.New(), env: env, dir: dir}, nil
}

// Unmarshal decodes the subtree at key into dst, then validates dst.
// Use empty key to decode the root.
func (l *Loader) Unmarshal(key string, dst any) error {
	return unmarshalKey(l.v, l.validate, key, dst)
}

func (l *Loader) Env() string         { return l.env }
func (l *Loader) IsDevelopment() bool { return l.env == "development" || l.env == "dev" }
func (l *Loader) IsProduction() bool  { return l.env == "production" || l.env == "prod" }
func (l *Loader) IsTest() bool        { return l.env == "test" }

// LoadDir is the one-shot variant of NewDirLoader+Unmarshal: read dir's YAML
// pair, decode root into dst, validate.
func LoadDir(dir string, dst any) error {
	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	v.SetEnvPrefix("APP")

	if err := loadDefaultConfig(v, dir); err != nil {
		slog.Warn("Could not load default config",
			slog.String("dir", dir), slog.Any("error", err))
	}
	if err := loadEnvConfig(v, dir); err != nil {
		slog.Warn("Could not load env config",
			slog.String("dir", dir), slog.Any("error", err))
	}

	return unmarshalKey(v, validator.New(), "", dst)
}

// LoadModule is sugar over LoadDir for the module convention:
// internal/module/<name>/config/.
func LoadModule(moduleName string, dst any) error {
	return LoadDir(filepath.Join("internal", "module", moduleName, "config"), dst)
}

func unmarshalKey(v *viper.Viper, validate *validator.Validate, key string, dst any) error {
	if key == "" {
		if err := v.Unmarshal(dst); err != nil {
			return fmt.Errorf("unmarshal root: %w", err)
		}
	} else {
		if err := v.UnmarshalKey(key, dst); err != nil {
			return fmt.Errorf("unmarshal %q: %w", key, err)
		}
	}
	if err := validate.Struct(dst); err != nil {
		if verr, ok := errors.AsType[validator.ValidationErrors](err); ok {
			msgs := make([]string, 0, len(verr))
			for _, e := range verr {
				msgs = append(msgs, e.Error())
			}
			return fmt.Errorf("validate %q: %s", key, strings.Join(msgs, "; "))
		}
		return fmt.Errorf("validate %q: %w", key, err)
	}
	return nil
}

func loadDefaultConfig(v *viper.Viper, dir string) error {
	path := filepath.Join(dir, "config.default.yml")
	if !fileExists(path) {
		return fmt.Errorf("default config not found: %s", path)
	}
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return err
	}
	slog.Info("Loaded default config", slog.String("path", path))
	return nil
}

func loadEnvConfig(v *viper.Viper, dir string) error {
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "development"
	}

	candidates := []string{
		filepath.Join(dir, fmt.Sprintf("config.%s.yml", env)),
	}
	if env == "development" {
		candidates = append(candidates, filepath.Join(dir, "config.dev.yml"))
	}
	if env == "production" {
		candidates = append(candidates, filepath.Join(dir, "config.prod.yml"))
	}

	var lastErr error
	for _, path := range candidates {
		if fileExists(path) {
			if err := mergeConfigFile(v, path); err == nil {
				slog.Info("Loaded config", slog.String("path", path))
				return nil
			} else {
				lastErr = err
			}
		}
	}
	if lastErr != nil {
		return fmt.Errorf("no valid config file found in %s, last error: %w", dir, lastErr)
	}
	return nil
}

func mergeConfigFile(v *viper.Viper, configFile string) error {
	v.SetConfigFile(configFile)
	if v.ConfigFileUsed() == "" {
		return v.ReadInConfig()
	}
	return v.MergeInConfig()
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
