package web

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

const brainInvalidationDebounce = 500 * time.Millisecond

func (a *App) startBrainInvalidationWatch() {
	if a == nil || a.shutdownCtx == nil || !brainGTDSyncEnabled() {
		return
	}
	targets := brainInvalidationTargets(currentBrainRoots())
	if len(targets) == 0 {
		return
	}
	a.workerWG.Add(1)
	go func() {
		defer a.workerWG.Done()
		a.runBrainInvalidationWatch(a.shutdownCtx, targets)
	}()
}

func brainInvalidationTargets(roots map[string]string) []string {
	targets := make([]string, 0, len(roots))
	seen := map[string]struct{}{}
	for _, root := range roots {
		commitments := filepath.Join(strings.TrimSpace(root), "commitments")
		info, err := os.Stat(commitments)
		if err != nil || !info.IsDir() {
			continue
		}
		clean := filepath.Clean(commitments)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		targets = append(targets, clean)
	}
	return targets
}

func (a *App) runBrainInvalidationWatch(ctx context.Context, targets []string) {
	if a == nil || len(targets) == 0 {
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("brain invalidation: watcher init failed: %v", err)
		return
	}
	defer watcher.Close()
	for _, target := range targets {
		if err := watcher.Add(target); err != nil {
			log.Printf("brain invalidation: watch %s: %v", target, err)
		}
	}
	var timer *time.Timer
	for {
		var timerC <-chan time.Time
		if timer != nil {
			timerC = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case err := <-watcher.Errors:
			if err != nil {
				log.Printf("brain invalidation: watch error: %v", err)
			}
		case event := <-watcher.Events:
			if !brainInvalidationEventRelevant(event) {
				continue
			}
			if timer == nil {
				timer = time.NewTimer(brainInvalidationDebounce)
				continue
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(brainInvalidationDebounce)
		case <-timerC:
			timer = nil
			refreshCtx, cancel := context.WithTimeout(context.Background(), sourceSyncCommandTimeout)
			if _, err := a.syncBrainGTDReviewLists(refreshCtx); err != nil && ctx.Err() == nil {
				log.Printf("brain invalidation: sync failed: %v", err)
			}
			cancel()
		}
	}
}

func brainInvalidationEventRelevant(event fsnotify.Event) bool {
	if strings.ToLower(filepath.Ext(strings.TrimSpace(event.Name))) != ".md" {
		return false
	}
	return event.Has(fsnotify.Create) ||
		event.Has(fsnotify.Write) ||
		event.Has(fsnotify.Remove) ||
		event.Has(fsnotify.Rename)
}
