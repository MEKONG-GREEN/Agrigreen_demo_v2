// Package integration nạp và cưỡng chế hợp đồng của CỔNG TÍCH HỢP (kiến trúc v4.1 §38–§42,
// R-INT-08; ADR-0033, ADR-0034, ADR-0035): feed theo cursor (nền tảng → đối tác) và hợp đồng
// nạp theo lô (đối tác → nền tảng), gồm cả luồng song phương. Mặc định là TỪ CHỐI: chỉ trường
// có trong hợp đồng được đi qua cổng; hợp đồng thiếu người phê duyệt, tự phê duyệt hay sai siêu
// dữ liệu coi như không có.
//
// Dạng tệp (README.md cùng thư mục):
//   - feeds/<tên>.v<N>.json, ingest/<tên>.v<N>.json: một hợp đồng, lược đồ MỘT bản ghi theo tập
//     con JSON Schema của van công bố (publication.Node); hợp đồng nạp có thể dùng nguyên lược đồ
//     của một hợp đồng van ("publication": "<tên>", ADR-0033 §3);
//   - envelope/feed_page.v1.json, envelope/ingest_batch.v1.json: phong bì chung (cursor, as_of,
//     max_age, entity_key, phiên bản tập dữ liệu);
//   - examples/<tên>.v<N>.json: một trang feed hoặc một lô nạp mẫu — tools/contractcheck kiểm
//     mẫu khớp phong bì và hợp đồng.
//
// Hai lớp cưỡng chế: lúc build tools/contractcheck (siêu dữ liệu, lược đồ, mẫu, tương thích ngược
// theo -base); lúc chạy cổng (W5.2–W5.4) gọi Get rồi ValidateRecord từng bản ghi.
package integration

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
)

//go:embed envelope/*.json feeds/*.json ingest/*.json
var files embed.FS

// Loại hợp đồng (thư mục chứa tệp).
const (
	KindFeed   = "feed"   // feeds/: nền tảng → đối tác, đọc theo cursor (R-INT-06)
	KindIngest = "ingest" // ingest/: đối tác → nền tảng, nạp theo lô 202 + mã lô (R-INT-03)
)

// Loại luồng §38.1.
const (
	FlowLicensed  = "licensed"  // dữ liệu được cấp phép
	FlowPublished = "published" // dữ liệu công bố (đối tác → nền tảng)
	FlowBilateral = "bilateral" // song phương — chỉ hai bên của giao dịch (ADR-0034)
	FlowReference = "reference" // tham chiếu dùng chung
)

// Trạng thái hợp đồng.
const (
	StatusDraft    = "draft"
	StatusProposed = "proposed" // ADR chưa ký / còn câu hỏi mở (Pending)
	StatusApproved = "approved"
	StatusRetired  = "retired"
)

// Lớp cấp phép (ADR-0030 §3, ADR-0035 §1).
const (
	GrantAggregate = "aggregate" // tập tổng hợp có ngưỡng k (ADR-0035)
	GrantPartner   = "partner"   // cấp phép đối tác theo bên cấp (ADR-0020/0030)
	GrantReference = "reference" // tham chiếu dùng chung — mọi đối tác có khóa
	GrantBilateral = "bilateral" // quan hệ cặp (ADR-0034)
)

// EntityKeySep nối giá trị các trường entity_key thành khóa thực thể của phong bì.
const EntityKeySep = ":"

// Freshness: độ tươi cam kết — giá trị max_age của phong bì (§38.3, §41.4).
type Freshness struct {
	MaxAge string `json:"max_age"` // ISO 8601, vd "PT15M"
	Note   string `json:"note,omitempty"`
}

// Delivery mô tả cách phục vụ feed (R-INT-06, R-INT-07, R-INT-11).
type Delivery struct {
	Cursor           bool     `json:"cursor"`
	RetentionDays    int      `json:"retention_days"`
	DatasetVersioned bool     `json:"dataset_versioned"`
	Snapshot         string   `json:"snapshot"` // none | signed_url
	Webhook          string   `json:"webhook"`  // thin | none
	Channels         []string `json:"channels"` // api | file | app
}

