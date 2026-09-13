package verify

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// storage wraps a minio client for one side.
type storage struct {
	side   Side
	client *minio.Client
}

func newStorage(s Side) (*storage, error) {
	host, secure, err := ParseEndpoint(s.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.Label, err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 64
	if s.Insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in via --insecure
	}
	c, err := minio.New(host, &minio.Options{
		Creds:     credentials.NewStaticV4(s.AccessKey, s.SecretKey, ""),
		Secure:    secure,
		Region:    s.Region,
		Transport: transport,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.Label, err)
	}
	return &storage{side: s, client: c}, nil
}

// ping proves the endpoint is reachable and the credentials are accepted.
// ListBuckets requires s3:ListAllMyBuckets; fall back to BucketExists on the
// first requested bucket if that is denied so restricted credentials still work.
func (s *storage) ping(ctx context.Context, fallbackBucket string) (buckets []string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	infos, err := s.client.ListBuckets(ctx)
	if err == nil {
		for _, b := range infos {
			buckets = append(buckets, b.Name)
		}
		return buckets, nil
	}
	if fallbackBucket == "" {
		return nil, err
	}
	if _, err2 := s.client.BucketExists(ctx, fallbackBucket); err2 != nil {
		return nil, fmt.Errorf("ListBuckets: %v; BucketExists(%s): %w", err, fallbackBucket, err2)
	}
	return nil, nil
}
