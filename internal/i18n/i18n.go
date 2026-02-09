// Package i18n provides embedded translations (de, en, fr, nl). Locale from LANG/LC_ALL/LANGUAGE; fallback en (British English).
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

//go:embed translations/de.json translations/en.json translations/fr.json translations/nl.json
var embedFS embed.FS

var (
	mu       sync.RWMutex
	messages map[string]string
	lang     string
)

// Supported languages: de, en (British English), fr, nl.
const (
	LangDE = "de"
	LangEN = "en"
	LangFR = "fr"
	LangNL = "nl"
)

func init() {
	lang = detectLang()
	loadLang(lang)
}

// detectLang reads LC_ALL, LANG, LANGUAGE (first part) and maps to de/en/fr/nl; unknown → en (British English).
func detectLang() string {
	for _, env := range []string{"LC_ALL", "LANG", "LANGUAGE"} {
		v := os.Getenv(env)
		if v == "" {
			continue
		}
		// "de_DE.UTF-8" -> "de", "en_GB" -> "en"
		part := v
		if i := strings.IndexAny(part, "._@"); i >= 0 {
			part = part[:i]
		}
		part = strings.ToLower(strings.TrimSpace(part))
		switch part {
		case "de", "en", "fr", "nl":
			return part
		case "en_gb", "en_us", "en_au":
			return LangEN
		}
	}
	return LangEN
}

func loadLang(l string) {
	mu.Lock()
	defer mu.Unlock()
	lang = l
	messages = nil
	data, err := embedFS.ReadFile("translations/" + l + ".json")
	if err != nil && l != LangEN {
		data, err = embedFS.ReadFile("translations/" + LangEN + ".json")
		if err == nil {
			lang = LangEN
		}
	}
	if err != nil {
		messages = make(map[string]string)
		return
	}
	_ = json.Unmarshal(data, &messages)
	if messages == nil {
		messages = make(map[string]string)
	}
}

// Lang returns the current language code (de, en, fr, nl).
func Lang() string {
	mu.RLock()
	defer mu.RUnlock()
	return lang
}

// T returns the translation for key; if missing, returns key. No formatting.
func T(key string) string {
	mu.RLock()
	m := messages
	mu.RUnlock()
	if m == nil {
		return key
	}
	if s, ok := m[key]; ok && s != "" {
		return s
	}
	return key
}

// Tf returns the translation for key with fmt-style formatting (e.g. %s, %d).
func Tf(key string, a ...interface{}) string {
	return fmt.Sprintf(T(key), a...)
}