// Ingest mô tả nạp theo lô (R-INT-03, ADR-0033 §3).
type Ingest struct {
	Route       string   `json:"route"` // "POST /v1/batches/<tên>"
	MaxBatch    int      `json:"max_batch"`
	BusinessKey []string `json:"business_key"` // khóa nghiệp vụ idempotent
	TargetPort  string   `json:"target_port"`  // port module chủ mà bộ xử lý inbox gọi
	Channels    []string `json:"channels"`     // api | file | app
}

// Aggregation: quy tắc ngưỡng k của tập tổng hợp (ADR-0035 §3). k CHƯA KÝ (C-4/K-8): KParam là
// tham số pf/masterdata có phiên bản; KDefault là giá trị tạm; job dựng từ chối k nhỏ hơn.
type Aggregation struct {
	KParam        string `json:"k_param"`
	KDefault      int    `json:"k_default"`
	KStatus       string `json:"k_status"`
	Unit          string `json:"unit"`        // household | plot | cooperative
	Suppression   string `json:"suppression"` // omit
	Complementary bool   `json:"complementary_suppression"`
	Hysteresis    int    `json:"hysteresis"` // ô đã ẩn giữ ẩn tới khi ≥ k + Hysteresis
	Grain         string `json:"grain"`
}

// Bilateral: hai bên của luồng song phương (ADR-0034 §1).
type Bilateral struct {
	PartyA     string   `json:"party_a"`
	PartyB     string   `json:"party_b"`
	AuthorSide string   `json:"author_side"` // a | b
	PairKey    []string `json:"pair_key"`
	PairFeed   string   `json:"pair_feed"`
}

// EffectiveDating: bản ghi có hiệu lực theo thời gian (ADR-0032).
type EffectiveDating struct {
	ValidFrom  string `json:"valid_from"`
	ValidTo    string `json:"valid_to"`
	Interval   string `json:"interval"`
	Succession string `json:"succession,omitempty"`
}

// Contract là một hợp đồng feed hoặc nạp của cổng tích hợp.
type Contract struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Kind    string `json:"kind"`
	Flow    string `json:"flow"`
	Status  string `json:"status"`
	// Pending: câu hỏi/khoản ký còn mở (vd "C-4", "C-12", "A7"); phải rỗng khi approved.
	Pending    []string `json:"pending,omitempty"`
	StatusNote string   `json:"status_note,omitempty"`
	Owner      string   `json:"owner"`
	Sources    []string `json:"sources,omitempty"`
	Consumers  []string `json:"consumers,omitempty"`
	ProposedBy string   `json:"proposed_by"`
	ApprovedBy string   `json:"approved_by"`
	ApprovedAt string   `json:"approved_at"`
	Purpose    string   `json:"purpose"`
	// PersonalData phải khai; true chỉ được với luồng song phương.
	PersonalData    *bool            `json:"personal_data"`
	GrantClass      string           `json:"grant_class"`
	GrantKind       string           `json:"grant_kind,omitempty"`
	EntityKey       []string         `json:"entity_key"`
	Freshness       *Freshness       `json:"freshness,omitempty"`
	Delivery        *Delivery        `json:"delivery,omitempty"`
	Ingest          *Ingest          `json:"ingest,omitempty"`
	Aggregation     *Aggregation     `json:"aggregation,omitempty"`
	Bilateral       *Bilateral       `json:"bilateral,omitempty"`
	EffectiveDating *EffectiveDating `json:"effective_dating,omitempty"`
	// Publication: dùng NGUYÊN lược đồ hợp đồng van contracts/publication/<tên>.json (ADR-0033 §3);
	// khi khai thì tệp này không được có properties riêng.
	Publication string `json:"publication,omitempty"`
	// Replaces: hợp đồng cũ được thay, dạng "publication/<tên>" hoặc "dataproduct/<tên>".
	Replaces    []string `json:"replaces,omitempty"`
	Description string   `json:"description,omitempty"`
	publication.Node

	record *publication.Node
}

// Envelope là phong bì chung của trang feed hoặc lô nạp.
type Envelope struct {
	Name        string `json:"name"`
	Version     int    `json:"version"`
	Description string `json:"description,omitempty"`
	publication.Node
}

