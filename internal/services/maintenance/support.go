package maintenance

import (
	"fmt"
	"time"
)

// intervalSecondsToDuration converts a job interval to a duration.
func intervalSecondsToDuration(seconds int64) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func nowUTC() time.Time { return time.Now().UTC() }

// jobBusyError reports a manual trigger overlapping a running job.
type jobBusyError struct {
	providerID string
	job        string
}

func (e *jobBusyError) Error() string {
	return fmt.Sprintf("job %q for provider %q is already running", e.job, e.providerID)
}
