package uss

import (
	"strings"
	"time"

	json "github.com/bytedance/sonic"
)

const flexTimeLayout = "2006-01-02 15:04:05"

// FlexTime unmarshals both "2006-01-02 15:04:05" and null from JSON.
type FlexTime struct {
	time.Time
}

func (t FlexTime) FormatDate() string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.DateOnly)
}

func (t *FlexTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if strings.ToLower(s) == "null" || s == "" {
		t.Time = time.Time{}
		return nil
	}
	// 用 ParseInLocation+time.Local 让 time.Time 直接带本地时区(CST)，
	// 避免 time.Parse 默认按 UTC 解析后被 driver 转换成本地时间(+8h)入库。
	parsed, err := time.ParseInLocation(flexTimeLayout, s, time.Local)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}

func (t FlexTime) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.Format(flexTimeLayout))
}

// NullString unmarshals both string and null from JSON.
type NullString struct {
	Val   string
	Valid bool
}

func (s *NullString) UnmarshalJSON(b []byte) error {
	str := strings.Trim(string(b), `"`)
	if strings.ToLower(str) == "null" || str == "" {
		s.Val, s.Valid = "", false
		return nil
	}
	s.Val = str
	s.Valid = true
	return nil
}

func (s NullString) MarshalJSON() ([]byte, error) {
	if !s.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(s.Val)
}

func (s NullString) String() string { return s.Val }

// NullInt32 unmarshals both int32 and null from JSON.
type NullInt32 struct {
	Val   int32
	Valid bool
}

func (n *NullInt32) UnmarshalJSON(b []byte) error {
	if strings.ToLower(string(b)) == "null" {
		n.Val, n.Valid = -1, false
		return nil
	}
	if err := json.Unmarshal(b, &n.Val); err != nil {
		return err
	}
	n.Valid = true
	return nil
}

func (n NullInt32) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.Val)
}

// NullBool unmarshals both bool and null from JSON.
type NullBool struct {
	Val   bool
	Valid bool
}

func (n *NullBool) UnmarshalJSON(b []byte) error {
	if strings.ToLower(string(b)) == "null" {
		n.Val, n.Valid = false, false
		return nil
	}
	if err := json.Unmarshal(b, &n.Val); err != nil {
		return err
	}
	n.Valid = true
	return nil
}

func (n NullBool) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.Val)
}

// NullInt unmarshals both int64 and null from JSON.
type NullInt struct {
	Val   int64
	Valid bool
}

func (n *NullInt) UnmarshalJSON(b []byte) error {
	if strings.ToLower(string(b)) == "null" {
		n.Val, n.Valid = 0, false
		return nil
	}
	if err := json.Unmarshal(b, &n.Val); err != nil {
		return err
	}
	n.Valid = true
	return nil
}

func (n NullInt) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(n.Val)
}

type Profile struct {
	RegDate          FlexTime   `json:"regDate"`
	Birthday         FlexTime   `json:"birthday"`
	LastLoginTime    FlexTime   `json:"lastLoginTime"`
	AppleID          NullString `json:"appleId"`
	MobileNo         NullString `json:"mobileNo"`
	QqNo             NullString `json:"qqNo"`
	LineID           NullString `json:"lineId"`
	WhatsAppID       NullString `json:"whatsAppId"`
	FaceBookID       NullString `json:"facebookId"`
	Twitter          NullString `json:"twitter"`
	Viber            NullString `json:"viber"`
	Zalo             NullString `json:"zalo"`
	IDNumber         NullString `json:"idNumber"`
	PayeeName        NullString `json:"payeeName"`
	ZipCode          NullString `json:"zipcode"`
	Address          NullString `json:"address"`
	Nickname         NullString `json:"nickname"`
	CustomerName     NullString `json:"customerName"`
	Wechat           NullString `json:"wechat"`
	Telegram         NullString `json:"telegram"`
	VerificationMode NullString `json:"verificationMode"`
	CustomerID       NullInt    `json:"customerId"`
	Gender           NullInt32  `json:"gender"`
	MaritalStatus    NullInt32  `json:"maritalStatus"`
	IDType           NullInt32  `json:"idType"`
	SourceOfIncome   NullInt32  `json:"sourceOfIncome"`
	Occupation       NullInt32  `json:"occupation"`
}

type CustomerAdditionalInfo struct {
	PermanentAddress  NullString `json:"permanentAddress"`
	PlaceOfBirth      NullString `json:"placeOfBirth"`
	Nationality       NullString `json:"nationality"`
	Region            NullString `json:"region"`
	Kakao             NullString `json:"kakao"`
	Google            NullString `json:"google"`
	CustomerID        NullInt    `json:"customerId"`
	UsState           NullInt32  `json:"usState"`
	EmailVerification bool       `json:"emailVerification"`
}

