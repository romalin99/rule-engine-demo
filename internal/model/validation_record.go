package model

import "time"

// ValidationRecord maps to TCG_UCS.VALIDATION_RECORD table.
type ValidationRecord struct {
	CreatedAt    time.Time `db:"CREATED_AT" json:"createdAt"`
	CustomerName string    `db:"CUSTOMER_NAME" json:"customerName"`
	MerchantCode string    `db:"MERCHANT_CODE" json:"merchantCode"`
	IP           string    `db:"IP" json:"ip"`
	Qas          string    `db:"QAS" json:"qas"`
	ID           int64     `db:"ID" json:"ID"`
	CustomerID   int64     `db:"CUSTOMER_ID" json:"customerId"`
	PassingScore int32     `db:"PASSING_SCORE" json:"passingScore"`
	Score        int32     `db:"SCORE" json:"score"`
	Success      int8      `db:"SUCCESS" json:"success"`
}

// QA holds the per-question answer and score for a single verification submission.
type QA struct {
	FieldID    string `json:"fieldId"    example:"KAKAO"`
	FieldType  string `json:"fieldType"  example:"ID|Social|Financial|History"`
	Score      int32  `json:"score"      example:"26"`
	TotalScore int32  `json:"totalScore" example:"39"`
	Correct    bool   `json:"correct"    example:"true"`
}
