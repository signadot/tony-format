package compact

import (
	"context"
)

// RunChase runs a loop that chases a moving target via HeadChan.
// process performs the work up to the given head.
func (c *Compactor) RunChase(ctx context.Context, process func(context.Context, int64) error) {
	for {
		select {
		case <-ctx.Done():
			return
		case head := <-c.HeadChan:
			if err := process(ctx, head); err != nil {
				// TODO: handle error (log, retry, etc)
				// For now, we just continue, assuming process handles its own retries or is robust.
			}
		}
	}
}
