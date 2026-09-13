package verify

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monyratha/migration-verify/internal/report"
)

// Verifier runs a full verification according to a Config.
type Verifier struct {
	cfg        Config
	src, tgt   *storage
	httpClient *http.Client
	Log        func(format string, args ...any)
}

// New creates the S3 clients for both sides. No network calls happen here.
func New(cfg Config) (*Verifier, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	src, err := newStorage(cfg.Source)
	if err != nil {
		return nil, err
	}
	tgt, err := newStorage(cfg.Target)
	if err != nil {
		return nil, err
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Target.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	return &Verifier{cfg: cfg, src: src, tgt: tgt, httpClient: &http.Client{Timeout: 60 * time.Second, Transport: tr},
		Log: func(string, ...any) {}}, nil
}

// Run executes every check and returns the report. The returned error is
// only non-nil for problems that prevent the verification from running at
// all (e.g. unreachable endpoints); verification failures are expressed in
// the report's Status.
func (v *Verifier) Run(ctx context.Context) (*report.Report, error) {
	start := time.Now()
	r := &report.Report{
		Tool:        "migration-verify",
		Version:     Version,
		GeneratedAt: start.UTC(),
		Source:      endpointInfo(v.cfg.Source),
		Target:      endpointInfo(v.cfg.Target),
		Options: map[string]any{
			"level": v.cfg.Level, "deepOnInconclusive": v.cfg.DeepOnInconclusive, "checkTags": v.cfg.CheckTags,
			"versions": v.cfg.CheckVersions, "failOnExtra": v.cfg.FailOnExtra, "prefix": v.cfg.Prefix,
			"concurrency": v.cfg.Concurrency, "smokeTest": v.cfg.SmokeTest, "ignoreMetaKeys": v.cfg.IgnoreMetaKeys,
			"allBuckets": v.cfg.AllBuckets, "maxDeepBytes": v.cfg.MaxDeepBytes,
			"requestTimeout": v.cfg.RequestTimeout.String(), "failOnWarn": v.cfg.FailOnWarn,
		},
	}

	// --- connectivity / credentials -------------------------------------
	first := ""
	if len(v.cfg.Buckets) > 0 {
		first = v.cfg.Buckets[0].Source
	}
	var srcBuckets, tgtBuckets []string
	var srcErr, tgtErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); srcBuckets, srcErr = v.src.ping(ctx, first) }()
	go func() { defer wg.Done(); tgtBuckets, tgtErr = v.tgt.ping(ctx, first) }()
	wg.Wait()
	r.Checks = append(r.Checks, connCheck("Source Connectivity", v.cfg.Source, srcErr, len(srcBuckets)))
	r.Checks = append(r.Checks, connCheck("Target Connectivity", v.cfg.Target, tgtErr, len(tgtBuckets)))
	if srcErr != nil || tgtErr != nil {
		r.Elapsed = time.Since(start).Round(time.Millisecond).String()
		r.Recompute()
		return r, fmt.Errorf("connectivity failed (source: %v, target: %v)", srcErr, tgtErr)
	}

	// --- bucket inventory -------------------------------------------------
	pairs := v.cfg.Buckets
	if v.cfg.AllBuckets {
		pairs = nil
		for _, b := range srcBuckets {
			pairs = append(pairs, BucketPair{Source: b, Target: b})
		}
		missing := setDiff(srcBuckets, tgtBuckets)
		extra := setDiff(tgtBuckets, srcBuckets)
		c := report.Check{Name: "Bucket Inventory", Status: report.Pass,
			Source: fmt.Sprintf("%d buckets", len(srcBuckets)), Target: fmt.Sprintf("%d buckets", len(tgtBuckets))}
		if len(missing) > 0 {
			c.Status = report.Fail
			c.Detail = "missing on target: " + strings.Join(missing, ", ")
		}
		if len(extra) > 0 {
			if c.Status == report.Pass {
				c.Status = report.Warn
			}
			c.Detail = strings.TrimSpace(c.Detail + " extra on target: " + strings.Join(extra, ", "))
		}
		r.Checks = append(r.Checks, c)
	}

	// --- per bucket -------------------------------------------------------
	for _, p := range pairs {
		v.Log("verifying bucket %s -> %s", p.Source, p.Target)
		r.Buckets = append(r.Buckets, v.verifyBucket(ctx, p))
	}

	r.Limitations = v.limitations(r)
	r.Elapsed = time.Since(start).Round(time.Millisecond).String()
	r.Recompute()
	return r, nil
}

