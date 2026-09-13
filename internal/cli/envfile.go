package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joho/godotenv"
)

// EnvFileName is the optional settings file read from the directory of the running executable.
const EnvFileName = ".env"

// lookupFunc returns the value of an environment variable and a label naming where it came from.
type lookupFunc func(name string) (value, source string, ok bool)

// layeredLookup resolves a variable from the process environment first, then from the values of
// the environment file at filePath. Empty values are treated as unset at both layers.
func layeredLookup(process func(string) (string, bool), file map[string]string, filePath string) lookupFunc {
	return func(name string) (string, string, bool) {
		if v, ok := process(name); ok && v != "" {
			return v, name, true
		}
		if v := file[name]; v != "" {
			return v, name + " (from " + filePath + ")", true
		}
		return "", "", false
	}
}

// readEnvFile parses the dotenv file at path without changing the process environment. A missing
// file yields no values and no error.
func readEnvFile(path string) (map[string]string, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("environment file: %w", err)
	}
	defer f.Close()
	values, err := godotenv.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("environment file %s: %w", path, err)
	}
	// godotenv accepts lines without "=" (empty key) and keys with spaces; reject both so a typo
	// does not silently drop a setting.
	for _, key := range sortedKeys(values) {
		if !validEnvKey(key) {
			return nil, fmt.Errorf("environment file %s: invalid variable name %q (want KEY=value)", path, key)
		}
	}
	return values, nil
}

func validEnvKey(key string) bool {
	if key == "" || key[0] >= '0' && key[0] <= '9' {
		return false
	}
	for _, r := range key {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// unknownArxgoKeys lists ARXGO_* variables in the file that name no flag of any operation, which
// usually means a misspelling.
func unknownArxgoKeys(values map[string]string) []string {
	known := map[string]bool{}
	for _, d := range flagTable {
		known[EnvName(d.name)] = true
	}
	var unknown []string
	for _, key := range sortedKeys(values) {
		if strings.HasPrefix(key, "ARXGO_") && !known[key] {
			unknown = append(unknown, key)
		}
	}
	return unknown
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// defaultEnvFile returns the .env path next to the running executable, with symlinks resolved, or
// "" when the executable location is unknown.
func defaultEnvFile() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Join(filepath.Dir(exe), EnvFileName)
}
