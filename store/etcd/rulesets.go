package etcd

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/clientv3/concurrency"
	"github.com/coreos/etcd/mvcc/mvccpb"
	"github.com/gogo/protobuf/proto"
	"github.com/heetch/regula"
	rerrors "github.com/heetch/regula/errors"
	"github.com/heetch/regula/rule"
	"github.com/heetch/regula/store"
	pb "github.com/heetch/regula/store/etcd/proto"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/segmentio/ksuid"
)

// versionSeparator separates the path from the version in the entries path in etcd.
// The purpose is to have the same ordering as the others namespace (latest, versions, ...).
const versionSeparator = "!"

// RulesetService manages the rulesets using etcd.
type RulesetService struct {
	Client    *clientv3.Client
	Logger    zerolog.Logger
	Namespace string
}

func computeLimit(l int) int {
	if l <= 0 || l > 100 {
		return 50 // TODO: make this one configurable
	}
	return l
}

// Get returns the ruleset related to the given path. By default, it returns the latest one.
// It returns the related ruleset version if it's specified.
func (s *RulesetService) Get(ctx context.Context, path, version string) (*store.RulesetEntry, error) {
	var (
		entry *store.RulesetEntry
		err   error
	)

	if version == "" {
		entry, err = s.Latest(ctx, path)
	} else {
		entry, err = s.OneByVersion(ctx, path, version)
	}
	if err != nil {
		return nil, err
	}

	resp, err := s.Client.KV.Get(ctx, s.versionsPath(path))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch versions of the entry: %s", path)
	}
	if resp.Count == 0 {
		s.Logger.Debug().Str("path", path).Msg("cannot find ruleset versions list")
		return nil, store.ErrNotFound
	}

	var v pb.Versions
	err = proto.Unmarshal(resp.Kvs[0].Value, &v)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal versions")
	}
	entry.Versions = v.Versions

	resp, err = s.Client.KV.Get(ctx, s.signaturesPath(path))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch signature of the entry: %s", path)
	}
	if resp.Count == 0 {
		s.Logger.Debug().Str("path", path).Msg("cannot find ruleset signature")
		return nil, store.ErrNotFound
	}

	var sig pb.Signature
	err = proto.Unmarshal(resp.Kvs[0].Value, &sig)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal signature")
	}
	entry.Signature = signatureFromProtobuf(&sig)

	return entry, nil
}

// List returns the latest version of each ruleset under the given prefix.
// If the prefix is empty, it returns entries from the beginning following the lexical order.
// The listing can be customised using the ListOptions type.
func (s *RulesetService) List(ctx context.Context, prefix string, opt *store.ListOptions) (*store.RulesetEntries, error) {
	options := make([]clientv3.OpOption, 0, 3)

	var key string

	limit := computeLimit(opt.Limit)

	if opt.ContinueToken != "" {
		lastPath, err := base64.URLEncoding.DecodeString(opt.ContinueToken)
		if err != nil {
			return nil, store.ErrInvalidContinueToken
		}

		key = string(lastPath)

		var rangeEnd string
		if opt.AllVersions {
			rangeEnd = clientv3.GetPrefixRangeEnd(s.entriesPath(prefix, ""))
		} else {
			rangeEnd = clientv3.GetPrefixRangeEnd(s.latestRulesetPath(prefix))
		}
		options = append(options, clientv3.WithRange(rangeEnd))
	} else {
		key = prefix
		options = append(options, clientv3.WithPrefix())
	}

	options = append(options, clientv3.WithLimit(int64(limit)))

	switch {
	case opt.PathsOnly:
		return s.listPathsOnly(ctx, key, prefix, limit, options)
	case opt.AllVersions:
		return s.listAllVersions(ctx, key, prefix, limit, options)
	default:
		return s.listLastVersion(ctx, key, prefix, limit, options)
	}
}

// listPathsOnly returns only the path for each ruleset.
func (s *RulesetService) listPathsOnly(ctx context.Context, key, prefix string, limit int, opts []clientv3.OpOption) (*store.RulesetEntries, error) {
	opts = append(opts, clientv3.WithKeysOnly())
	resp, err := s.Client.KV.Get(ctx, s.latestRulesetPath(key), opts...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch all entries")
	}

	// if a prefix is provided it must always return results
	// otherwise it doesn't exist.
	if resp.Count == 0 && prefix != "" {
		return nil, store.ErrNotFound
	}

	var entries store.RulesetEntries
	entries.Revision = strconv.FormatInt(resp.Header.Revision, 10)
	for _, pair := range resp.Kvs {
		p := strings.TrimPrefix(string(pair.Key), s.latestRulesetPath("")+"/")
		entries.Entries = append(entries.Entries, store.RulesetEntry{Path: p})
	}

	if len(entries.Entries) < limit || !resp.More {
		return &entries, nil
	}

	lastEntry := entries.Entries[len(entries.Entries)-1]

	// we want to start immediately after the last key
	entries.Continue = base64.URLEncoding.EncodeToString([]byte(lastEntry.Path + "\x00"))

	return &entries, nil
}

