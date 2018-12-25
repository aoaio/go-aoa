package mclock

import (
	"time"

	"github.com/aristanetworks/goarista/monotime"
)

type AbsTime time.Duration 

func Now() AbsTime {
	return AbsTime(monotime.Now())
}
