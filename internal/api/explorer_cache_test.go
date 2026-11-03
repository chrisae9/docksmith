package api

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExplorerCache_SingleFlight(t *testing.T) {
	c := newExplorerCache(time.Minute)
	var calls int32
	compute := func(ctx context.Context) (*ExplorerData, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(20 * time.Millisecond)
		return &ExplorerData{}, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.get(context.Background(), "default", compute)
			require.NoError(t, err)
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestExplorerCache_RespectsTTL(t *testing.T) {
	c := newExplorerCache(10 * time.Millisecond)
	var calls int32
	compute := func(ctx context.Context) (*ExplorerData, error) {
		atomic.AddInt32(&calls, 1)
		return &ExplorerData{}, nil
	}

	_, err := c.get(context.Background(), "default", compute)
	require.NoError(t, err)
	_, err = c.get(context.Background(), "default", compute)
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "second call inside the TTL must be served from cache")

	time.Sleep(20 * time.Millisecond)
	_, err = c.get(context.Background(), "default", compute)
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls), "after the TTL the cache recomputes")
}

func TestExplorerCache_DoesNotCacheErrors(t *testing.T) {
	c := newExplorerCache(time.Minute)
	compute := func(ctx context.Context) (*ExplorerData, error) {
		return nil, errors.New("docker down")
	}
	_, err := c.get(context.Background(), "default", compute)
	require.Error(t, err)

	compute = func(ctx context.Context) (*ExplorerData, error) { return &ExplorerData{}, nil }
	got, err := c.get(context.Background(), "default", compute)
	require.NoError(t, err)
	assert.NotNil(t, got)
}
