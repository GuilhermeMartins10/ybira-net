package daemon

import (
	"context"
	"sync/atomic"

	"github.com/ybira-net/ybira-net/internal/types"
)

type TeeDropCounters struct {
	AggDrops   atomic.Int64
	StoreDrops atomic.Int64
}

func Tee(ctx context.Context, in <-chan types.FlowEvent, drops *TeeDropCounters) (<-chan types.FlowEvent, <-chan types.FlowEvent) {
	aggCh := make(chan types.FlowEvent, 1024)
	storeCh := make(chan types.FlowEvent, 1024)

	go func() {
		defer close(aggCh)
		defer close(storeCh)
		for {
			select {
			case ev, ok := <-in:
				if !ok {
					return
				}
				select {
				case aggCh <- ev:
				default:
					drops.AggDrops.Add(1)
				}
				select {
				case storeCh <- ev:
				default:
					drops.StoreDrops.Add(1)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return aggCh, storeCh
}
