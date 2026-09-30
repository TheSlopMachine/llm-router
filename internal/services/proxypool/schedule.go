package proxypool

import (
	"context"
	"time"
)

var idleRefreshIntervals = [...]time.Duration{5 * time.Minute, 10 * time.Minute, 15 * time.Minute}

func (s *Service) schedule(ctx context.Context) {
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	idleStep := 0
	s.setNextRefresh(time.Minute)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.refreshWake:
			idleStep = 0
			resetTimer(timer, time.Minute)
			s.setNextRefresh(time.Minute)
		case <-timer.C:
			s.RequestRefresh()
			s.mu.Lock()
			active := !s.lastUse.IsZero() && time.Since(s.lastUse) < 8*time.Minute
			s.mu.Unlock()
			interval, nextIdleStep := nextRefreshInterval(active, idleStep)
			idleStep = nextIdleStep
			resetTimer(timer, interval)
			s.setNextRefresh(interval)
		}
	}
}

func nextRefreshInterval(active bool, idleStep int) (time.Duration, int) {
	if active {
		return time.Minute, 0
	}
	if idleStep < 0 {
		idleStep = 0
	}
	if idleStep >= len(idleRefreshIntervals) {
		idleStep = len(idleRefreshIntervals) - 1
	}
	interval := idleRefreshIntervals[idleStep]
	if idleStep < len(idleRefreshIntervals)-1 {
		idleStep++
	}
	return interval, idleStep
}

func (s *Service) setNextRefresh(interval time.Duration) {
	s.mu.Lock()
	s.interval = interval
	s.nextRefresh = time.Now().Add(interval)
	s.mu.Unlock()
}

func resetTimer(timer *time.Timer, interval time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(interval)
}
