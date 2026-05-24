package platform

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type EnvFlag struct {
	Name          string
	AltName       string
	NameCompat    string
	AltNameCompat string
}

func NewEnvFlag(name string) EnvFlag {
	nameCompat := name
	if strings.HasPrefix(nameCompat, "exclave.") {
		nameCompat = strings.Replace(nameCompat, "exclave.", "v2ray.", 1)
	}
	return EnvFlag{
		Name:          name,
		AltName:       NormalizeEnvName(name),
		NameCompat:    nameCompat,
		AltNameCompat: NormalizeEnvName(nameCompat),
	}
}

func (f EnvFlag) GetValue(defaultValue func() string) string {
	if v, found := os.LookupEnv(f.Name); found {
		return v
	}
	if len(f.AltName) > 0 {
		if v, found := os.LookupEnv(f.AltName); found {
			return v
		}
	}

	if v, found := os.LookupEnv(f.NameCompat); found {
		return v
	}
	if len(f.AltName) > 0 {
		if v, found := os.LookupEnv(f.AltNameCompat); found {
			return v
		}
	}

	return defaultValue()
}

func (f EnvFlag) GetValueAsInt(defaultValue int) int {
	useDefaultValue := false
	s := f.GetValue(func() string {
		useDefaultValue = true
		return ""
	})
	if useDefaultValue {
		return defaultValue
	}
	v, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return defaultValue
	}
	return int(v)
}

func NormalizeEnvName(name string) string {
	return strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(name)), ".", "_")
}

func getExecutableDir() string {
	exec, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exec)
}

func getExecutableSubDir(dir string) func() string {
	return func() string {
		return filepath.Join(getExecutableDir(), dir)
	}
}

func GetPluginDirectory() string {
	const name = "exclave.location.plugin"
	pluginDir := NewEnvFlag(name).GetValue(getExecutableSubDir("plugins"))
	return pluginDir
}

func GetConfigurationPath() string {
	const name = "exclave.location.config"
	configPath := NewEnvFlag(name).GetValue(getExecutableDir)
	return filepath.Join(configPath, "config.json")
}

// GetConfDirPath reads "v2ray.location.confdir"
func GetConfDirPath() string {
	const name = "exclave.location.confdir"
	configPath := NewEnvFlag(name).GetValue(func() string { return "" })
	return configPath
}
