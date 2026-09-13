package verify

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/monyratha/migration-verify/internal/report"
)

// smokeTest exercises the application-level S3 operations against the
// target bucket: PUT, HEAD, GET, presigned GET, DELETE. It writes a single
// small probe object under cfg.SmokeTestPrefix and always removes it.
// This is opt-in (--smoke-test) because it writes to the target.
func (v *Verifier) smokeTest(ctx context.Context, bucket string) report.Check {
	start := time.Now()
	var rnd [8]byte
	_, _ = rand.Read(rnd[:])
	key := v.cfg.SmokeTestPrefix + "probe-" + hex.EncodeToString(rnd[:]) + ".txt"
	payload := []byte("migration-verify smoke test " + time.Now().UTC().Format(time.RFC3339Nano))
	var steps []string
	fail := func(step string, err error) report.Check {
		// best-effort cleanup
		_ = v.tgt.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
		return report.Check{Name: "App Smoke Test", Status: report.Fail,
			Detail:  fmt.Sprintf("%s failed: %v (ok: %s)", step, err, strings.Join(steps, ",")),
			Elapsed: time.Since(start).Round(time.Millisecond).String()}
	}

	_, err := v.tgt.client.PutObject(ctx, bucket, key, bytes.NewReader(payload), int64(len(payload)),
		minio.PutObjectOptions{ContentType: "text/plain", UserMetadata: map[string]string{"probe": "1"}})
	if err != nil {
		return fail("PUT", err)
	}
	steps = append(steps, "PUT")

	st, err := v.tgt.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return fail("HEAD", err)
	}
	if st.Size != int64(len(payload)) {
		return fail("HEAD", fmt.Errorf("size %d != %d", st.Size, len(payload)))
	}
	steps = append(steps, "HEAD")

	obj, err := v.tgt.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return fail("GET", err)
	}
	got, err := io.ReadAll(obj)
	obj.Close()
	if err != nil {
		return fail("GET", err)
	}
	if !bytes.Equal(got, payload) {
		return fail("GET", fmt.Errorf("body differs"))
	}
	steps = append(steps, "GET")

	u, err := v.tgt.client.PresignedGetObject(ctx, bucket, key, 5*time.Minute, nil)
	if err != nil {
		return fail("PRESIGN", err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return fail("PRESIGNED-GET", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Equal(body, payload) {
		return fail("PRESIGNED-GET", fmt.Errorf("status %d", resp.StatusCode))
	}
	steps = append(steps, "PRESIGNED-GET")

	if err := v.tgt.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fail("DELETE", err)
	}
	if _, err := v.tgt.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{}); err == nil {
		return fail("DELETE", fmt.Errorf("object still present after delete"))
	}
	steps = append(steps, "DELETE")

	return report.Check{Name: "App Smoke Test", Status: report.Pass,
		Detail:  "target: " + strings.Join(steps, ", ") + " ok (probe " + key + " removed)",
		Elapsed: time.Since(start).Round(time.Millisecond).String()}
}
