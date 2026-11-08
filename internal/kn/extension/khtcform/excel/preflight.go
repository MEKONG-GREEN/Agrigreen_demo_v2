package excel

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// preflight checks ZIP and XML bounds before excelize constructs any worksheet
// or shared-string structures. No XML entities, external targets, macros,
// embedded objects or executable packages are accepted. It never evaluates f.
func preflight(data []byte) error {
	if len(data) > MaxWorkbookBytes {
		return ErrLimit
	}
	if len(data) < 4 || !bytes.Equal(data[:4], []byte{'P', 'K', 3, 4}) {
		return ErrWorkbook
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ErrWorkbook
	}
	if len(archive.File) > MaxZipFiles {
		return ErrLimit
	}
	seen := map[string]bool{}
	var total uint64
	cells, shared, sheets := 0, 0, 0
	for _, file := range archive.File {
		name := file.Name
		lower := strings.ToLower(name)
		if name == "" || len(name) > 512 || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.Contains(name, ":") || strings.Contains(name, "../") || path.Clean(name) != strings.TrimSuffix(name, "/") || seen[lower] {
			return ErrWorkbook
		}
		seen[lower] = true
		if file.FileInfo().IsDir() {
			continue
		}
		if strings.HasSuffix(lower, ".bin") || strings.Contains(lower, "vbaproject") || strings.Contains(lower, "externallinks/") || strings.Contains(lower, "embeddings/") || strings.Contains(lower, "activex/") || strings.Contains(lower, "customui/") || strings.Contains(lower, "connections") {
			return ErrWorkbook
		}
		if file.UncompressedSize64 > MaxPartBytes || file.UncompressedSize64 > MaxUnzipBytes-total {
			return ErrLimit
		}
		total += file.UncompressedSize64
		if !strings.HasSuffix(lower, ".xml") && !strings.HasSuffix(lower, ".rels") {
			return ErrWorkbook
		}
		r, err := file.Open()
		if err != nil {
			return ErrWorkbook
		}
		raw, readErr := io.ReadAll(io.LimitReader(r, MaxPartBytes+1))
		closeErr := r.Close()
		if readErr != nil || closeErr != nil {
			return ErrWorkbook
		}
		if len(raw) > MaxPartBytes {
			return ErrLimit
		}
		if err := scanXML(raw, lower, &cells, &shared, &sheets); err != nil {
			return err
		}
	}
	if !seen["[content_types].xml"] || !seen["xl/workbook.xml"] || sheets < 1 {
		return ErrWorkbook
	}
	return nil
}

func coordinateBound(ref string) error {
	if len(ref) > 10 {
		return ErrLimit
	}
	col, row, err := excelize.CellNameToCoordinates(ref)
	if err != nil {
		return ErrWorkbook
	}
	if row < 1 || col < 1 || row > maxXMLRows || col > MaxColumns+1 {
		return ErrLimit
	}
	return nil
}

func rangeBound(ref string) error {
	if len(ref) > 21 {
		return ErrLimit
	}
	parts := strings.Split(ref, ":")
	if len(parts) < 1 || len(parts) > 2 {
		return ErrWorkbook
	}
	for _, part := range parts {
		if err := coordinateBound(part); err != nil {
			return err
		}
	}
	return nil
}

