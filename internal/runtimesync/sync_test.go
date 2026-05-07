package runtimesync

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ziyan-c/proxy-control-plane/internal/domain"
)

func TestSyncNodeSkipsWhenHashesMatch(t *testing.T) {
	user := runtimeUser("account-1", "uuid-1", "")
	store := &fakeStore{
		targetUsers: []domain.RuntimeUser{user},
	}
	client := &fakeRuntimeClient{
		users: []domain.RuntimeUser{user},
	}
	syncer := New(store, client, Options{})

	result, err := syncer.SyncNode(context.Background(), testNode(), time.Now().UTC())
	if err != nil {
		t.Fatalf("SyncNode returned error: %v", err)
	}

	if !result.Unchanged {
		t.Fatal("SyncNode did not mark unchanged state")
	}
	if len(client.added) != 0 || len(client.removed) != 0 {
		t.Fatalf("unexpected operations: added=%v removed=%v", client.added, client.removed)
	}
	if store.markErr != "" {
		t.Fatalf("mark error = %q, want empty", store.markErr)
	}
}

func TestSyncNodeDiffsManagedRuntimeUsers(t *testing.T) {
	targetKeep := runtimeUser("account-keep", "uuid-keep-new", "xtls-rprx-vision")
	targetAdd := runtimeUser("account-add", "uuid-add", "")
	runtimeKeepOldFlow := runtimeUser("account-keep", "uuid-keep-old", "")
	runtimeRemove := runtimeUser("account-remove", "uuid-remove", "")
	unmanaged := domain.RuntimeUser{Email: "legacy@example.com", UUID: "legacy-uuid"}

	store := &fakeStore{
		targetUsers: []domain.RuntimeUser{targetKeep, targetAdd},
	}
	client := &fakeRuntimeClient{
		users: []domain.RuntimeUser{runtimeKeepOldFlow, runtimeRemove, unmanaged},
	}
	syncer := New(store, client, Options{})

	result, err := syncer.SyncNode(context.Background(), testNode(), time.Now().UTC())
	if err != nil {
		t.Fatalf("SyncNode returned error: %v", err)
	}

	wantRemoved := []string{runtimeKeepOldFlow.Email, runtimeRemove.Email}
	if !reflect.DeepEqual(client.removed, wantRemoved) {
		t.Fatalf("removed = %v, want %v", client.removed, wantRemoved)
	}
	wantAdded := []domain.RuntimeUser{targetAdd, targetKeep}
	if !reflect.DeepEqual(client.added, wantAdded) {
		t.Fatalf("added = %#v, want %#v", client.added, wantAdded)
	}
	if result.Added != 2 || result.Removed != 2 || result.Unchanged {
		t.Fatalf("result = %+v, want 2 added and 2 removed", result)
	}
	if store.markErr != "" {
		t.Fatalf("mark error = %q, want empty", store.markErr)
	}
}

func TestSyncNodeAddsManagedUserWhenLegacyStaticUserMatches(t *testing.T) {
	target := runtimeUser("account-legacy", "uuid-legacy", "")
	store := &fakeStore{
		targetUsers: []domain.RuntimeUser{target},
	}
	client := &fakeRuntimeClient{
		users: []domain.RuntimeUser{
			{Email: "", UUID: "uuid-legacy"},
		},
	}
	syncer := New(store, client, Options{})

	result, err := syncer.SyncNode(context.Background(), testNode(), time.Now().UTC())
	if err != nil {
		t.Fatalf("SyncNode returned error: %v", err)
	}

	if result.Unchanged {
		t.Fatal("SyncNode should not treat legacy static user as managed state")
	}
	if !reflect.DeepEqual(client.added, []domain.RuntimeUser{target}) {
		t.Fatalf("added = %#v, want %#v", client.added, []domain.RuntimeUser{target})
	}
	if len(client.removed) != 0 {
		t.Fatalf("unexpected operations: added=%v removed=%v", client.added, client.removed)
	}
}

func TestSyncNodeMarksRuntimeErrors(t *testing.T) {
	store := &fakeStore{}
	client := &fakeRuntimeClient{
		listErr: errors.New("runtime offline"),
	}
	syncer := New(store, client, Options{})

	_, err := syncer.SyncNode(context.Background(), testNode(), time.Now().UTC())
	if err == nil {
		t.Fatal("SyncNode returned nil error")
	}
	if store.markErr != "runtime offline" {
		t.Fatalf("mark error = %q, want runtime offline", store.markErr)
	}
}

func TestUsersHashIsStable(t *testing.T) {
	left := []domain.RuntimeUser{
		runtimeUser("account-b", "UUID-B", ""),
		runtimeUser("account-a", "UUID-A", "xtls-rprx-vision"),
	}
	right := []domain.RuntimeUser{
		runtimeUser("account-a", "uuid-a", "xtls-rprx-vision"),
		runtimeUser("account-b", "uuid-b", ""),
	}

	if UsersHash(left) != UsersHash(right) {
		t.Fatalf("hash should be stable across order and UUID case")
	}
}

type fakeStore struct {
	targetUsers []domain.RuntimeUser
	markErr     string
}

func (s *fakeStore) ListRuntimeSyncNodes(context.Context) ([]domain.ProxyNode, error) {
	return nil, nil
}

func (s *fakeStore) ListRuntimeTargetUsers(context.Context, string, time.Time) ([]domain.RuntimeUser, error) {
	return append([]domain.RuntimeUser(nil), s.targetUsers...), nil
}

func (s *fakeStore) MarkProxyNodeRuntimeSync(_ context.Context, _ string, _ time.Time, syncErr string) error {
	s.markErr = syncErr
	return nil
}

type fakeRuntimeClient struct {
	users   []domain.RuntimeUser
	added   []domain.RuntimeUser
	removed []string
	listErr error
}

func (c *fakeRuntimeClient) ListUsers(context.Context, domain.ProxyNode) ([]domain.RuntimeUser, error) {
	if c.listErr != nil {
		return nil, c.listErr
	}
	return append([]domain.RuntimeUser(nil), c.users...), nil
}

func (c *fakeRuntimeClient) AddUser(_ context.Context, _ domain.ProxyNode, user domain.RuntimeUser) error {
	c.added = append(c.added, user)
	return nil
}

func (c *fakeRuntimeClient) RemoveUser(_ context.Context, _ domain.ProxyNode, email string) error {
	c.removed = append(c.removed, email)
	return nil
}

func runtimeUser(accountID string, uuid string, flow string) domain.RuntimeUser {
	return domain.RuntimeUser{
		ProxyAccountID: accountID,
		Email:          domain.RuntimeProxyAccountEmail(accountID),
		UUID:           uuid,
		Flow:           flow,
	}
}

func testNode() domain.ProxyNode {
	return domain.ProxyNode{
		ID:                "node-1",
		Name:              "node-1",
		Runtime:           "xray",
		RuntimeAPIEnabled: true,
		RuntimeAPIHost:    "10.13.13.1",
		RuntimeAPIPort:    10085,
		RuntimeInboundTag: "proxy-control-plane-vless-in",
	}
}
