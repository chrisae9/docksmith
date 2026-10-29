package compose

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeyedMutex_SerializesSameKey(t *testing.T) {
	m := NewKeyedMutex()
	unlockA := m.Lock("a")
	acquired := make(chan struct{})
	go func() {
		unlock := m.Lock("a")
		close(acquired)
		unlock()
	}()

	select {
	case <-acquired:
		t.Fatal("second Lock must block while the first holds the key")
	case <-time.After(20 * time.Millisecond):
	}
	unlockA()
	<-acquired
}

func TestKeyedMutex_ConcurrentLabelWritesSurvive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`services:
  gluetun:
    image: qmcgaw/gluetun:latest
    labels:
      docksmith.ignore: "false"
  flaresolverr:
    image: flaresolverr/flaresolverr:latest
    labels:
      docksmith.ignore: "false"
`), 0644))

	m := NewKeyedMutex()
	var wg sync.WaitGroup
	for _, svc := range []struct{ name, label string }{
		{"gluetun", "docksmith.version-pin-minor"},
		{"flaresolverr", "docksmith.version-pin-patch"},
	} {
		wg.Add(1)
		go func(name, label string) {
			defer wg.Done()
			unlock := m.Lock(path)
			defer unlock()

			cf, err := LoadComposeFile(path)
			require.NoError(t, err)
			service, err := cf.FindServiceByContainerName(name)
			require.NoError(t, err)
			require.NoError(t, service.SetLabel(label, "true"))
			require.NoError(t, cf.Save())
		}(svc.name, svc.label)
	}
	wg.Wait()

	cf, err := LoadComposeFile(path)
	require.NoError(t, err)
	gluetun, err := cf.FindServiceByContainerName("gluetun")
	require.NoError(t, err)
	flaresolverr, err := cf.FindServiceByContainerName("flaresolverr")
	require.NoError(t, err)

	gluetunLabels, err := gluetun.GetAllLabels()
	require.NoError(t, err)
	flaresolverrLabels, err := flaresolverr.GetAllLabels()
	require.NoError(t, err)
	assert.Equal(t, "true", gluetunLabels["docksmith.version-pin-minor"])
	assert.Equal(t, "true", flaresolverrLabels["docksmith.version-pin-patch"])
}
