package trafficsync

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ziyan-c/proxy-control-plane/internal/domain"
)

func TestCollectNodeRecordsXrayStatsDeltas(t *testing.T) {
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{}
	client := &fakeStatsClient{
		deltas: []domain.TrafficDelta{
			{ProxyAccountID: "account-1", UploadBytes: 100, DownloadBytes: 200},
			{ProxyAccountID: "account-2", UploadBytes: 0, DownloadBytes: 300},
		},
	}
	syncer := New(store, client, Options{Reset: true})

	result, err := syncer.CollectNode(context.Background(), domain.ProxyNode{ID: "node-1", Name: "node-1"}, now)
	if err != nil {
		t.Fatalf("CollectNode() error = %v", err)
	}
	if !client.reset {
		t.Fatal("QueryTraffic reset = false, want true")
	}
	if store.nodeID != "node-1" || !store.recordedAt.Equal(now) {
		t.Fatalf("store nodeID=%q recordedAt=%s", store.nodeID, store.recordedAt)
	}
	if !reflect.DeepEqual(store.deltas, client.deltas) {
		t.Fatalf("recorded deltas = %#v, want %#v", store.deltas, client.deltas)
	}
	if result.UserCount != 2 || result.RowsInserted != 2 || result.UploadBytes != 100 || result.DownloadBytes != 500 {
		t.Fatalf("result = %+v", result)
	}
}

type fakeStore struct {
	nodeID     string
	deltas     []domain.TrafficDelta
	recordedAt time.Time
}

func (s *fakeStore) ListRuntimeSyncNodes(context.Context) ([]domain.ProxyNode, error) {
	return nil, nil
}

func (s *fakeStore) RecordTrafficUsageBatch(_ context.Context, nodeID string, deltas []domain.TrafficDelta, recordedAt time.Time) (int, error) {
	s.nodeID = nodeID
	s.deltas = append([]domain.TrafficDelta(nil), deltas...)
	s.recordedAt = recordedAt
	return len(deltas), nil
}

type fakeStatsClient struct {
	deltas []domain.TrafficDelta
	reset  bool
}

func (c *fakeStatsClient) QueryTraffic(_ context.Context, _ domain.ProxyNode, reset bool) ([]domain.TrafficDelta, error) {
	c.reset = reset
	return append([]domain.TrafficDelta(nil), c.deltas...), nil
}