func (v *Verifier) verifyBucket(ctx context.Context, p BucketPair) report.BucketReport {
	start := time.Now()
	br := report.BucketReport{SourceBucket: p.Source, TargetBucket: p.Target, Prefix: v.cfg.Prefix}
	add := func(c report.Check) { br.Checks = append(br.Checks, c) }

	// bucket-level configuration
	var sc, tc bucketConfig
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); sc = v.src.bucketConfig(ctx, p.Source) }()
	go func() { defer wg.Done(); tc = v.tgt.bucketConfig(ctx, p.Target) }()
	wg.Wait()
	checks, versioned := compareBucketConfig(sc, tc, v.cfg.FailOnExtra)
	br.Checks = append(br.Checks, checks...)
	if !(sc.Exists && tc.Exists) {
		br.Elapsed = time.Since(start).Round(time.Millisecond).String()
		return br
	}

	doVersions := v.cfg.CheckVersions == "on" || (v.cfg.CheckVersions == "auto" && versioned)

	// listing (current versions) — both sides in parallel
	var src, tgt map[string][]*objInfo
	var srcErr, tgtErr error
	wg.Add(2)
	go func() { defer wg.Done(); src, srcErr = v.src.listObjects(ctx, p.Source, v.cfg.Prefix, false) }()
	go func() { defer wg.Done(); tgt, tgtErr = v.tgt.listObjects(ctx, p.Target, v.cfg.Prefix, false) }()
	wg.Wait()
	if srcErr != nil || tgtErr != nil {
		add(report.Check{Name: "Object Listing", Status: report.Error, Detail: fmt.Sprintf("source: %v; target: %v", srcErr, tgtErr)})
		br.Elapsed = time.Since(start).Round(time.Millisecond).String()
		return br
	}
	add(report.Check{Name: "Object Listing", Status: report.Pass, Detail: fmt.Sprintf("listed %d source / %d target objects", len(src), len(tgt))})
	v.Log("  listed %d source / %d target objects", len(src), len(tgt))

	var srcKeys, tgtKeys []string
	for k, vs := range src {
		srcKeys = append(srcKeys, k)
		br.Summary.SourceBytes += latest(vs).Size
	}
	for k, vs := range tgt {
		tgtKeys = append(tgtKeys, k)
		br.Summary.TargetBytes += latest(vs).Size
	}
	sort.Strings(srcKeys)
	sort.Strings(tgtKeys)
	br.Summary.SourceObjects = len(srcKeys)
	br.Summary.TargetObjects = len(tgtKeys)

	missing := setDiff(srcKeys, tgtKeys)
	extra := setDiff(tgtKeys, srcKeys)
	var common []string
	for _, k := range srcKeys {
		if _, ok := tgt[k]; ok {
			common = append(common, k)
		}
	}
	br.Summary.MissingObjects = len(missing)
	br.Summary.ExtraObjects = len(extra)
	for _, k := range missing {
		br.Objects = append(br.Objects, report.ObjectResult{Key: k, Status: report.Fail, Reasons: []string{"missing"},
			Detail: "present on source, absent on target", Source: latest(src[k]).side()})
	}
	for _, k := range extra {
		st := report.Fail
		if !v.cfg.FailOnExtra {
			st = report.Warn
		}
		br.Objects = append(br.Objects, report.ObjectResult{Key: k, Status: st, Reasons: []string{"extra"},
			Detail: "present on target, absent on source", Target: latest(tgt[k]).side()})
	}

	// object count / names
	cnt := report.Check{Name: "Object Count", Status: report.Pass, Source: fmt.Sprint(len(srcKeys)), Target: fmt.Sprint(len(tgtKeys))}
	if len(srcKeys) != len(tgtKeys) {
		cnt.Status = report.Fail
		cnt.Detail = "object counts differ"
	}
	add(cnt)
	names := report.Check{Name: "Object Names", Status: report.Pass, Detail: fmt.Sprintf("%d common", len(common))}
	if len(missing) > 0 {
		names.Status = report.Fail
		names.Detail = fmt.Sprintf("%d missing on target: %s", len(missing), preview(missing))
	}
	if len(extra) > 0 {
		st := report.Fail
		if !v.cfg.FailOnExtra {
			st = report.Warn
		}
		if st.Worse(names.Status) {
			names.Status = st
		}
		names.Detail = strings.TrimSpace(names.Detail + fmt.Sprintf("; %d extra on target: %s", len(extra), preview(extra)))
	}
	add(names)

	// per-object comparison
	c := v.compareObjects(ctx, p, common, src, tgt)
	br.Objects = append(br.Objects, c.results...)
	br.Summary.MatchedObjects = c.matched
	br.Summary.MismatchedObjects = len(common) - c.matched - c.errors
	br.Summary.ErrorObjects = c.errors
	br.Summary.ETagInconclusive = c.etagInconclusive
	br.Summary.DeepVerified = c.deepVerified
	br.Summary.DeepBytes = c.deepBytes

	tally := func(name string, n int, applicable bool, extraDetail string) {
		ch := report.Check{Name: name, Status: report.Pass, Detail: fmt.Sprintf("%d of %d compared", len(common)-n, len(common))}
		if !applicable {
			ch.Status = report.Skipped
			ch.Detail = extraDetail
		} else if n > 0 {
			ch.Status = report.Fail
			ch.Detail = fmt.Sprintf("%d mismatched of %d", n, len(common))
		}
		add(ch)
	}
	tally("Object Size", c.sizeMismatch, true, "")
	et := report.Check{Name: "Checksum / ETag", Status: report.Pass, Detail: fmt.Sprintf("%d of %d match", len(common)-c.etagMismatch-c.etagInconclusive, len(common))}
	if c.etagMismatch > 0 {
		et.Status = report.Fail
		et.Detail = fmt.Sprintf("%d mismatched", c.etagMismatch)
	}
	if c.etagInconclusive > 0 {
		if et.Status == report.Pass {
			et.Status = report.Warn
		}
		et.Detail += fmt.Sprintf("; %d inconclusive (multipart/encrypted ETag, content not verified)", c.etagInconclusive)
	}
	add(et)
	tally("Content Type", c.contentTypeMis, v.cfg.Level >= 2, "level 1 run: HEAD not performed")
	tally("Metadata", c.metadataMis, v.cfg.Level >= 2, "level 1 run: HEAD not performed")
	tally("Tags", c.tagsMis, v.cfg.Level >= 2 && v.cfg.CheckTags, "tag comparison disabled")
	if c.deepVerified > 0 || v.cfg.Level >= 3 {
		dc := report.Check{Name: "Content (SHA-256)", Status: report.Pass, Detail: fmt.Sprintf("%d objects fully hashed on both sides", c.deepVerified)}
		if c.contentMis > 0 {
			dc.Status = report.Fail
			dc.Detail = fmt.Sprintf("%d objects differ in content", c.contentMis)
		}
		add(dc)
	} else {
		add(report.Check{Name: "Content (SHA-256)", Status: report.Skipped, Detail: "not needed: all comparable ETags matched (use --level 3 to force)"})
	}
	if c.errors > 0 {
		add(report.Check{Name: "Object Inspection", Status: report.Error, Detail: fmt.Sprintf("%d objects could not be inspected", c.errors)})
	}

	// versions
	if doVersions {
		var sv, tv map[string][]*objInfo
		var se, te error
		wg.Add(2)
		go func() { defer wg.Done(); sv, se = v.src.listObjects(ctx, p.Source, v.cfg.Prefix, true) }()
		go func() { defer wg.Done(); tv, te = v.tgt.listObjects(ctx, p.Target, v.cfg.Prefix, true) }()
		wg.Wait()
		if se != nil || te != nil {
			add(report.Check{Name: "Versions", Status: report.Error, Detail: fmt.Sprintf("source: %v; target: %v", se, te)})
		} else {
			diffs, st, tt := compareVersions(sv, tv)
			br.Versions = diffs
			br.Summary.SourceVersions, br.Summary.TargetVersions, br.Summary.VersionMismatches = st, tt, len(diffs)
			vc := report.Check{Name: "Versions", Status: report.Pass, Source: fmt.Sprint(st), Target: fmt.Sprint(tt),
				Detail: "version histories match (version IDs are not comparable and were ignored)"}
			if len(diffs) > 0 {
				vc.Status = report.Fail
				vc.Detail = fmt.Sprintf("%d keys with differing version history", len(diffs))
			}
			add(vc)
		}
	} else {
		add(report.Check{Name: "Versions", Status: report.NA, Detail: "bucket is not versioned"})
	}

	if v.cfg.SmokeTest {
		add(v.smokeTest(ctx, p.Target))
	}

	sort.Slice(br.Objects, func(i, j int) bool { return br.Objects[i].Key < br.Objects[j].Key })
	br.Elapsed = time.Since(start).Round(time.Millisecond).String()
	return br
}

