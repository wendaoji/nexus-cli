// Package i18n 提供中英双语支持。
//
// 设计：源码里的文案以英文书写，中文放在 catalog 里按英文原文索引。
// 因此新增文案时即使忘了翻译，也只是显示英文，不会显示键名或空白。
//
// 语言选择优先级：
//
//	--lang 参数 > NEXUS_CLI_LANG > LC_ALL > LC_MESSAGES > LANG > 默认（en）
//
// 值以 "zh" 开头即视为中文，其余为英文。
package i18n

import (
	"fmt"
	"os"
	"strings"
)

// Lang 是支持的语言。
type Lang string

const (
	EN Lang = "en"
	ZH Lang = "zh"
)

var current = EN

// Detect 按优先级推断语言，explicit 为命令行参数（空表示未指定）。
func Detect(explicit string) Lang {
	for _, v := range []string{explicit, os.Getenv("NEXUS_CLI_LANG"), os.Getenv("LC_ALL"), os.Getenv("LC_MESSAGES"), os.Getenv("LANG")} {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(v), "zh") {
			return ZH
		}
		return EN
	}
	return EN
}

// Set 设定当前语言（explicit 为空时按环境推断）。
func Set(explicit string) { current = Detect(explicit) }

// Current 返回当前语言。
func Current() Lang { return current }

// IsZH 表示当前是否中文。
func IsZH() bool { return current == ZH }

// T 翻译一段文案；args 走 fmt.Sprintf。
func T(s string, args ...any) string {
	if current == ZH {
		if zh, ok := catalog[s]; ok {
			s = zh
		}
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// H 返回多行帮助文本：中文环境优先用 key 对应的中文块，否则用英文原文。
func H(key, english string) string {
	if current == ZH {
		if zh, ok := helpText[key]; ok {
			return zh
		}
	}
	return english
}
