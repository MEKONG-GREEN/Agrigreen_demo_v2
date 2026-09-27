package domain

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Bộ lọc thân bài theo danh sách cho phép (allow-list). Mọi thứ không có trong
// danh sách bị bỏ: <script>, <style>, <iframe>… bị bỏ CẢ NỘI DUNG; thuộc tính
// on*, style, class… bị bỏ; liên kết chỉ nhận http, https, mailto và att:<id>
// (tham chiếu tệp đính kèm của chính tài liệu).
//
// Với Markdown: văn bản giữ nguyên (cú pháp Markdown), thẻ HTML nhúng đi qua
// cùng bộ lọc, và đích liên kết Markdown có lược đồ lạ (javascript:, data:…)
// bị thay bằng "#". Client vẫn phải hiển thị Markdown ở chế độ tắt HTML thô.

// allowedTags: thẻ được giữ → thuộc tính được giữ.
var allowedTags = map[string][]string{
	"p": nil, "br": nil, "hr": nil,
	"h1": nil, "h2": nil, "h3": nil, "h4": nil,
	"strong": nil, "b": nil, "em": nil, "i": nil, "u": nil, "s": nil, "sub": nil, "sup": nil,
	"blockquote": nil, "ul": nil, "ol": nil, "li": nil, "code": nil, "pre": nil,
	"table": nil, "thead": nil, "tbody": nil, "tr": nil,
	"th": {"colspan", "rowspan"}, "td": {"colspan", "rowspan"},
	"a":   {"href", "title"},
	"img": {"src", "alt", "width", "height"},
}

// droppedWithContent: bỏ cả thẻ lẫn nội dung bên trong.
var droppedWithContent = map[string]bool{
	"script": true, "style": true, "iframe": true, "object": true, "embed": true, "noscript": true,
	"template": true, "svg": true, "math": true, "textarea": true, "select": true, "frameset": true,
	"frame": true, "applet": true, "noembed": true, "xmp": true, "title": true, "head": true,
}

var voidTags = map[string]bool{"br": true, "hr": true, "img": true}

// safeURL kiểm URL của href/src. img chỉ nhận https và att:.
func safeURL(raw string, img bool) (string, bool) {
	s := strings.TrimSpace(raw)
	// Bỏ ký tự điều khiển/khoảng trắng mà trình duyệt bỏ qua khi đọc lược đồ ("java\tscript:").
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if s == "" {
		return "", false
	}
	if rest, ok := strings.CutPrefix(strings.ToLower(s), "att:"); ok {
		if attIDRe.MatchString(rest) {
			return "att:" + rest, true
		}
		return "", false
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return u.String(), u.Host != ""
	case "http", "mailto":
		if img {
			return "", false
		}
		return u.String(), u.Scheme == "mailto" || u.Host != ""
	case "":
		// Chỉ neo trong trang (#muc-1); không nhận đường dẫn tương đối.
		if !img && strings.HasPrefix(s, "#") {
			return s, true
		}
	}
	return "", false
}

var attIDRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Sanitize lọc thân bài theo định dạng. Kết quả an toàn để lưu và hiển thị.
func Sanitize(body string, f BodyFormat) string {
	out := sanitizeTags(body, f == FormatMarkdown)
	if f == FormatMarkdown {
		out = sanitizeMarkdownLinks(out)
	}
	return strings.TrimSpace(out)
}

func sanitizeTags(body string, markdown bool) string {
	z := html.NewTokenizer(strings.NewReader(body))
	var b strings.Builder
	var open []string // thẻ đã mở được giữ
	skip := 0         // độ sâu trong thẻ bị bỏ cả nội dung
	skipTag := ""
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			for i := len(open) - 1; i >= 0; i-- {
				b.WriteString("</" + open[i] + ">")
			}
			return b.String()
		case html.TextToken:
			if skip > 0 {
				continue
			}
			if markdown {
				// Giữ nguyên văn bản Markdown (kể cả thực thể), chỉ chặn ký tự '<' còn sót.
				b.WriteString(strings.ReplaceAll(string(z.Raw()), "<", "&lt;"))
			} else {
				b.WriteString(html.EscapeString(string(z.Text())))
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			name := tok.Data
			if skip > 0 {
				if name == skipTag && tt == html.StartTagToken {
					skip++
				}
				continue
			}
			if droppedWithContent[name] {
				if tt == html.StartTagToken {
					skip, skipTag = 1, name
				}
				continue
			}
			attrs, ok := allowedTags[name]
			if !ok {
				continue
			}
			b.WriteString("<" + name)
			for _, a := range tok.Attr {
				key := strings.ToLower(a.Key)
				if a.Namespace != "" || !slices.Contains(attrs, key) {
					continue // on*, style, class, id… đều bị bỏ
				}
				val := a.Val
				switch key {
				case "href", "src":
					v, ok := safeURL(val, key == "src")
					if !ok {
						continue
					}
					val = v
				case "width", "height", "colspan", "rowspan":
					n, err := strconv.Atoi(strings.TrimSpace(val))
					if err != nil || n < 1 || n > 4000 {
						continue
					}
					val = strconv.Itoa(n)
				}
				b.WriteString(" " + key + `="` + html.EscapeString(val) + `"`)
			}
			if name == "a" {
				b.WriteString(` rel="noopener noreferrer nofollow"`)
			}
			if voidTags[name] {
				b.WriteString(" />")
				continue
			}
			b.WriteString(">")
			if tt == html.SelfClosingTagToken {
				b.WriteString("</" + name + ">")
				continue
			}
			open = append(open, name)
		case html.EndTagToken:
			tok := z.Token()
			name := tok.Data
			if skip > 0 {
				if name == skipTag {
					skip--
				}
				continue
			}
			if voidTags[name] {
				continue
			}
			// Chỉ đóng thẻ đang mở; đóng luôn các thẻ con chưa đóng.
			if i := slices.Index(open, name); i >= 0 {
				for j := len(open) - 1; j >= i; j-- {
					b.WriteString("</" + open[j] + ">")
				}
				open = open[:i]
			}
		case html.CommentToken, html.DoctypeToken:
			// bỏ
		}
	}
}

// Đích liên kết Markdown: [nhãn](đích) và [nhãn]: đích.
var (
	mdInlineLinkRe = regexp.MustCompile(`\]\(\s*<?\s*([^)\s>]*)`)
	mdRefLinkRe    = regexp.MustCompile(`(?m)^(\s{0,3}\[[^\]]+\]:\s*<?)(\S+)`)
)

func sanitizeMarkdownLinks(s string) string {
	fix := func(target string, img bool) string {
		if target == "" {
			return target
		}
		if v, ok := safeURL(target, img); ok {
			return v
		}
		return "#"
	}
	s = mdInlineLinkRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := mdInlineLinkRe.FindStringSubmatch(m)
		return strings.Replace(m, sub[1], fix(sub[1], false), 1)
	})
	return mdRefLinkRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := mdRefLinkRe.FindStringSubmatch(m)
		return sub[1] + fix(sub[2], false)
	})
}

// PlainText lấy phần chữ của thân bài (bỏ thẻ) để lập chỉ mục tìm kiếm.
func PlainText(body string) string {
	z := html.NewTokenizer(strings.NewReader(body))
	var b strings.Builder
	for {
		switch z.Next() {
		case html.ErrorToken:
			return b.String()
		case html.TextToken:
			b.Write(z.Text())
			b.WriteByte(' ')
		}
	}
}
