package sqlfn

import (
	"crypto/md5"  //nolint:gosec // non-cryptographic use: qlbridge-parity fingerprint factor (hash.md5)
	"crypto/sha1" //nolint:gosec // non-cryptographic use: qlbridge-parity fingerprint factor (hash.sha1)
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
)

// registerHashes adds the digest and base64 builtins (qlbridge: hash.md5,
// hash.sha1, hash.sha256, hash.sha512, encoding.b64encode, encoding.b64decode
// — dots are not identifier characters in this grammar, so the names use the
// HASH_ prefix / B64 forms, with the bare digest names as aliases).
func registerHashes() {
	register([]string{"MD5", "HASH_MD5"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		sum := md5.Sum([]byte(s)) //nolint:gosec // fingerprint factor, not for security
		return hex.EncodeToString(sum[:])
	})
	register([]string{"SHA1", "HASH_SHA1"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		sum := sha1.Sum([]byte(s)) //nolint:gosec // fingerprint factor, not for security
		return hex.EncodeToString(sum[:])
	})
	register([]string{"SHA256", "HASH_SHA256"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])
	})
	register([]string{"SHA512", "HASH_SHA512"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		sum := sha512.Sum512([]byte(s))
		return hex.EncodeToString(sum[:])
	})
	// B64ENCODE / B64DECODE: standard (RFC 4648) base64.
	register([]string{"B64ENCODE"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		return base64.StdEncoding.EncodeToString([]byte(s))
	})
	register([]string{"B64DECODE"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil
		}
		return string(b)
	})
}
