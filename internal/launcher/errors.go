package launcher

import "errors"

// ErrHealthCheckFailed is returned when -summary_json health thresholds are not met.
var ErrHealthCheckFailed = errors.New("gossipper: health check failed")
