# Application Configuration with Cobra + Viper

> **Frostyard override.** Frostyard CLIs get their configuration and common flags from `github.com/frostyard/clix`; do not add Viper layering in a consumer repository. See the `golang-cli` skill.

→ See `samber/cc-skills-golang@golang-cli` skill for complete Cobra+Viper setup, flag binding, precedence rules, and configuration layering.

## Where Config Lives

```
myapp/
├── cmd/myapp/
│   ├── main.go                # Entry point
│   ├── root.go                # Root command + Viper init
│   ├── serve.go               # Subcommand with flags
│   └── config.go              # Config struct + loader
└── configs/
    └── config.yaml            # Default config file
```

## Config Struct

Define configuration as a struct with `mapstructure` tags matching your YAML keys:

```go
// cmd/myapp/config.go
package main

import (
    "fmt"

    "github.com/spf13/viper"
)

type Config struct {
    Port     int    `mapstructure:"port"`
    Host     string `mapstructure:"host"`
    LogLevel string `mapstructure:"log-level"`
    Database struct {
        DSN     string `mapstructure:"dsn"`
        MaxConn int    `mapstructure:"max-conn"`
    } `mapstructure:"database"`
}

func loadConfig() (Config, error) {
    var cfg Config
    if err := viper.Unmarshal(&cfg); err != nil {
        return Config{}, fmt.Errorf("unmarshaling config: %w", err)
    }
    return cfg, nil
}
```

Configuration MUST be loaded from env vars, files, or flags — NEVER hardcoded. Sensitive values MUST come from env vars or secret managers, NEVER config files.
