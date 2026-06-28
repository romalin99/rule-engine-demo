// Package apperror defines reusable error codes and types for the UCS-FE system.
package apperror

// ErrorCode identifies the specific reason for an error, forming the final
// segment of a full error code, e.g. "ucs-fe.merchant.not_found".
type ErrorCode string

func (e ErrorCode) String() string { return string(e) }

const (
	// General
	SysErr         ErrorCode = "unknown_err"
	ParamErr       ErrorCode = "param_err"
	ReqErr         ErrorCode = "req_param_err"
	FormatError    ErrorCode = "format_error"
	UploadError    ErrorCode = "upload_error"
	JSONErr        ErrorCode = "json_err"
	AuthErr        ErrorCode = "auth_err"
	SignErr        ErrorCode = "sign_err"
	FrequencyErr   ErrorCode = "frequency_err"
	NetworkTimeout ErrorCode = "network_timeout"
	StatusErr      ErrorCode = "status_err"
	AmountInvalid  ErrorCode = "amount_invalid"

	// Database
	DataNotFound     ErrorCode = "data_not_found"
	DataHasExisted   ErrorCode = "data_has_existed"
	InsertFailed     ErrorCode = "insert_failed"
	UpdateFailed     ErrorCode = "update_failed"
	DeleteFailed     ErrorCode = "delete_failed"
	NoDataUpdate     ErrorCode = "no_data_update"
	DBBindParamError ErrorCode = "db_bind_param_error"
	SqlExecutionFail ErrorCode = "sql_execution_fail"

	// Merchant
	MerchantNotFound ErrorCode = "merchant_not_found"
	MerchantExisted  ErrorCode = "merchant_already_exist"
	MerchantIsNull   ErrorCode = "merchant_is_null"
	MerchantNotExist ErrorCode = "merchant_not_exist"

	// Business
	FieldConfigNotExist      ErrorCode = "field_config_not_exist"
	ValidationRecordNotExist ErrorCode = "validation_record_not_exist"
	ExceedLimit              ErrorCode = "exceed_limit"
	TaskSubmitFail           ErrorCode = "task_submit_fail"
	InvalidParam             ErrorCode = "invalid_param"
	InvalidDateParam         ErrorCode = "invalid_date_param"
	TimeRangeError           ErrorCode = "time_range_error"
	NoLogRecord              ErrorCode = "no_log_record"
	InvalidOperand           ErrorCode = "invalid_operand"

	// Downstream clients
	UcsClientErr ErrorCode = "ucs_client_err"
	UssClientErr ErrorCode = "uss_client_err"
	TacClientErr ErrorCode = "tac_client_err"
	PssClientErr ErrorCode = "pss_client_err"
	WpsClientErr ErrorCode = "wps_client_err"
	McsClientErr ErrorCode = "mcs_client_err"

	// Customer
	CustomerIllegalMerchantError ErrorCode = "customer_merchant_error"

	// Player verification — USS / MCS / WPS upstream failures
	UssCustomerFetchFailed             ErrorCode = "uss_customer_fetch_failed"
	UssCustomerFetchPersonalInfoFailed ErrorCode = "uss_customer_fetch_personal_info_failed"
	UssPlayerCountOfMerchantFailed     ErrorCode = "uss_player_count_of_merchant_failed"
	UssPasswordResetFailed             ErrorCode = "uss_password_reset_failed"
	McsVerifyPlayerInfoFailed          ErrorCode = "mcs_verify_player_info_failed"
	WpsEmailSmsFailed                  ErrorCode = "wps_email_sms_failed"

	// Player verification — business / runtime
	ParseJSONFailed       ErrorCode = "parse_json_failed"
	QuestionLimitExceeded ErrorCode = "question_limit_exceeded"
	RedisNotFound         ErrorCode = "redis_not_found"
	PhoneAlreadyBound     ErrorCode = "phone_already_bound"
	EmailAlreadyBound     ErrorCode = "email_already_bound"

	// Help-center / service-terms
	HelpCenterRequestParam  ErrorCode = "help_center_request_param_invalid"
	HelpCenterPersistFailed ErrorCode = "help_center_persist_failed"

	// Account-control / time-usage (TP-5138)
	TimeUsageRequestParam ErrorCode = "time_usage_request_param_invalid"
	TimeUsageQueryFailed  ErrorCode = "time_usage_query_failed"
)

// Module identifies the subsystem that produced the error, forming the middle
// segment of a full error code, e.g. "ucs-fe.merchant.not_found".
type Module string

func (m Module) String() string { return string(m) }

const (
	ModuleOracle             Module = "oracle"
	ModuleRedis              Module = "redis"
	ModuleMongo              Module = "mongo"
	ModuleSecurityQuestion   Module = "security_question"
	ModuleCustomer           Module = "customer"
	ModuleProfile            Module = "profile"
	ModuleNone               Module = "non"
	ModuleMerchant           Module = "merchant"
	ModuleRemember           Module = "remember"
	ModulePassword           Module = "password"
	ModuleCustomerPermission Module = "customer_permission"
	ModuleSort               Module = "sort"
	ModuleMail               Module = "mail"
	ModuleDynamicField       Module = "dynamic_field"
	ModuleRelay              Module = "relay"
	ModuleRegister           Module = "register"
)
