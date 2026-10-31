package goast

import (
	"go/ast"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// goField is one persisted field with what the struct tags said about it.
type goField struct {
	field     mml.Field
	tagged    bool
	pk        bool
	refTarget string
}

// collectFields returns d's exported, persisted fields in declaration order,
// expanding embedded structs declared in the same directory. seen stops a
// cycle of embeddings.
func collectFields(d *structDecl, known map[string]*structDecl, seen map[string]bool) []goField {
	seen[d.name] = true
	var out []goField
	for _, f := range d.typ.Fields.List {
		if len(f.Names) == 0 {
			out = append(out, embedded(d, f, known, seen)...)
			continue
		}
		for _, n := range f.Names {
			if !n.IsExported() {
				continue
			}
			if gf, ok := makeField(d, f, n, known); ok {
				out = append(out, gf)
			}
		}
	}
	return out
}

func embedded(
	d *structDecl,
	f *ast.Field,
	known map[string]*structDecl,
	seen map[string]bool,
) []goField {
	name := typeString(f.Type)
	name = strings.TrimPrefix(name, "*")
	inner, ok := known[d.pkgDir+"."+name]
	if !ok || seen[name] {
		return nil
	}
	return collectFields(inner, known, seen)
}

func makeField(
	d *structDecl,
	f *ast.Field,
	n *ast.Ident,
	known map[string]*structDecl,
) (goField, bool) {
	tag := structTag(f)
	name, tagged, omitempty := tagName(tag)
	if name == "-" {
		return goField{}, false
	}
	if name == "" {
		name = snake(n.Name)
	}
	pos := position(d.fset, n.Pos())
	raw := typeString(f.Type)
	lt, nullable := logicalType(raw)
	gf := goField{
		field: mml.Field{
			Name: name, Type: lt, RawType: raw, Nullable: nullable || omitempty, Pos: pos,
			Annotations: mml.ParseTag(tag.Get("mml"), pos),
		},
		tagged: tagged || tag.Get("mml") != "",
	}
	_, gf.pk = gf.field.Annotations.Get("pk")
	gf.pk = gf.pk || strings.Contains(tag.Get("gorm"), "primaryKey")
	markRef(d, &gf, raw, known)
	return gf, true
}

func structTag(f *ast.Field) reflect.StructTag {
	if f.Tag == nil {
		return ""
	}
	s, err := strconv.Unquote(f.Tag.Value)
	if err != nil {
		return ""
	}
	return reflect.StructTag(s)
}

// markRef makes a field typed as another struct of the same directory a
// reference to it: Customer *Customer, or Lines []OrderLine.
func markRef(d *structDecl, gf *goField, raw string, known map[string]*structDecl) {
	elem := strings.TrimLeft(raw, "*[]")
	if _, ok := known[d.pkgDir+"."+elem]; !ok || elem == d.name {
		return
	}
	gf.refTarget = elem
	gf.field.Type = mml.TypeObject
	if strings.HasPrefix(strings.TrimLeft(raw, "*"), "[]") {
		gf.field.Type = mml.TypeArray
	}
}

// snake converts a Go identifier to snake_case without singularizing, the
// way sqlx and most mappers name an untagged field's column: OrderLines is
// order_lines and CustomerID is customer_id.
func snake(name string) string {
	runes := []rune(name)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) ||
				unicode.IsUpper(prev) && nextLower {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// typeString renders a field's type expression: *time.Time, []byte, [3]string.
func typeString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + typeString(t.Elt)
		}
		if lit, ok := t.Len.(*ast.BasicLit); ok {
			return "[" + lit.Value + "]" + typeString(t.Elt)
		}
		return "[...]" + typeString(t.Elt)
	case *ast.MapType:
		return "map[" + typeString(t.Key) + "]" + typeString(t.Value)
	case *ast.IndexExpr:
		return typeString(t.X) + "[" + typeString(t.Index) + "]"
	default:
		return "any"
	}
}

// logicalType maps a Go type to the IR's logical type and whether the Go
// type itself admits a missing value (a pointer or a sql.Null* wrapper).
func logicalType(raw string) (mml.LogicalType, bool) {
	nullable := strings.HasPrefix(raw, "*")
	base := strings.TrimPrefix(raw, "*")
	if strings.HasPrefix(base, "sql.Null") || strings.HasPrefix(base, "null.") {
		nullable = true
	}
	switch {
	case base == "[]byte" || base == "json.RawMessage":
		if base == "json.RawMessage" {
			return mml.TypeJSON, nullable
		}
		return mml.TypeBytes, nullable
	case strings.HasPrefix(base, "[]") || strings.HasPrefix(base, "["):
		return mml.TypeArray, nullable
	case strings.HasPrefix(base, "map["):
		return mml.TypeJSON, nullable
	}
	return scalarType(base), nullable
}

func scalarType(base string) mml.LogicalType {
	types := map[string]mml.LogicalType{
		"string": mml.TypeString, "sql.NullString": mml.TypeString, "null.String": mml.TypeString,
		"bool": mml.TypeBool, "sql.NullBool": mml.TypeBool,
		"float32": mml.TypeFloat, "float64": mml.TypeFloat, "sql.NullFloat64": mml.TypeFloat,
		"time.Time": mml.TypeTimestamptz, "sql.NullTime": mml.TypeTimestamptz,
		"null.Time": mml.TypeTimestamptz, "civil.Date": mml.TypeDate,
		"civil.DateTime": mml.TypeTimestampNaive, "civil.Time": mml.TypeTime,
		"time.Duration":   mml.TypeInterval,
		"decimal.Decimal": mml.TypeDecimal, "decimal.NullDecimal": mml.TypeDecimal,
		"uuid.UUID": mml.TypeUUID, "uuid.NullUUID": mml.TypeUUID,
	}
	if t, ok := types[base]; ok {
		return t
	}
	for _, p := range []string{"int", "uint", "sql.NullInt", "null.Int"} {
		if strings.HasPrefix(base, p) {
			return mml.TypeInt
		}
	}
	return mml.TypeUnknown
}
