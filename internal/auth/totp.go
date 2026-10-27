package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 specifies HMAC-SHA1; authenticator apps expect it
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	totpPeriod = 30
	totpDigits = 6
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random 160-bit secret in the base32 form authenticator apps use.
func NewTOTPSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b32.EncodeToString(b)
}

// TOTPURI is the otpauth:// link (shown as a QR code by most apps' import dialogs).
func TOTPURI(issuer, account, secret string) string {
	q := url.Values{"secret": {secret}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + q.Encode()
}

func totpAt(secret string, step int64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, n%1_000_000), nil
}

// VerifyTOTP checks a code against the current step and one step either side (clock drift). It returns the
// matching step, which callers store so the same code cannot be used twice (replay protection).
func VerifyTOTP(secret, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, false
	}
	cur := now.Unix() / totpPeriod
	for _, step := range []int64{cur, cur - 1, cur + 1} {
		want, err := totpAt(secret, step)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 && step > lastStep {
			return step, true
		}
	}
	return 0, false
}

// TOTPCode is the code for a time; used by tests and nothing else.
func TOTPCode(secret string, at time.Time) string {
	c, _ := totpAt(secret, at.Unix()/totpPeriod)
	return c
}

// NewRecoveryCodes returns n single-use codes like "k3f9-a82d-1x0q".
func NewRecoveryCodes(n int) []string {
	out := make([]string, n)
	for i := range out {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			panic(err)
		}
		s := strings.ToLower(b32.EncodeToString(b))[:12]
		out[i] = s[0:4] + "-" + s[4:8] + "-" + s[8:12]
	}
	return out
}

// HashRecoveryCode normalises and hashes a recovery code for storage and lookup.
func HashRecoveryCode(code string) string {
	return hashToken(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), "-", "")))
}