var (
	nameRe     = regexp.MustCompile(`^[a-z][a-z0-9_]{2,47}$`)
	pubNameRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)
	durationRe = regexp.MustCompile(`^P(?:[0-9]+D)?(?:T(?:[0-9]+H)?(?:[0-9]+M)?(?:[0-9]+S)?)?$`)
	pendingRe  = regexp.MustCompile(`^[A-Z]{1,3}-?[0-9]{1,3}[a-z]?$`)
	replacesRe = regexp.MustCompile(`^(publication|dataproduct)/[a-z][a-z0-9_]{2,63}$`)
	flows      = map[string]bool{FlowLicensed: true, FlowPublished: true, FlowBilateral: true, FlowReference: true}
	statuses   = map[string]bool{StatusDraft: true, StatusProposed: true, StatusApproved: true, StatusRetired: true}
	units      = map[string]bool{"household": true, "plot": true, "cooperative": true}
	channels   = map[string]bool{"api": true, "file": true, "app": true}
)

func decodeStrict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// Parse đọc một hợp đồng. Lỗi khi JSON hỏng hoặc có khóa lạ; vấn đề nội dung xem MetaProblems.
func Parse(raw []byte) (*Contract, error) {
	var c Contract
	if err := decodeStrict(raw, &c); err != nil {
		return nil, fmt.Errorf("integration: JSON không hợp lệ: %w", err)
	}
	if c.Publication == "" {
		n := c.Node
		c.record = &n
	}
	return &c, nil
}

// ParseEnvelope đọc một phong bì.
func ParseEnvelope(raw []byte) (*Envelope, error) {
	var e Envelope
	if err := decodeStrict(raw, &e); err != nil {
		return nil, fmt.Errorf("integration: phong bì JSON không hợp lệ: %w", err)
	}
	return &e, nil
}

// Problems kiểm phong bì.
func (e *Envelope) Problems() []string {
	var p []string
	if !nameRe.MatchString(e.Name) || e.Version < 1 {
		p = append(p, "phong bì phải có name và version ≥ 1")
	}
	if len(e.Properties) == 0 {
		p = append(p, "phong bì rỗng: cần properties")
	}
	return append(p, e.Node.Problems()...)
}

// Validate kiểm JSON theo phong bì (mặc định từ chối khóa lạ ở mọi cấp có properties).
func (e *Envelope) Validate(raw []byte) error {
	return (&publication.Contract{Name: e.Name, Node: e.Node}).Validate(raw)
}

// ID là "<tên>.v<N>" — tên tệp không đuôi, giá trị header AG-Data-Contract.
func (c *Contract) ID() string { return fmt.Sprintf("%s.v%d", c.Name, c.Version) }

// Dir là thư mục của loại hợp đồng.
func Dir(kind string) string {
	if kind == KindIngest {
		return "ingest"
	}
	return "feeds"
}

func (c *Contract) asPublication() *publication.Contract {
	return &publication.Contract{Name: c.Name, Version: c.Version, Owner: c.Owner, ProposedBy: c.ProposedBy,
		ApprovedBy: c.ApprovedBy, ApprovedAt: c.ApprovedAt, Purpose: c.Purpose, Fields: []string{"_"}, Node: c.Node}
}

