package model

import (
	"database/sql"
	"time"
)

// TimelimitUsageAccumulation maps to TCG_UCS.TIMELIMIT_USAGE_ACCUMULATION table.
//
// 每个玩家（CUSTOMER_ID + MERCHANT_CODE）永远只有一行，由心跳 MERGE 原地滚动更新。
//   - CURRENT_DATE_KEY  ── TRUNC(beat) 的 UTC 当天 00:00，跨天判断依据
//   - CURRENT_WEEK_KEY  ── ISO 周键 'IYYY"-W"IW'，e.g.'2026-W24'，跨周判断依据
//   - CURRENT_MONTH_KEY ── 'YYYY-MM'，e.g.'2026-06'，跨月判断依据
//   - DAILY/WEEKLY/MONTHLY_USAGE_SEC ── 当前周期累计在线秒数（unit=S 直接写入）
//   - DAILY/WEEKLY/MONTHLY_LIMIT_SEC ── UCS 上限快照(秒)，可空，每次心跳 NVL 覆盖
//   - BEAT_COUNT_DAILY  ── 当天心跳次数，跨天/周/月重置为 1
type TimelimitUsageAccumulation struct {
	CurrentDateKey time.Time `db:"CURRENT_DATE_KEY" json:"currentDateKey"`
	LastBeatAt     time.Time `db:"LAST_BEAT_AT"     json:"lastBeatAt"`
	CreatedAt      time.Time `db:"CREATED_AT"       json:"createdAt"`
	UpdatedAt      time.Time `db:"UPDATED_AT"       json:"updatedAt"`

	MerchantCode    string `db:"MERCHANT_CODE"     json:"merchantCode"`
	CurrentWeekKey  string `db:"CURRENT_WEEK_KEY"  json:"currentWeekKey"`
	CurrentMonthKey string `db:"CURRENT_MONTH_KEY" json:"currentMonthKey"`

	DailyLimitSec   sql.NullInt64 `db:"DAILY_LIMIT_SEC"   json:"dailyLimitSec"`
	WeeklyLimitSec  sql.NullInt64 `db:"WEEKLY_LIMIT_SEC"  json:"weeklyLimitSec"`
	MonthlyLimitSec sql.NullInt64 `db:"MONTHLY_LIMIT_SEC" json:"monthlyLimitSec"`

	ID              int64 `db:"ID"                json:"id"`
	CustomerID      int64 `db:"CUSTOMER_ID"       json:"customerId"`
	DailyUsageSec   int64 `db:"DAILY_USAGE_SEC"   json:"dailyUsageSec"`
	WeeklyUsageSec  int64 `db:"WEEKLY_USAGE_SEC"  json:"weeklyUsageSec"`
	MonthlyUsageSec int64 `db:"MONTHLY_USAGE_SEC" json:"monthlyUsageSec"`
	BeatCountDaily  int64 `db:"BEAT_COUNT_DAILY"  json:"beatCountDaily"`
}

// TimeUsageBeat 承载一次心跳累加 MERGE 所需的全部入参，由消费 Kafka 心跳的服务层组装。
//
//   - BeatAt      ── Kafka timestamp(ms) 经 time.UnixMilli(ms).UTC() 得到，传入前务必为 UTC；
//     日/ISO周/月三个周期键由 SQL 端 TRUNC / TO_CHAR 统一派生，保证边界一律按 UTC 切分。
//   - IntervalSec ── 已按 intervalUnit 归一为秒（心跳 intervalUnit = SECOND 时直接取 interval）。
//   - *LimitSec   ── 来自 UCS GET /rootpath/time-limits 的上限快照(秒)；UCS 不可用时传
//     sql.NullInt64{}（NULL），MERGE 用 NVL 保留行内上一次已知值。
type TimeUsageBeat struct {
	BeatAt time.Time

	MerchantCode string

	DailyLimitSec   sql.NullInt64
	WeeklyLimitSec  sql.NullInt64
	MonthlyLimitSec sql.NullInt64

	CustomerID  int64
	IntervalSec int64
}
