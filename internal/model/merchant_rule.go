package model

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	json "github.com/bytedance/sonic"
	"github.com/samber/lo"

	"tcg-rulex-engine/pkg/logs"
)

// MerchantRule maps to TCG_UCS.MERCHANT_RULE table.
//
// parsedQuestions and parsedTranslations are lazy-initialised caches for the
// QUESTIONS and FIELD_TRANSLATIONS CLOB columns.  They are populated on first
// access and reused for all subsequent calls within the same MerchantRule
// instance, eliminating redundant JSON unmarshal work in the hot verification
// path.  The caches are not protected by a mutex because MerchantRule instances
// are not shared across goroutines after they are loaded from the DB.
type MerchantRule struct {
	CreatedAt          time.Time            `db:"CREATED_AT" json:"createdAt"`
	UpdatedAt          time.Time            `db:"UPDATED_AT" json:"updatedAt"`
	parsedQuestions    map[string]*Question `db:"-" json:"-"`
	parsedTranslations FieldTranslationsMap `db:"-" json:"-"`
	BindingType        string               `db:"BINDING_TYPE" json:"bindingType"`
	MerchantCode       string               `db:"MERCHANT_CODE" json:"merchantCode"`
	Operator           string               `db:"OPERATOR" json:"operator"`
	Questions          string               `db:"QUESTIONS"  json:"questions"`
	FieldTranslations  string               `db:"FIELD_TRANSLATIONS" json:"fieldTranslations"`
	ID                 int64                `db:"ID"  json:"ID"`
	EmptyScore         int32                `db:"EMPTY_SCORE" json:"emptyScore"`
	AccountRetryLimit  int32                `db:"ACCOUNT_RETRY_LIMIT" json:"accountRetryLimit"`
	IPRetryLimit       int32                `db:"IP_RETRY_LIMIT" json:"ipRetryLimit"`
	PassingScore       int32                `db:"PASSING_SCORE" json:"passingScore"`
	LockHour           int32                `db:"LOCK_HOUR" json:"lockHour"`
	IsDefault          int8                 `db:"IS_DEFAULT" json:"isDefault"`
}

// Question represents a single verification question configuration stored in the QUESTIONS CLOB.
type Question struct {
	FieldID        string `json:"fieldId"`
	FieldName      string `json:"fieldName"`
	FieldAttribute string `json:"fieldAttribute"`
	FieldType      string `json:"fieldType"`
	Accuracy       string `json:"accuracy"`
	Score          int32  `json:"score"`
	Valid          bool   `json:"valid"`
}

// QuestionInfo is the public-facing question shape returned to callers.
type QuestionInfo struct {
	FieldID           string         `json:"fieldId"`
	FieldName         string         `json:"fieldName"`
	FieldAttribute    string         `json:"fieldAttribute"`
	FieldType         string         `json:"fieldType"`
	FieldDropdownList []DropdownItem `json:"fieldDropdownList"`
}

type FieldIdTranslation struct {
	FieldID          string `json:"fieldId"`
	FieldTranslation string `json:"fieldTranslation"`
}

type FieldTranslationsMap map[string][]FieldIdTranslation

// parseQuestionsJSON deserializes the QUESTIONS CLOB into a map keyed by fieldId.
// It is the shared implementation used by both MerchantRule and MerchantRuleConfig.
func parseQuestionsJSON(raw string) (map[string]*Question, error) {
	if raw == "" {
		return nil, fmt.Errorf("questions field is empty")
	}
	var result map[string]*Question
	if err := json.UnmarshalString(raw, &result); err != nil {
		return nil, fmt.Errorf("unmarshal questions failed: %w", err)
	}
	return result, nil
}

// ParseQuestions deserializes the QUESTIONS CLOB into a map keyed by fieldId.
// The result is cached after the first parse: subsequent calls within the same
// MerchantRule instance return the cached map without re-allocating.
func (m *MerchantRule) ParseQuestions() (map[string]*Question, error) {
	if m.parsedQuestions != nil {
		return m.parsedQuestions, nil
	}
	q, err := parseQuestionsJSON(m.Questions)
	if err != nil {
		return nil, err
	}
	m.parsedQuestions = q
	return q, nil
}

// ParseValidQuestions returns only questions with Valid == true.
func (m *MerchantRule) ParseValidQuestions() (map[string]*Question, error) {
	all, err := m.ParseQuestions()
	if err != nil {
		return nil, err
	}
	return lo.PickBy(all, func(_ string, q *Question) bool {
		return q != nil && q.Valid
	}), nil
}

