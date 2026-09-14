package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/monyratha/migration-verify/internal/report"
)

// objInfo is the normalised view of one object (or one version) on one side.
type objInfo struct {
	Key            string
	ETag           string
	Size           int64
	LastModified   time.Time
	VersionID      string
	IsDeleteMarker bool
	IsLatest       bool
	StorageClass   string

	// populated by HEAD (level >= 2)
	ContentType string
	Metadata    map[string]string // canonical lower-case header name -> value
	Tags        map[string]string
	Encrypted   bool
	Headed      bool
	HeadErr     error

	// populated by deep verification
	SHA256 string
}

var md5Hex = regexp.MustCompile(`^[0-9a-f]{32}$`)

// listObjects returns every object under prefix keyed by object name.
// With versions=true, every version (incl. delete markers) is returned as a
// slice per key, ordered by LastModified ascending.
func (s *storage) listObjects(ctx context.Context, bucket, prefix string, versions bool) (map[string][]*objInfo, error) {
	out := map[string][]*objInfo{}
	opts := minio.ListObjectsOptions{Prefix: prefix, Recursive: true, WithVersions: versions}
	for o := range s.client.ListObjects(ctx, bucket, opts) {
		if o.Err != nil {
			return nil, o.Err
		}
		out[o.Key] = append(out[o.Key], &objInfo{
			Key:            o.Key,
			ETag:           strings.Trim(o.ETag, `"`),
			Size:           o.Size,
			LastModified:   o.LastModified,
			VersionID:      o.VersionID,
			IsDeleteMarker: o.IsDeleteMarker,
			IsLatest:       o.IsLatest,
			StorageClass:   o.StorageClass,
		})
	}
	for _, vs := range out {
		sort.SliceStable(vs, func(i, j int) bool { return vs[i].LastModified.Before(vs[j].LastModified) })
	}
	return out, nil
}

// head enriches o with HEAD metadata (and tags when requested).
func (s *storage) head(ctx context.Context, bucket string, o *objInfo, withTags bool, ignore map[string]bool) {
	o.Headed = true
	st, err := s.client.StatObject(ctx, bucket, o.Key, minio.StatObjectOptions{VersionID: o.VersionID})
	if err != nil {
		o.HeadErr = err
		return
	}
	o.ContentType = st.ContentType
	if st.ETag != "" {
		o.ETag = strings.Trim(st.ETag, `"`)
	}
	o.Metadata = map[string]string{}
	for k, v := range st.Metadata {
		lk := strings.ToLower(k)
		switch {
		case lk == "content-type", lk == "x-amz-tagging-count", lk == "x-amz-storage-class":
			continue // compared separately / informational
		case lk == "x-amz-server-side-encryption":
			o.Encrypted = true
			continue
		case strings.HasPrefix(lk, "x-amz-server-side-encryption"):
			o.Encrypted = true
			continue
		}
		if ignore[lk] {
			continue
		}
		o.Metadata[lk] = strings.Join(v, ",")
	}
	if st.StorageClass != "" {
		o.StorageClass = st.StorageClass
	}
	if withTags {
		t, err := s.client.GetObjectTagging(ctx, bucket, o.Key, minio.GetObjectTaggingOptions{VersionID: o.VersionID})
		if err != nil {
			o.HeadErr = fmt.Errorf("tags: %w", err)
			return
		}
		if t != nil {
			o.Tags = t.ToMap()
		}
	}
}

