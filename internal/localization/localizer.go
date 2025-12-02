package localization

import (
	"strings"

	"github.com/moehoshio/NekoLcServer/internal/config"
)

// Localizer delivers localized strings backed by language bundles.
type Localizer struct {
	fallback string
	bundles  map[string]config.LanguagePack
}

// New creates a Localizer with a fallback language code.
func New(defaultLang string, bundle config.LanguageBundle) *Localizer {
	cleaned := map[string]config.LanguagePack{}
	for code, pack := range bundle {
		cleaned[strings.ToLower(code)] = pack
	}
	return &Localizer{
		fallback: strings.ToLower(defaultLang),
		bundles:  cleaned,
	}
}

// ResolveCode returns a supported language code or the fallback if unknown.
func (l *Localizer) ResolveCode(lang string) string {
	code := strings.ToLower(strings.TrimSpace(lang))
	if _, ok := l.bundles[code]; ok {
		return code
	}
	return l.fallback
}

// Error returns a localized error message for the given key.
func (l *Localizer) Error(lang, key string) string {
	return l.lookup(lang, key, func(pack config.LanguagePack) map[string]string {
		return pack.Errors
	})
}

// Maintenance returns a localized maintenance string for the given key.
func (l *Localizer) Maintenance(lang, key string) string {
	return l.lookup(lang, key, func(pack config.LanguagePack) map[string]string {
		return pack.Maintenance
	})
}

// Updates returns a localized update string for the given key.
func (l *Localizer) Updates(lang, key string) string {
	return l.lookup(lang, key, func(pack config.LanguagePack) map[string]string {
		return pack.Updates
	})
}

func (l *Localizer) lookup(lang, key string, selector func(config.LanguagePack) map[string]string) string {
	resolved := l.ResolveCode(lang)
	if pack, ok := l.bundles[resolved]; ok {
		if values := selector(pack); values != nil {
			if msg, ok := values[key]; ok && msg != "" {
				return msg
			}
		}
	}
	if fallback, ok := l.bundles[l.fallback]; ok {
		if values := selector(fallback); values != nil {
			if msg, ok := values[key]; ok && msg != "" {
				return msg
			}
		}
	}
	return key
}
