package envelope

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/crypto/curve25519"
)

var update = flag.Bool("update", false, "ghi lại testdata/vectors.json")

const vectorsPath = "testdata/vectors.json"

// drbg là nguồn "ngẫu nhiên" tất định (SHA-256 theo bộ đếm) — CHỈ để sinh vector.
type drbg struct {
	seed [32]byte
	ctr  uint64
	buf  []byte
}

func newDRBG(label string) *drbg {
	return &drbg{seed: sha256.Sum256([]byte("agrigreen/envelope/v1/" + label))}
}

func (d *drbg) Read(p []byte) (int, error) {
	for len(d.buf) < len(p) {
		var c [8]byte
		binary.BigEndian.PutUint64(c[:], d.ctr)
		d.ctr++
		h := sha256.Sum256(append(d.seed[:], c[:]...))
		d.buf = append(d.buf, h[:]...)
	}
	n := copy(p, d.buf)
	d.buf = d.buf[n:]
	return n, nil
}

type vectorFile struct {
	Description string   `json:"description"`
	Version     int      `json:"version"`
	Alg         string   `json:"alg"`
	Vectors     []vector `json:"vectors"`
}

type vector struct {
	Name                  string          `json:"name"`
	RecipientSecretKeyHex string          `json:"recipient_secret_key_hex"`
	RecipientPublicKeyHex string          `json:"recipient_public_key_hex"`
	PlaintextHex          string          `json:"plaintext_hex"`
	DEKHex                string          `json:"dek_hex"`
	NonceHex              string          `json:"nonce_hex"`
	CiphertextHex         string          `json:"ciphertext_hex"`
	WrappedDEKHex         string          `json:"wrapped_dek_hex"`
	SHA256CiphertextHex   string          `json:"sha256_ciphertext_hex"`
	Metadata              json.RawMessage `json:"metadata"`
}

var cases = []struct {
	name      string
	plaintext []byte
}{
	{"empty", []byte{}},
	{"ascii", []byte("AgriGreen envelope v1")},
	{"utf8-vietnamese", []byte("Ảnh hiện trường của hộ ở xã Tân Phú Đông — mã hóa ngay khi chụp.")},
	{"binary-1000", func() []byte {
		b := make([]byte, 1000)
		for i := range b {
			b[i] = byte(i * 7)
		}
		return b
	}()},
}

func generate(t *testing.T) vectorFile {
	t.Helper()
	vf := vectorFile{
		Description: "Vector cố định cho phong bì mã hóa v1 (docs/dev/envelope-format.md). Sinh bằng: go test ./internal/shared/envelope -run TestVectors -update. " +
			"Mọi trường *_hex là hex chữ thường; ciphertext = bản mã ‖ tag GCM 16 byte; wrapped_dek = crypto_box_seal(dek, recipient_public_key) 80 byte. " +
			"wrapped_dek có khóa tạm ngẫu nhiên nên phía khác không tái tạo được — chỉ kiểm bằng cách mở.",
		Version: Version, Alg: Alg,
	}
	for _, c := range cases {
		r := newDRBG(c.name)
		pub, priv, err := GenerateKeyPair(r)
		if err != nil {
			t.Fatal(err)
		}
		// Chụp DEK và nonce do SealFrom đọc từ r, theo đúng thứ tự tài liệu.
		tee := &recorder{r: r}
		s, err := SealFrom(tee, c.plaintext, pub)
		if err != nil {
			t.Fatal(err)
		}
		dek, nonce := tee.got[:DEKSize], tee.got[DEKSize:DEKSize+NonceSize]
		md, err := json.Marshal(NewMetadata("0191f3a0-7c2e-7b3d-9a41-6f1e2d3c4b5a", s, "application/octet-stream", "2026-12-03T07:14:22+07:00"))
		if err != nil {
			t.Fatal(err)
		}
		vf.Vectors = append(vf.Vectors, vector{
			Name:                  c.name,
			RecipientSecretKeyHex: hex.EncodeToString(priv[:]),
			RecipientPublicKeyHex: hex.EncodeToString(pub[:]),
			PlaintextHex:          hex.EncodeToString(c.plaintext),
			DEKHex:                hex.EncodeToString(dek),
			NonceHex:              hex.EncodeToString(nonce),
			CiphertextHex:         hex.EncodeToString(s.Ciphertext),
			WrappedDEKHex:         hex.EncodeToString(s.WrappedDEK),
			SHA256CiphertextHex:   SHA256Hex(s.Ciphertext),
			Metadata:              md,
		})
	}
	return vf
}

type recorder struct {
	r   *drbg
	got []byte
}

