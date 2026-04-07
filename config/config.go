package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig           `yaml:"server"`
	Agents   map[string]AgentConfig `yaml:"agents"`
	Watch    WatchConfig            `yaml:"watch"`
	Executor ExecutorConfig         `yaml:"executor"`
}

type ServerConfig struct {
	PollInterval time.Duration `yaml:"poll_interval"`
	WorkerCount  int           `yaml:"worker_count"`
	DBPath       string        `yaml:"db_path"`
}

type AgentConfig struct {
	Backend string `yaml:"backend"`
	Model   string `yaml:"model"`
}

type WatchConfig struct {
	GitHub GitHubWatchConfig `yaml:"github"`
	Jira   JiraWatchConfig   `yaml:"jira"`
}

type GitHubWatchConfig struct {
	Repo  string `yaml:"repo"`
	Label string `yaml:"label"`
}

type JiraWatchConfig struct {
	Project string `yaml:"project"`
	Status  string `yaml:"status"`
}

type ExecutorConfig struct {
	MaxRetries      int           `yaml:"max_retries"`
	CommandTimeout  time.Duration `yaml:"command_timeout"`
	AllowedCommands []string      `yaml:"allowed_commands"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
