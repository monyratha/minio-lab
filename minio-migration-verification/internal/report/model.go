// Package report defines the machine-readable verification report model and
// the JSON / HTML / text renderers for it.
package report

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Status is the outcome of a single check, a bucket, or the whole run.
type Status string

const (
	Pass    Status = "PASS"
	Fail    Status = "FAIL"
	Warn    Status = "WARN" // check ran, found something worth attention, but not a migration failure
	NA      Status = "N/A"  // check not applicable (e.g. versions on an un-versioned bucket)
	Skipped Status = "SKIPPED"
	Error   Status = "ERROR" // check could not be executed (API error, permissions, ...)
)

// Worse returns true if s is a worse outcome than other, using the ordering
// PASS < N/A < SKIPPED < WARN < FAIL < ERROR.
func (s Status) Worse(other Status) bool {
	return rank(s) > rank(other)
}

func rank(s Status) int {
	switch s {
	case Pass:
		return 0
	case NA:
		return 1
	case Skipped:
		return 2
	case Warn:
		return 3
	case Fail:
		return 4
	case Error:
		return 5
	}
	return 6
}

// Endpoint describes one side of the comparison. Secrets are never included.
type Endpoint struct {
	Label     string `json:"label"`
	Endpoint  string `json:"endpoint"`
	Secure    bool   `json:"secure"`
	AccessKey string `json:"accessKey,omitempty"`
	Region    string `json:"region,omitempty"`
}

// Check is one named verification step with its outcome.
type Check struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Detail  string `json:"detail,omitempty"`
	Source  string `json:"source,omitempty"` // observed value on source, when meaningful
	Target  string `json:"target,omitempty"` // observed value on target, when meaningful
	Elapsed string `json:"elapsed,omitempty"`
}

// ObjectSide is what was observed for one object on one storage.
type ObjectSide struct {
	Size         int64             `json:"size"`
	ETag         string            `json:"etag"`
	ContentType  string            `json:"contentType,omitempty"`
	LastModified time.Time         `json:"lastModified"`
	VersionID    string            `json:"versionId,omitempty"`
	StorageClass string            `json:"storageClass,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"` // user metadata + standard content headers
	Tags         map[string]string `json:"tags,omitempty"`
	SHA256       string            `json:"sha256,omitempty"` // only when deep content verification ran
}

// ObjectResult records a per-object problem. Objects that fully match are
// counted but not listed individually, to keep the report small on large
// buckets (use --list-matched to include them).
type ObjectResult struct {
	Key     string      `json:"key"`
	Status  Status      `json:"status"`
	Reasons []string    `json:"reasons,omitempty"` // which fields mismatched: size, etag, contentType, metadata, tags, content, versions
	Detail  string      `json:"detail,omitempty"`
	Source  *ObjectSide `json:"source,omitempty"`
	Target  *ObjectSide `json:"target,omitempty"`
}

// VersionDiff records a per-key version-history mismatch.
type VersionDiff struct {
	Key           string   `json:"key"`
	SourceVersion int      `json:"sourceVersions"`
	TargetVersion int      `json:"targetVersions"`
	SourceDeleteM int      `json:"sourceDeleteMarkers"`
	TargetDeleteM int      `json:"targetDeleteMarkers"`
	Reasons       []string `json:"reasons,omitempty"`
}

// Summary is the per-bucket numeric roll-up.
type Summary struct {
	SourceObjects     int   `json:"sourceObjects"`
	TargetObjects     int   `json:"targetObjects"`
	SourceBytes       int64 `json:"sourceBytes"`
	TargetBytes       int64 `json:"targetBytes"`
	MatchedObjects    int   `json:"matchedObjects"`
	MissingObjects    int   `json:"missingObjects"`    // in source, not in target
	ExtraObjects      int   `json:"extraObjects"`      // in target, not in source
	MismatchedObjects int   `json:"mismatchedObjects"` // present on both, differ in some attribute
	ErrorObjects      int   `json:"errorObjects"`      // could not be inspected
	ETagInconclusive  int   `json:"etagInconclusive"`  // ETag not comparable (multipart/encrypted) and no deep check ran
	DeepVerified      int   `json:"deepVerified"`      // objects whose content was fully downloaded and hashed
	DeepBytes         int64 `json:"deepBytes"`
	SourceVersions    int   `json:"sourceVersions,omitempty"`
	TargetVersions    int   `json:"targetVersions,omitempty"`
	VersionMismatches int   `json:"versionMismatches,omitempty"`
}