func (x *recorder) Read(p []byte) (int, error) {
	n, err := x.r.Read(p)
	x.got = append(x.got, p[:n]...)
	return n, err
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestVectors mở bộ vector cố định; cùng tệp này được app Flutter kiểm
// (packages/agrigreen_mobile/test/envelope_vectors_test.dart).
func TestVectors(t *testing.T) {
	if *update {
		b, err := json.MarshalIndent(generate(t), "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(vectorsPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vectorsPath, append(b, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	var vf vectorFile
	if err := json.Unmarshal(raw, &vf); err != nil {
		t.Fatal(err)
	}
	if vf.Version != 1 || vf.Alg != Alg || len(vf.Vectors) != len(cases) {
		t.Fatalf("tệp vector sai phiên bản hoặc số lượng: %d %s %d", vf.Version, vf.Alg, len(vf.Vectors))
	}
	for _, v := range vf.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			sk, pk := unhex(t, v.RecipientSecretKeyHex), unhex(t, v.RecipientPublicKeyHex)
			derived, err := curve25519.X25519(sk, curve25519.Basepoint)
			if err != nil || !bytes.Equal(derived, pk) {
				t.Fatal("khóa công khai không khớp khóa bí mật")
			}
			pub, _ := Key32(pk)
			priv, _ := Key32(sk)
			pt, dek, nonce, ct, wrapped := unhex(t, v.PlaintextHex), unhex(t, v.DEKHex), unhex(t, v.NonceHex), unhex(t, v.CiphertextHex), unhex(t, v.WrappedDEKHex)
			if len(wrapped) != WrappedDEKLen || len(ct) != len(pt)+TagSize {
				t.Fatalf("độ dài: wrapped %d, ct %d", len(wrapped), len(ct))
			}
			gotDEK, err := UnwrapDEK(wrapped, pub, priv)
			if err != nil || !bytes.Equal(gotDEK, dek) {
				t.Fatalf("mở wrapped_dek: %v", err)
			}
			// AES-GCM tất định với (dek, nonce): mã hóa lại phải ra đúng ciphertext.
			again, err := EncryptWithDEK(dek, nonce, pt)
			if err != nil || !bytes.Equal(again, ct) {
				t.Fatal("mã hóa lại không ra ciphertext của vector")
			}
			got, err := Open(Sealed{Nonce: nonce, Ciphertext: ct, WrappedDEK: wrapped}, pub, priv)
			if err != nil || !bytes.Equal(got, pt) {
				t.Fatalf("Open: %v", err)
			}
			if SHA256Hex(ct) != v.SHA256CiphertextHex {
				t.Fatal("sha256_ciphertext sai")
			}
			var md Metadata
			if err := json.Unmarshal(v.Metadata, &md); err != nil {
				t.Fatal(err)
			}
			if md.V != 1 || md.Alg != Alg || !bytes.Equal(md.Nonce, nonce) || !bytes.Equal(md.WrappedDEK, wrapped) || md.SHA256Ciphertext != v.SHA256CiphertextHex {
				t.Fatalf("metadata không khớp: %+v", md)
			}
			var rawMD map[string]any
			_ = json.Unmarshal(v.Metadata, &rawMD)
			if rawMD["nonce"] != base64.StdEncoding.EncodeToString(nonce) {
				t.Fatal("metadata.nonce phải là base64 chuẩn")
			}
		})
	}
	// Tệp phải đúng là đầu ra của bộ sinh (không ai sửa tay).
	gen := generate(t)
	if !reflect.DeepEqual(normalize(t, gen), normalize(t, vf)) {
		t.Fatal("testdata/vectors.json lệch với bộ sinh — chạy lại với -update và xem diff")
	}
}

func normalize(t *testing.T, vf vectorFile) any {
	t.Helper()
	b, _ := json.Marshal(vf)
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRoundTripAndTamper(t *testing.T) {
	pub, priv, err := GenerateKeyPair(nil)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("ảnh minh chứng")
	s, err := Seal(msg, pub)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.WrappedDEK) != 80 || len(s.Nonce) != 12 {
		t.Fatalf("độ dài: %d %d", len(s.WrappedDEK), len(s.Nonce))
	}
	got, err := Open(s, pub, priv)
	if err != nil || !bytes.Equal(got, msg) {
		t.Fatalf("Open: %v", err)
	}
	bad := Sealed{Nonce: s.Nonce, Ciphertext: bytes.Clone(s.Ciphertext), WrappedDEK: s.WrappedDEK}
	bad.Ciphertext[0] ^= 1
	if _, err := Open(bad, pub, priv); !errors.Is(err, ErrOpen) {
		t.Fatalf("bản mã bị sửa phải lỗi ErrOpen, nhận %v", err)
	}
	otherPub, otherPriv, _ := GenerateKeyPair(rand.Reader)
	if _, err := Open(s, otherPub, otherPriv); !errors.Is(err, ErrOpen) {
		t.Fatalf("sai khóa phải lỗi ErrOpen, nhận %v", err)
	}
	if _, err := UnwrapDEK(s.WrappedDEK[:10], pub, priv); !errors.Is(err, ErrMalformed) {
		t.Fatalf("sai độ dài phải lỗi ErrMalformed, nhận %v", err)
	}
}
