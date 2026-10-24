//go:build vpssmoke

package objstore_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
)

// Explicit integration profile: missing live endpoints is a failure, never SKIP.
// Caddy's CI-only HTTP listener is loopback-bound; the signed browser Host is
// preserved through Caddy to the real SeaweedFS verifier.
func TestVPSPublicS3Proxy(t *testing.T) {
	cfg, err := objstore.S3ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicEndpoint == "" || os.Getenv("AG_P0_SMOKE_DISPOSABLE") != "1" {
		t.Fatal("explicit disposable public S3 configuration required")
	}
	store, err := objstore.NewS3(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	key := "p0-smoke/" + ids.New().String() + ".txt"
	body := "synthetic public-host presign smoke"
	if _, err := store.Put(context.Background(), key, strings.NewReader(body), int64(len(body)), objstore.SHA256Hex([]byte(body)), "text/plain"); err != nil {
		t.Fatal(err)
	}
	raw, err := store.PresignGet(context.Background(), key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != cfg.PublicEndpoint {
		t.Fatal("signed URL does not use public host")
	}
	host := u.Host
	// Connect to local published proxy port while retaining the signed Host.
	u.Host = "127.0.0.1:9080"
	request, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = host
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 || string(got) != body {
		t.Fatalf("proxy download HTTP %d; content-match=%v", response.StatusCode, string(got) == body)
	}
	query := u.Query()
	query.Set("X-Amz-Signature", strings.Repeat("0", 64))
	u.RawQuery = query.Encode()
	request, err = http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = host
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("tampered signature must fail: HTTP %d", response.StatusCode)
	}
}
