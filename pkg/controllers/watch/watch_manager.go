package watch

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// WatchManager manages resource watchers for continuous replication
type WatchManager struct {
	sourceClient      dynamic.Interface
	destClient        dynamic.Interface
	informerFactory   dynamicinformer.DynamicSharedInformerFactory
	watchers          map[schema.GroupVersionResource]cache.SharedIndexInformer
	stopCh            chan struct{}
	backgroundSyncCh  chan struct{}
	mu               sync.RWMutex
}

// NewWatchManager creates a new WatchManager
func NewWatchManager(sourceClient, destClient dynamic.Interface) *WatchManager {
	return &WatchManager{
		sourceClient:     sourceClient,
		destClient:       destClient,
		watchers:        make(map[schema.GroupVersionResource]cache.SharedIndexInformer),
		stopCh:          make(chan struct{}),
		backgroundSyncCh: make(chan struct{}),
	}
}

// StartWatching starts watching resources for a namespace
func (w *WatchManager) StartWatching(ctx context.Context, namespace string, resources []schema.GroupVersionResource, syncHandler func(obj interface{}) error) error {
	log := log.FromContext(ctx)

	w.mu.Lock()
	defer w.mu.Unlock()

	// Create informer factory for the namespace
	w.informerFactory = dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		w.sourceClient,
		time.Hour*1,
		namespace,
		nil,
	)

	for _, gvr := range resources {
		informer := w.informerFactory.ForResource(gvr).Informer()

		// Add event handlers
		informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if err := syncHandler(obj); err != nil {
					log.Error(err, "failed to sync added resource", "gvr", gvr.String())
				}
			},
			UpdateFunc: func(old, new interface{}) {
				if err := syncHandler(new); err != nil {
					log.Error(err, "failed to sync updated resource", "gvr", gvr.String())
				}
			},
			DeleteFunc: func(obj interface{}) {
				if err := syncHandler(obj); err != nil {
					log.Error(err, "failed to sync deleted resource", "gvr", gvr.String())
				}
			},
		})

		w.watchers[gvr] = informer
	}

	// Start all informers
	w.informerFactory.Start(w.stopCh)

	// Wait for all caches to sync
	for gvr, informer := range w.watchers {
		if !cache.WaitForCacheSync(w.stopCh, informer.HasSynced) {
			return fmt.Errorf("failed to sync cache for %s", gvr.String())
		}
	}

	return nil
}

// StartBackgroundSync starts the background sync process
func (w *WatchManager) StartBackgroundSync(ctx context.Context, interval time.Duration, syncFunc func() error) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := syncFunc(); err != nil {
					log.FromContext(ctx).Error(err, "background sync failed")
				}
				w.backgroundSyncCh <- struct{}{}
			}
		}
	}()
}

// Stop stops all watchers and background sync
func (w *WatchManager) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	close(w.stopCh)
	close(w.backgroundSyncCh)
	w.watchers = make(map[schema.GroupVersionResource]cache.SharedIndexInformer)
}

// IsWatching returns true if resources are being watched
func (w *WatchManager) IsWatching() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.watchers) > 0
}
