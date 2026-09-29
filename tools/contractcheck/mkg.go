package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// Quy tắc vùng mkg (R-03, mục 5): mọi sự kiện của vùng mkg lên RabbitMQ đều
// thấy được từ vùng nền tảng, nên payload rời mkg PHẢI là hợp đồng van.
//
//	(a) struct //ag:event trong internal/mkg/** phải kèm //ag:publication;
//	(b) //ag:publication chỉ dùng trong internal/mkg/** (van là một chiều MKG → nền tảng);
//	(c) kiểu có phương thức PublicationName() (cài đặt port.Publication) phải có
//	    //ag:publication và trả đúng tên hợp đồng đã chú thích;
//	(d) mọi literal outbox.Event trong internal/mkg/** phải có Payload chứng minh
//	    được là hợp đồng van: literal của struct //ag:publication, hoặc tham số /
//	    biến cục bộ có kiểu như vậy, hoặc tham số kiểu interface Publication khai
//	    ở một package internal/mkg/*/port (được (c) bảo đảm);
//	(e) không gán e.Payload = ... cho biến outbox.Event trong internal/mkg/** (không kiểm được).

func inMKG(rel string) bool { return strings.HasPrefix(rel, "internal/mkg/") }

func (ix *index) mkgRules(ann map[string]*annotated) []string {
	var p []string
	for _, a := range ann {
		rel := a.decl.file.rel
		if a.event != "" && a.publication == "" && inMKG(rel) {
			p = append(p, fmt.Sprintf("%s: %s là payload sự kiện của vùng mkg nhưng không có //ag:publication — sự kiện mkg phải đi qua van công bố (R-03, mục 5)", a.pos, a.decl.name))
		}
		if a.publication != "" && !inMKG(rel) {
			p = append(p, fmt.Sprintf("%s: //ag:publication chỉ dùng trong internal/mkg (van một chiều MKG → nền tảng)", a.pos))
		}
	}
	for _, sf := range ix.files {
		if !inMKG(sf.rel) {
			p = append(p, ix.internalKindOutsideMKG(sf)...)
			continue
		}
		for _, decl := range sf.file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fd.Recv != nil && fd.Name.Name == "PublicationName" && len(fd.Recv.List) > 0 {
				p = append(p, ix.checkPublicationName(sf, fd, ann)...)
			}
			if fd.Body != nil {
				p = append(p, ix.checkPayloads(sf, fd, ann)...)
			}
		}
	}
	return p
}

func annKey(pkg, name string) string { return pkg + "." + name }

func (ix *index) checkPublicationName(sf *srcFile, fd *ast.FuncDecl, ann map[string]*annotated) []string {
	typ := recvName(fd.Recv.List[0].Type)
	a := ann[annKey(sf.pkgPath, typ)]
	if a == nil || a.publication == "" {
		return []string{fmt.Sprintf("%s: %s cài đặt PublicationName nhưng không có //ag:publication", ix.pos(fd.Pos()), typ)}
	}
	var lit string
	if fd.Body != nil && len(fd.Body.List) == 1 {
		if r, ok := fd.Body.List[0].(*ast.ReturnStmt); ok && len(r.Results) == 1 {
			if bl, ok := r.Results[0].(*ast.BasicLit); ok && bl.Kind == token.STRING {
				lit, _ = strconv.Unquote(bl.Value)
			}
		}
	}
	if lit != a.publication {
		return []string{fmt.Sprintf("%s: %s.PublicationName phải là `return %q` (khớp //ag:publication)", ix.pos(fd.Pos()), typ, a.publication)}
	}
	return nil
}

// isOutboxEvent cho biết biểu thức kiểu là outbox.Event của internal/shared/outbox.
func isOutboxEvent(e ast.Expr, sf *srcFile) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Event" {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && strings.HasSuffix(sf.imports[x.Name], "/internal/shared/outbox")
}

