package config

import _ "embed"

//go:embed defaults/config.example.yaml
var DefaultConfig []byte
