package trafficsync

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ziyan-c/proxy-control-plane/internal/domain"
)

type Store interface {
	ListRuntimeSyncNodes(ctx context.Context) ([]domain.ProxyNode, error)
	RecordTrafficUsageBatch(ctx context.Context, nodeID string, deltas []domain.TrafficDelta, recordedAt time.Time) (int, error)
}

type StatsClient interface {
	QueryTraffic(ctx context.Context, node domain.ProxyNode, reset bool) ([]domain.TrafficDelta, error)
}

type Options struct {
	Interval    time.Duration
	Timeout     time.Duration
	Concurrency int
	Reset       bool
}

type Syncer struct {
	store  Store
	client StatsClient
	opts   Options
}

type NodeResult struct {
	NodeID        string
	NodeName      string
	UserCount     int
	RowsInserted  int
	UploadBytes   int64
	DownloadBytes int64
}

const defaultInterval = 5 * time.Minute

func New(store Store, client StatsClient, opts Options) *Syncer {
	if opts.Interval <= 0 {
		opts.Interval = defaultInterval
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 3
	}
	return &Syncer{
		store:  store,
		client: client,
		opts:   opts,
	}
}

func (s *Syncer) Run(ctx context.Context) {
	log.Printf("traffic sync enabled: interval=%s concurrency=%d", s.opts.Interval, s.opts.Concurrency)
	s.syncAll(ctx)

	ticker := time.NewTicker(s.opts.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("traffic sync stopped")
			return
		case <-ticker.C:
			s.syncAll(ctx)
		}
	}
}

func (s *Syncer) syncAll(ctx context.Context) {
	nodes, err := s.store.ListRuntimeSyncNodes(ctx)
	if err != nil {
		log.Printf("traffic sync list nodes failed: %v", err)
		return
	}
	if len(nodes) == 0 {
		return
	}

	sem := make(chan struct{}, s.opts.Concurrency)
	var wg sync.WaitGroup
	for _, node := range nodes {
		node := node
		if strings.TrimSpace(node.RuntimeAPIHost) == "" || node.RuntimeAPIPort == 0 {
			log.Printf("traffic sync skip node %s: incomplete runtime API config", node.Name)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			runCtx := ctx
			cancel := func() {}
			if s.opts.Timeout > 0 {
				runCtx, cancel = context.WithTimeout(ctx, s.opts.Timeout)
			}
			defer cancel()

			result, err := s.CollectNode(runCtx, node, time.Now().UTC())
			if err != nil {
				log.Printf("traffic sync node %s failed: %v", node.Name, err)
				return
			}
			log.Printf("traffic sync node %s collected: users=%d rows=%d upload_gb=%.6f download_gb=%.6f",
				node.Name, result.UserCount, result.RowsInserted, domain.BytesToGB(result.UploadBytes), domain.BytesToGB(result.DownloadBytes))
		}()
	}
	wg.Wait()
}

func (s *Syncer) CollectNode(ctx context.Context, node domain.ProxyNode, now time.Time) (NodeResult, error) {
	result := NodeResult{
		NodeID:   node.ID,
		NodeName: node.Name,
	}
	deltas, err := s.client.QueryTraffic(ctx, node, s.opts.Reset)
	if err != nil {
		return result, err
	}
	result.UserCount = len(deltas)
	for _, delta := range deltas {
		result.UploadBytes += delta.UploadBytes
		result.DownloadBytes += delta.DownloadBytes
	}
	rowsInserted, err := s.store.RecordTrafficUsageBatch(ctx, node.ID, deltas, now)
	if err != nil {
		return result, err
	}
	result.RowsInserted = rowsInserted
	return result, nil
}
