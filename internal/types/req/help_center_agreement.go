package req

// HelpCenterAgreementItem is one agreement consent payload item.
type HelpCenterAgreementItem struct {
	Tag           string `json:"tag"`
	Title         string `json:"title"`
	VersionNumber string `json:"versionNumber"`
	AgreeType     string `json:"agreeType"`
	NodeID        int64  `json:"nodeId"`
}