// MetaProblems kiểm siêu dữ liệu và tính nhất quán (chưa cần lược đồ van đã giải).
func (c *Contract) MetaProblems() []string {
	var out []string
	add := func(f string, a ...any) { out = append(out, fmt.Sprintf(f, a...)) }
	if !nameRe.MatchString(c.Name) {
		add("name phải khớp ^[a-z][a-z0-9_]{2,47}$")
	}
	pc := c.asPublication()
	if len(c.Properties) > 0 {
		pc.Fields = nil
	}
	for _, mp := range pc.MetaProblems() {
		if strings.HasPrefix(mp, "name phải khớp") {
			continue
		}
		out = append(out, mp)
	}
	if c.Kind != KindFeed && c.Kind != KindIngest {
		add("kind phải là feed | ingest")
	}
	if !flows[c.Flow] {
		add("flow phải là licensed | published | bilateral | reference (§38.1)")
	}
	if !statuses[c.Status] {
		add("status phải là draft | proposed | approved | retired")
	}
	for _, q := range c.Pending {
		if !pendingRe.MatchString(q) {
			add("pending %q phải là mã câu hỏi/khoản ký (vd C-4, C-12, A7)", q)
		}
	}
	if c.Status == StatusApproved && len(c.Pending) > 0 {
		add("approved nhưng còn pending %v — đóng câu hỏi trước khi duyệt", c.Pending)
	}
	if !strings.HasPrefix(c.Owner, "pf/") {
		add("owner phải là module nền tảng pf/<module> (cổng tích hợp thuộc vùng nền tảng)")
	}
	if strings.TrimSpace(c.Purpose) == "" {
		add("thiếu purpose")
	}
	switch {
	case c.PersonalData == nil:
		add("personal_data phải khai")
	case *c.PersonalData && c.Flow != FlowBilateral:
		add("personal_data = true chỉ được với luồng song phương (RB-3)")
	}
	if len(c.EntityKey) == 0 {
		add("thiếu entity_key (khóa thực thể: thứ tự giao và idempotent, R-INT-06)")
	}
	if c.Publication != "" && len(c.Properties) > 0 {
		add("khai publication thì không được có properties riêng — lược đồ lấy nguyên từ hợp đồng van")
	}
	if c.Publication == "" && len(c.Properties) == 0 {
		add("hợp đồng rỗng: cần properties hoặc publication")
	}
	if c.Publication != "" && !pubNameRe.MatchString(c.Publication) {
		add("publication phải là tên hợp đồng van")
	}
	for _, r := range c.Replaces {
		if !replacesRe.MatchString(r) {
			add("replaces %q phải có dạng publication/<tên> hoặc dataproduct/<tên>", r)
		}
	}
	if f := c.Freshness; f != nil && !durationRe.MatchString(f.MaxAge) {
		add("freshness.max_age phải là thời lượng ISO 8601 (vd PT15M)")
	}
	switch c.Kind {
	case KindFeed:
		out = append(out, c.feedProblems()...)
	case KindIngest:
		out = append(out, c.ingestProblems()...)
	}
	out = append(out, c.flowProblems()...)
	if e := c.EffectiveDating; e != nil {
		if e.ValidFrom == "" || e.ValidTo == "" {
			add("effective_dating cần valid_from và valid_to")
		}
		if e.Interval != "[)" {
			add(`effective_dating.interval phải là "[)" (ADR-0032)`)
		}
	}
	return out
}

func (c *Contract) feedProblems() []string {
	var out []string
	if len(c.Sources) == 0 {
		out = append(out, "feed thiếu sources (module chủ dữ liệu)")
	}
	for _, s := range c.Sources {
		if !strings.HasPrefix(s, "pf/") {
			out = append(out, "feed: sources chỉ được là module pf/ (dữ liệu vùng mkg không ra cổng qua feed nền tảng): "+s)
		}
	}
	if c.Freshness == nil {
		out = append(out, "feed thiếu freshness.max_age")
	}
	if c.Ingest != nil {
		out = append(out, "feed không được khai ingest")
	}
	d := c.Delivery
	if d == nil {
		return append(out, "feed thiếu delivery")
	}
	if !d.Cursor {
		out = append(out, "feed phải đọc theo cursor (delivery.cursor = true, R-INT-06)")
	}
	if d.RetentionDays < 30 {
		out = append(out, "delivery.retention_days phải ≥ 30 (R-INT-06)")
	}
	if d.Snapshot != "none" && d.Snapshot != "signed_url" {
		out = append(out, "delivery.snapshot phải là none | signed_url")
	}
	if d.Webhook != "thin" && d.Webhook != "none" {
		out = append(out, "delivery.webhook phải là thin | none (R-INT-07: không đẩy dữ liệu)")
	}
	out = append(out, channelProblems("delivery", d.Channels)...)
	if c.Flow == FlowReference && !d.DatasetVersioned {
		out = append(out, "feed tham chiếu phải theo phiên bản (delivery.dataset_versioned = true)")
	}
	if c.Aggregation != nil && !d.DatasetVersioned {
		out = append(out, "tập tổng hợp phải dựng sẵn theo phiên bản (delivery.dataset_versioned = true, R-INT-05)")
	}
	return out
}

