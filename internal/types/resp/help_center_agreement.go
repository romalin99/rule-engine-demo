package resp

// HelpCenterAgreementItem 单条玩家协议同意纪录（查询返回）。
type HelpCenterAgreementItem struct {
	Tag           string `json:"tag"`
	Title         string `json:"title"`
	VersionNumber string `json:"versionNumber"`
	AgreeType     string `json:"agreeType"`
	NodeID        int64  `json:"nodeId"`
}

// HelpCenterAgreementsResponse GET /help-center/agreements 的成功返回体。
type HelpCenterAgreementsResponse struct {
	Value   []HelpCenterAgreementItem `json:"value"`
	Success bool                      `json:"success"`
}

// InitAgreeTermsResp is the successful response body for POST /help-center/agree-init.
// Value represents the number of players initialized during execution.
type InitAgreeTermsResp struct {
	Value   int64 `json:"value"`
	Success bool  `json:"success"`
}
