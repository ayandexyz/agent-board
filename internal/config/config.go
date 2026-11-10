package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	General GeneralConfig
	Board   BoardConfig
	Agent   AgentConfig
	TUI     TUIConfig
	LLM     LLMConfig
	DB      DBConfig
	MCP     MCPConfig
	Git     GitConfig

	ConfigPath        string
	ProjectConfigPath string
	ProjectName       string
	ProjectKey        string
}

func (c *Config) Project() Project {
	return Project{Key: c.ProjectKey, Name: c.ProjectName}
}

func LoadFromDir(baseDir string, project Project) (*Config, error) {
	cfg := SetDefaults()
	cfg.ProjectName = project.Name
	cfg.ProjectKey = project.Key

	if err := EnsureDirs(baseDir, project.Key); err != nil {
		return nil, err
	}

	globalPath := filepath.Join(baseDir, "config.toml")
	_ = WriteDefaultConfig(globalPath)
	if err := loadFromFile(cfg, globalPath); err != nil {
		return nil, err
	}

	projPath := filepath.Join(baseDir, "projects", project.Key, "config.toml")
	_ = WriteDefaultConfig(projPath)
	if err := loadFromFile(cfg, projPath); err != nil {
		return nil, err
	}

	if cfg.DB.Path == "" {
		cfg.DB.Path = filepath.Join(baseDir, "projects", project.Key, "board.db")
	}

	if cfg.Board.Prefix == "" {
		cfg.Board.Prefix = DefaultPrefix(project.Name)
	}

	cfg.ConfigPath = globalPath
	cfg.ProjectConfigPath = projPath

	applyEnvVars(cfg)

	return cfg, nil
}

func Load() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("config.load: home directory: %w", err)
	}

	baseDir := filepath.Join(homeDir, ".agentboard")

	project := ResolveProject(ExecGit, getWorkingDir())
	if err := MigrateLegacyProject(baseDir, project); err != nil {
		return nil, fmt.Errorf("config.load: %w", err)
	}

	return LoadFromDir(baseDir, project)
}

func getWorkingDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return dir
}

func GetBaseDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".agentboard"
	}
	return filepath.Join(homeDir, ".agentboard")
}
