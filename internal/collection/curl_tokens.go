package collection

import (
	"errors"
	"strconv"
	"strings"
)

// curl 命令的词法切分：'…'（不处理转义）、"…"（只认 \" 与 \\）、$'…'（ANSI-C 转义）、
// 行尾 \ 与 ^ 续行、空白分隔。返回的词元已去掉引号，等价于 shell 交给 curl 的 argv。

func isCurlLineBreak(s string, i int) bool {
	return i < len(s) && (s[i] == '\n' || s[i] == '\r')
}

// skipCurlLineBreak 跳过 i 处的换行（兼容 CRLF），返回换行序列的最后一个字节下标。
func skipCurlLineBreak(s string, i int) int {
	if i+1 < len(s) && s[i] == '\r' && s[i+1] == '\n' {
		return i + 1
	}
	return i
}

func tokenizeCurl(s string) ([]string, error) {
	var (
		toks []string
		cur  strings.Builder
		open bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case (c == '\\' || c == '^') && isCurlLineBreak(s, i+1):
			i = skipCurlLineBreak(s, i+1)
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if open {
				toks = append(toks, cur.String())
				cur.Reset()
				open = false
			}
		case c == '\'':
			text, next, err := readCurlSingleQuoted(s, i)
			if err != nil {
				return nil, err
			}
			cur.WriteString(text)
			open, i = true, next
		case c == '"':
			text, next, err := readCurlDoubleQuoted(s, i)
			if err != nil {
				return nil, err
			}
			cur.WriteString(text)
			open, i = true, next
		case c == '$' && i+1 < len(s) && s[i+1] == '\'':
			text, next, err := readCurlAnsiQuoted(s, i)
			if err != nil {
				return nil, err
			}
			cur.WriteString(text)
			open, i = true, next
		default:
			cur.WriteByte(c)
			open = true
		}
	}
	if open {
		toks = append(toks, cur.String())
	}
	return toks, nil
}

func readCurlSingleQuoted(s string, i int) (string, int, error) {
	j := strings.IndexByte(s[i+1:], '\'')
	if j < 0 {
		return "", 0, errors.New("curl 命令里单引号未闭合")
	}
	return s[i+1 : i+1+j], i + 1 + j, nil
}

func readCurlDoubleQuoted(s string, i int) (string, int, error) {
	var b strings.Builder
	for j := i + 1; j < len(s); j++ {
		switch {
		case s[j] == '"':
			return b.String(), j, nil
		case s[j] == '\\' && j+1 < len(s) && (s[j+1] == '"' || s[j+1] == '\\'):
			b.WriteByte(s[j+1])
			j++
		default:
			b.WriteByte(s[j])
		}
	}
	return "", 0, errors.New("curl 命令里双引号未闭合")
}

func readCurlAnsiQuoted(s string, i int) (string, int, error) {
	var b strings.Builder
	for j := i + 2; j < len(s); j++ {
		switch {
		case s[j] == '\'':
			return b.String(), j, nil
		case s[j] == '\\' && j+1 < len(s):
			text, used := curlAnsiEscape(s[j+1:])
			b.WriteString(text)
			j += used
		default:
			b.WriteByte(s[j])
		}
	}
	return "", 0, errors.New("curl 命令里 $'…' 未闭合")
}

// curlAnsiEscape 解一个 ANSI-C 转义（入参从反斜杠后的字符开始），返回替换文本与消费字节数。
func curlAnsiEscape(s string) (string, int) {
	if s == "" {
		return "", 1
	}
	switch s[0] {
	case 'n':
		return "\n", 1
	case 'r':
		return "\r", 1
	case 't':
		return "\t", 1
	case '0':
		return "\x00", 1
	case '\\':
		return "\\", 1
	case '\'':
		return "'", 1
	case '"':
		return "\"", 1
	case 'x':
		if len(s) >= 3 {
			if v, err := strconv.ParseUint(s[1:3], 16, 8); err == nil {
				return string(rune(v)), 3
			}
		}
	case 'u':
		if len(s) >= 5 {
			if v, err := strconv.ParseUint(s[1:5], 16, 32); err == nil {
				return string(rune(v)), 5
			}
		}
	}
	return string(s[0:1]), 1
}
