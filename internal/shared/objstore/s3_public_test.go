package objstore_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
)

// The browser host is signed from the outset. Rewriting the URL after signing
// would invalidate SigV4, and signing must not try to connect to the public host.
func TestS3PresignUsesPublicHostAndInternalObjectIO(t *testing.T) {
	calls := 0
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodHead {
			t.Errorf("unexpected network request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Length", "4")
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.Header().Set("ETag", `"abcd"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer internal.Close()
	store, err := objstore.NewS3(context.Background(), objstore.S3Config{
		Endpoint: strings.TrimPrefix(internal.URL, "http://"), Bucket: "agrigreen",
		AccessKey: "test-access", SecretKey: "test-secret", Region: "us-east-1",
		PublicEndpoint: "files.example.test", PublicUseTLS: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := store.PresignGet(context.Background(), "reports/a-b.xlsx", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "files.example.test" || u.Path != "/agrigreen/reports/a-b.xlsx" {
		t.Fatalf("public URL: %s://%s%s", u.Scheme, u.Host, u.Path)
	}
	if q := u.Query(); q.Get("X-Amz-SignedHeaders") != "host" || q.Get("X-Amz-Signature") == "" || q.Get("X-Amz-Expires") != "60" {
		t.Fatal("missing host-bound SigV4 signature or TTL")
	}
	if calls != 2 {
		t.Fatalf("expected bucket and object HEAD internally, got %d calls", calls)
	}
}

func TestS3PublicConfigFromEnv(t *testing.T) {
	for k, v := range map[string]string{
		"AG_S3_ENDPOINT": "seaweedfs:9000", "AG_S3_BUCKET": "agrigreen", "AG_S3_ACCESS_KEY": "test-access",
		"AG_S3_SECRET_KEY": "test-secret", "AG_S3_PUBLIC_ENDPOINT": "files.example.test", "AG_S3_PUBLIC_USE_TLS": "",
	} {
		t.Setenv(k, v)
	}
	c, err := objstore.S3ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicEndpoint != "files.example.test" || !c.PublicUseTLS {
		t.Fatal("public TLS must default true")
	}
	t.Setenv("AG_S3_PUBLIC_USE_TLS", "bad")
	if _, err := objstore.S3ConfigFromEnv(); err == nil {
		t.Fatal("invalid public TLS accepted")
	}
}
