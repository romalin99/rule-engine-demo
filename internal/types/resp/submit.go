package resp

// SubmitVerifyData is the response body for a successful verification submission.
type SubmitVerifyData struct {
	BindType        string `json:"bindType,omitempty"  example:"phone | email"`
	OneTimePassword string `json:"oneTimePassword,omitempty" example:"88888888"`
	ScoreChecked    bool   `json:"scoreChecked,omitempty" example:"false"`
}

//type EmailPhoneBoundData struct {
//	IsEmailResetEnabled bool `json:"isEmailResetEnabled" example:"true"`
//	IsSmsResetEnabled bool `json:"isSmsResetEnabled" example:"true"`
//}

// SubmitVerifyDataScoreNotChecked is returned when the submitted score was not checked.
type SubmitVerifyDataScoreNotChecked struct {
	ScoreChecked bool `json:"scoreChecked" example:"false"`
}

// SubmitVerifyDataScoreChecked is returned when the score was checked and OTP is issued.
type SubmitVerifyDataScoreChecked struct {
	BindType        string `json:"bindType"        example:"email"`
	OneTimePassword string `json:"oneTimePassword" example:"wcycrpbp"`
}

// SubmitVerifyResponseScoreNotChecked is the top-level response when score is not checked.
type SubmitVerifyResponseScoreNotChecked struct {
	Value struct {
		Message string                          `json:"message" example:"success"`
		Code    int                             `json:"code"    example:"0"`
		Data    SubmitVerifyDataScoreNotChecked `json:"data"`
	} `json:"value"`
	Success bool `json:"success" example:"true"`
}

// SubmitVerifyResponseScoreChecked is the top-level response when score is checked.
type SubmitVerifyResponseScoreChecked struct {
	Value struct {
		Data    SubmitVerifyDataScoreChecked `json:"data"`
		Message string                       `json:"message" example:"success"`
		Code    int                          `json:"code"    example:"0"`
	} `json:"value"`
	Success bool `json:"success" example:"true"`
}
