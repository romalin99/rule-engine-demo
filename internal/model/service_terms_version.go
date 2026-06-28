package model

import "time"

// ServiceTermsVersion maps to TCG_UCS.SERVICE_TERMS_VERSION table.
type ServiceTermsVersion struct {
	AgreeTime  time.Time `db:"AGREE_TIME" json:"agreeTime"`
	CreateTime time.Time `db:"CREATE_TIME" json:"createTime"`
	UpdateTime time.Time `db:"UPDATE_TIME" json:"updateTime"`

	AgreeType    string `db:"AGREE_TYPE"    json:"agreeType"`
	CustomerIP   string `db:"CUSTOMER_IP"   json:"customerIp"`
	MerchantCode string `db:"MERCHANT_CODE" json:"merchantCode"`
	Tag          string `db:"TAG"           json:"tag"`
	Title        string `db:"TITLE"         json:"title"`
	VersionNo    string `db:"VERSION_NO"    json:"versionNo"`

	CustomerID int64 `db:"CUSTOMER_ID" json:"customerId"`
	ID         int64 `db:"ID"          json:"id"`

	NodeID int64 `db:"NODE_ID" json:"nodeId"`
}
