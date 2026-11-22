package handler

import (
	"net/url"
	"strings"
	"testing"
)

func TestArchive199WordQueryBeforeConversion(t *testing.T) {
	for _, key := range []string{"offset", "limit"} {
		for _, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000), "999999999999999999", "-1", "+1", " 1", "0.1"} {
			if _, err := archiveWordsQuery(url.Values{key: {raw}}); err == nil {
				t.Fatalf("%s accepted unsafe numeric syntax", key)
			}
		}
	}
	for _, q := range []url.Values{{"offset": {"100001"}}, {"limit": {"0"}}, {"limit": {"51"}}, {"offset": {"0", "1"}}} {
		if _, err := archiveWordsQuery(q); err == nil {
			t.Fatal("out-of-bounds or ambiguous paging accepted")
		}
	}
	q, err := archiveWordsQuery(url.Values{"offset": {"100000"}, "limit": {"50"}})
	if err != nil || q.Offset != 100000 || q.Limit != 50 {
		t.Fatalf("boundary: %+v %v", q, err)
	}
}
