// Package clienthttp 收纳本仓库各个下游服务 HTTP 客户端
// （uss / mcs / wps 等）共用的默认参数与小工具，避免在
// 多个子包里维护多份漂移的 magic number。
//
// 这里只放"所有客户端的默认起点"，单个服务如果需要
// 不同节奏（更长超时、更密集重试），应在自己的 Client
// 初始化里覆盖这些值，而不是修改本包。
package clienthttp

import "time"

// HTTP 客户端默认超时与重试参数。集中在此便于按环境调优，
// 避免散落的 magic number 与多份拷贝漂移。
const (
	// DefaultTLSHandshakeTimeout 限制 TLS 握手阶段耗时，
	// 防止后端 TLS 异常时客户端长时间挂住连接池。
	DefaultTLSHandshakeTimeout = 5 * time.Second

	// DefaultRetryDelay 是两次重试之间的固定等待。
	// 选 700ms 是为了让上游瞬时抖动（GC pause / 短网络抖动）有恢复窗口，
	// 同时三次重试总耗时仍可控（~2.1s + 3 * singleReqTimeout）。
	DefaultRetryDelay = 700 * time.Millisecond

	// DefaultSingleReqTimeout 是单次 HTTP 请求(含读响应体)的上限，
	// 用于 per-request context.WithTimeout，区别于整体重试预算。
	DefaultSingleReqTimeout = 5 * time.Second
)
