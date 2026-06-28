package masking

import (
	"strings"

	json "github.com/bytedance/sonic"
)

// SensitiveFields 因安全问题, 控制打印非完整日志 — 字段集合统一由 pkg/masking 维护
// SensitiveFields is the set of fieldId values whose logged content must be
// partially redacted to prevent personal or financial data from appearing in
// plain-text log files.
var SensitiveFields = map[string]struct{}{
	"ID":                     {},
	"BANK_ACCOUNT":           {},
	"CARD_HOLDER_NAME":       {},
	"VIRTUAL_WALLET_ADDRESS": {},
	"VIRTUAL_WALLET_NAME":    {},
	"E_WALLET_ACCOUNT":       {},
	"E_WALLET_NAME":          {},
	"QQ":                     {},
	"WECHAT_ID":              {},
	"LINE_ID":                {},
	"FB_ID":                  {},
	"WHATSAPP":               {},
	"ZALO":                   {},
	"TELEGRAM":               {},
	"VIBER":                  {},
	"TWITTER":                {},
	"EMAIL":                  {},
	"MOBILE_NUMBER":          {},
	"WITHDRAWER_NAME":        {},
	"APPLE_ID":               {},
	"KAKAO":                  {},
	"GOOGLE":                 {},
}

// Value returns value unchanged when fieldId is not in SensitiveFields.
// When fieldId is sensitive and the rune-length of value exceeds 5, the first
// 5 runes are kept and every subsequent rune is replaced with '*', so the
// masked length matches the original (multi-byte characters count as one rune).
func Value(fieldId, value string) string {
	if _, ok := SensitiveFields[fieldId]; !ok {
		return value
	}
	// Single pass: count total runes and capture the byte offset of the 6th rune.
	// Ranging over a string yields byte indices with no []rune allocation.
	count, split := 0, len(value)
	for i := range value {
		count++
		if count == 6 {
			split = i // byte start of the 6th rune = end of the first 5
		}
	}
	if count <= 5 {
		return value
	}
	var b strings.Builder
	b.Grow(split + count - 5) // exact: front bytes + one '*' per masked rune
	b.WriteString(value[:split])
	for range count - 5 {
		b.WriteByte('*')
	}
	return b.String()
}

// verifyItem mirrors req.VerifyItem for JSON parsing without importing the
// internal request type into this shared package.
type verifyItem struct {
	FieldID    string `json:"fieldId"`
	FieldValue string `json:"fieldValue"`
}

type verifyDataItem struct {
	Item verifyItem `json:"item"`
	Bind bool       `json:"bind"`
}

type submitVerifyRequest struct {
	CustomerName string           `json:"customerName"`
	Data         []verifyDataItem `json:"data"`
}

// RequestBody parses a SubmitVerifyRequest JSON body, masks sensitive fieldValue
// entries in place, and returns the re-serialised JSON.  The original body is
// returned unchanged if parsing fails so the caller always has loggable output.
func RequestBody(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	var r submitVerifyRequest
	if err := json.Unmarshal(body, &r); err != nil {
		return body
	}
	for i := range r.Data {
		r.Data[i].Item.FieldValue = Value(r.Data[i].Item.FieldID, r.Data[i].Item.FieldValue)
	}
	masked, err := json.Marshal(r)
	if err != nil {
		return body
	}
	return masked
}