// hash streams the object and returns its SHA-256 and byte count.
func (s *storage) hash(ctx context.Context, bucket string, o *objInfo) (string, int64, error) {
	r, err := s.client.GetObject(ctx, bucket, o.Key, minio.GetObjectOptions{VersionID: o.VersionID})
	if err != nil {
		return "", 0, err
	}
	defer r.Close()
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return "", n, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// etagVerdict classifies an ETag comparison.
type etagVerdict int

const (
	etagMatch        etagVerdict = iota
	etagMismatch                 // both are trustworthy MD5s and differ -> content differs
	etagInconclusive             // differ but at least one side is multipart / encrypted / non-MD5
)

func compareETags(a, b *objInfo) etagVerdict {
	if a.ETag == b.ETag {
		return etagMatch
	}
	if a.Encrypted || b.Encrypted || !md5Hex.MatchString(a.ETag) || !md5Hex.MatchString(b.ETag) {
		return etagInconclusive
	}
	return etagMismatch
}

func (o *objInfo) side() *report.ObjectSide {
	if o == nil {
		return nil
	}
	return &report.ObjectSide{
		Size: o.Size, ETag: o.ETag, ContentType: o.ContentType, LastModified: o.LastModified,
		VersionID: o.VersionID, StorageClass: o.StorageClass, Metadata: o.Metadata, Tags: o.Tags, SHA256: o.SHA256,
	}
}

// objectCounters accumulates per-check tallies while objects are compared.
type objectCounters struct {
	mu               sync.Mutex
	sizeMismatch     int
	etagMismatch     int
	etagInconclusive int
	contentTypeMis   int
	metadataMis      int
	tagsMis          int
	contentMis       int
	deepVerified     int
	deepBytes        int64
	errors           int
	matched          int
	results          []report.ObjectResult
}

// compareObjects runs the per-object comparison for keys present on both
// sides using a bounded worker pool. Missing/extra keys are handled by the caller.
func (v *Verifier) compareObjects(ctx context.Context, pair BucketPair, common []string, src, tgt map[string][]*objInfo) *objectCounters {
	c := &objectCounters{}
	ignore := map[string]bool{}
	for _, k := range v.cfg.IgnoreMetaKeys {
		ignore[strings.ToLower(k)] = true
	}
	var deepBudget int64 = v.cfg.MaxDeepBytes
	var budgetMu sync.Mutex

	sem := make(chan struct{}, v.cfg.Concurrency)
	var wg sync.WaitGroup
	var done int64
	lastLog := time.Now()
	var logMu sync.Mutex
	progress := func() {
		n := atomic.AddInt64(&done, 1)
		logMu.Lock()
		defer logMu.Unlock()
		if n == int64(len(common)) || time.Since(lastLog) >= 5*time.Second {
			lastLog = time.Now()
			v.Log("  %s: compared %d/%d objects", pair.Source, n, len(common))
		}
	}
	for _, key := range common {
		s := latest(src[key])
		t := latest(tgt[key])
		wg.Add(1)
		sem <- struct{}{}
		go func(key string, s, t *objInfo) {
			defer wg.Done()
			defer func() { <-sem }()
			defer progress()
			res := report.ObjectResult{Key: key, Status: report.Pass}
			var reasons []string
			var details []string

			if s.Size != t.Size {
				reasons = append(reasons, "size")
				details = append(details, fmt.Sprintf("size %d != %d", s.Size, t.Size))
			}

			if v.cfg.Level >= 2 {
				var hw sync.WaitGroup
				hw.Add(2)
				go func() { defer hw.Done(); v.src.head(ctx, pair.Source, s, v.cfg.CheckTags, ignore) }()
				go func() { defer hw.Done(); v.tgt.head(ctx, pair.Target, t, v.cfg.CheckTags, ignore) }()
				hw.Wait()
				if s.HeadErr != nil || t.HeadErr != nil {
					res.Status = report.Error
					if s.HeadErr != nil {
						details = append(details, "source HEAD: "+s.HeadErr.Error())
					}
					if t.HeadErr != nil {
						details = append(details, "target HEAD: "+t.HeadErr.Error())
					}
				} else {
					if s.ContentType != t.ContentType {
						reasons = append(reasons, "contentType")
						details = append(details, fmt.Sprintf("content-type %q != %q", s.ContentType, t.ContentType))
					}
					if d := diffMaps(s.Metadata, t.Metadata); d != "" {
						reasons = append(reasons, "metadata")
						details = append(details, "metadata: "+d)
					}
					if v.cfg.CheckTags {
						if d := diffMaps(s.Tags, t.Tags); d != "" {
							reasons = append(reasons, "tags")
							details = append(details, "tags: "+d)
						}
					}
				}
			}

			verdict := compareETags(s, t)
			needDeep := v.cfg.Level >= 3
			switch verdict {
			case etagMismatch:
				reasons = append(reasons, "etag")
				details = append(details, fmt.Sprintf("etag %s != %s", s.ETag, t.ETag))
			case etagInconclusive:
				if s.Size == t.Size && v.cfg.DeepOnInconclusive {
					needDeep = true
				} else if s.Size == t.Size {
					reasons = append(reasons, "etag-inconclusive")
					details = append(details, fmt.Sprintf("etag %s vs %s not comparable (multipart/encrypted); content not verified", s.ETag, t.ETag))
				}
			}

			if needDeep && s.Size == t.Size && res.Status != report.Error {
				allowed := true
				if deepBudget > 0 {
					budgetMu.Lock()
					if deepBudget-s.Size*2 < 0 {
						allowed = false
					} else {
						deepBudget -= s.Size * 2
					}
					budgetMu.Unlock()
				}
				if !allowed {
					if verdict == etagInconclusive {
						reasons = append(reasons, "etag-inconclusive")
						details = append(details, "deep verification skipped: --max-deep-bytes budget exhausted")
					}
				} else {
					var hs, ht string
					var ns, nt int64
					var es, et error
					var dw sync.WaitGroup
					dw.Add(2)
					go func() { defer dw.Done(); hs, ns, es = v.src.hash(ctx, pair.Source, s) }()
					go func() { defer dw.Done(); ht, nt, et = v.tgt.hash(ctx, pair.Target, t) }()
					dw.Wait()
					c.mu.Lock()
					c.deepBytes += ns + nt
					c.mu.Unlock()
					if es != nil || et != nil {
						res.Status = report.Error
						if es != nil {
							details = append(details, "source GET: "+es.Error())
						}
						if et != nil {
							details = append(details, "target GET: "+et.Error())
						}
					} else {
						s.SHA256, t.SHA256 = hs, ht
						c.mu.Lock()
						c.deepVerified++
						c.mu.Unlock()
						if hs != ht {
							reasons = append(reasons, "content")
							details = append(details, fmt.Sprintf("sha256 %s != %s", hs, ht))
						} else if verdict == etagInconclusive {
							details = append(details, "etags differ but content verified identical by sha256")
						}
					}
				}
			}

			if len(reasons) > 0 && res.Status != report.Error {
				res.Status = report.Fail
				if len(reasons) == 1 && reasons[0] == "etag-inconclusive" {
					res.Status = report.Warn
				}
			}
			res.Reasons = reasons
			res.Detail = strings.Join(details, "; ")
			res.Source, res.Target = s.side(), t.side()

			c.mu.Lock()
			defer c.mu.Unlock()
			for _, r := range reasons {
				switch r {
				case "size":
					c.sizeMismatch++
				case "etag":
					c.etagMismatch++
				case "etag-inconclusive":
					c.etagInconclusive++
				case "contentType":
					c.contentTypeMis++
				case "metadata":
					c.metadataMis++
				case "tags":
					c.tagsMis++
				case "content":
					c.contentMis++
				}
			}
			switch res.Status {
			case report.Error:
				c.errors++
				c.results = append(c.results, res)
			case report.Pass:
				c.matched++
				// list on-demand deep checks (level < 3) so the report explains the
				// download; at level 3 every object is hashed and the bucket summary
				// already says so, so listing them all would only bloat the report
				if v.cfg.ListMatched || (s.SHA256 != "" && v.cfg.Level < 3) {
					if s.SHA256 != "" {
						res.Reasons = append(res.Reasons, "deep-verified")
					}
					c.results = append(c.results, res)
				}
			default:
				c.results = append(c.results, res)
			}
		}(key, s, t)
	}
	wg.Wait()
	sort.Slice(c.results, func(i, j int) bool { return c.results[i].Key < c.results[j].Key })
	return c
}

// latest returns the current version of a key (the last element after
// sorting by LastModified, or the one flagged IsLatest).
func latest(vs []*objInfo) *objInfo {
	for _, v := range vs {
		if v.IsLatest {
			return v
		}
	}
	return vs[len(vs)-1]
}

// diffMaps returns a compact description of differing keys, or "" if equal.
func diffMaps(a, b map[string]string) string {
	var diffs []string
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		av, aok := a[k]
		bv, bok := b[k]
		switch {
		case aok && !bok:
			diffs = append(diffs, fmt.Sprintf("%s missing on target (source=%q)", k, av))
		case !aok && bok:
			diffs = append(diffs, fmt.Sprintf("%s extra on target (target=%q)", k, bv))
		case av != bv:
			diffs = append(diffs, fmt.Sprintf("%s %q != %q", k, av, bv))
		}
	}
	return strings.Join(diffs, ", ")
}
