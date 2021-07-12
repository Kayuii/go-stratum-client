package stratum

import "fmt"

type StratumErrorCode int

// https://zh.braiins.com/stratum-v1/docs
// 20 - Other/Unknown
// 21 - Job not found (=stale)
// 22 - Duplicate share
// 23 - Low difficulty share
// 24 - Unauthorized worker
// 25 - Not subscribed

const (
	STRATUM_ERROR_Other_Unknown        StratumErrorCode = 20
	STRATUM_ERROR_JOB_NOT_FOUND        StratumErrorCode = 21
	STRATUM_ERROR_DUPLICATE_SHARE      StratumErrorCode = 22
	STRATUM_ERROR_LOW_DIFFICULTY_SHARE StratumErrorCode = 23
	STRATUM_ERROR_UNAUTHORIZED_WORKER  StratumErrorCode = 24
	STRATUM_ERROR_NOT_SUBSCRIBED       StratumErrorCode = 25
)

type StratumError struct {
	Code      StratumErrorCode `json:"code"`
	Message   string           `json:"message"`
	Traceback interface{}      `json:"traceback"`
}

func (se *StratumError) Error() string {
	return fmt.Sprintf("code=%v msg=%v traceback=%v", se.Code, se.Message, se.Traceback)
}
