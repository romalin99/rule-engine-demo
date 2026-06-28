package model

import "time"

// FieldIdUssMapping 对应表 TCG_UCS.FIELD_ID_USS_MAPPING
type FieldIdUssMapping struct {
	CreateTime time.Time `db:"CREATE_TIME" json:"createTime"`
	UpdateTime time.Time `db:"UPDATE_TIME" json:"updateTime"`
	FieldID    string    `db:"FIELD_ID"    json:"fieldId"`
	FieldName  string    `db:"FIELD_NAME"  json:"fieldName"`
	ID         int64     `db:"ID"          json:"id"`
	McsID      int64     `db:"MCS_ID"      json:"mcsId"`
	UssID      int32     `db:"USS_ID"      json:"ussId"`
}
