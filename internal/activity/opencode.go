package activity

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed opencode_plugin.mjs
var openCodePlugin []byte

const (
	openCodePluginFile = "opencode-plugin.mjs"
	openCodeConfigFile = "opencode-config.json"
)

// OpenCodeConfig writes the board's OpenCode plugin and a config document that
// loads it into cacheDir, and returns the config path to export as
// OPENCODE_CONFIG. Nothing here is per session: the plugin reads the session
// identity out of its pane's environment, and OpenCode merges this config's
// plugin list with the user's own rather than replacing it.
func OpenCodeConfig(cacheDir string) (string, error) {
	pluginPath := filepath.Join(cacheDir, openCodePluginFile)
	if err := os.WriteFile(pluginPath, openCodePlugin, 0o644); err != nil {
		return "", fmt.Errorf("activity.OpenCodeConfig: %w", err)
	}
	raw, err := json.Marshal(struct {
		Plugin []string `json:"plugin"`
	}{Plugin: []string{"file://" + pluginPath}})
	if err != nil {
		return "", fmt.Errorf("activity.OpenCodeConfig: %w", err)
	}
	configPath := filepath.Join(cacheDir, openCodeConfigFile)
	if err := os.WriteFile(configPath, raw, 0o644); err != nil {
		return "", fmt.Errorf("activity.OpenCodeConfig: %w", err)
	}
	return configPath, nil
}
