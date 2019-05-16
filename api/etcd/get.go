package etcd

import (
	"context"

	"github.com/coreos/etcd/clientv3"
	"github.com/gogo/protobuf/proto"
	"github.com/heetch/regula"
	"github.com/heetch/regula/api"
	pb "github.com/heetch/regula/api/etcd/proto"
	"github.com/pkg/errors"
)

// Get returns the ruleset related to the given path.
func (s *RulesetService) Get(ctx context.Context, path, version string) (*regula.Ruleset, error) {
	if path == "" {
		return nil, api.ErrRulesetNotFound
	}

	ops := []clientv3.Op{
		clientv3.OpGet(s.signaturesPath(path)),
		clientv3.OpGet(s.rulesPath(path, "")+versionSeparator, clientv3.WithKeysOnly()),
	}

	if version == "" {
		ops = append(ops, clientv3.OpGet(s.rulesPath(path, "")+versionSeparator, clientv3.WithLastKey()...))
	} else {
		ops = append(ops, clientv3.OpGet(s.rulesPath(path, version)))
	}

	// running all the requests within a single transaction so only one network round trip is performed.
	resp, err := s.Client.KV.Txn(ctx).Then(ops...).Commit()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch ruleset: %s", path)
	}

	if len(resp.Responses) != 3 {
		return nil, api.ErrRulesetNotFound
	}

	// decode signature
	var sig pb.Signature
	gresp := resp.Responses[0].GetResponseRange()
	if gresp.Count == 0 {
		return nil, api.ErrRulesetNotFound
	}
	err = proto.Unmarshal(gresp.Kvs[0].Value, &sig)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal signature")
	}

	// the versions must be parsed from the keys returned by the second operation
	versions := make([]string, len(resp.Responses[1].GetResponseRange().Kvs))
	for i, kv := range resp.Responses[1].GetResponseRange().Kvs {
		_, versions[i] = s.pathVersionFromKey(string(kv.Key))
	}

	// decode rules
	var rules pb.Rules
	err = proto.Unmarshal(resp.Responses[2].GetResponseRange().Kvs[0].Value, &rules)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal rules")
	}

	// if the version wasn't specified, get the latest returned
	if version == "" {
		version = versions[len(versions)-1]
	}

	return &regula.Ruleset{
		Path:      path,
		Version:   version,
		Rules:     rulesFromProtobuf(&rules),
		Signature: signatureFromProtobuf(&sig),
		Versions:  versions,
	}, nil
}
