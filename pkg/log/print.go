package log

import (
	"fmt"
	"strings"
)

// Sprint 格式化参数，每个参数之间隔一个空格，返回格式化后的字符串。
//
// 比 fmt.Sprint 更可控：fmt.Sprint 仅在"相邻两个操作数都不是字符串"时才插入空格，
// 因此 fmt.Sprint("a", 1, "b") 会得到 "a1b" 这类意外结果。
func Sprint(args ...any) string {
	if len(args) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.Grow(len(args) * 16)
	for i, arg := range args {
		if i > 0 {
			builder.WriteByte(' ')
		}
		fmt.Fprint(&builder, arg)
	}
	return builder.String()
}
