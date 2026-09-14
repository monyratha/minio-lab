package verify

import (
	"strings"
	"testing"
	"time"

	"github.com/monyratha/migration-verify/internal/report"
)

func TestCompareETags(t *testing.T) {
	md5a := "f1c9645dbc14efddc7d8a322685f26eb"
	md5b := "ba4aae7b7c72dcdee71af2d5a0843d70"
	cases := []struct {
		name string
		a, b *objInfo
		want etagVerdict
	}{
		{"equal", &objInfo{ETag: md5a}, &objInfo{ETag: md5a}, etagMatch},
		{"plain md5 differ", &objInfo{ETag: md5a}, &objInfo{ETag: md5b}, etagMismatch},
		{"multipart on one side", &objInfo{ETag: md5a}, &objInfo{ETag: md5b + "-1"}, etagInconclusive},
		{"multipart both", &objInfo{ETag: md5a + "-3"}, &objInfo{ETag: md5b + "-2"}, etagInconclusive},
		{"encrypted", &objInfo{ETag: md5a, Encrypted: true}, &objInfo{ETag: md5b}, etagInconclusive},
	}
	for _, c := range cases {
		if got := compareETags(c.a, c.b); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestDiffMaps(t *testing.T) {
	if d := diffMaps(map[string]string{"a": "1"}, map[string]string{"a": "1"}); d != "" {
		t.Errorf("equal maps should produce no diff, got %q", d)
	}
	if d := diffMaps(nil, nil); d != "" {
		t.Errorf("nil maps should produce no diff, got %q", d)
	}
	d := diffMaps(map[string]string{"a": "1", "b": "2"}, map[string]string{"a": "x", "c": "3"})
	for _, want := range []string{`a "1" != "x"`, `b missing on target`, `c extra on target`} {
		if !contains(d, want) {
			t.Errorf("diff %q missing %q", d, want)
		}
	}
}

func TestSetDiff(t *testing.T) {
	got := setDiff([]string{"c", "a", "b"}, []string{"b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Errorf("setDiff = %v", got)
	}
}

func TestCompareVersions(t *testing.T) {
	t0 := time.Now()
	v := func(etag string, size int64, del bool, off int) *objInfo {
		return &objInfo{ETag: etag, Size: size, IsDeleteMarker: del, LastModified: t0.Add(time.Duration(off) * time.Second)}
	}
	src := map[string][]*objInfo{
		"same":    {v("aa", 1, false, 0), v("bb", 2, false, 1)},
		"missing": {v("aa", 1, false, 0), v("bb", 2, false, 1)},
		"delmark": {v("aa", 1, false, 0), v("", 0, true, 1)},
	}
	tgt := map[string][]*objInfo{
		"same":    {v("aa", 1, false, 5), v("bb", 2, false, 6)}, // different version IDs/timestamps are fine
		"missing": {v("bb", 2, false, 5)},
		"delmark": {v("aa", 1, false, 5)},
	}
	diffs, s, tt := compareVersions(src, tgt)
	if s != 6 || tt != 4 {
		t.Errorf("totals = %d/%d", s, tt)
	}
	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %+v", diffs)
	}
	for _, d := range diffs {
		if d.Key == "same" {
			t.Errorf("'same' should not differ: %+v", d)
		}
	}
}

func TestRecompute(t *testing.T) {
	r := &report.Report{
		Checks: []report.Check{{Name: "conn", Status: report.Pass}},
		Buckets: []report.BucketReport{
			{Checks: []report.Check{{Status: report.Pass}, {Status: report.NA}, {Status: report.Skipped}}, Summary: report.Summary{SourceObjects: 4, MatchedObjects: 4}},
			{Checks: []report.Check{{Status: report.Pass}, {Status: report.Warn}}, Summary: report.Summary{SourceObjects: 1}},
		},
	}
	r.Recompute()
	if r.Buckets[0].Status != report.Pass {
		t.Errorf("N/A + SKIPPED should normalise to PASS, got %s", r.Buckets[0].Status)
	}
	if r.Status != report.Warn {
		t.Errorf("overall should be WARN, got %s", r.Status)
	}
	if r.Totals.SourceObjects != 5 {
		t.Errorf("totals not summed: %+v", r.Totals)
	}
	r.Buckets[1].Checks = append(r.Buckets[1].Checks, report.Check{Status: report.Fail})
	r.Recompute()
	if r.Status != report.Fail {
		t.Errorf("overall should be FAIL, got %s", r.Status)
	}
}

func TestParseHelpers(t *testing.T) {
	h, sec, err := ParseEndpoint("https://minio.example.com:9000")
	if err != nil || h != "minio.example.com:9000" || !sec {
		t.Errorf("ParseEndpoint https: %v %v %v", h, sec, err)
	}
	h, sec, _ = ParseEndpoint("localhost:9000")
	if h != "localhost:9000" || sec {
		t.Errorf("ParseEndpoint bare: %v %v", h, sec)
	}
	if p := ParseBucketArg("src:dst"); p.Source != "src" || p.Target != "dst" {
		t.Errorf("ParseBucketArg = %+v", p)
	}
	if p := ParseBucketArg("same"); p.Source != "same" || p.Target != "same" {
		t.Errorf("ParseBucketArg = %+v", p)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestReportOrderingHelpers(t *testing.T) {
	r := &report.Report{Buckets: []report.BucketReport{
		{SourceBucket: "ok", Checks: []report.Check{{Name: "Bucket Exists", Status: report.Pass}},
			Summary: report.Summary{MatchedObjects: 10, DeepVerified: 10}},
		{SourceBucket: "warned", Checks: []report.Check{{Name: "Tags", Status: report.Warn, Detail: "1 differ"}}},
		{SourceBucket: "gone", Checks: []report.Check{{Name: "Bucket Exists", Status: report.Fail, Detail: "target bucket does not exist"}}},
	}}
	r.Recompute()

	if got := r.ProblemBuckets(); got != 2 {
		t.Errorf("ProblemBuckets = %d, want 2", got)
	}
	ordered := r.BucketsByStatus()
	if ordered[0].SourceBucket != "gone" || ordered[1].SourceBucket != "warned" || ordered[2].SourceBucket != "ok" {
		t.Errorf("BucketsByStatus should put FAIL, then WARN, then PASS; got %s %s %s",
			ordered[0].SourceBucket, ordered[1].SourceBucket, ordered[2].SourceBucket)
	}
	if r.Buckets[0].SourceBucket != "ok" {
		t.Errorf("BucketsByStatus must not reorder the report itself")
	}
	if h := ordered[0].Headline(); h != "Bucket Exists: target bucket does not exist" {
		t.Errorf("Headline for a failed bucket = %q", h)
	}
	if h := ordered[2].Headline(); h != "10 objects, 10 hashed" {
		t.Errorf("Headline for a passed bucket = %q", h)
	}
}

func TestObjectsProblemsFirst(t *testing.T) {
	br := &report.BucketReport{Objects: []report.ObjectResult{
		{Key: "a", Status: report.Pass},
		{Key: "z", Status: report.Fail},
		{Key: "m", Status: report.Warn},
		{Key: "b", Status: report.Fail},
	}}
	sortObjects(br)
	var keys []string
	for _, o := range br.Objects {
		keys = append(keys, o.Key)
	}
	if got := strings.Join(keys, ","); got != "b,z,m,a" {
		t.Errorf("objects should be sorted FAIL, WARN, PASS then by key; got %s", got)
	}
}
