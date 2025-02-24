package watch

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

// WatchManager manages resource watchers
type WatchManager struct {
	sourceClient     dynamic.Interface
	destClient       dynamic.Interface
	informers        map[schema.GroupVersionResource]cache.SharedIndexInformer
	stopCh           chan struct{}
	backgroundStopCh chan struct{}
	watching         bool
	mu               sync.RWMutex
}

// NewWatchManager creates a new watch manager
func NewWatchManager(sourceClient, destClient dynamic.Interface) *WatchManager {
	return &WatchManager{
		sourceClient:     sourceClient,
		destClient:       destClient,
		informers:        make(map[schema.GroupVersionResource]cache.SharedIndexInformer),
		stopCh:           make(chan struct{}),
		backgroundStopCh: make(chan struct{}),
	}
}

// StartWatching starts watching resources
func (w *WatchManager) StartWatching(ctx context.Context, namespace string, resources []schema.GroupVersionResource, handler func(interface{}) error) error {
	log.Info(fmt.Sprintf("starting resource watchers for namespace %s (%d resources)", namespace, len(resources)))

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.watching {
		log.Info("watchers already running")
		return nil
	}

	// Create dynamic informer factory
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(w.sourceClient, time.Hour*24, namespace, nil)

	// Create informers for each resource type
	for _, gvr := range resources {
		log.Info(fmt.Sprintf("creating informer for %s.%s/%s", gvr.Resource, gvr.Group, gvr.Version))

		informer := factory.ForResource(gvr).Informer()
		w.informers[gvr] = informer

		informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				log.Info(fmt.Sprintf("resource added: %s.%s/%s", gvr.Resource, gvr.Group, gvr.Version))
				if err := handler(obj); err != nil {
					log.WithError(err).Error("failed to handle add event")
				}
			},
			UpdateFunc: func(old, new interface{}) {
				oldObj := old.(*unstructured.Unstructured)
				newObj := new.(*unstructured.Unstructured)

				if oldObj.GetResourceVersion() != newObj.GetResourceVersion() {
					log.Info(fmt.Sprintf("resource updated: %s.%s/%s - %s", gvr.Resource, gvr.Group, gvr.Version, newObj.GetName()))
					if err := handler(new); err != nil {
						log.WithError(err).Error("failed to handle update event")
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				log.Info(fmt.Sprintf("resource deleted: %s.%s/%s", gvr.Resource, gvr.Group, gvr.Version))
				if err := handler(obj); err != nil {
					log.WithError(err).Error("failed to handle delete event")
				}
			},
		})
	}

	// Start all informers
	log.Info("starting informers")
	factory.Start(w.stopCh)

	// Wait for all caches to sync
	log.Info("waiting for caches to sync")
	for gvr, informer := range w.informers {
		if !cache.WaitForCacheSync(w.stopCh, informer.HasSynced) {
			close(w.stopCh)
			return fmt.Errorf("failed to sync cache for %s", gvr.String())
		}
		log.Info(fmt.Sprintf("cache synced for %s.%s/%s", gvr.Resource, gvr.Group, gvr.Version))
	}

	w.watching = true
	log.Info("all watchers started successfully")
	return nil
}

// StopWatching stops all watchers
func (w *WatchManager) StopWatching() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.watching {
		return
	}

	close(w.stopCh)
	w.watching = false
	w.informers = make(map[schema.GroupVersionResource]cache.SharedIndexInformer)
}

// IsWatching returns whether watchers are running
func (w *WatchManager) IsWatching() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.watching
}

// StartBackgroundSync starts a background sync process
func (w *WatchManager) StartBackgroundSync(ctx context.Context, interval time.Duration, syncFn func() error) {
	log.Info(fmt.Sprintf("starting background sync with interval %s", interval))

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				log.Info("running background sync")
				if err := syncFn(); err != nil {
					log.WithError(err).Error("background sync failed")
				}
			case <-w.backgroundStopCh:
				log.Info("stopping background sync")
				return
			}
		}
	}()
}

// StopBackgroundSync stops the background sync process
func (w *WatchManager) StopBackgroundSync() {
	close(w.backgroundStopCh)
}
