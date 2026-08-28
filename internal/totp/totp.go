package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

func Secret() string {
	b := make([]byte, 20)
	rand.Read(b)
	return strings.TrimRight(base32.StdEncoding.EncodeToString(b), "=")
}

func URI(secret, account, issuer string) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=6&period=30",
		issuer, account, secret, issuer)
}

func Verify(secret, code string) bool {
	code = strings.TrimSpace(code)
	t := time.Now().Unix() / 30
	for i := int64(-1); i <= 1; i++ {
		if fmt.Sprintf("%06d", at(secret, t+i)) == code {
			return true
		}
	}
	return false
}

func at(secret string, counter int64) uint32 {
	s := secret
	if len(s)%8 != 0 {
		s += strings.Repeat("=", 8-len(s)%8)
	}
	key, err := base32.StdEncoding.DecodeString(strings.ToUpper(s))
	if err != nil {
		return 0
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return bin % 1000000
}