// listLastVersion returns only the latest version for each ruleset.
func (s *RulesetService) listLastVersion(ctx context.Context, key, prefix string, limit int, opts []clientv3.OpOption) (*store.RulesetEntries, error) {
	resp, err := s.Client.KV.Get(ctx, s.latestRulesetPath(key), opts...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch latests keys")
	}

	// if a prefix is provided it must always return results
	// otherwise it doesn't exist.
	if resp.Count == 0 && prefix != "" {
		return nil, store.ErrNotFound
	}

	ops := make([]clientv3.Op, 0, resp.Count)
	txn := s.Client.KV.Txn(ctx)
	for _, pair := range resp.Kvs {
		ops = append(ops, clientv3.OpGet(string(pair.Value)))
	}
	txnresp, err := txn.Then(ops...).Commit()
	if err != nil {
		return nil, errors.Wrap(err, "transaction failed to fetch all entries")
	}

	var entries store.RulesetEntries
	entries.Revision = strconv.FormatInt(resp.Header.Revision, 10)
	entries.Entries = make([]store.RulesetEntry, len(resp.Kvs))

	// Responses handles responses for each OpGet calls in the transaction.
	for i, resps := range txnresp.Responses {
		var pbrse pb.RulesetEntry
		rr := resps.GetResponseRange()

		// Given that we are getting a leaf in the tree (a ruleset entry), we are sure that we will always have one value in the Kvs slice.
		err = proto.Unmarshal(rr.Kvs[0].Value, &pbrse)
		if err != nil {
			s.Logger.Debug().Err(err).Bytes("entry", rr.Kvs[0].Value).Msg("list: unmarshalling failed")
			return nil, errors.Wrap(err, "failed to unmarshal entry")
		}

		entries.Entries[i] = store.RulesetEntry{
			Path:    pbrse.Path,
			Version: pbrse.Version,
			Ruleset: rulesetFromProtobuf(pbrse.Ruleset),
		}
	}

	if len(entries.Entries) < limit || !resp.More {
		return &entries, nil
	}

	lastEntry := entries.Entries[len(entries.Entries)-1]

	// we want to start immediately after the last key
	entries.Continue = base64.URLEncoding.EncodeToString([]byte(lastEntry.Path + "\x00"))

	return &entries, nil
}

// listAllVersions returns all available versions for each ruleset.
func (s *RulesetService) listAllVersions(ctx context.Context, key, prefix string, limit int, opts []clientv3.OpOption) (*store.RulesetEntries, error) {
	resp, err := s.Client.KV.Get(ctx, s.entriesPath(key, ""), opts...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch all entries")
	}

	// if a prefix is provided it must always return results
	// otherwise it doesn't exist.
	if resp.Count == 0 && prefix != "" {
		return nil, store.ErrNotFound
	}

	var entries store.RulesetEntries
	entries.Revision = strconv.FormatInt(resp.Header.Revision, 10)
	entries.Entries = make([]store.RulesetEntry, len(resp.Kvs))
	for i, pair := range resp.Kvs {
		var pbrse pb.RulesetEntry

		err = proto.Unmarshal(pair.Value, &pbrse)
		if err != nil {
			s.Logger.Debug().Err(err).Bytes("entry", pair.Value).Msg("list: unmarshalling failed")
			return nil, errors.Wrap(err, "failed to unmarshal entry")
		}

		entries.Entries[i] = store.RulesetEntry{
			Path:    pbrse.Path,
			Version: pbrse.Version,
			Ruleset: rulesetFromProtobuf(pbrse.Ruleset),
		}
	}

	if len(entries.Entries) < limit || !resp.More {
		return &entries, nil
	}

	lastEntry := entries.Entries[len(entries.Entries)-1]

	// we want to start immediately after the last key
	entries.Continue = base64.URLEncoding.EncodeToString([]byte(lastEntry.Path + versionSeparator + lastEntry.Version + "\x00"))

	return &entries, nil
}

