package req

type VerifyItem struct {
	FieldID    string `json:"fieldId"`
	FieldValue string `json:"fieldValue"`
}

type VerifyDataItem struct {
	Item VerifyItem `json:"item"`
	Bind bool       `json:"bind"`
}

// SubmitVerifyRequest is the request body for submitting player verification materials.
type SubmitVerifyRequest struct {
	CustomerName string           `json:"customerName" example:"test11111"`
	Data         []VerifyDataItem `json:"data"`
}
