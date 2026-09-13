// Command migration-verify compares a source S3/MinIO deployment with a
// target one after a migration and writes a PASS/FAIL report.
//
// Exit codes: 0 = PASS (or WARN), 1 = FAIL, 2 = could not run / ERROR.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/monyratha/migration-verify/internal/report"
	"github.com/monyratha/migration-verify/internal/verify"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(s string) error { *m = append(*m, s); return nil }

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func main() {
	var cfg verify.Config
	var buckets, ignoreMeta multiFlag
	var jsonOut, htmlOut string
	var quiet bool

	fs := flag.NewFlagSet("migration-verify", flag.ExitOnError)
	fs.StringVar(&cfg.Source.Endpoint, "source", env("SOURCE_ENDPOINT", ""), "source S3 endpoint, e.g. http://localhost:9000 (env SOURCE_ENDPOINT)")
	fs.StringVar(&cfg.Target.Endpoint, "target", env("TARGET_ENDPOINT", ""), "target S3 endpoint, e.g. http://localhost:9002 (env TARGET_ENDPOINT)")
	fs.StringVar(&cfg.Source.AccessKey, "source-access-key", env("SOURCE_ACCESS_KEY", ""), "source access key (prefer env SOURCE_ACCESS_KEY)")
	fs.StringVar(&cfg.Source.SecretKey, "source-secret-key", env("SOURCE_SECRET_KEY", ""), "source secret key (prefer env SOURCE_SECRET_KEY)")
	fs.StringVar(&cfg.Target.AccessKey, "target-access-key", env("TARGET_ACCESS_KEY", ""), "target access key (prefer env TARGET_ACCESS_KEY)")
	fs.StringVar(&cfg.Target.SecretKey, "target-secret-key", env("TARGET_SECRET_KEY", ""), "target secret key (prefer env TARGET_SECRET_KEY)")
	fs.StringVar(&cfg.Source.Region, "source-region", env("SOURCE_REGION", ""), "source region (optional)")
	fs.StringVar(&cfg.Target.Region, "target-region", env("TARGET_REGION", ""), "target region (optional)")
	fs.StringVar(&cfg.Source.Label, "source-name", env("SOURCE_NAME", "source"), "label for the source in reports")
	fs.StringVar(&cfg.Target.Label, "target-name", env("TARGET_NAME", "target"), "label for the target in reports")
	fs.BoolVar(&cfg.Source.Insecure, "source-insecure", false, "skip TLS verification for source")
	fs.BoolVar(&cfg.Target.Insecure, "target-insecure", false, "skip TLS verification for target")

	fs.Var(&buckets, "bucket", "bucket to verify; repeatable; use src:dst for a different target name")
	fs.BoolVar(&cfg.AllBuckets, "all-buckets", false, "verify every bucket present on the source")
	fs.StringVar(&cfg.Prefix, "prefix", "", "only verify objects under this key prefix")
	fs.IntVar(&cfg.Level, "level", 2, "1 = list only (name/size/etag), 2 = +HEAD (content-type/metadata/tags), 3 = +download and SHA-256 every object")
	fs.BoolVar(&cfg.DeepOnInconclusive, "deep-on-inconclusive", true, "download and hash objects whose ETags differ but are not comparable (multipart/encrypted)")
	fs.Int64Var(&cfg.MaxDeepBytes, "max-deep-bytes", 0, "cap total bytes downloaded for deep verification (0 = unlimited)")
	fs.BoolVar(&cfg.CheckTags, "tags", true, "compare object tags (level >= 2)")
	fs.StringVar(&cfg.CheckVersions, "versions", "auto", "compare version histories: auto (when source bucket is versioned), on, off")
	fs.BoolVar(&cfg.FailOnExtra, "fail-on-extra", true, "treat objects that exist only on the target as FAIL (false = WARN)")
	fs.Var(&ignoreMeta, "ignore-meta-key", "metadata header to ignore when comparing (repeatable, case-insensitive), e.g. x-amz-meta-migrated-by")
	fs.BoolVar(&cfg.ListMatched, "list-matched", false, "include fully matching objects in the report's object list")
	fs.IntVar(&cfg.Concurrency, "concurrency", 8, "parallel object comparisons")
	fs.BoolVar(&cfg.SmokeTest, "smoke-test", false, "run PUT/HEAD/GET/presigned-GET/DELETE with a probe object on the target bucket (writes to target)")
	fs.StringVar(&cfg.SmokeTestPrefix, "smoke-test-prefix", ".migration-verify-smoke/", "key prefix for the smoke-test probe object")

	fs.StringVar(&jsonOut, "json", "migration-report.json", "path of the JSON report ('' to disable)")
	fs.StringVar(&htmlOut, "html", "migration-report.html", "path of the HTML report ('' to disable)")
	fs.BoolVar(&quiet, "quiet", false, "suppress the text summary on stdout")
	showVersion := fs.Bool("version", false, "print version and exit")

	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `migration-verify %s — verify a source S3/MinIO bucket was migrated to a target.

Usage:
  migration-verify --source URL --target URL --bucket NAME [flags]

Credentials are read from SOURCE_ACCESS_KEY / SOURCE_SECRET_KEY and
TARGET_ACCESS_KEY / TARGET_SECRET_KEY (flags override the environment).

Flags:
`, verify.Version)
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:])
	if *showVersion {
		fmt.Println("migration-verify", verify.Version)
		return
	}
	for _, b := range buckets {
		for _, part := range strings.Split(b, ",") {
			if part = strings.TrimSpace(part); part != "" {
				cfg.Buckets = append(cfg.Buckets, verify.ParseBucketArg(part))
			}
		}
	}
	cfg.IgnoreMetaKeys = ignoreMeta

	v, err := verify.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		fs.Usage()
		os.Exit(2)
	}
	if !quiet {
		v.Log = func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	rep, runErr := v.Run(ctx)
	if jsonOut != "" {
		if err := report.WriteJSON(rep, jsonOut); err != nil {
			fmt.Fprintln(os.Stderr, "write json:", err)
		}
	}
	if htmlOut != "" {
		if err := report.WriteHTML(rep, htmlOut); err != nil {
			fmt.Fprintln(os.Stderr, "write html:", err)
		}
	}
	if !quiet {
		report.WriteText(rep, os.Stdout)
		if jsonOut != "" || htmlOut != "" {
			fmt.Printf("\nReports: %s %s\n", jsonOut, htmlOut)
		}
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "error:", runErr)
		os.Exit(2)
	}
	switch rep.Status {
	case report.Pass, report.Warn:
		os.Exit(0)
	case report.Fail:
		os.Exit(1)
	default:
		os.Exit(2)
	}
}