// Latest returns the latest version of the ruleset entry which corresponds to the given path.
// It returns store.ErrNotFound if the path doesn't exist or if it's not a ruleset.
func (s *RulesetService) Latest(ctx context.Context, path string) (*store.RulesetEntry, error) {
	if path == "" {
		return nil, store.ErrNotFound
	}

	resp, err := s.Client.KV.Get(ctx, s.entriesPath(path, "")+versionSeparator, clientv3.WithLastKey()...)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch the entry: %s", path)
	}

	// Count will be 0 if the path doesn't exist or if it's not a ruleset.
	if resp.Count == 0 {
		return nil, store.ErrNotFound
	}

	var entry pb.RulesetEntry
	err = proto.Unmarshal(resp.Kvs[0].Value, &entry)
	if err != nil {
		s.Logger.Debug().Err(err).Bytes("entry", resp.Kvs[0].Value).Msg("latest: unmarshalling failed")
		return nil, errors.Wrap(err, "failed to unmarshal entry")
	}

	return &store.RulesetEntry{
		Path:    entry.Path,
		Version: entry.Version,
		Ruleset: rulesetFromProtobuf(entry.Ruleset),
	}, nil
}

// OneByVersion returns the ruleset entry which corresponds to the given path at the given version.
// It returns store.ErrNotFound if the path doesn't exist or if it's not a ruleset.
func (s *RulesetService) OneByVersion(ctx context.Context, path, version string) (*store.RulesetEntry, error) {
	if path == "" {
		return nil, store.ErrNotFound
	}

	resp, err := s.Client.KV.Get(ctx, s.entriesPath(path, version))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to fetch the entry: %s", path)
	}

	// Count will be 0 if the path doesn't exist or if it's not a ruleset.
	if resp.Count == 0 {
		return nil, store.ErrNotFound
	}

	var entry pb.RulesetEntry
	err = proto.Unmarshal(resp.Kvs[0].Value, &entry)
	if err != nil {
		s.Logger.Debug().Err(err).Bytes("entry", resp.Kvs[0].Value).Msg("one-by-version: unmarshalling failed")
		return nil, errors.Wrap(err, "failed to unmarshal entry")
	}

	return &store.RulesetEntry{
		Path:    entry.Path,
		Version: entry.Version,
		Ruleset: rulesetFromProtobuf(entry.Ruleset),
	}, nil
}

// putVersions stores the new version or appends it to the existing ones under the key <namespace>/rulesets/versions/<path>.
func (s *RulesetService) putVersions(stm concurrency.STM, path, version string) error {
	var v pb.Versions

	res := stm.Get(s.versionsPath(path))
	if res != "" {
		err := proto.Unmarshal([]byte(res), &v)
		if err != nil {
			s.Logger.Debug().Err(err).Str("path", path).Msg("put: versions unmarshalling failed")
			return errors.Wrap(err, "failed to unmarshal versions")
		}
	}

	v.Versions = append(v.Versions, version)
	bvs, err := proto.Marshal(&v)
	if err != nil {
		return errors.Wrap(err, "failed to encode versions")
	}
	stm.Put(s.versionsPath(path), string(bvs))

	return nil
}

func (s *RulesetService) putEntry(stm concurrency.STM, rse *store.RulesetEntry) error {
	pbrse := pb.RulesetEntry{
		Path:    rse.Path,
		Version: rse.Version,
		Ruleset: rulesetToProtobuf(rse.Ruleset),
	}

	b, err := proto.Marshal(&pbrse)
	if err != nil {
		return errors.Wrap(err, "failed to encode entry")
	}
	stm.Put(s.entriesPath(rse.Path, rse.Version), string(b))

	return nil
}

