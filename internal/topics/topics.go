// Package topics holds Kafka topic name constants and their message payload types.
package topics

// ---------------------------------------------------------------
// Topic name constants
// ---------------------------------------------------------------

const (
	TopicBiPromotionRank           = "bi-rank-promotion-event"
	TopicRankEvent                 = "rank-event"
	TopicCohortInitializationEvent = "cohort-initialization-event" // new user registration push
	TopicTaskUpdateEvent           = "task-update-event"           // task completion
	TopicPropEvent                 = "prop-event"                  // send prop
	TopicPurchaseEvent             = "purchase-event"
	TopicTaskProgressesEvent       = "task-progresses-event" // task progress

	// TopicPlayerBeatTimeLimitUsage 玩家在线时长心跳 (TP-5138)。
	// 注意：viper 会把 toml 里的 topic map key 统一小写，SubscribeTopic 内部以小写名查找
	// per-topic 配置，但传给 kgo.ConsumeTopics 的仍是此处的原始大写名（即真实 Kafka topic 名）。
	TopicPlayerBeatTimeLimitUsage = "PLAYER_BEAT_TIMELIMIT_USAGE"
)

// ---------------------------------------------------------------
// Message payload types
// ---------------------------------------------------------------

// TaskUpdate is the payload for TopicTaskUpdateEvent.
type TaskUpdate struct {
	Message              string `json:"message"`
	TaskType             int64  `json:"task_type"`
	TaskId               int64  `json:"task_id"`
	TaskItemId           int64  `json:"task_item_id"`
	TaskItemProgressesId int64  `json:"task_item_progresses_id"`
}

// SendProp is the payload for TopicPropEvent.
type SendProp struct {
	SendUserId    int64 `json:"send_user_id"`
	ReceiveUserId int64 `json:"receive_user_id"`
	PropId        int64 `json:"prop_id"`
	TrackType     int64 `json:"track_type"` // 0=none 1=line 2=parabola 3=spiral
}

// BuyErrorInfo is the payload for TopicPurchaseEvent error cases.
type BuyErrorInfo struct {
	PurchaseKey string  `json:"purchase_key"`
	Price       float64 `json:"price"`
	Coins       int64   `json:"coins"`
}
