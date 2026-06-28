// merchant_rule_test.go — 商户问卷规则 / 字段翻译解析（外围业务模块）。
//
// 运行 / Run:  go test ./internal/model/ -run MerchantRule -v
// 用例 / Cases: ParseQuestions、有效问题过滤/GetValidQuestionInfos、序列化 round-trip、
//   ParseFieldTranslationsMap、GetTranslationsByLanguage(精确/回退 EN/空/非法 JSON)。

package model_test

import (
	"context"
	"strings"
	"testing"

	"tcg-rulex-engine/internal/model"
)

// validQuestionsJSON contains 3 questions: 2 valid, 1 invalid.
const validQuestionsJSON = `{
	"MOBILE_NUMBER": {"fieldId":"MOBILE_NUMBER","fieldName":"Mobile","fieldAttribute":"I","fieldType":"Social","accuracy":"exact","score":10,"valid":true},
	"EMAIL":         {"fieldId":"EMAIL",         "fieldName":"Email", "fieldAttribute":"I","fieldType":"Social","accuracy":"exact","score":8, "valid":true},
	"NICKNAME":      {"fieldId":"NICKNAME",      "fieldName":"Nickname","fieldAttribute":"I","fieldType":"Social","accuracy":"exact","score":5,"valid":false}
}`

// ─── MerchantRule.ParseQuestions ─────────────────────────────────────────────

