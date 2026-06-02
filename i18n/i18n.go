package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"go.yaml.in/yaml/v3"
	"golang.org/x/sys/windows"
	"golang.org/x/text/language"
)

type I18n struct {
	bundle      *i18n.Bundle
	localizers  sync.Map     // map[string]*i18n.Localizer，Localizer 内部是 goroutine-safe
	defaultLang string       // Init 后只读
	systemLang  atomic.Value // 存 string，允许运行时被 SetSystemLang 覆盖
}

//go:embed locales/*
var localeFS embed.FS

// New 创建一个i18n实例
func New() *I18n {
	return &I18n{}
}

// Init 初始化i18n实例
func (i *I18n) Init() error {
	// 默认语言
	i.defaultLang = "en"
	// 检测系统语言
	i.systemLang.Store(i.detectSystemLang(i.defaultLang))

	i.bundle = i18n.NewBundle(language.Make(i.defaultLang))
	i.bundle.RegisterUnmarshalFunc("yml", yaml.Unmarshal)
	i.bundle.RegisterUnmarshalFunc("yaml", yaml.Unmarshal)
	i.bundle.RegisterUnmarshalFunc("json", json.Unmarshal)

	// 遍历所有文件
	if err := fs.WalkDir(localeFS, "locales", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if ext := strings.ToLower(filepath.Ext(path)); ext != ".yml" && ext != ".yaml" && ext != ".json" {
			return nil
		}
		bytes, err := localeFS.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := i.bundle.ParseMessageFileBytes(bytes, path); err != nil {
			return fmt.Errorf("parse message file %s: %w", path, err)
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

// Tran 翻译至对应的语言
func (i *I18n) Tran(messageID, lang string, data map[string]any) string {
	if i.bundle == nil {
		return fmt.Sprintf("[i18n not initialized] %s", messageID)
	}

	loc := i.getLocalizer(lang)
	msg, err := loc.Localize(&i18n.LocalizeConfig{
		MessageID:    messageID,
		TemplateData: data,
		PluralCount:  nil,
	})
	if err != nil {
		// return placeholder
		return fmt.Sprintf("[error translation: %v]", err)
	}
	return msg
}

// TranSys 使用系统语言进行翻译
func (i *I18n) TranSys(messageID string, data map[string]any) string {
	lang, _ := i.systemLang.Load().(string)
	return i.Tran(messageID, lang, data)
}

// SetSystemLang 手动覆盖检测到的系统语言（可选）
func (i *I18n) SetSystemLang(lang string) {
	i.systemLang.Store(lang)
}

// getLocalizer 获取指定语言的Localizer
func (i *I18n) getLocalizer(lang string) *i18n.Localizer {
	// canonicalize: replace _ with -, trim, lower-case，保证缓存 key 归一化
	lang = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(lang, "_", "-")))
	if lang == "" {
		lang = i.defaultLang
	}

	if cached, ok := i.localizers.Load(lang); ok {
		return cached.(*i18n.Localizer)
	}

	// 未命中再创建；LoadOrStore 保证多 goroutine 并发只有一个 Localizer 生效
	// 传入 defaultLang 作为兜底，避免目标语言文件缺失时返回错误占位符
	loc := i18n.NewLocalizer(i.bundle, lang, i.defaultLang)
	actual, _ := i.localizers.LoadOrStore(lang, loc)
	return actual.(*i18n.Localizer)
}

// detectSystemLang 优先读 LANG/LC_* 环境变量，否则调用 Win32 API 获取用户首选 UI 语言
func (i *I18n) detectSystemLang(defaultLang string) string {
	// 1. check common env vars
	envVars := []string{"LANG", "LC_ALL", "LC_MESSAGES"}
	for _, v := range envVars {
		if val := os.Getenv(v); val != "" {
			if parsed := i.parseLangFromEnv(val); parsed != "" {
				return parsed
			}
		}
	}
	// 2. windows API
	if langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME); err == nil && len(langs) > 0 {
		if parsed := i.parseLangFromEnv(langs[0]); parsed != "" {
			return parsed
		}
	}
	return defaultLang
}

// parseLangFromEnv 解析变量值中的语言
// 传入和返回示例：
// "zh_CN.UTF-8" -> "zh"
// "en-US" -> "en"
// "zh-CN" -> "zh"
func (i *I18n) parseLangFromEnv(raw string) string {
	r := strings.TrimSpace(raw)
	r = strings.ToLower(r)
	// remove encoding suffixes
	if idx := strings.Index(r, "."); idx != -1 {
		r = r[:idx]
	}
	// get first component before @ (variant)
	if idx := strings.Index(r, "@"); idx != -1 {
		r = r[:idx]
	}
	// split by non-letter
	r = strings.ReplaceAll(r, "_", "-")
	parts := strings.Split(r, "-")
	if len(parts) == 0 {
		return ""
	}
	primary := parts[0]
	if primary == "" {
		return ""
	}
	return primary // e.g., "zh" or "en"
}
