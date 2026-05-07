package runtimesync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ziyan-c/proxy-control-plane/internal/domain"
)

type Store interface {
	ListRuntimeSyncNodes(ctx context.Context) ([]domain.ProxyNode, error)
	ListRuntimeTargetUsers(ctx context.Context, nodeID string, now time.Time) ([]domain.RuntimeUser, error)
	MarkProxyNodeRuntimeSync(ctx context.Context, nodeID string, syncedAt time.Time, syncErr string) error
}

type RuntimeClient interface {
	ListUsers(ctx context.Context, node domain.ProxyNode) ([]domain.RuntimeUser, error)
	AddUser(ctx context.Context, node domain.ProxyNode, user domain.RuntimeUser) error
	RemoveUser(ctx context.Context, node domain.ProxyNode, email string) error
}

type Options struct {
	Interval    time.Duration
	Timeout     time.Duration
	Concurrency int
}

type Syncer struct {
	store  Store
	client RuntimeClient
	opts   Options
}

type NodeResult struct {
	NodeID      string
	NodeName    string
	TargetHash  string
	RuntimeHash string
	Added       int
	Removed     int
	Unchanged   bool
}

const defaultInterval = 10 * time.Minute

func New(store Store, client RuntimeClient, opts Options) *Syncer {
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
	log.Printf("runtime sync enabled: interval=%s concurrency=%d", s.opts.Interval, s.opts.Concurrency)
	s.syncAll(ctx)

	ticker := time.NewTicker(s.opts.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("runtime sync stopped")
			return
		case <-ticker.C:
			s.syncAll(ctx)
		}
	}
}

func (s *Syncer) syncAll(ctx context.Context) {
	nodes, err := s.store.ListRuntimeSyncNodes(ctx)
	if err != nil {
		log.Printf("runtime sync list nodes failed: %v", err)
		return
	}
	if len(nodes) == 0 {
		return
	}

	sem := make(chan struct{}, s.opts.Concurrency)
	var wg sync.WaitGroup
	for _, node := range nodes {
		node := node
		if strings.TrimSpace(node.RuntimeAPIHost) == "" || node.RuntimeAPIPort == 0 || strings.TrimSpace(node.RuntimeInboundTag) == "" {
			log.Printf("runtime sync skip node %s: incomplete runtime API config", node.Name)
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

			result, err := s.SyncNode(runCtx, node, time.Now().UTC())
			if err != nil {
				log.Printf("runtime sync node %s failed: %v", node.Name, err)
				return
			}
			if result.Unchanged {
				log.Printf("runtime sync node %s unchanged hash=%s", node.Name, result.TargetHash)
				return
			}
			log.Printf("runtime sync node %s applied: added=%d removed=%d target_hash=%s runtime_hash=%s",
				node.Name, result.Added, result.Removed, result.TargetHash, result.RuntimeHash)
		}()
	}
	wg.Wait()
}