func TestMerchantRule_ParseQuestions(t *testing.T) {
	tests := []struct {
		name      string
		questions string
		wantLen   int
		wantErr   bool
	}{
		{"valid JSON with 3 questions", validQuestionsJSON, 3, false},
		{"empty string returns error", "", 0, true},
		{"invalid JSON returns error", "{bad-json", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &model.MerchantRule{Questions: tt.questions}
			got, err := m.ParseQuestions()
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseQuestions() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(got) != tt.wantLen {
				t.Errorf("ParseQuestions() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

// ─── MerchantRule.ParseValidQuestions ────────────────────────────────────────

func TestMerchantRule_ParseValidQuestions(t *testing.T) {
	m := &model.MerchantRule{Questions: validQuestionsJSON}
	got, err := m.ParseValidQuestions()
	if err != nil {
		t.Fatalf("ParseValidQuestions() unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2 (NICKNAME has valid=false)", len(got))
	}
	if _, ok := got["MOBILE_NUMBER"]; !ok {
		t.Error("expected MOBILE_NUMBER in result")
	}
	if _, ok := got["EMAIL"]; !ok {
		t.Error("expected EMAIL in result")
	}
	if _, ok := got["NICKNAME"]; ok {
		t.Error("NICKNAME (valid=false) must be excluded")
	}
}

func TestMerchantRule_ParseValidQuestions_EmptyInput(t *testing.T) {
	m := &model.MerchantRule{}
	_, err := m.ParseValidQuestions()
	if err == nil {
		t.Error("expected error for empty Questions")
	}
}

// ─── MerchantRule.GetValidQuestionInfos ──────────────────────────────────────

func TestMerchantRule_GetValidQuestionInfos(t *testing.T) {
	m := &model.MerchantRule{Questions: validQuestionsJSON}
	got, err := m.GetValidQuestionInfos()
	if err != nil {
		t.Fatalf("GetValidQuestionInfos() unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
	for _, qi := range got {
		if qi.FieldID == "" {
			t.Error("QuestionInfo.FieldId must not be empty")
		}
		if qi.FieldID == "NICKNAME" {
			t.Error("NICKNAME (valid=false) must not appear in results")
		}
	}
}

// ─── MerchantRule.MarshalQuestions ───────────────────────────────────────────

func TestMerchantRule_MarshalQuestions_RoundTrip(t *testing.T) {
	input := map[string]*model.Question{
		"MOBILE_NUMBER": {
			FieldID:        "MOBILE_NUMBER",
			FieldName:      "Mobile",
			FieldAttribute: "I",
			FieldType:      "Social",
			Score:          10,
			Valid:          true,
		},
		"EMAIL": {
			FieldID:   "EMAIL",
			FieldName: "Email",
			Score:     8,
			Valid:     true,
		},
	}

	m := &model.MerchantRule{}
	if err := m.MarshalQuestions(input); err != nil {
		t.Fatalf("MarshalQuestions() error = %v", err)
	}
	if m.Questions == "" {
		t.Fatal("Questions field must not be empty after MarshalQuestions")
	}

	// Round-trip: parse back and verify content.
	got, err := m.ParseQuestions()
	if err != nil {
		t.Fatalf("ParseQuestions() after MarshalQuestions error = %v", err)
	}
	if len(got) != 2 {
		t.Errorf("round-trip len = %d, want 2", len(got))
	}
	if q := got["MOBILE_NUMBER"]; q == nil || q.FieldName != "Mobile" {
		t.Errorf("MOBILE_NUMBER.FieldName = %v, want Mobile", q)
	}
	if q := got["EMAIL"]; q == nil || q.Score != 8 {
		t.Errorf("EMAIL.Score = %v, want 8", q)
	}
}

// ─── MerchantRuleConfig.ParseQuestions ───────────────────────────────────────

func TestMerchantRuleConfig_ParseQuestions(t *testing.T) {
	t.Run("valid questions JSON", func(t *testing.T) {
		cfg := &model.MerchantRuleConfig{
			MerchantCode: "test_merchant",
			Questions:    validQuestionsJSON,
		}
		got, err := cfg.ParseQuestions()
		if err != nil {
			t.Fatalf("ParseQuestions() error = %v", err)
		}
		if len(got) != 3 {
			t.Errorf("len = %d, want 3", len(got))
		}
	})

	t.Run("empty Questions string", func(t *testing.T) {
		cfg := &model.MerchantRuleConfig{MerchantCode: "test_merchant"}
		_, err := cfg.ParseQuestions()
		if err == nil {
			t.Error("expected error for empty Questions")
		}
		if !strings.Contains(err.Error(), "test_merchant") {
			t.Errorf("error should mention merchantCode, got: %v", err)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		cfg := &model.MerchantRuleConfig{
			MerchantCode: "test_merchant",
			Questions:    "not-valid-json",
		}
		_, err := cfg.ParseQuestions()
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

// ══════════════════════════════════════════════════════════════════════════════
// ParseFieldTranslationsMap
// ══════════════════════════════════════════════════════════════════════════════

const fieldTranslationsJSON = `{
	"EN": [
		{"fieldId": "MOBILE_NUMBER", "fieldTranslation": "Mobile Number"},
		{"fieldId": "EMAIL",         "fieldTranslation": "Email Address"}
	],
	"ZH": [
		{"fieldId": "MOBILE_NUMBER", "fieldTranslation": "手机号码"},
		{"fieldId": "EMAIL",         "fieldTranslation": "电子邮箱"}
	]
}`

func TestParseFieldTranslationsMap_ValidJSON(t *testing.T) {
	m := &model.MerchantRule{FieldTranslations: fieldTranslationsJSON}
	result := m.ParseFieldTranslationsMap()
	if result == nil {
		t.Fatal("result must not be nil")
	}
	if len(result) != 2 {
		t.Errorf("language count: got %d, want 2", len(result))
	}
	en := result["EN"]
	if len(en) != 2 {
		t.Fatalf("EN translation count: got %d, want 2", len(en))
	}
	if en[0].FieldID != "MOBILE_NUMBER" && en[1].FieldID != "MOBILE_NUMBER" {
		t.Error("EN should contain MOBILE_NUMBER")
	}
}

func TestParseFieldTranslationsMap_EmptyString(t *testing.T) {
	m := &model.MerchantRule{FieldTranslations: ""}
	result := m.ParseFieldTranslationsMap()
	if result == nil {
		t.Fatal("result must not be nil (should be empty map)")
	}
	if len(result) != 0 {
		t.Errorf("length: got %d, want 0", len(result))
	}
}

func TestParseFieldTranslationsMap_EmptyObject(t *testing.T) {
	m := &model.MerchantRule{FieldTranslations: "{}"}
	result := m.ParseFieldTranslationsMap()
	if result == nil {
		t.Fatal("result must not be nil")
	}
	if len(result) != 0 {
		t.Errorf("length: got %d, want 0", len(result))
	}
}

func TestParseFieldTranslationsMap_InvalidJSON(t *testing.T) {
	m := &model.MerchantRule{FieldTranslations: "{bad-json"}
	result := m.ParseFieldTranslationsMap()
	if result == nil {
		t.Fatal("result must not be nil (should be empty map on error)")
	}
	if len(result) != 0 {
		t.Errorf("length: got %d, want 0 (error → empty map)", len(result))
	}
}

// ══════════════════════════════════════════════════════════════════════════════
// GetTranslationsByLanguage
// ══════════════════════════════════════════════════════════════════════════════

func TestGetTranslationsByLanguage_ExactLanguage(t *testing.T) {
	m := &model.MerchantRule{FieldTranslations: fieldTranslationsJSON}
	result := m.GetTranslationsByLanguage(context.Background(), "ZH")
	if len(result) != 2 {
		t.Fatalf("field count: got %d, want 2", len(result))
	}
	if result["MOBILE_NUMBER"] != "手机号码" {
		t.Errorf("MOBILE_NUMBER: got %q, want 手机号码", result["MOBILE_NUMBER"])
	}
	if result["EMAIL"] != "电子邮箱" {
		t.Errorf("EMAIL: got %q, want 电子邮箱", result["EMAIL"])
	}
}

func TestGetTranslationsByLanguage_FallbackToEN(t *testing.T) {
	m := &model.MerchantRule{FieldTranslations: fieldTranslationsJSON}
	result := m.GetTranslationsByLanguage(context.Background(), "FR")
	if len(result) != 2 {
		t.Fatalf("field count: got %d, want 2 (fallback to EN)", len(result))
	}
	if result["MOBILE_NUMBER"] != "Mobile Number" {
		t.Errorf("MOBILE_NUMBER: got %q, want 'Mobile Number' (EN fallback)", result["MOBILE_NUMBER"])
	}
}

func TestGetTranslationsByLanguage_EmptyCLOB(t *testing.T) {
	m := &model.MerchantRule{FieldTranslations: ""}
	result := m.GetTranslationsByLanguage(context.Background(), "EN")
	if result == nil {
		t.Fatal("result must not be nil")
	}
	if len(result) != 0 {
		t.Errorf("length: got %d, want 0", len(result))
	}
}

func TestGetTranslationsByLanguage_EmptyObjectCLOB(t *testing.T) {
	m := &model.MerchantRule{FieldTranslations: "{}"}
	result := m.GetTranslationsByLanguage(context.Background(), "EN")
	if len(result) != 0 {
		t.Errorf("length: got %d, want 0", len(result))
	}
}

func TestGetTranslationsByLanguage_InvalidJSON(t *testing.T) {
	m := &model.MerchantRule{FieldTranslations: "not json"}
	result := m.GetTranslationsByLanguage(context.Background(), "EN")
	if result == nil {
		t.Fatal("result must not be nil on error")
	}
	if len(result) != 0 {
		t.Errorf("length: got %d, want 0", len(result))
	}
}

func TestGetTranslationsByLanguage_SingleLanguageNoFallback(t *testing.T) {
	singleLangJSON := `{"VN": [{"fieldId": "MOBILE_NUMBER", "fieldTranslation": "Số điện thoại"}]}`
	m := &model.MerchantRule{FieldTranslations: singleLangJSON}

	result := m.GetTranslationsByLanguage(context.Background(), "VN")
	if len(result) != 1 {
		t.Fatalf("field count: got %d, want 1", len(result))
	}
	if result["MOBILE_NUMBER"] != "Số điện thoại" {
		t.Errorf("MOBILE_NUMBER: got %q, want 'Số điện thoại'", result["MOBILE_NUMBER"])
	}

	resultFallback := m.GetTranslationsByLanguage(context.Background(), "ZH")
	if len(resultFallback) != 0 {
		t.Errorf("ZH fallback: got %d, want 0 (no EN fallback available)", len(resultFallback))
	}
}
