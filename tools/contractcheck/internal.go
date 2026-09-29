package main

import (
	"fmt"
	"go/ast"
	"path/filepath"
	"regexp"
	"strings"
)

// Sự kiện NỘI BỘ MKG (ADR-0018, Q-MKG-01).
//
// Vhost RabbitMQ mkg.internal tách khỏi vhost nền tảng; tài khoản nền tảng không
// có quyền trên nó. Payload chỉ đi vào vhost đó được chú thích
//
//	//ag:internal-event <tên> v<N>
//
// và cổng bắt:
//
//	(f) //ag:internal-event chỉ dùng trong internal/mkg/**, tên mkg.*, không kèm
//	    //ag:event hay //ag:publication trên cùng struct;
//	(g) schema bắt buộc ở contracts/events-internal/<tên>.v<N>.json (cùng quy tắc
//	    kiểu, required, tương thích ngược như contracts/events/); tên không được
//	    trùng một sự kiện nền tảng ở contracts/events/;
//	(h) literal outbox.Event trong internal/mkg/**: Kind: outbox.KindInternal thì
//	    Payload phải chứng minh được là struct //ag:internal-event; Kind vắng mặt /
//	    KindPlatform mà Payload là struct //ag:internal-event → hỏng (không bao giờ
//	    phát sự kiện nội bộ lên exchange nền tảng); Kind phải là một trong hai hằng;
//	(i) outbox.KindInternal không được dùng ngoài internal/mkg/**.

const internalEventsDir = "events-internal"

var internalTagRe = regexp.MustCompile(`(?m)^//ag:internal-event\s+(\S+)\s+v(\d+)\b`)

// internalSchemaRules: (g) phần tệp schema.
func internalSchemaRules(root string, platform, internal map[string]Schema) []string {
	var p []string
	dir := filepath.Join(root, "contracts", internalEventsDir)
	platformNames := map[string]bool{}
	for _, s := range platform {
		platformNames[s.EventType] = true
	}
	for key, s := range internal {
		f := filepath.Join(dir, fmt.Sprintf("%s.v%d.json", s.EventType, s.SchemaVersion))
		if !strings.HasPrefix(s.EventType, "mkg.") {
			p = append(p, fmt.Sprintf("%s: sự kiện nội bộ phải thuộc vùng mkg (mkg.*), có %s (ADR-0018)", f, key))
		}
		if platformNames[s.EventType] {
			p = append(p, fmt.Sprintf("%s: %s đã là sự kiện nền tảng ở contracts/events/ — một tên chỉ thuộc một vhost (ADR-0018)", f, s.EventType))
		}
	}
	return p
}

// internalEventRules: (f) và (g) phần struct.
func (ix *index) internalEventRules(ann map[string]*annotated, internal map[string]Schema) []string {
	var p []string
	for _, a := range ann {
		if a.internal == "" {
			continue
		}
		if !inMKG(a.decl.file.rel) {
			p = append(p, fmt.Sprintf("%s: //ag:internal-event chỉ dùng trong internal/mkg (vhost mkg.internal, ADR-0018)", a.pos))
		}
		if a.event != "" || a.publication != "" {
			p = append(p, fmt.Sprintf("%s: %s không được vừa là sự kiện nội bộ vừa là sự kiện nền tảng / payload van", a.pos, a.decl.name))
		}
		st, isStruct := a.decl.expr.(*ast.StructType)
		if !isStruct {
			continue // "không phải struct" đã báo ở Check
		}
		s, ok := internal[a.internal]
		if !ok {
			p = append(p, fmt.Sprintf("%s: struct %s chú thích ag:internal-event %s nhưng không có schema trong contracts/%s/", a.pos, a.decl.name, a.internal, internalEventsDir))
			continue
		}
		p = append(p, ix.checkStruct(a.decl.name, "", st, a.decl.file, &s.Node, "schema nội bộ "+a.internal, 0)...)
	}
	return p
}

// outboxKind phân loại giá trị trường Kind của literal outbox.Event:
// internal = KindInternal; ok = false khi không phải một trong hai hằng.
func outboxKind(e ast.Expr, sf *srcFile) (internal, ok bool) {
	if e == nil {
		return false, true
	}
	sel, isSel := e.(*ast.SelectorExpr)
	if !isSel {
		return false, false
	}
	x, isIdent := sel.X.(*ast.Ident)
	if !isIdent || !strings.HasSuffix(sf.imports[x.Name], "/internal/shared/outbox") {
		return false, false
	}
	switch sel.Sel.Name {
	case "KindInternal":
		return true, true
	case "KindPlatform":
		return false, true
	}
	return false, false
}

// payloadType trả kiểu có tên của biểu thức payload (literal, tham số, biến cục bộ).
func (ix *index) payloadType(e ast.Expr, sf *srcFile, params, locals map[string]ast.Expr) (pkg, name string, ok bool) {
	var typ ast.Expr
	if t := litType(e); t != nil {
		typ = t
	} else if id, isIdent := e.(*ast.Ident); isIdent {
		if t, found := params[id.Name]; found {
			typ = t
		} else if t, found := locals[id.Name]; found {
			typ = t
		}
	}
	if typ == nil {
		return "", "", false
	}
	if s, isStar := typ.(*ast.StarExpr); isStar {
		typ = s.X
	}
	return ix.lookup(typ, sf)
}

// internalPayload trả tên kiểu nếu payload chứng minh được là struct //ag:internal-event.
func (ix *index) internalPayload(e ast.Expr, sf *srcFile, params, locals map[string]ast.Expr, ann map[string]*annotated) string {
	pkg, name, ok := ix.payloadType(e, sf, params, locals)
	if !ok {
		return ""
	}
	if a := ann[annKey(pkg, name)]; a != nil && a.internal != "" {
		return name
	}
	return ""
}

// proveInternal trả "" nếu payload chứng minh được là sự kiện nội bộ.
func (ix *index) proveInternal(e ast.Expr, sf *srcFile, params, locals map[string]ast.Expr, ann map[string]*annotated) string {
	pkg, name, ok := ix.payloadType(e, sf, params, locals)
	if !ok {
		return "không chứng minh được là sự kiện nội bộ (dùng literal, tham số hoặc biến có kiểu //ag:internal-event)"
	}
	if a := ann[annKey(pkg, name)]; a != nil && a.internal != "" {
		return ""
	}
	return fmt.Sprintf("có kiểu %s không có //ag:internal-event", name)
}

// internalKindOutsideMKG: (i).
func (ix *index) internalKindOutsideMKG(sf *srcFile) []string {
	var p []string
	ast.Inspect(sf.file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "KindInternal" {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && strings.HasSuffix(sf.imports[x.Name], "/internal/shared/outbox") {
			p = append(p, fmt.Sprintf("%s: outbox.KindInternal chỉ dùng trong internal/mkg — sự kiện nội bộ là của vùng MKG (ADR-0018)", ix.pos(sel.Pos())))
		}
		return true
	})
	return p
}