func (s *Syncer) SyncNode(ctx context.Context, node domain.ProxyNode, now time.Time) (NodeResult, error) {
	result := NodeResult{
		NodeID:   node.ID,
		NodeName: node.Name,
	}

	targetUsers, err := s.store.ListRuntimeTargetUsers(ctx, node.ID, now)
	if err != nil {
		s.markNodeError(ctx, node.ID, err)
		return result, err
	}
	runtimeUsers, err := s.client.ListUsers(ctx, node)
	if err != nil {
		s.markNodeError(ctx, node.ID, err)
		return result, err
	}

	targetByEmail := usersByEmail(targetUsers)
	runtimeManagedUsers := managedUsers(runtimeUsers)
	runtimeByEmail := usersByEmail(runtimeManagedUsers)

	result.TargetHash = UsersHash(targetUsers)
	result.RuntimeHash = UsersHash(mapUsers(runtimeByEmail))
	if result.TargetHash == result.RuntimeHash {
		result.Unchanged = true
		if err := s.store.MarkProxyNodeRuntimeSync(ctx, node.ID, now, ""); err != nil {
			return result, err
		}
		return result, nil
	}

	for _, email := range sortedMapKeys(runtimeByEmail) {
		runtimeUser := runtimeByEmail[email]
		targetUser, ok := targetByEmail[email]
		if ok && sameUser(runtimeUser, targetUser) {
			continue
		}
		if err := s.client.RemoveUser(ctx, node, email); err != nil {
			s.markNodeError(ctx, node.ID, err)
			return result, fmt.Errorf("remove runtime user %s: %w", email, err)
		}
		result.Removed++
	}

	for _, email := range sortedMapKeys(targetByEmail) {
		targetUser := targetByEmail[email]
		runtimeUser, ok := runtimeByEmail[email]
		if ok && sameUser(runtimeUser, targetUser) {
			continue
		}
		if err := s.client.AddUser(ctx, node, targetUser); err != nil {
			s.markNodeError(ctx, node.ID, err)
			return result, fmt.Errorf("add runtime user %s: %w", email, err)
		}
		result.Added++
	}

	if err := s.store.MarkProxyNodeRuntimeSync(ctx, node.ID, now, ""); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Syncer) markNodeError(ctx context.Context, nodeID string, err error) {
	if err == nil {
		return
	}
	if markErr := s.store.MarkProxyNodeRuntimeSync(ctx, nodeID, time.Time{}, err.Error()); markErr != nil {
		log.Printf("runtime sync mark node %s failed: %v", nodeID, markErr)
	}
}

func managedUsers(users []domain.RuntimeUser) []domain.RuntimeUser {
	result := make([]domain.RuntimeUser, 0, len(users))
	for _, user := range users {
		accountID, ok := domain.ProxyAccountIDFromRuntimeEmail(user.Email)
		if !ok {
			continue
		}
		user.ProxyAccountID = accountID
		result = append(result, normalizeUser(user))
	}
	return result
}

func usersByEmail(users []domain.RuntimeUser) map[string]domain.RuntimeUser {
	result := make(map[string]domain.RuntimeUser, len(users))
	for _, user := range users {
		user = normalizeUser(user)
		if user.Email == "" {
			continue
		}
		result[user.Email] = user
	}
	return result
}

func mapUsers(users map[string]domain.RuntimeUser) []domain.RuntimeUser {
	result := make([]domain.RuntimeUser, 0, len(users))
	for _, user := range users {
		result = append(result, user)
	}
	return result
}

func userIdentity(user domain.RuntimeUser) string {
	user = normalizeUser(user)
	return strings.ToLower(user.UUID) + "\x00" + user.Flow
}

func sameUser(left domain.RuntimeUser, right domain.RuntimeUser) bool {
	left = normalizeUser(left)
	right = normalizeUser(right)
	return left.Email == right.Email &&
		strings.EqualFold(left.UUID, right.UUID) &&
		left.Flow == right.Flow
}

func UsersHash(users []domain.RuntimeUser) string {
	normalized := make([]domain.RuntimeUser, 0, len(users))
	for _, user := range users {
		user = normalizeUser(user)
		if user.Email == "" || user.UUID == "" {
			continue
		}
		normalized = append(normalized, user)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Email < normalized[j].Email
	})

	h := sha256.New()
	for _, user := range normalized {
		h.Write([]byte(user.Email))
		h.Write([]byte{0})
		h.Write([]byte(strings.ToLower(user.UUID)))
		h.Write([]byte{0})
		h.Write([]byte(user.Flow))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func normalizeUser(user domain.RuntimeUser) domain.RuntimeUser {
	user.ProxyAccountID = strings.TrimSpace(user.ProxyAccountID)
	user.Email = strings.TrimSpace(user.Email)
	user.UUID = strings.TrimSpace(user.UUID)
	user.Flow = strings.TrimSpace(user.Flow)
	return user
}

func sortedMapKeys(values map[string]domain.RuntimeUser) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
