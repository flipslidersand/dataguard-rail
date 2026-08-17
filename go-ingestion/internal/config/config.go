// Package config は sources.yaml から DataSource 定義を読み込む。
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SourceType はデータソースの種別。
type SourceType string

const (
	// CSV はローカル CSV ファイルソース。
	CSV SourceType = "csv"
	// Postgres は PostgreSQL クエリソース。
	Postgres SourceType = "postgres"
)

// DataSource は 1 つの取込み元定義 (data-model.md の DataSource)。
type DataSource struct {
	Name     string     `yaml:"name"`
	Type     SourceType `yaml:"type"`
	Path     string     `yaml:"path"`     // csv 用
	DSN      string     `yaml:"dsn"`      // postgres 用
	Query    string     `yaml:"query"`    // postgres 用
	Schedule string     `yaml:"schedule"` // cron (Phase 3 では未使用)
}

// Config は sources.yaml のトップレベル。
type Config struct {
	Sources []DataSource `yaml:"sources"`
}

// Load は sources.yaml を読み込み、検証済みの Config を返す。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	if len(cfg.Sources) == 0 {
		return nil, fmt.Errorf("config %q: sources が空です", path)
	}
	for i := range cfg.Sources {
		if err := cfg.Sources[i].validate(); err != nil {
			return nil, err
		}
	}
	return &cfg, nil
}

// validate は DataSource の必須フィールドを種別ごとに検証する。
func (d *DataSource) validate() error {
	if d.Name == "" {
		return fmt.Errorf("source: name が必須です")
	}
	switch d.Type {
	case CSV:
		if d.Path == "" {
			return fmt.Errorf("source %q: csv には path が必須です", d.Name)
		}
	case Postgres:
		if d.DSN == "" || d.Query == "" {
			return fmt.Errorf("source %q: postgres には dsn と query が必須です", d.Name)
		}
	default:
		return fmt.Errorf("source %q: 未対応の type %q (csv|postgres)", d.Name, d.Type)
	}
	return nil
}