// UnmarshalJSON 保证 customerAdditionalInfo = null 时
// UsState 为 {Val: -1, Valid: false} 而非零值 {Val: 0, Valid: false}
func (c *CustomerAdditionalInfo) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		c.UsState = NullInt32{Val: -1, Valid: false}
		return nil
	}

	type Alias CustomerAdditionalInfo
	tmp := Alias{
		UsState: NullInt32{Val: -1, Valid: false}, // 预设默认值
	}
	if err := json.Unmarshal(b, &tmp); err != nil {
		return err
	}
	*c = CustomerAdditionalInfo(tmp)
	return nil
}

type Value struct {
	CustomerName           NullString             `json:"customerName"`
	Email                  NullString             `json:"email"`
	MerchantCode           NullString             `json:"merchantCode"`
	CustomerAdditionalInfo CustomerAdditionalInfo `json:"customerAdditionalInfo"`
	Profile                Profile                `json:"profile"`
	CustomerID             NullInt                `json:"customerId"`
	MerchantID             NullInt                `json:"merchantId"`
}

type CustomerInfo struct {
	Value   Value `json:"value"`
	Success bool  `json:"success"`
}

// PasswordResetTokenRequest is the request body for password reset token generation.
type PasswordResetTokenRequest struct {
	CustomerName string `json:"customerName"`
	MerchantCode string `json:"merchantCode"`
}

// PasswordResetTokenResponse is the response body for password reset token generation.
type PasswordResetTokenResponse struct {
	Value   string `json:"value"`
	Success bool   `json:"success"`
}

///////

// CustomerProfileConditionResp is the response for GET /customer/profile/condition/plain.
// Only Total is used; list items are intentionally omitted (size=1 minimises transfer).
type CustomerProfileConditionResp struct {
	Value   CustomerProfileConditionValue `json:"value"`
	Success bool                          `json:"success"`
}

// CustomerProfileConditionValue holds the pagination metadata returned by the endpoint.
type CustomerProfileConditionValue struct {
	Total      int64 `json:"total"`
	TotalPages int64 `json:"totalPages"`
}

type CustomerPersonalInfo struct {
	Value   CustomerPersonalInfoValue `json:"value"`
	Success bool                      `json:"success"`
}

type CustomerPersonalInfoValue struct {
	Birthday               FlexTime   `json:"birthday"`
	PasswordLastModifyDate FlexTime   `json:"passwordLastModifyDate"`
	PlaceOfBirth           NullString `json:"placeOfBirth"`
	MayaID                 NullString `json:"mayaId"`
	MerchantCode           NullString `json:"merchantCode"`
	CustomerName           NullString `json:"customerName"`
	StateValue             NullString `json:"stateValue"`
	AppleUID               NullString `json:"appleUid"`
	Nickname               NullString `json:"nickname"`
	PayeeName              NullString `json:"payeeName"`
	MobileNo               NullString `json:"mobileNo"`
	CountryCode            NullString `json:"countryCode"`
	QqNo                   NullString `json:"qqNo"`
	Wechat                 NullString `json:"wechat"`
	LineID                 NullString `json:"lineId"`
	FacebookID             NullString `json:"facebookId"`
	WhatsAppID             NullString `json:"whatsAppId"`
	IDNumber               NullString `json:"idNumber"`
	Zalo                   NullString `json:"zalo"`
	Password               NullString `json:"password"`
	Telegram               NullString `json:"telegram"`
	Google                 NullString `json:"google"`
	FacebookUID            NullString `json:"facebookUid"`
	GoogleID               NullString `json:"googleId"`
	IDVerificationStatus   NullString `json:"idVerificationStatus"`
	GlifeID                NullString `json:"glifeId"`
	PaymentPassword        NullString `json:"paymentPassword"`
	City                   NullString `json:"city"`
	Kakao                  NullString `json:"kakao"`
	Zipcode                NullString `json:"zipcode"`
	Address                NullString `json:"address"`
	Twitter                NullString `json:"twitter"`
	Viber                  NullString `json:"viber"`
	AppleID                NullString `json:"appleId"`
	Email                  NullString `json:"email"`
	PermanentAddress       NullString `json:"permanentAddress"`
	Region                 NullString `json:"region"`
	Nationality            NullString `json:"nationality"`
	CustomerID             NullInt    `json:"customerId"`
	RecommenderID          NullInt    `json:"recommenderId"`
	Gender                 NullInt32  `json:"gender"`
	IDType                 NullInt32  `json:"idType"`
	Type                   NullInt32  `json:"type"`
	MaritalStatus          NullInt32  `json:"maritalStatus"`
	Occupation             NullInt32  `json:"occupation"`
	SourceOfIncome         NullInt32  `json:"sourceOfIncome"`
	UsState                NullInt32  `json:"usState"`
	IsEmailVerification    NullBool   `json:"isEmailVerification"`
	IDVerification         NullBool   `json:"idVerification"`
	IsOfficialAppLogin     NullBool   `json:"isOfficialAppLogin"`
	IsMobileVerified       NullBool   `json:"isMobileVerified"`
}