// GetValidQuestionInfos returns the public question shapes for all valid questions.
func (m *MerchantRule) GetValidQuestionInfos() ([]QuestionInfo, error) {
	all, err := m.ParseQuestions()
	if err != nil {
		return nil, err
	}

	result := make([]QuestionInfo, 0, len(all))
	for _, q := range all {
		if q == nil || !q.Valid || q.FieldID == "" {
			continue
		}
		result = append(result, QuestionInfo{
			FieldID:        q.FieldID,
			FieldName:      q.FieldName,
			FieldAttribute: q.FieldAttribute,
			FieldType:      q.FieldType,
		})
	}
	slices.SortFunc(result, func(a, b QuestionInfo) int {
		return cmp.Compare(a.FieldID, b.FieldID)
	})
	return result, nil
}

// MarshalQuestions serializes a question map back into the QUESTIONS CLOB field.
func (m *MerchantRule) MarshalQuestions(questions map[string]*Question) error {
	b, err := json.MarshalString(questions)
	if err != nil {
		return fmt.Errorf("marshal questions failed: %w", err)
	}
	m.Questions = b
	return nil
}

// ParseFieldTranslationsMap deserializes the FIELD_TRANSLATIONS CLOB into a FieldTranslationsMap.
// JSON 结构: { "EN": [{fieldId, fieldTranslation}, ...], "ZH": [...], ... }
// Always returns a non-nil map; if FieldTranslations is empty or invalid, returns an empty map.
// The result is cached after the first parse to avoid redundant JSON allocations in the hot path.
func (m *MerchantRule) ParseFieldTranslationsMap() FieldTranslationsMap {
	if m.parsedTranslations != nil {
		return m.parsedTranslations
	}
	if m.FieldTranslations == "" || m.FieldTranslations == "{}" {
		m.parsedTranslations = make(FieldTranslationsMap)
		return m.parsedTranslations
	}
	var result FieldTranslationsMap
	if err := json.UnmarshalString(m.FieldTranslations, &result); err != nil {
		m.parsedTranslations = make(FieldTranslationsMap)
		return m.parsedTranslations
	}
	m.parsedTranslations = result
	return result
}

// GetTranslationsByLanguage returns a fieldId → fieldTranslation map for the given language.
// Falls back to "EN" if the requested language is not found; returns an empty map if neither exists.
func (m *MerchantRule) GetTranslationsByLanguage(ctx context.Context, language string) map[string]string {
	start := time.Now()
	all := m.ParseFieldTranslationsMap()
	list, ok := all[language]
	fallback := ""
	if !ok {
		list = all["EN"]
		fallback = " (fallback to EN)"
	}
	result := make(map[string]string, len(list))
	for _, item := range list {
		result[item.FieldID] = item.FieldTranslation
	}
	logs.Info(ctx, "GetTranslationsByLanguage language=%s%s, fields=%d, elapsed=%dms",
		language, fallback, len(result), time.Since(start).Milliseconds())
	return result
}

// MerchantRuleConfig holds the core rule fields used during the verification flow.
//
// parsedQuestions is a lazy-initialised cache for the QUESTIONS CLOB column,
// mirroring the same pattern used by MerchantRule.  It is populated on the
// first call to ParseQuestions and reused for all subsequent calls within the
// same instance, eliminating redundant JSON unmarshal work if the call pattern
// ever expands beyond the current single call per request.
// The cache is not protected by a mutex because MerchantRuleConfig instances
// are not shared across goroutines after they are loaded from the DB.
type MerchantRuleConfig struct {
	parsedQuestions   map[string]*Question `db:"-" json:"-"`
	MerchantCode      string               `db:"MERCHANT_CODE"      json:"merchantCode"`
	BindingType       string               `db:"BINDING_TYPE"        json:"bindingType"`
	Questions         string               `db:"QUESTIONS"           json:"questions"`
	FieldTranslations string               `db:"FIELD_TRANSLATIONS"  json:"fieldTranslations"`
	EmptyScore        int32                `db:"EMPTY_SCORE"         json:"emptyScore"`
	PassingScore      int32                `db:"PASSING_SCORE"       json:"passingScore"`
}

// ParseQuestions deserializes the QUESTIONS CLOB into a map keyed by fieldId.
// The result is cached after the first parse: subsequent calls within the same
// MerchantRuleConfig instance return the cached map without re-allocating.
func (c *MerchantRuleConfig) ParseQuestions() (map[string]*Question, error) {
	if c.parsedQuestions != nil {
		return c.parsedQuestions, nil
	}
	q, err := parseQuestionsJSON(c.Questions)
	if err != nil {
		return nil, fmt.Errorf("%w for merchant: %s", err, c.MerchantCode)
	}
	c.parsedQuestions = q
	return q, nil
}
