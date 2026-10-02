// Package bindkey 负责绑定密钥的生成、规范化和校验。
// 密钥长度 32 位，字符集用 Crockford Base32（去掉容易看错的 I/L/O/U）。
package bindkey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// New 生成一个新的明文密钥。20 字节正好编码成 32 个 Base32 字符。
func New() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return encode(b), nil
}

func encode(b []byte) string {
	var sb strings.Builder
	var buf, bits uint32
	for _, c := range b {
		buf = buf<<8 | uint32(c)
		bits += 8
		for bits >= 5 {
			bits -= 5
			sb.WriteByte(alphabet[(buf>>bits)&31])
		}
	}
	if bits > 0 {
		sb.WriteByte(alphabet[(buf<<(5-bits))&31])
	}
	return sb.String()
}

// Normalize 把用户输入的密钥统一成大写、去掉分隔符，并纠正易混字符。
func Normalize(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.NewReplacer("-", "", " ", "", "\t", "", "\n", "").Replace(s)
	s = strings.NewReplacer("I", "1", "L", "1", "O", "0", "U", "V").Replace(s)
	return s
}

func Valid(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune(alphabet, r) {
			return false
		}
	}
	return true
}

// Hash 对密钥做 SHA-256。密钥本身是 160 位随机值，不需要再加盐或慢哈希。
func Hash(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// Format 把密钥每 4 位分一组，方便展示和抄写。
func Format(plain string) string {
	var parts []string
	for i := 0; i < len(plain); i += 4 {
		end := i + 4
		if end > len(plain) {
			end = len(plain)
		}
		parts = append(parts, plain[i:end])
	}
	return strings.Join(parts, "-")
}

// Prefix 取前 6 位作为可读标识，后台列表里用。
func Prefix(plain string) string {
	if len(plain) <= 6 {
		return plain
	}
	return plain[:6]
}
