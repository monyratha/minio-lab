package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/minio/minio-go/v7"

	"github.com/monyratha/migration-verify/internal/report"
)

// bucketConfig is the normalised bucket-level configuration of one side.
type bucketConfig struct {
	Exists     bool
	Versioning string // "" | Enabled | Suspended
	ObjectLock string // "" | Enabled
	Tags       map[string]string
	Policy     string // canonical JSON, "" when none
	Lifecycle  string // canonical XML-ish summary, "" when none
	Encryption string // "" | algorithm
	Errors     map[string]error
}

func (s *storage) bucketConfig(ctx context.Context, bucket string) bucketConfig {
	bc := bucketConfig{Errors: map[string]error{}}
	exists, err := s.client.BucketExists(ctx, bucket)
	if err != nil {
		bc.Errors["exists"] = err
		return bc
	}
	bc.Exists = exists
	if !exists {
		return bc
	}

	if v, err := s.client.GetBucketVersioning(ctx, bucket); err != nil {
		bc.Errors["versioning"] = err
	} else {
		bc.Versioning = v.Status
	}

	if lock, _, _, _, err := s.client.GetObjectLockConfig(ctx, bucket); err != nil {
		if code := minio.ToErrorResponse(err).Code; code != "ObjectLockConfigurationNotFoundError" && code != "" {
			bc.Errors["objectLock"] = err
		}
	} else {
		bc.ObjectLock = lock
	}

	if t, err := s.client.GetBucketTagging(ctx, bucket); err != nil {
		if code := minio.ToErrorResponse(err).Code; code != "NoSuchTagSet" {
			bc.Errors["tags"] = err
		}
	} else if t != nil {
		bc.Tags = t.ToMap()
	}

	if p, err := s.client.GetBucketPolicy(ctx, bucket); err != nil {
		bc.Errors["policy"] = err
	} else {
		bc.Policy = canonicalJSON(p)
	}

	if lc, err := s.client.GetBucketLifecycle(ctx, bucket); err != nil {
		if code := minio.ToErrorResponse(err).Code; code != "NoSuchLifecycleConfiguration" {
			bc.Errors["lifecycle"] = err
		}
	} else if lc != nil && len(lc.Rules) > 0 {
		b, _ := json.Marshal(lc)
		bc.Lifecycle = string(b)
	}

	if enc, err := s.client.GetBucketEncryption(ctx, bucket); err != nil {
		if code := minio.ToErrorResponse(err).Code; code != "ServerSideEncryptionConfigurationNotFoundError" {
			bc.Errors["encryption"] = err
		}
	} else if enc != nil && len(enc.Rules) > 0 {
		b, _ := json.Marshal(enc)
		bc.Encryption = string(b)
	}
	return bc
}

// canonicalJSON re-marshals JSON with sorted keys so that formatting
// differences do not count as a policy mismatch.
func canonicalJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	b, _ := json.Marshal(v) // encoding/json sorts map keys
	return string(b)
}

// compareBucketConfig produces the bucket-level checks.
func compareBucketConfig(src, tgt bucketConfig, strictExtras bool) (checks []report.Check, versioned bool) {
	add := func(name string, status report.Status, s, t, detail string) {
		checks = append(checks, report.Check{Name: name, Status: status, Source: s, Target: t, Detail: detail})
	}
	cfgCheck := func(name, key, s, t string) {
		if e, ok := src.Errors[key]; ok {
			add(name, report.Error, "", t, "source: "+e.Error())
			return
		}
		if e, ok := tgt.Errors[key]; ok {
			add(name, report.Error, s, "", "target: "+e.Error())
			return
		}
		if s == t {
			if s == "" {
				add(name, report.Pass, "none", "none", "")
			} else {
				add(name, report.Pass, s, t, "")
			}
			return
		}
		add(name, report.Fail, orNone(s), orNone(t), "bucket configuration differs")
	}

	if e, ok := src.Errors["exists"]; ok {
		add("Bucket Exists", report.Error, "", "", "source: "+e.Error())
		return checks, false
	}
	if e, ok := tgt.Errors["exists"]; ok {
		add("Bucket Exists", report.Error, "", "", "target: "+e.Error())
		return checks, false
	}
	switch {
	case src.Exists && tgt.Exists:
		add("Bucket Exists", report.Pass, "yes", "yes", "")
	case !src.Exists:
		add("Bucket Exists", report.Fail, "no", boolStr(tgt.Exists), "source bucket does not exist")
		return checks, false
	default:
		add("Bucket Exists", report.Fail, "yes", "no", "target bucket does not exist")
		return checks, false
	}

	cfgCheck("Versioning", "versioning", src.Versioning, tgt.Versioning)
	versioned = src.Versioning == "Enabled" || src.Versioning == "Suspended"
	cfgCheck("Object Lock", "objectLock", src.ObjectLock, tgt.ObjectLock)
	cfgCheck("Bucket Tags", "tags", mapString(src.Tags), mapString(tgt.Tags))
	cfgCheck("Bucket Policy", "policy", src.Policy, tgt.Policy)
	cfgCheck("Lifecycle", "lifecycle", src.Lifecycle, tgt.Lifecycle)
	cfgCheck("Encryption", "encryption", src.Encryption, tgt.Encryption)
	return checks, versioned
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func boolStr(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// mapString renders a map deterministically (k=v;k=v).
func mapString(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, m[k]))
	}
	return strings.Join(parts, ";")
}
