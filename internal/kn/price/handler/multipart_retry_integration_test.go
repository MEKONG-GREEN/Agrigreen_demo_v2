package handler

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
)

func TestMultipartRetry183_SurveyHTTP(t *testing.T) {
	e := newSurveyEnv(t, true)
	// Tạo đợt qua middleware thật.
	rawRound := `{"name":"Hè Thu 2026","season":"he_thu","year":2026,"collect_from":"2026-07-14","collect_to":"2026-08-25","crops":["rau_mau"],"open":true,"communes":[{"code":"C2","targets":{"rau_mau":2}}],"officers":[{"commune":"C2","full_name":"Lê Văn Hải","phone":"0912000111","unit":"TTKN"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/price/surveys", bytes.NewBufferString(rawRound))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(idempotency.Header, "survey183-round")
	created := calJSON(t, e.raw("tinh", req, http.StatusCreated).Body.Bytes())
	round := fmt.Sprint(created["round"].(map[string]any)["id"])
	csv := surveyHeader + srow("Lê Văn Hải", "An Châu", "Rau màu", "Dưa leo Loại 1", "Nguyễn Văn An", "0901999183", "0.5", "10000", "9000", "500000", "500000")
	makeBody := func(content, hash string) ([]byte, string) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		if err := mw.SetBoundary("----AgFormBoundary183survey"); err != nil {
			t.Fatal(err)
		}
		fw, err := mw.CreateFormFile("file", "nhap.csv")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
		if hash != "" {
			if err := mw.WriteField("sha256", hash); err != nil {
				t.Fatal(err)
			}
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes(), mw.FormDataContentType()
	}
	send := func(path, key string, body []byte, ct string, want int) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", ct)
		r.Header.Set(idempotency.Header, key)
		return e.raw("tinh", r, want)
	}
	body, ct := makeBody(csv, "")
	previewPath := "/v1/price/surveys/" + round + "/imports/preview"
	preview := send(previewPath, "survey183-preview", body, ct, http.StatusOK)
	previewReplay := send(previewPath, "survey183-preview", body, ct, http.StatusOK)
	if !bytes.Equal(preview.Body.Bytes(), previewReplay.Body.Bytes()) || previewReplay.Header().Get(idempotency.ReplayedHeader) != "true" {
		t.Fatal("preview không replay")
	}
	pv := calJSON(t, preview.Body.Bytes())
	body, ct = makeBody(csv, fmt.Sprint(pv["sha256"]))
	path := "/v1/price/surveys/" + round + "/imports"
	first := send(path, "survey183-commit", body, ct, http.StatusCreated)
	replayed := send(path, "survey183-commit", body, ct, http.StatusCreated)
	if !bytes.Equal(first.Body.Bytes(), replayed.Body.Bytes()) || replayed.Header().Get(idempotency.ReplayedHeader) != "true" {
		t.Fatal("commit không replay")
	}
	changed, _ := makeBody(csv+"\n", fmt.Sprint(pv["sha256"]))
	if got := send(path, "survey183-commit", changed, ct, http.StatusUnprocessableEntity); !bytes.Contains(got.Body.Bytes(), []byte(`"code":"idempotency_key_reused"`)) {
		t.Fatalf("đổi thân/cùng khóa: %s", got.Body.String())
	}
	imports := e.call("tinh", http.MethodGet, "/v1/price/surveys/"+round+"/imports", nil, http.StatusOK)
	if len(listOf(imports["items"])) != 1 {
		t.Fatalf("retry tạo thêm lần nhập: %v", imports)
	}
	forms := e.call("tinh", http.MethodGet, "/v1/price/surveys/"+round+"/forms", nil, http.StatusOK)
	if len(listOf(forms["items"])) != 1 {
		t.Fatalf("retry tạo thêm phiếu hoặc không ghi: %v", forms)
	}
}
