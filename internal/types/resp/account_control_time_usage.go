package resp

// TimeUsage 玩家在线时长用量（秒），按日 / 周 / 月三个周期返回。
//
// 数据来源 TCG_UCS.TIMELIMIT_USAGE_ACCUMULATION（由心跳 MERGE 滚动累加）。
// 周期边界一律按 UTC 切分；若某周期已跨期但尚无新心跳到达，服务层会把对应窗口
// 的用量校正为 0（详见 service.GetTimeUsage 的周期滚动校正逻辑）。
type TimeUsage struct {
	DailyUsageSec   int64 `json:"dailyUsageSec"`
	WeeklyUsageSec  int64 `json:"weeklyUsageSec"`
	MonthlyUsageSec int64 `json:"monthlyUsageSec"`
}
