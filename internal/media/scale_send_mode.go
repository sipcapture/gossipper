package media

import (
	"os"
	"strings"
)

var scaleDirectSend bool

// EnableScaleDirectSend sets experimental in-scheduler batch send (see -media_iouring).
func EnableScaleDirectSend(enable bool) {
	scaleDirectSend = enable
}

// ScaleDirectSend reports whether the scheduler sends batches inline (no sender worker queue).
func ScaleDirectSend() bool {
	if scaleDirectSend {
		return true
	}
	v := strings.TrimSpace(os.Getenv("GOSSIPPER_MEDIA_IOURING"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}