func (ix *index) checkPayloads(sf *srcFile, fd *ast.FuncDecl, ann map[string]*annotated) []string {
	var p []string
	params := map[string]ast.Expr{}
	if fd.Type.Params != nil {
		for _, f := range fd.Type.Params.List {
			for _, n := range f.Names {
				params[n.Name] = f.Type
			}
		}
	}
	locals := map[string]ast.Expr{} // x := T{...} / var x T
	isEventVar := func(e ast.Expr) bool {
		id, ok := e.(*ast.Ident)
		if !ok {
			return false
		}
		t := locals[id.Name]
		if t == nil {
			t = params[id.Name]
		}
		if s, ok := t.(*ast.StarExpr); ok {
			t = s.X
		}
		return t != nil && isOutboxEvent(t, sf)
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			for i, l := range s.Lhs {
				if sel, ok := l.(*ast.SelectorExpr); ok && (sel.Sel.Name == "Payload" || sel.Sel.Name == "Kind") && isEventVar(sel.X) {
					p = append(p, fmt.Sprintf("%s: gán .%s của outbox.Event trực tiếp trong vùng mkg — dựng outbox.Event bằng literal để kiểm được hợp đồng van / sự kiện nội bộ", ix.pos(s.Pos()), sel.Sel.Name))
				}
				if id, ok := l.(*ast.Ident); ok && i < len(s.Rhs) && s.Tok == token.DEFINE {
					if t := litType(s.Rhs[i]); t != nil {
						locals[id.Name] = t
					}
				}
			}
		case *ast.ValueSpec:
			if s.Type != nil {
				for _, n := range s.Names {
					locals[n.Name] = s.Type
				}
			}
		case *ast.CompositeLit:
			if !isOutboxEvent(s.Type, sf) {
				return true
			}
			var payload, kind ast.Expr
			for _, el := range s.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					p = append(p, fmt.Sprintf("%s: outbox.Event phải dùng literal có tên trường", ix.pos(s.Pos())))
					return true
				}
				if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Payload" {
					payload = kv.Value
				}
				if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Kind" {
					kind = kv.Value
				}
			}
			internal, ok := outboxKind(kind, sf)
			if !ok {
				p = append(p, fmt.Sprintf("%s: Kind của outbox.Event trong vùng mkg phải là hằng outbox.KindPlatform hoặc outbox.KindInternal (ADR-0018)", ix.pos(kind.Pos())))
				return true
			}
			if payload == nil {
				return true
			}
			if internal {
				if msg := ix.proveInternal(payload, sf, params, locals, ann); msg != "" {
					p = append(p, fmt.Sprintf("%s: Payload của outbox.Event nội bộ (KindInternal) %s", ix.pos(payload.Pos()), msg))
				}
				return true
			}
			if name := ix.internalPayload(payload, sf, params, locals, ann); name != "" {
				p = append(p, fmt.Sprintf("%s: Payload %s là sự kiện nội bộ MKG (//ag:internal-event) — không được phát lên exchange nền tảng, cần Kind: outbox.KindInternal (ADR-0018)", ix.pos(payload.Pos()), name))
				return true
			}
			if msg := ix.provePublication(payload, sf, params, locals, ann); msg != "" {
				p = append(p, fmt.Sprintf("%s: Payload của outbox.Event trong vùng mkg %s", ix.pos(payload.Pos()), msg))
			}
		}
		return true
	})
	return p
}

// litType trả kiểu của T{...} hoặc &T{...}.
func litType(e ast.Expr) ast.Expr {
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	if cl, ok := e.(*ast.CompositeLit); ok {
		return cl.Type
	}
	return nil
}

// provePublication trả "" nếu biểu thức chứng minh được là payload van.
func (ix *index) provePublication(e ast.Expr, sf *srcFile, params, locals map[string]ast.Expr, ann map[string]*annotated) string {
	var typ ast.Expr
	if t := litType(e); t != nil {
		typ = t
	} else if id, ok := e.(*ast.Ident); ok {
		if t, ok := params[id.Name]; ok {
			typ = t
		} else if t, ok := locals[id.Name]; ok {
			typ = t
		}
	}
	if typ == nil {
		return "không chứng minh được là hợp đồng van (dùng literal, tham số hoặc biến có kiểu //ag:publication)"
	}
	if s, ok := typ.(*ast.StarExpr); ok {
		typ = s.X
	}
	pkg, name, ok := ix.lookup(typ, sf)
	if !ok {
		return "có kiểu không phân giải được"
	}
	if a := ann[annKey(pkg, name)]; a != nil && a.publication != "" {
		return ""
	}
	if d := ix.decl(pkg, name); d != nil {
		if _, isIface := d.expr.(*ast.InterfaceType); isIface && name == "Publication" &&
			strings.HasPrefix(d.file.rel, "internal/mkg/") && strings.Contains(d.file.rel, "/port/") {
			return ""
		}
	}
	return fmt.Sprintf("có kiểu %s không có //ag:publication — mặc định từ chối", name)
}