func channelProblems(at string, cs []string) []string {
	var out []string
	hasAPI := false
	for _, ch := range cs {
		if !channels[ch] {
			out = append(out, fmt.Sprintf("%s.channels: %q không hỗ trợ (api | file | app)", at, ch))
		}
		hasAPI = hasAPI || ch == "api"
	}
	if !hasAPI {
		out = append(out, at+".channels phải có api")
	}
	return out
}

func (c *Contract) ingestProblems() []string {
	var out []string
	if c.Flow != FlowPublished && c.Flow != FlowBilateral {
		out = append(out, "hợp đồng nạp chỉ có flow published | bilateral")
	}
	if c.Delivery != nil {
		out = append(out, "hợp đồng nạp không khai delivery (feed của cặp dùng bilateral.pair_feed)")
	}
	in := c.Ingest
	if in == nil {
		return append(out, "hợp đồng nạp thiếu ingest")
	}
	if in.Route != "POST /v1/batches/"+c.Name {
		out = append(out, fmt.Sprintf("ingest.route phải là %q (ADR-0033 §1)", "POST /v1/batches/"+c.Name))
	}
	if in.MaxBatch < 1 || in.MaxBatch > 1000 {
		out = append(out, "ingest.max_batch phải trong 1..1000")
	}
	if len(in.BusinessKey) == 0 {
		out = append(out, "ingest.business_key bắt buộc (idempotent theo khóa nghiệp vụ, §41.3)")
	}
	if strings.TrimSpace(in.TargetPort) == "" {
		out = append(out, "ingest.target_port bắt buộc")
	}
	out = append(out, channelProblems("ingest", in.Channels)...)
	return out
}

func (c *Contract) flowProblems() []string {
	var out []string
	switch c.Flow {
	case FlowLicensed:
		if c.GrantClass != GrantAggregate && c.GrantClass != GrantPartner {
			out = append(out, "flow licensed cần grant_class aggregate | partner")
		}
	case FlowReference:
		if c.GrantClass != GrantReference {
			out = append(out, "flow reference cần grant_class reference")
		}
	case FlowBilateral:
		if c.GrantClass != GrantBilateral {
			out = append(out, "flow bilateral cần grant_class bilateral")
		}
	case FlowPublished:
		if c.GrantClass != GrantPartner {
			out = append(out, "flow published cần grant_class partner")
		}
	}
	if c.GrantClass == GrantPartner && strings.TrimSpace(c.GrantKind) == "" {
		out = append(out, "grant_class partner cần grant_kind (loại cấp phép ADR-0020 §2)")
	}
	if (c.GrantClass == GrantAggregate) != (c.Aggregation != nil) {
		out = append(out, "grant_class aggregate khi và chỉ khi có aggregation (ADR-0035)")
	}
	if a := c.Aggregation; a != nil {
		if strings.TrimSpace(a.KParam) == "" {
			out = append(out, "aggregation.k_param bắt buộc (k chưa ký — C-4/K-8)")
		}
		if a.KDefault < 2 {
			out = append(out, "aggregation.k_default phải ≥ 2")
		}
		if strings.TrimSpace(a.KStatus) == "" {
			out = append(out, "aggregation.k_status bắt buộc (ghi rõ k đã ký hay tạm)")
		}
		if !units[a.Unit] {
			out = append(out, "aggregation.unit phải là household | plot | cooperative")
		}
		if a.Suppression != "omit" {
			out = append(out, "aggregation.suppression chỉ hỗ trợ omit")
		}
		if !a.Complementary {
			out = append(out, "aggregation.complementary_suppression phải true (ADR-0035 §3: ẩn ô bù)")
		}
		if a.Hysteresis < 1 {
			out = append(out, "aggregation.hysteresis phải ≥ 1 (ADR-0035 §3: ẩn tới k + 2)")
		}
	}
	if (c.Flow == FlowBilateral) != (c.Bilateral != nil) {
		out = append(out, "flow bilateral khi và chỉ khi có bilateral (ADR-0034 §1)")
	}
	if b := c.Bilateral; b != nil {
		if b.PartyA == "" || b.PartyB == "" || b.PartyA == b.PartyB {
			out = append(out, "bilateral.party_a và party_b bắt buộc và khác nhau")
		}
		if b.AuthorSide != "a" && b.AuthorSide != "b" {
			out = append(out, "bilateral.author_side phải là a | b")
		}
		if len(b.PairKey) == 0 {
			out = append(out, "bilateral.pair_key bắt buộc")
		}
		if b.PairFeed != "bilateral" {
			out = append(out, `bilateral.pair_feed phải là "bilateral" (GET /v1/feeds/bilateral?pair=…)`)
		}
		if c.Aggregation != nil {
			out = append(out, "song phương không bao giờ vào tổng hợp (ADR-0034 §5)")
		}
	}
	return out
}

