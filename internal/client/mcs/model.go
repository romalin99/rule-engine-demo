package mcs

import (
	"fmt"
	"net/http"

	json "github.com/bytedance/sonic"
)

// VerifyFinanceHistoryReq is the top-level request body for verifyTransactionHistory.
type VerifyFinanceHistoryReq struct {
	VerifyPlayerFinanceInfo VerifyPlayerFinanceInfo `json:"verifyPlayerFinanceInfo"`
	VerifyPlayerHistoryInfo VerifyPlayerHistoryInfo `json:"verifyPlayerTransactionHistory"`
}

// VerifyPlayerHistoryInfo holds transaction history verification fields.
type VerifyPlayerHistoryInfo struct {
	LastDepositAmount          string `json:"lastDepositAmount"`
	LastDepositAmountRange     string `json:"lastDepositAmountRange"`
	LastDepositMethod          string `json:"lastDepositMethod"`
	LastDepositTime            string `json:"lastDepositTime"`
	LastWithdrawAmount         string `json:"lastWithdrawAmount"`
	LastWithdrawAmountRange    string `json:"lastWithdrawAmountRange"`
	LastWithdrawMethod         string `json:"lastWithdrawMethod"`
	LastWithdrawTime           string `json:"lastWithdrawTime"`
	LastDepositTimeRangeInDay  int    `json:"lastDepositTimeRangeInDay"`
	LastWithdrawTimeRangeInDay int    `json:"lastWithdrawTimeRangeInDay"`
}

// VerifyPlayerFinanceInfo holds financial binding verification fields.
type VerifyPlayerFinanceInfo struct {
	BcNumber        string `json:"bcNumber"`
	BcHolderName    string `json:"bcHolderName"`
	EwAccount       string `json:"ewAccount"`
	EwHolderName    string `json:"ewHolderName"`
	VwAddress       string `json:"vwAddress"`
	VwHolderName    string `json:"vwHolderName"`
	IsCaseSensitive bool   `json:"isCaseSensitive"`
}

// VerifyFinanceHistoryResp is the top-level response for verifyTransactionHistory.
// JSON 外层结构：{ "success": true, "value": { ... } }
type VerifyFinanceHistoryResp struct {
	Success bool                       `json:"success"`
	Value   VerifyFinanceHistoryResult `json:"value"`
}

// VerifyFinanceHistoryResult holds the nested value payload.
type VerifyFinanceHistoryResult struct {
	VerifyPlayerHistoryInfo VerifyPlayerHistoryResult     `json:"verifyPlayerTransactionHistory"`
	VerifyPlayerFinanceInfo VerifyPlayerFinanceInfoResult `json:"verifyPlayerFinanceInfo"`
}

// VerifyPlayerHistoryResult holds per-field match scores for transaction history.
type VerifyPlayerHistoryResult struct {
	LastDepositAmount  int `json:"lastDepositAmount"`
	LastDepositMethod  int `json:"lastDepositMethod"`
	LastDepositTime    int `json:"lastDepositTime"`
	LastWithdrawAmount int `json:"lastWithdrawAmount"`
	LastWithdrawMethod int `json:"lastWithdrawMethod"`
	LastWithdrawTime   int `json:"lastWithdrawTime"`
}

// VerifyPlayerFinanceInfoResult holds per-field match scores for financial info.
type VerifyPlayerFinanceInfoResult struct {
	// 银行卡
	BcNumber     int `json:"bcNumber"`
	BcHolderName int `json:"bcHolderName"`
	BcBankCode   int `json:"bcBankCode"`  // 新增
	BcSubBranch  int `json:"bcSubBranch"` // 新增
	BcCity       int `json:"bcCity"`      // 新增
	BcProvince   int `json:"bcProvince"`  // 新增
	// 虚拟钱包
	VwAddress    int `json:"vwAddress"`
	VwHolderName int `json:"vwHolderName"`
	VwBankCode   int `json:"vwBankCode"` // 新增
	// 电子钱包
	EwAccount    int `json:"ewAccount"`
	EwHolderName int `json:"ewHolderName"`
	EwBankCode   int `json:"ewBankCode"` // 新增
}

// String implements fmt.Stringer.
func (r *VerifyFinanceHistoryResp) String() string {
	if r == nil {
		return "<nil>"
	}
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Sprintf("<VerifyFinanceHistoryResp marshal error: %v>", err)
	}
	return string(b)
}

// GetRegisterIPResp 是 /register/getRegisterIp 的顶层响应。
// 成功示例: {"success": true, "value": {"registerIp": "10.123.130.128"}}
// 失败示例: {"success": false, "message": "customer_not_exist", "errorCode": "mcsfe.register.customer_not_exist"}
type GetRegisterIPResp struct {
	Message   string             `json:"message,omitempty"`
	ErrorCode string             `json:"errorCode,omitempty"`
	Value     GetRegisterIPValue `json:"value"`
	Success   bool               `json:"success"`
}

// GetRegisterIPValue 是 GetRegisterIPResp.value 的嵌套结构。
type GetRegisterIPValue struct {
	RegisterIP string `json:"registerIp"`
}

// PlayerHeaders holds the common per-request identity headers for player APIs.
type PlayerHeaders struct {
	CustomerID   string `json:"customerId,omitempty"`
	CustomerName string `json:"customerName,omitempty"`
	Merchant     string `json:"merchant,omitempty"`
	CustomerIP   string `json:"customerIp,omitempty"`
}

func (p PlayerHeaders) String() string {
	b, _ := json.Marshal(p)
	return string(b)
}

func (p PlayerHeaders) apply(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("CustomerId", p.CustomerID)
	req.Header.Set("CustomerName", p.CustomerName)
	req.Header.Set("Merchant", p.Merchant)
	req.Header.Set("CustomerIP", p.CustomerIP)
}