func scanXML(raw []byte, name string, cells, shared, sheets *int) error {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	worksheet := strings.HasPrefix(name, "xl/worksheets/") && strings.HasSuffix(name, ".xml")
	depth, cellDepth, textBytes, tokens, rowCount := 0, -1, 0, 0, 0
	cellRefs, rowRefs := map[string]bool{}, map[int]bool{}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ErrWorkbook
		}
		tokens++
		if tokens > maxXMLCells*10 {
			return ErrLimit
		}
		switch t := token.(type) {
		case xml.Directive:
			return ErrWorkbook
		case xml.ProcInst:
			if t.Target != "xml" {
				return ErrWorkbook
			}
		case xml.StartElement:
			depth++
			if depth > 64 || len(t.Attr) > 64 {
				return ErrLimit
			}
			for _, a := range t.Attr {
				if len(a.Value) > MaxCellBytes {
					return ErrLimit
				}
			}
			if t.Name.Local == "Relationship" {
				for _, a := range t.Attr {
					if a.Name.Local == "TargetMode" && strings.EqualFold(a.Value, "External") {
						return ErrWorkbook
					}
					if a.Name.Local == "Target" && (len(a.Value) > 2048 || strings.ContainsAny(a.Value, ":\\") || strings.HasPrefix(a.Value, "//")) {
						return ErrWorkbook
					}
				}
			}
			if name == "[content_types].xml" {
				for _, a := range t.Attr {
					v := strings.ToLower(a.Value)
					if strings.Contains(v, "macroenabled") || strings.Contains(v, "vbaproject") || strings.Contains(v, "oleobject") {
						return ErrWorkbook
					}
				}
			}
			if name == "xl/workbook.xml" && t.Name.Local == "sheet" {
				*sheets++
				if *sheets > MaxSheets {
					return ErrLimit
				}
			}
			if name == "xl/sharedstrings.xml" && t.Name.Local == "sst" {
				for _, a := range t.Attr {
					if a.Name.Local == "count" || a.Name.Local == "uniqueCount" {
						if len(a.Value) > 7 {
							return ErrLimit
						}
						n, err := strconv.Atoi(a.Value)
						if err != nil || n < 0 {
							return ErrWorkbook
						}
						if n > maxXMLCells {
							return ErrLimit
						}
					}
				}
			}
			if name == "xl/sharedstrings.xml" && t.Name.Local == "si" {
				*shared++
				if *shared > maxXMLCells {
					return ErrLimit
				}
				cellDepth, textBytes = depth, 0
			}
			if worksheet {
				switch t.Name.Local {
				case "c":
					*cells++
					if *cells > maxXMLCells {
						return ErrLimit
					}
					cellDepth, textBytes = depth, 0
					ref := ""
					for _, a := range t.Attr {
						if a.Name.Local == "r" {
							ref = a.Value
						}
					}
					if err := coordinateBound(ref); err != nil {
						return err
					}
					col, line, _ := excelize.CellNameToCoordinates(ref)
					canonical, _ := excelize.CoordinatesToCellName(col, line)
					if ref != canonical || cellRefs[canonical] {
						return ErrWorkbook
					}
					cellRefs[canonical] = true
				case "row":
					rowCount++
					if rowCount > maxXMLRows {
						return ErrLimit
					}
					hasIndex := false
					for _, a := range t.Attr {
						if a.Name.Local == "r" {
							hasIndex = true
							if len(a.Value) > 4 {
								return ErrLimit
							}
							n, err := strconv.Atoi(a.Value)
							if err != nil || n < 1 {
								return ErrWorkbook
							}
							if n > maxXMLRows {
								return ErrLimit
							}
							if rowRefs[n] {
								return ErrWorkbook
							}
							rowRefs[n] = true
						}
					}
					if !hasIndex {
						return ErrWorkbook
					}
				case "col":
					for _, a := range t.Attr {
						if a.Name.Local == "min" || a.Name.Local == "max" {
							if len(a.Value) > 3 {
								return ErrLimit
							}
							n, err := strconv.Atoi(a.Value)
							if err != nil || n < 1 {
								return ErrWorkbook
							}
							if n > MaxColumns+1 {
								return ErrLimit
							}
						}
					}
				case "dimension", "mergeCell":
					for _, a := range t.Attr {
						if a.Name.Local == "ref" {
							if err := rangeBound(a.Value); err != nil {
								return err
							}
						}
					}
				case "oleObjects", "drawing", "legacyDrawing", "legacyDrawingHF", "extLst":
					return ErrWorkbook
				}
			}
		case xml.CharData:
			if cellDepth >= 0 {
				textBytes += len(t)
				if textBytes > MaxCellBytes {
					return ErrLimit
				}
			}
		case xml.EndElement:
			if depth == cellDepth {
				cellDepth = -1
			}
			depth--
		}
	}
	return nil
}
