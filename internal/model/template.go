package model

type TemplateFieldsInfo struct {
	ErrorCode any    `json:"errorCode"`
	Message   string `json:"message"`
	Value     Value  `json:"value"`
	Success   bool   `json:"success"`
}

type DropdownItem struct {
	DropdownValue string `json:"dropdownValue"`
	DropdownID    int    `json:"dropdownId"`
}

type Value struct {
	MobileCountryCode                 any             `json:"mobileCountryCode"`
	Remark                            any             `json:"remark"`
	TemplateName                      string          `json:"templateName"`
	TemplateFields                    []TemplateField `json:"templateFields"`
	TemplateID                        int             `json:"templateId"`
	IsMobileCountryCodeDisplayEnabled bool            `json:"isMobileCountryCodeDisplayEnabled"`
	IsFixedMobileCountryCodeEnabled   bool            `json:"isFixedMobileCountryCodeEnabled"`
}

type TemplateField struct {
	UpdatedBy               any            `json:"updatedBy"`
	UpdatedAt               any            `json:"updatedAt"`
	CustomDisplayName       any            `json:"customDisplayName"`
	Format                  any            `json:"format"`
	FieldID                 string         `json:"fieldId"`
	FieldName               string         `json:"fieldName"`
	FieldAttribute          string         `json:"fieldAttribute"`
	FieldType               string         `json:"fieldType"`
	CreatedBy               string         `json:"createdBy"`
	Status                  string         `json:"status"`
	FieldDropdownList       []DropdownItem `json:"fieldDropdownList"`
	CreatedAt               int64          `json:"createdAt"`
	FormatMax               int            `json:"formatMax"`
	FormatMin               int            `json:"formatMin"`
	IsFeDisplay             bool           `json:"isFeDisplay"`
	IsPlayerEditableEnabled bool           `json:"isPlayerEditableEnabled,omitempty"`
	IsPlayerEditable        bool           `json:"isPlayerEditable"`
	IsRequiredEnabled       bool           `json:"isRequiredEnabled"`
	IsRequired              bool           `json:"isRequired"`
	IsUnique                bool           `json:"isUnique"`
	IsUniqueEnabled         bool           `json:"isUniqueEnabled"`
	IsFeDisplayEnabled      bool           `json:"isFeDisplayEnabled"`
	KycVerification         bool           `json:"kycVerification"`
}
