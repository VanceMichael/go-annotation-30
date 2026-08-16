package model

import "errors"

// 平台使用哨兵错误标识失败类别，调用方通过 errors.Is 判定并映射到退出码或 HTTP 状态码。
var (
	// ErrUnknownGenre 题材代码无法识别。
	ErrUnknownGenre = errors.New("model: 未知题材类型")
	// ErrUnknownRating 分级代码无法识别。
	ErrUnknownRating = errors.New("model: 未知内容分级")
	// ErrUnknownStage 审核阶段代码无法识别。
	ErrUnknownStage = errors.New("model: 未知审核阶段")

	// ErrInvalidSeries 剧目登记信息非法。
	ErrInvalidSeries = errors.New("model: 剧目登记信息非法")
	// ErrInvalidEpisode 剧集信息非法。
	ErrInvalidEpisode = errors.New("model: 剧集信息非法")
	// ErrInvalidParty 分账参与方信息非法。
	ErrInvalidParty = errors.New("model: 分账参与方信息非法")

	// ErrSeriesUnknown 剧目不存在。
	ErrSeriesUnknown = errors.New("model: 剧目不存在")
	// ErrEpisodeUnknown 剧集不存在。
	ErrEpisodeUnknown = errors.New("model: 剧集不存在")
	// ErrPartyUnknown 分账参与方不存在。
	ErrPartyUnknown = errors.New("model: 分账参与方不存在")

	// ErrStageConflict 审核阶段流转不被允许。
	ErrStageConflict = errors.New("model: 审核阶段流转不被允许")
	// ErrRatingBlocked 内容分级判定为不予播出。
	ErrRatingBlocked = errors.New("model: 内容分级不予播出")
	// ErrReviewRejected 审核驳回。
	ErrReviewRejected = errors.New("model: 审核驳回")

	// ErrRateLimited 审核通道限流，调用方应退避后重试。
	ErrRateLimited = errors.New("model: 审核通道限流")
	// ErrGatewayUnavailable 结算通道不可用。
	ErrGatewayUnavailable = errors.New("model: 结算通道不可用")

	// ErrShareMismatch 分账比例合计不等于 10000 个基点。
	ErrShareMismatch = errors.New("model: 分账比例合计不符")
	// ErrSettleMismatch 分账金额合计与待分账总额不一致。
	ErrSettleMismatch = errors.New("model: 分账金额合计与总额不一致")

	// ErrTallyMissing 缺少播放量统计数据。
	ErrTallyMissing = errors.New("model: 缺少播放量统计数据")
	// ErrReportIncomplete 报表数据不完整。
	ErrReportIncomplete = errors.New("model: 报表数据不完整")
)
