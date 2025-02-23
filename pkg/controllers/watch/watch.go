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
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// WatchManager manages resource watchers
type WatchManager struct {
	sourceClient     dynamic.Interface
	destClient       dynamic.Interface
	informers        map[schema.GroupVersionResource]cache.SharedIndexInformer
	stopCh           chan struct{}
	backgroundStopCh chan struct{}
	watching         bool
	mu              sync.RWMutex
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
	log := log.FromContext(ctx)
	log.V(1).Info("starting resource watchers",
		"namespace", namespace,
		"resourceCount", len(resources))

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.watching {
		log.V(1).Info("watchers already running")
		return nil
	}

	// Create dynamic informer factory
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(w.sourceClient, time.Hour*24, namespace, nil)

	// Create informers for each resource type
	for _, gvr := range resources {
		log.V(1).Info("creating informer",
			"group", gvr.Group,
			"version", gvr.Version,
			"resource", gvr.Resource)

		informer := factory.ForResource(gvr).Informer()
		w.informers[gvr] = informer

		informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				log.V(1).Info("resource added",
					"group", gvr.Group,
					"version", gvr.Version,
					"resource", gvr.Resource)
				if err := handler(obj); err != nil {
					log.Error(err, "failed to handle add event")
				}
			},
			UpdateFunc: func(old, new interface{}) {
				oldObj := old.(*unstructured.Unstructured)
				newObj := new.(*unstructured.Unstructured)

				if oldObj.GetResourceVersion() != newObj.GetResourceVersion() {
					log.V(1).Info("resource updated",
						"group", gvr.Group,
						"version", gvr.Version,
						"resource", gvr.Resource,
						"name", newObj.GetName())
					if err := handler(new); err != nil {
						log.Error(err, "failed to handle update event")
					}
				}
			},
			DeleteFunc: func(obj interface{}) {
				log.V(1).Info("resource deleted",
					"group", gvr.Group,
					"version", gvr.Version,
					"resource", gvr.Resource)
				if err := handler(obj); err != nil {
					log.Error(err, "failed to handle delete event")
				}
			},
		})
	}

	// Start all informers
	log.V(1).Info("starting informers")
	factory.Start(w.stopCh)

	// Wait for all caches to sync
	log.V(1).Info("waiting for caches to sync")
	for gvr, informer := range w.informers {
		if !cache.WaitForCacheSync(w.stopCh, informer.HasSynced) {
			close(w.stopCh)
			return fmt.Errorf("failed to sync cache for %s", gvr.String())
		}
		log.V(1).Info("cache synced",
			"group", gvr.Group,
			"version", gvr.Version,
			"resource", gvr.Resource)
	}

	w.watching = true
	log.V(1).Info("all watchers started successfully")
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
	log := log.FromContext(ctx)
	log.V(1).Info("starting background sync",
		"interval", interval)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				log.V(1).Info("running background sync")
				if err := syncFn(); err != nil {
					log.Error(err, "background sync failed")
				}
			case <-w.backgroundStopCh:
				log.V(1).Info("stopping background sync")
				return
			}
		}
	}()
}

// StopBackgroundSync stops the background sync process
func (w *WatchManager) StopBackgroundSync() {
	close(w.backgroundStopCh)
}
