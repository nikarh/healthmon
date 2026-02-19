//go:build dev

package config

func defaultWSOriginPatterns() []string {
	return []string{"localhost:*", "127.0.0.1:*"}
}