// Put adds a version of the given ruleset using an uuid.
func (s *RulesetService) Put(ctx context.Context, path string, ruleset *regula.Ruleset) (*store.RulesetEntry, error) {
	sig, err := validateRuleset(path, ruleset)
	if err != nil {
		return nil, err
	}

	var entry store.RulesetEntry

	txfn := func(stm concurrency.STM) error {
		// generate a checksum from the ruleset for comparison purpose
		h := md5.New()
		err = json.NewEncoder(h).Encode(ruleset)
		if err != nil {
			return errors.Wrap(err, "failed to generate checksum")
		}
		checksum := string(h.Sum(nil))

		// if nothing changed return latest ruleset
		if stm.Get(s.checksumsPath(path)) == checksum {
			v := stm.Get(stm.Get(s.latestRulesetPath(path)))

			var pbrse pb.RulesetEntry
			err = proto.Unmarshal([]byte(v), &pbrse)
			if err != nil {
				s.Logger.Debug().Err(err).Str("entry", v).Msg("put: entry unmarshalling failed")
				return errors.Wrap(err, "failed to unmarshal entry")
			}

			s.Logger.Debug().Str("path", path).Msg("ruleset didn't change, returning without creating a new version")

			entry.Path = pbrse.Path
			entry.Version = pbrse.Version
			entry.Ruleset = rulesetFromProtobuf(pbrse.Ruleset)

			return store.ErrNotModified
		}

		// make sure signature didn't change
		rawSig := stm.Get(s.signaturesPath(path))
		if rawSig != "" {
			var pbsig pb.Signature

			err := proto.Unmarshal([]byte(rawSig), &pbsig)
			if err != nil {
				s.Logger.Debug().Err(err).Str("signature", rawSig).Msg("put: signature unmarshalling failed")
				return errors.Wrap(err, "failed to decode ruleset signature")
			}

			err = compareSignature(signatureFromProtobuf(&pbsig), sig)
			if err != nil {
				return err
			}
		}

		// if no signature found, create one
		if rawSig == "" {
			b, err := proto.Marshal(signatureToProtobuf(sig))
			if err != nil {
				return errors.Wrap(err, "failed to encode updated signature")
			}

			stm.Put(s.signaturesPath(path), string(b))
		}

		// update checksum
		stm.Put(s.checksumsPath(path), checksum)

		// create a new ruleset version
		k, err := ksuid.NewRandom()
		if err != nil {
			return errors.Wrap(err, "failed to generate ruleset version")
		}
		version := k.String()

		err = s.putVersions(stm, path, version)
		if err != nil {
			return err
		}

		re := store.RulesetEntry{
			Path:    path,
			Version: version,
			Ruleset: ruleset,
		}

		err = s.putEntry(stm, &re)
		if err != nil {
			return err
		}

		// update the pointer to the latest ruleset
		stm.Put(s.latestRulesetPath(path), s.entriesPath(path, version))

		entry = re
		return nil
	}

	_, err = concurrency.NewSTM(s.Client, txfn, concurrency.WithAbortContext(ctx))
	if err != nil && err != store.ErrNotModified && !store.IsValidationError(err) {
		return nil, errors.Wrap(err, "failed to put ruleset")
	}

	return &entry, err
}

func compareSignature(base, other *regula.Signature) error {
	if base.ReturnType != other.ReturnType {
		return &store.ValidationError{
			Field:  "return type",
			Value:  other.ReturnType,
			Reason: fmt.Sprintf("signature mismatch: return type must be of type %s", base.ReturnType),
		}
	}

	for name, tp := range other.ParamTypes {
		stp, ok := base.ParamTypes[name]
		if !ok {
			return &store.ValidationError{
				Field:  "param",
				Value:  name,
				Reason: "signature mismatch: unknown parameter",
			}
		}

		if tp != stp {
			return &store.ValidationError{
				Field:  "param type",
				Value:  tp,
				Reason: fmt.Sprintf("signature mismatch: param must be of type %s", stp),
			}
		}
	}

	return nil
}

func validateRuleset(path string, rs *regula.Ruleset) (*regula.Signature, error) {
	err := validateRulesetName(path)
	if err != nil {
		return nil, err
	}

	sig := regula.NewSignature(rs)

	for _, r := range rs.Rules {
		params := r.Params()
		err = validateParamNames(params)
		if err != nil {
			return nil, err
		}
	}

	return sig, nil
}

// regex used to validate ruleset names.
var rgxRuleset = regexp.MustCompile(`^[a-z]+(?:[a-z0-9-\/]?[a-z0-9])*$`)

func validateRulesetName(path string) error {
	if !rgxRuleset.MatchString(path) {
		return &store.ValidationError{
			Field:  "path",
			Value:  path,
			Reason: "invalid format",
		}
	}

	return nil
}

// regex used to validate parameters name.
var rgxParam = regexp.MustCompile(`^[a-z]+(?:[a-z0-9-]?[a-z0-9])*$`)

// list of reserved words that shouldn't be used as parameters.
var reservedWords = []string{
	"version",
	"list",
	"eval",
	"watch",
	"revision",
}