// Resolve gắn lược đồ bản ghi: hợp đồng khai publication lấy Node của hợp đồng van qua lookup.
func (c *Contract) Resolve(lookup func(name string) (*publication.Contract, error)) error {
	if c.Publication == "" {
		n := c.Node
		c.record = &n
		return nil
	}
	pc, err := lookup(c.Publication)
	if err != nil {
		return fmt.Errorf("publication %s: %w", c.Publication, err)
	}
	if !pc.Typed() {
		return fmt.Errorf("publication %s: hợp đồng van phải có properties (có kiểu)", c.Publication)
	}
	n := pc.Node
	c.record = &n
	return nil
}

// Record là lược đồ một bản ghi (nil nếu chưa Resolve hợp đồng khai publication).
func (c *Contract) Record() *publication.Node { return c.record }

// RecordProblems kiểm lược đồ bản ghi và các khóa trỏ vào nó (sau Resolve).
func (c *Contract) RecordProblems() []string {
	r := c.record
	if r == nil {
		return []string{"lược đồ bản ghi chưa giải (publication " + c.Publication + ")"}
	}
	var out []string
	if c.Publication != "" { // lược đồ của chính tệp đã được MetaProblems kiểm
		out = append(out, r.Problems()...)
	}
	req := map[string]bool{}
	for _, f := range r.Required {
		req[f] = true
	}
	field := func(what, f string) *publication.Node {
		n := r.Properties[f]
		if n == nil {
			out = append(out, fmt.Sprintf("%s %s không có trong lược đồ bản ghi", what, f))
		}
		return n
	}
	for _, f := range c.EntityKey {
		n := field("entity_key", f)
		if n == nil {
			continue
		}
		if !req[f] || n.Type.Has("null") {
			out = append(out, "entity_key "+f+" phải bắt buộc và không null")
		}
		if (!n.Type.Has("string") && !n.Type.Has("integer")) || n.Format == "date-time" {
			out = append(out, "entity_key "+f+" phải là string (không date-time) hoặc integer")
		}
	}
	if c.Ingest != nil {
		for _, f := range c.Ingest.BusinessKey {
			if n := field("ingest.business_key", f); n != nil && !req[f] {
				out = append(out, "ingest.business_key "+f+" phải bắt buộc")
			}
		}
	}
	if c.Bilateral != nil {
		for _, f := range c.Bilateral.PairKey {
			if n := field("bilateral.pair_key", f); n != nil && !req[f] {
				out = append(out, "bilateral.pair_key "+f+" phải bắt buộc")
			}
		}
	}
	if e := c.EffectiveDating; e != nil {
		if n := field("effective_dating.valid_from", e.ValidFrom); n != nil && (n.Format != "date" || !req[e.ValidFrom]) {
			out = append(out, "effective_dating.valid_from phải là trường date bắt buộc")
		}
		if n := field("effective_dating.valid_to", e.ValidTo); n != nil && (n.Format != "date" || !n.Type.Has("null")) {
			out = append(out, "effective_dating.valid_to phải là trường date có thể null (phiên bản đang mở)")
		}
		if e.Succession != "" {
			if n := field("effective_dating.succession", e.Succession); n != nil && !n.Type.Has("array") {
				out = append(out, "effective_dating.succession phải là mảng")
			}
		}
	}
	return out
}

