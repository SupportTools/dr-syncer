package sshkeys

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
)

const (
	// DefaultSecretMountPath is the default path where the secret is mounted
	DefaultSecretMountPath = "/etc/ssh/keys"

	// DefaultAuthorizedKeysPath is the default path for the authorized_keys file
	DefaultAuthorizedKeysPath = "/home/syncer/.ssh/authorized_keys"

	// DefaultWatchInterval is the default interval for watching the secret
	DefaultWatchInterval = 30 * time.Second
)

// SecretWatcher watches for changes to the mounted secret and updates the authorized_keys file
type SecretWatcher struct {
	// MountPath is the path where the secret is mounted
	MountPath string

	// AuthorizedKeysPath is the path to the authorized_keys file
	AuthorizedKeysPath string

	// WatchInterval is the interval for watching the secret
	WatchInterval time.Duration

	// Logger is the logger to use
	Logger *logrus.Entry

	// stopCh is used to signal the watcher to stop
	stopCh chan struct{}
}

// NewSecretWatcher creates a new secret watcher
func NewSecretWatcher(mountPath, authorizedKeysPath string, watchInterval time.Duration) *SecretWatcher {
	if mountPath == "" {
		mountPath = DefaultSecretMountPath
	}
	if authorizedKeysPath == "" {
		authorizedKeysPath = DefaultAuthorizedKeysPath
	}
	if watchInterval <= 0 {
		watchInterval = DefaultWatchInterval
	}

	return &SecretWatcher{
		MountPath:          mountPath,
		AuthorizedKeysPath: authorizedKeysPath,
		WatchInterval:      watchInterval,
		Logger:             log.WithField("subcomponent", "secret-watcher"),
		stopCh:             make(chan struct{}),
	}
}

// Start starts the secret watcher
func (w *SecretWatcher) Start(ctx context.Context) error {
	w.Logger.WithFields(logrus.Fields{
		"mountPath":          w.MountPath,
		"authorizedKeysPath": w.AuthorizedKeysPath,
		"watchInterval":      w.WatchInterval,
	}).Info("Starting secret watcher")

	// Ensure the authorized_keys directory exists
	authorizedKeysDir := filepath.Dir(w.AuthorizedKeysPath)
	if err := os.MkdirAll(authorizedKeysDir, 0700); err != nil {
		return fmt.Errorf("failed to create authorized_keys directory: %v", err)
	}

	// Initial load
	if err := w.loadAuthorizedKeys(); err != nil {
		w.Logger.WithError(err).Error("Failed to load authorized keys")
	}

	// Start watching for changes
	go w.watch(ctx)

	return nil
}

// Stop stops the secret watcher
func (w *SecretWatcher) Stop() {
	close(w.stopCh)
}

// watch watches for changes to the mounted secret
func (w *SecretWatcher) watch(ctx context.Context) {
	ticker := time.NewTicker(w.WatchInterval)
	defer ticker.Stop()

	var lastModTime time.Time

	for {
		select {
		case <-ctx.Done():
			w.Logger.Info("Context cancelled, stopping secret watcher")
			return
		case <-w.stopCh:
			w.Logger.Info("Stopping secret watcher")
			return
		case <-ticker.C:
			// Check if the secret has changed
			secretPath := filepath.Join(w.MountPath, "authorized_keys")
			info, err := os.Stat(secretPath)
			if err != nil {
				if os.IsNotExist(err) {
					w.Logger.WithField("path", secretPath).Info("Secret file does not exist")
				} else {
					w.Logger.WithError(err).WithField("path", secretPath).Error("Failed to stat secret file")
				}
				continue
			}

			// Check if the modification time has changed
			if info.ModTime().After(lastModTime) {
				w.Logger.WithField("path", secretPath).Info("Secret file has changed, reloading")
				lastModTime = info.ModTime()

				if err := w.loadAuthorizedKeys(); err != nil {
					w.Logger.WithError(err).Error("Failed to load authorized keys")
				}
			}
		}
	}
}

// loadAuthorizedKeys loads the authorized_keys file from the mounted secret
func (w *SecretWatcher) loadAuthorizedKeys() error {
	// Read the secret file
	secretPath := filepath.Join(w.MountPath, "authorized_keys")
	data, err := ioutil.ReadFile(secretPath)
	if err != nil {
		return fmt.Errorf("failed to read secret file: %v", err)
	}

	// Write to the authorized_keys file
	if err := ioutil.WriteFile(w.AuthorizedKeysPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write authorized_keys file: %v", err)
	}

	w.Logger.WithFields(logrus.Fields{
		"secretPath":         secretPath,
		"authorizedKeysPath": w.AuthorizedKeysPath,
		"size":               len(data),
	}).Info("Successfully loaded authorized keys")

	return nil
}

// InitializeSecretWatcher initializes the secret watcher
func InitializeSecretWatcher(ctx context.Context, client kubernetes.Interface, namespace string) error {
	// Create the secret watcher
	watcher := NewSecretWatcher("", "", 0)

	// Start the watcher
	if err := watcher.Start(ctx); err != nil {
		return fmt.Errorf("failed to start secret watcher: %v", err)
	}

	return nil
}
