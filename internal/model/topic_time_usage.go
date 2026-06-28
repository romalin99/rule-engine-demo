package model

import "strings"

// PlayerBeatTimelimitUsage 是 PLAYER_BEAT_TIMELIMIT_USAGE topic 的消息体（玩家在线时长心跳）。
type PlayerBeatTimelimitUsage struct {
	MerchantCode string `json:"merchantCode"`
	IntervalUnit string `json:"intervalUnit"`
	Timestamp    int64  `json:"timestamp"` // epoch 毫秒
	CustomerID   int64  `json:"customerID"`
	Interval     int64  `json:"interval"`
}

// IntervalSeconds 将 interval 按 intervalUnit 归一化为秒。
// 返回 (秒数, 单位是否可识别)；单位无法识别时返回 (0, false)，调用方应跳过该消息。
func (m *PlayerBeatTimelimitUsage) IntervalSeconds() (int64, bool) {
	switch strings.ToUpper(strings.TrimSpace(m.IntervalUnit)) {
	case "SECOND", "SECONDS", "SEC", "S":
		return m.Interval, true
	case "MINUTE", "MINUTES", "MIN", "M":
		return m.Interval * 60, true
	case "HOUR", "HOURS", "HR", "H":
		return m.Interval * 3600, true
	case "DAY", "DAYS", "D":
		return m.Interval * 86400, true
	default:
		return 0, false
	}
}