// ValidateRecord kiểm MỘT bản ghi theo hợp đồng (mặc định từ chối).
func (c *Contract) ValidateRecord(raw []byte) error {
	if c.record == nil {
		return fmt.Errorf("%w: hợp đồng %s chưa giải lược đồ", publication.ErrDenied, c.ID())
	}
	return (&publication.Contract{Name: c.ID(), Node: *c.record}).Validate(raw)
}

// EntityKeyOf dựng khóa thực thể từ một bản ghi: giá trị các trường entity_key nối bằng ":".
func (c *Contract) EntityKeyOf(raw []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return "", err
	}
	parts := make([]string, 0, len(c.EntityKey))
	for _, f := range c.EntityKey {
		switch v := m[f].(type) {
		case string:
			parts = append(parts, v)
		case json.Number:
			parts = append(parts, v.String())
		default:
			return "", fmt.Errorf("entity_key %s thiếu hoặc không phải string/integer", f)
		}
	}
	return strings.Join(parts, EntityKeySep), nil
}

// Servable: approved (không còn pending) phục vụ mọi nơi; draft/proposed chỉ ngoài production;
// retired không bao giờ.
func (c *Contract) Servable(production bool) bool {
	switch c.Status {
	case StatusApproved:
		return len(c.Pending) == 0
	case StatusDraft, StatusProposed:
		return !production
	}
	return false
}

type feedExample struct {
	Feed            string `json:"feed"`
	ContractVersion int    `json:"contract_version"`
	MaxAge          string `json:"max_age"`
	Dataset         *struct {
		KUsed *int `json:"k_used"`
	} `json:"dataset"`
	Items []exampleItem `json:"items"`
}

type ingestExample struct {
	Contract        string        `json:"contract"`
	ContractVersion int           `json:"contract_version"`
	Items           []exampleItem `json:"items"`
}

type exampleItem struct {
	EntityKey string          `json:"entity_key"`
	Op        string          `json:"op"`
	Record    json.RawMessage `json:"record"`
}

// CheckExample kiểm một mẫu (trang feed hoặc lô nạp) theo phong bì env và hợp đồng.
func (c *Contract) CheckExample(raw []byte, env *Envelope) []string {
	var out []string
	if err := env.Validate(raw); err != nil {
		return []string{"mẫu không khớp phong bì " + env.Name + ": " + err.Error()}
	}
	var items []exampleItem
	if c.Kind == KindFeed {
		var ex feedExample
		if err := json.Unmarshal(raw, &ex); err != nil {
			return []string{"mẫu hỏng: " + err.Error()}
		}
		if ex.Feed != c.Name || ex.ContractVersion != c.Version {
			out = append(out, fmt.Sprintf("mẫu khai feed %s v%d, hợp đồng là %s", ex.Feed, ex.ContractVersion, c.ID()))
		}
		if c.Freshness != nil && ex.MaxAge != c.Freshness.MaxAge {
			out = append(out, fmt.Sprintf("mẫu max_age %s khác freshness.max_age %s", ex.MaxAge, c.Freshness.MaxAge))
		}
		if c.Delivery != nil && c.Delivery.DatasetVersioned && ex.Dataset == nil {
			out = append(out, "feed theo phiên bản: mẫu phải có dataset")
		}
		if a := c.Aggregation; a != nil && ex.Dataset != nil {
			if ex.Dataset.KUsed == nil || *ex.Dataset.KUsed < a.KDefault {
				out = append(out, fmt.Sprintf("tập tổng hợp: dataset.k_used phải có và ≥ k_default %d (ADR-0035 §3)", a.KDefault))
			}
		}
		items = ex.Items
	} else {
		var ex ingestExample
		if err := json.Unmarshal(raw, &ex); err != nil {
			return []string{"mẫu hỏng: " + err.Error()}
		}
		if ex.Contract != c.Name || ex.ContractVersion != c.Version {
			out = append(out, fmt.Sprintf("mẫu khai contract %s v%d, hợp đồng là %s", ex.Contract, ex.ContractVersion, c.ID()))
		}
		if c.Ingest != nil && len(ex.Items) > c.Ingest.MaxBatch {
			out = append(out, fmt.Sprintf("lô mẫu %d bản ghi > max_batch %d", len(ex.Items), c.Ingest.MaxBatch))
		}
		items = ex.Items
	}
	if len(items) == 0 {
		out = append(out, "mẫu phải có ít nhất một bản ghi")
	}
	for i, it := range items {
		if it.Op == "retract" && (len(it.Record) == 0 || string(it.Record) == "null") {
			continue
		}
		if err := c.ValidateRecord(it.Record); err != nil {
			out = append(out, fmt.Sprintf("items[%d]: %v", i, err))
			continue
		}
		key, err := c.EntityKeyOf(it.Record)
		if err != nil {
			out = append(out, fmt.Sprintf("items[%d]: %v", i, err))
		} else if key != it.EntityKey {
			out = append(out, fmt.Sprintf("items[%d]: entity_key %q khác khóa dựng từ bản ghi %q", i, it.EntityKey, key))
		}
	}
	return out
}