func (v *Verifier) limitations(r *report.Report) []string {
	l := []string{
		"ETag equality is used as the content check: it is the MD5 of the object for single-part, unencrypted uploads and is therefore a reliable content fingerprint in that case.",
		"Multipart ETags (suffix -N) depend on part size and encrypted objects (SSE) have non-MD5 ETags; when such ETags differ the object is marked inconclusive and, with --deep-on-inconclusive (default), both copies are downloaded and compared by SHA-256.",
		"Version IDs are server-generated and cannot survive an S3-level migration; version histories are compared by ordered (delete-marker, size, ETag) sequence instead.",
		"Object LastModified timestamps are not compared: the target timestamp is the migration time, not the original.",
		"Bucket policies, lifecycle, tags and encryption configuration are compared for equality only; semantic equivalence (e.g. differently formatted but equivalent policies) beyond JSON key ordering is not detected.",
		"ACLs, object retention/legal-hold per object, and bucket notification/replication configuration are not compared.",
	}
	if v.cfg.Level == 1 {
		l = append(l, "Level 1 run: Content-Type, metadata and tags were not compared (no HEAD requests).")
	}
	if r.Totals.ETagInconclusive > 0 {
		l = append(l, fmt.Sprintf("%d objects have inconclusive ETags and were not content-verified; rerun with --level 3 or --deep-on-inconclusive.", r.Totals.ETagInconclusive))
	}
	return l
}

func endpointInfo(s Side) report.Endpoint {
	_, secure, _ := ParseEndpoint(s.Endpoint)
	return report.Endpoint{Label: s.Label, Endpoint: s.Endpoint, Secure: secure, AccessKey: s.AccessKey, Region: s.Region}
}

func connCheck(name string, s Side, err error, buckets int) report.Check {
	if err != nil {
		return report.Check{Name: name, Status: report.Fail, Detail: fmt.Sprintf("%s: %v", s.Endpoint, err)}
	}
	return report.Check{Name: name, Status: report.Pass, Detail: fmt.Sprintf("%s reachable, credentials accepted (%d buckets visible)", s.Endpoint, buckets)}
}

// setDiff returns the sorted elements of a that are not in b.
func setDiff(a, b []string) []string {
	in := make(map[string]bool, len(b))
	for _, x := range b {
		in[x] = true
	}
	var out []string
	for _, x := range a {
		if !in[x] {
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func preview(keys []string) string {
	if len(keys) <= 5 {
		return strings.Join(keys, ", ")
	}
	return strings.Join(keys[:5], ", ") + fmt.Sprintf(", ... (+%d)", len(keys)-5)
}