// BucketReport is the result for one source→target bucket pair.
type BucketReport struct {
	SourceBucket string         `json:"sourceBucket"`
	TargetBucket string         `json:"targetBucket"`
	Prefix       string         `json:"prefix,omitempty"`
	Status       Status         `json:"status"`
	Checks       []Check        `json:"checks"`
	Summary      Summary        `json:"summary"`
	Objects      []ObjectResult `json:"objects,omitempty"` // problems (and matches when --list-matched)
	Versions     []VersionDiff  `json:"versionDiffs,omitempty"`
	Elapsed      string         `json:"elapsed"`
}

// Report is the top-level document written to migration-report.json.
type Report struct {
	Tool        string         `json:"tool"`
	Version     string         `json:"version"`
	GeneratedAt time.Time      `json:"generatedAt"`
	Source      Endpoint       `json:"source"`
	Target      Endpoint       `json:"target"`
	Options     map[string]any `json:"options"`
	Status      Status         `json:"status"`
	Checks      []Check        `json:"checks"` // global checks: connectivity, credentials, bucket inventory
	Buckets     []BucketReport `json:"buckets"`
	Totals      Summary        `json:"totals"`
	Elapsed     string         `json:"elapsed"`
	Limitations []string       `json:"limitations,omitempty"`
}

// ProblemBuckets returns how many buckets did not pass.
func (r *Report) ProblemBuckets() int {
	n := 0
	for _, b := range r.Buckets {
		if b.Status.Worse(Pass) && b.Status != NA && b.Status != Skipped {
			n++
		}
	}
	return n
}

// BucketsByStatus returns the buckets with problems first (worst status
// first), then the passing ones, each group in report order. The report's
// own slice is left untouched so JSON keeps the verification order.
func (r *Report) BucketsByStatus() []BucketReport {
	out := make([]BucketReport, len(r.Buckets))
	copy(out, r.Buckets)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Status.Worse(out[j].Status) })
	return out
}

// Headline is the one-line reason for a bucket's status: the first check
// that is not PASS, or the object counts when the checks all passed.
func (b *BucketReport) Headline() string {
	for _, c := range b.Checks {
		if c.Status == Fail || c.Status == Error || c.Status == Warn {
			if c.Detail != "" {
				return c.Name + ": " + c.Detail
			}
			return c.Name + ": " + string(c.Status)
		}
	}
	s := b.Summary
	parts := []string{fmt.Sprintf("%d objects", s.MatchedObjects)}
	if s.DeepVerified > 0 {
		parts = append(parts, fmt.Sprintf("%d hashed", s.DeepVerified))
	}
	return strings.Join(parts, ", ")
}

// Recompute derives bucket and overall status from the individual checks.
func (r *Report) Recompute() {
	overall := Pass
	for _, c := range r.Checks {
		if c.Status.Worse(overall) {
			overall = c.Status
		}
	}
	var totals Summary
	for i := range r.Buckets {
		b := &r.Buckets[i]
		bs := Pass
		for _, c := range b.Checks {
			if c.Status.Worse(bs) {
				bs = c.Status
			}
		}
		if bs == NA || bs == Skipped {
			bs = Pass
		}
		b.Status = bs
		if bs.Worse(overall) {
			overall = bs
		}
		totals.SourceObjects += b.Summary.SourceObjects
		totals.TargetObjects += b.Summary.TargetObjects
		totals.SourceBytes += b.Summary.SourceBytes
		totals.TargetBytes += b.Summary.TargetBytes
		totals.MatchedObjects += b.Summary.MatchedObjects
		totals.MissingObjects += b.Summary.MissingObjects
		totals.ExtraObjects += b.Summary.ExtraObjects
		totals.MismatchedObjects += b.Summary.MismatchedObjects
		totals.ErrorObjects += b.Summary.ErrorObjects
		totals.ETagInconclusive += b.Summary.ETagInconclusive
		totals.DeepVerified += b.Summary.DeepVerified
		totals.DeepBytes += b.Summary.DeepBytes
		totals.SourceVersions += b.Summary.SourceVersions
		totals.TargetVersions += b.Summary.TargetVersions
		totals.VersionMismatches += b.Summary.VersionMismatches
	}
	// N/A and SKIPPED never fail a run; WARN stays WARN. Normalise so the
	// overall status is one of PASS / WARN / FAIL / ERROR.
	if overall == NA || overall == Skipped {
		overall = Pass
	}
	r.Status = overall
	r.Totals = totals
}
