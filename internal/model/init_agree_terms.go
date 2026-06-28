package model

import "time"

// InitAgreeTermsStat holds the aggregated statistics for a single merchant.
// Returned by FindStatByMerchantCode.
type InitAgreeTermsStat struct {
	MerchantCode string `db:"MERCHANT_CODE" json:"merchantCode"`
	TermsCount   int64  `db:"TERMS_COUNT"   json:"termsCount"`
	PlayerCount  int64  `db:"PLAYER_COUNT"  json:"playerCount"`
}

// InitAgreeTerms maps to TCG_UCS.INIT_AGREE_TERMS table.
type InitAgreeTerms struct {
	CreateTime time.Time `db:"CREATE_TIME" json:"createTime"`
	UpdateTime time.Time `db:"UPDATE_TIME" json:"updateTime"`

	AgreeType    string `db:"AGREE_TYPE"    json:"agreeType"`
	MerchantCode string `db:"MERCHANT_CODE" json:"merchantCode"`
	Tag          string `db:"TAG"           json:"tag"`
	Title        string `db:"TITLE"         json:"title"`
	VersionNo    string `db:"VERSION_NO"    json:"versionNo"`

	ID          int64 `db:"ID"           json:"id"`
	NodeID      int64 `db:"NODE_ID"      json:"nodeId"`
	PlayerCount int64 `db:"PLAYER_COUNT" json:"playerCount"`
}
