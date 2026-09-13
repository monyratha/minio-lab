// Package verify compares a source S3/MinIO bucket against a target bucket
// and produces a report. It is independent of the tool used to migrate the
// data (Chorus, mc mirror, rclone, ...): it only talks S3 to both sides.
package verify

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Side holds the connection parameters for one storage.
type Side struct {
	Label     string
	Endpoint  string // http(s)://host:port
	AccessKey string
	SecretKey string
	Region    string
	Insecure  bool // skip TLS verification

	HeaderTimeout time.Duration // max wait for response headers per request
}

// BucketPair maps a source bucket to a target bucket (usually the same name).
type BucketPair struct {
	Source string
	Target string
}

// Config controls one verification run.
type Config struct {
	Source Side
	Target Side

	Buckets    []BucketPair
	AllBuckets bool   // verify every bucket found on source
	Prefix     string // only objects under this prefix

	// Level 1 = list only (names, sizes, ETags).
	// Level 2 = + HEAD each object (content-type, metadata, tags).
	// Level 3 = + download & hash every object.
	Level int

	// DeepOnInconclusive downloads and hashes objects whose ETags differ but
	// cannot be trusted as content hashes (multipart / encrypted objects).
	DeepOnInconclusive bool

	CheckTags       bool
	CheckVersions   string // auto | on | off
	FailOnExtra     bool   // extra objects on target are FAIL (true) or WARN (false)
	IgnoreMetaKeys  []string
	ListMatched     bool // include PASS objects in the report's object list
	Concurrency     int
	SmokeTest       bool // run PUT/GET/HEAD/DELETE/presign on the target bucket
	SmokeTestPrefix string
	MaxDeepBytes    int64 // 0 = unlimited; cap on bytes downloaded for deep checks
	RequestTimeout  time.Duration
	FailOnWarn      bool // treat an overall WARN as FAIL (exit 1)
}

// Validate fills defaults and rejects impossible combinations.
func (c *Config) Validate() error {
	for _, s := range []*Side{&c.Source, &c.Target} {
		if s.Endpoint == "" {
			return errors.New("source and target endpoints are required")
		}
		if s.AccessKey == "" || s.SecretKey == "" {
			return fmt.Errorf("%s: access key and secret key are required (use SOURCE_/TARGET_ACCESS_KEY env vars)", s.Label)
		}
	}
	if !c.AllBuckets && len(c.Buckets) == 0 {
		return errors.New("at least one --bucket is required (or --all-buckets)")
	}
	if c.Level < 1 || c.Level > 3 {
		return errors.New("--level must be 1, 2 or 3")
	}
	if c.Concurrency < 1 {
		c.Concurrency = 8
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 2 * time.Minute
	}
	c.Source.HeaderTimeout, c.Target.HeaderTimeout = c.RequestTimeout, c.RequestTimeout
	switch c.CheckVersions {
	case "", "auto":
		c.CheckVersions = "auto"
	case "on", "off":
	default:
		return errors.New("--versions must be auto, on or off")
	}
	if c.SmokeTestPrefix == "" {
		c.SmokeTestPrefix = ".migration-verify-smoke/"
	}
	return nil
}

// ParseEndpoint splits http(s)://host:port into host:port + secure flag.
func ParseEndpoint(raw string) (host string, secure bool, err error) {
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false, err
	}
	if u.Host == "" {
		return "", false, fmt.Errorf("invalid endpoint %q", raw)
	}
	return u.Host, u.Scheme == "https", nil
}

// ParseBucketArg accepts "name" or "source:target" (different target name).
func ParseBucketArg(arg string) BucketPair {
	if i := strings.Index(arg, ":"); i > 0 {
		return BucketPair{Source: arg[:i], Target: arg[i+1:]}
	}
	return BucketPair{Source: arg, Target: arg}
}
