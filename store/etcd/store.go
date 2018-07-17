package etcd

import (
	"context"
	"encoding/json"
	ppath "path"
	"strconv"

	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/mvcc/mvccpb"
	"github.com/heetch/regula/rule"
	"github.com/heetch/regula/store"
	"github.com/pkg/errors"
	"github.com/segmentio/ksuid"
)

var _ store.Store = new(Store)

// Store manages the storage of rulesets in etcd.
type Store struct {
	Client    *clientv3.Client
	Namespace string
}

// List returns all the rulesets entries under the given prefix.
func (s *Store) List(ctx context.Context, prefix string) (*store.RulesetEntries, error) {
	resp, err := s.Client.KV.Get(ctx, ppath.Join(s.Namespace, prefix), clientv3.WithPrefix())
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch all entries")
	}

	var entries store.RulesetEntries
	entries.Revision = strconv.FormatInt(resp.Header.Revision, 10)
	entries.Entries = make([]store.RulesetEntry, len(resp.Kvs))
	for i, pair := range resp.Kvs {
		err = json.Unmarshal(pair.Value, &entries.Entries[i])
		if err != nil {
			return nil, errors.Wrap(err, "failed to unmarshal entry")
		}
	}

	return &entries, nil
}

// Latest returns the latest version of the ruleset entry which corresponds to the given path.
// It returns store.ErrNotFound if the path doesn't exist or if it's not a ruleset.
func (s *Store) Latest(ctx context.Context, path string) (*store.RulesetEntry, error) {
	resp, err := s.Client.KV.Get(ctx, ppath.Join(s.Namespace, path)+"/", clientv3.WithLastKey()...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch the entry: %s", path)
	}

	// Count will be 0 if the path doesn't exist or if it's not a ruleset.
	if resp.Count == 0 {
		return nil, store.ErrNotFound
	}

	var entry store.RulesetEntry
	err = json.Unmarshal(resp.Kvs[0].Value, &entry)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal entry")
	}

	return &entry, nil
}

// OneByVersion returns the ruleset entry which corresponds to the given path at the given version.
// It returns store.ErrNotFound if the path doesn't exist or if it's not a ruleset.
func (s *Store) OneByVersion(ctx context.Context, path, version string) (*store.RulesetEntry, error) {
	resp, err := s.Client.KV.Get(ctx, ppath.Join(s.Namespace, path, version))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch the entry: %s", path)
	}

	// Count will be 0 if the path doesn't exist or if it's not a ruleset.
	if resp.Count == 0 {
		return nil, store.ErrNotFound
	}

	var entry store.RulesetEntry
	err = json.Unmarshal(resp.Kvs[0].Value, &entry)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal entry")
	}

	return &entry, nil
}

// Put adds a version of the given ruleset using an uuid.
func (s *Store) Put(ctx context.Context, path string, ruleset *rule.Ruleset) (*store.RulesetEntry, error) {
	k, err := ksuid.NewRandom()
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate version")
	}

	v := k.String()

	re := store.RulesetEntry{
		Path:    path,
		Version: v,
		Ruleset: ruleset,
	}

	raw, err := json.Marshal(&re)
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode entry")
	}

	_, err = s.Client.KV.Put(ctx, ppath.Join(s.Namespace, path, v), string(raw))
	if err != nil {
		return nil, errors.Wrap(err, "failed to put entry")
	}

	return &re, nil
}

// Watch the given prefix for anything new.
func (s *Store) Watch(ctx context.Context, prefix string, revision string) (*store.Events, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	opts := []clientv3.OpOption{clientv3.WithPrefix()}
	if i, _ := strconv.ParseInt(revision, 10, 64); i > 0 {
		// watch from the next revision
		opts = append(opts, clientv3.WithRev(i+1))
	}

	wc := s.Client.Watch(ctx, ppath.Join(s.Namespace, prefix), opts...)
	select {
	case wresp := <-wc:
		events := make([]store.Event, len(wresp.Events))
		for i, ev := range wresp.Events {
			switch ev.Type {
			case mvccpb.PUT:
				events[i].Type = store.PutEvent
			case mvccpb.DELETE:
				events[i].Type = store.DeleteEvent
			}

			var e store.RulesetEntry
			err := json.Unmarshal(ev.Kv.Value, &e)
			if err != nil {
				return nil, errors.Wrap(err, "failed to unmarshal entry")
			}
			events[i].Path = e.Path
			events[i].Ruleset = e.Ruleset
		}

		return &store.Events{
			Events:   events,
			Revision: strconv.FormatInt(wresp.Header.Revision, 10),
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