func validateParamNames(params []rule.Param) error {
	for i := range params {
		if !rgxParam.MatchString(params[i].Name) {
			return &store.ValidationError{
				Field:  "param",
				Value:  params[i].Name,
				Reason: "invalid format",
			}
		}

		for _, w := range reservedWords {
			if params[i].Name == w {
				return &store.ValidationError{
					Field:  "param",
					Value:  params[i].Name,
					Reason: "forbidden value",
				}
			}
		}
	}

	return nil
}

// Watch the given prefix for anything new.
func (s *RulesetService) Watch(ctx context.Context, prefix string, revision string) (*store.RulesetEvents, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	opts := []clientv3.OpOption{clientv3.WithPrefix()}
	if i, _ := strconv.ParseInt(revision, 10, 64); i > 0 {
		// watch from the next revision
		opts = append(opts, clientv3.WithRev(i+1))
	}

	wc := s.Client.Watch(ctx, s.entriesPath(prefix, ""), opts...)
	for {
		select {
		case wresp := <-wc:
			if err := wresp.Err(); err != nil {
				return nil, errors.Wrapf(err, "failed to watch prefix: '%s'", prefix)
			}

			if len(wresp.Events) == 0 {
				continue
			}

			events := make([]store.RulesetEvent, len(wresp.Events))
			for i, ev := range wresp.Events {
				switch ev.Type {
				case mvccpb.PUT:
					events[i].Type = store.RulesetPutEvent
				default:
					s.Logger.Debug().Str("type", string(ev.Type)).Msg("watch: ignoring event type")
					continue
				}

				var pbrse pb.RulesetEntry
				err := proto.Unmarshal(ev.Kv.Value, &pbrse)
				if err != nil {
					s.Logger.Debug().Bytes("entry", ev.Kv.Value).Msg("watch: unmarshalling failed")
					return nil, errors.Wrap(err, "failed to unmarshal entry")
				}
				events[i].Path = pbrse.Path
				events[i].Ruleset = rulesetFromProtobuf(pbrse.Ruleset)
				events[i].Version = pbrse.Version
			}

			return &store.RulesetEvents{
				Events:   events,
				Revision: strconv.FormatInt(wresp.Header.Revision, 10),
			}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

}

// Eval evaluates a ruleset given a path and a set of parameters. It implements the regula.Evaluator interface.
func (s *RulesetService) Eval(ctx context.Context, path string, params rule.Params) (*regula.EvalResult, error) {
	re, err := s.Latest(ctx, path)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, rerrors.ErrRulesetNotFound
		}

		return nil, err
	}

	v, err := re.Ruleset.Eval(params)
	if err != nil {
		return nil, err
	}

	return &regula.EvalResult{
		Value:   v,
		Version: re.Version,
	}, nil
}

// EvalVersion evaluates a ruleset given a path and a set of parameters. It implements the regula.Evaluator interface.
func (s *RulesetService) EvalVersion(ctx context.Context, path, version string, params rule.Params) (*regula.EvalResult, error) {
	re, err := s.OneByVersion(ctx, path, version)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, rerrors.ErrRulesetNotFound
		}

		return nil, err
	}

	v, err := re.Ruleset.Eval(params)
	if err != nil {
		return nil, err
	}

	return &regula.EvalResult{
		Value:   v,
		Version: re.Version,
	}, nil
}

// entriesPath returns the path where the rulesets are stored in etcd.
func (s *RulesetService) entriesPath(p, v string) string {
	// If the version parameter is not empty, we concatenate to the path separated by the versionSeparator value.
	if v != "" {
		p += versionSeparator + v
	}
	return path.Join(s.Namespace, "rulesets", "entries", p)
}

// checksumsPath returns the path where the checksums are stored in etcd.
func (s *RulesetService) checksumsPath(p string) string {
	return path.Join(s.Namespace, "rulesets", "checksums", p)
}

// signaturesPath returns the path where the signatures are stored in etcd.
func (s *RulesetService) signaturesPath(p string) string {
	return path.Join(s.Namespace, "rulesets", "signatures", p)
}

// latestRulesetPath returns the path where the latest version of each ruleset is stored in etcd.
func (s *RulesetService) latestRulesetPath(p string) string {
	return path.Join(s.Namespace, "rulesets", "latest", p)
}

// versionsPath returns the path where the versions of each rulesets are stored in etcd.
func (s *RulesetService) versionsPath(p string) string {
	return path.Join(s.Namespace, "rulesets", "versions", p)
}