var (
	loadOnce  sync.Once
	loaded    map[string]*Contract
	envelopes map[string]*Envelope
	loadErr   error
)

func load() {
	loaded = map[string]*Contract{}
	envelopes = map[string]*Envelope{}
	envs, err := fs.Glob(files, "envelope/*.json")
	if err != nil {
		loadErr = err
		return
	}
	for _, f := range envs {
		raw, err := files.ReadFile(f)
		if err != nil {
			loadErr = err
			return
		}
		e, err := ParseEnvelope(raw)
		if err != nil {
			loadErr = fmt.Errorf("%s: %w", f, err)
			return
		}
		if len(e.Problems()) == 0 {
			envelopes[e.Name] = e
		}
	}
	for _, kind := range []string{KindFeed, KindIngest} {
		paths, err := fs.Glob(files, Dir(kind)+"/*.json")
		if err != nil {
			loadErr = err
			return
		}
		for _, f := range paths {
			raw, err := files.ReadFile(f)
			if err != nil {
				loadErr = err
				return
			}
			c, err := Parse(raw)
			if err != nil {
				loadErr = fmt.Errorf("%s: %w", f, err)
				return
			}
			if len(c.MetaProblems()) > 0 || c.Kind != kind || path.Base(f) != c.ID()+".json" {
				continue // mặc định từ chối
			}
			if c.Resolve(publication.Get) != nil || len(c.RecordProblems()) > 0 {
				continue
			}
			loaded[c.ID()] = c
		}
	}
}

// Get trả hợp đồng nhúng theo tên và phiên bản. Hợp đồng có vấn đề coi như không có (mặc định
// từ chối). Gọi Servable trước khi phục vụ.
func Get(name string, version int) (*Contract, error) {
	loadOnce.Do(load)
	if loadErr != nil {
		return nil, loadErr
	}
	c, ok := loaded[fmt.Sprintf("%s.v%d", name, version)]
	if !ok {
		return nil, fmt.Errorf("%w: không có hợp đồng tích hợp %s v%d hợp lệ", publication.ErrDenied, name, version)
	}
	return c, nil
}

// All trả mọi hợp đồng hợp lệ, sắp theo ID.
func All() ([]*Contract, error) {
	loadOnce.Do(load)
	if loadErr != nil {
		return nil, loadErr
	}
	out := make([]*Contract, 0, len(loaded))
	for _, c := range loaded {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out, nil
}

// EnvelopeFor trả phong bì của loại hợp đồng: feed_page cho feed, ingest_batch cho nạp.
func EnvelopeFor(kind string) (*Envelope, error) {
	loadOnce.Do(load)
	if loadErr != nil {
		return nil, loadErr
	}
	name := EnvelopeName(kind)
	e, ok := envelopes[name]
	if !ok {
		return nil, errors.New("integration: không có phong bì " + name)
	}
	return e, nil
}

// EnvelopeName là tên phong bì của loại hợp đồng.
func EnvelopeName(kind string) string {
	if kind == KindIngest {
		return "ingest_batch"
	}
	return "feed_page"
}
