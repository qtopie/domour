package plugin

import (
	"fmt"
	"path/filepath"
	"plugin"
	"sync"
)

const (
	APP_DATA_DIR = "/opt/homa"
)

// PluginManager manages dynamically loaded plugins
type PluginManager struct {
	mu       sync.Mutex
	plugins  map[string]map[string]interface{} // category -> plugin name -> plugin instance
	basePath string                            // Base directory for plugins
}

// NewPluginManager creates a new PluginManager
func NewPluginManager(basePath string) *PluginManager {
	return &PluginManager{
		plugins:  make(map[string]map[string]interface{}),
		basePath: basePath,
	}
}

// LoadPlugin dynamically loads a plugin by category and name
func (pm *PluginManager) LoadPlugin(category, pluginName string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Ensure the category map exists
	if _, exists := pm.plugins[category]; !exists {
		pm.plugins[category] = make(map[string]interface{})
	}

	// Construct the plugin file path
	pluginPath := filepath.Join(pm.basePath, category, pluginName+".so")

	// Open the plugin file
	p, err := plugin.Open(pluginPath)
	if err != nil {
		return fmt.Errorf("failed to open plugin %s: %w", pluginName, err)
	}

	// Lookup the exported symbol "Plugin"
	symbol, err := p.Lookup("Plugin")
	if err != nil {
		return fmt.Errorf("failed to find symbol 'Plugin' in %s: %w", pluginName, err)
	}

	// Register the plugin under the category
	pm.plugins[category][pluginName] = symbol
	fmt.Printf("Plugin %s loaded successfully under category %s\n", pluginName, category)
	return nil
}

// GetPlugin retrieves a loaded plugin by category and name
func (pm *PluginManager) GetPlugin(category, pluginName string) (interface{}, bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pluginsInCategory, exists := pm.plugins[category]
	if !exists {
		return nil, false
	}

	plugin, exists := pluginsInCategory[pluginName]
	return plugin, exists
}

// ListPlugins lists all loaded plugins by category
func (pm *PluginManager) ListPlugins() map[string][]string {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	result := make(map[string][]string)
	for category, plugins := range pm.plugins {
		for pluginName := range plugins {
			result[category] = append(result[category], pluginName)
		}
	}
	return result
}
