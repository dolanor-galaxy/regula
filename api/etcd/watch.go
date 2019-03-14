package etcd

import (
	"context"
	"strconv"

	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/mvcc/mvccpb"
	"github.com/gogo/protobuf/proto"
	"github.com/heetch/regula/api"
	pb "github.com/heetch/regula/api/etcd/proto"
	"github.com/pkg/errors"
)

// Watch the given prefix for anything new.
func (s *RulesetService) Watch(ctx context.Context, prefix string, revision string) (*api.RulesetEvents, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	opts := []clientv3.OpOption{clientv3.WithPrefix()}
	if i, _ := strconv.ParseInt(revision, 10, 64); i > 0 {
		// watch from the next revision
		opts = append(opts, clientv3.WithRev(i+1))
	}

	events := api.RulesetEvents{
		Revision: revision,
	}

	wc := s.Client.Watch(ctx, s.rulesPath(prefix, ""), opts...)
	for {
		select {
		case wresp := <-wc:
			if err := wresp.Err(); err != nil {
				return nil, errors.Wrapf(err, "failed to watch prefix: '%s'", prefix)
			}

			if len(wresp.Events) == 0 {
				continue
			}

			list := make([]api.RulesetEvent, len(wresp.Events))
			for i, ev := range wresp.Events {
				switch ev.Type {
				case mvccpb.PUT:
					list[i].Type = api.RulesetPutEvent
				default:
					s.Logger.Debug().Str("type", string(ev.Type)).Msg("watch: ignoring event type")
					continue
				}

				var pbrs pb.Rules
				err := proto.Unmarshal(ev.Kv.Value, &pbrs)
				if err != nil {
					s.Logger.Debug().Bytes("entry", ev.Kv.Value).Msg("watch: unmarshalling failed")
					return nil, errors.Wrap(err, "failed to unmarshal entry")
				}
				path, version := s.pathVersionFromKey(string(ev.Kv.Key))
				list[i].Path = path
				list[i].Rules = rulesFromProtobuf(&pbrs)
				list[i].Version = version
			}

			events.Events = list
			events.Revision = strconv.FormatInt(wresp.Header.Revision, 10)
			return &events, nil
		case <-ctx.Done():
			events.Timeout = true
			return &events, ctx.Err()
		}
	}
}