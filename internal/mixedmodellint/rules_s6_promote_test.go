package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestRelationshipSatellites(t *testing.T) {
	t.Parallel()
	people := func(fields ...m.Field) m.Entity {
		e := table("people", []string{"person_id"}, append([]m.Field{
			col("person_id", m.TypeInt), col("spouse_id", m.TypeInt),
		}, fields...)...)
		return referencing(&e, "spouse_id", "people")
	}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
		level    m.Severity
	}{
		"a self-reference with a dated satellite": {
			entities: []m.Entity{people(col("spouse_since", m.TypeDate))},
			want:     []string{"6.4-A people spouse_id"}, level: m.SeverityWarning,
		},
		"a satellite whose type does not fit": {
			entities: []m.Entity{people(col("spouse_since", m.TypeString))},
			want:     []string{"6.4-A people spouse_id"}, level: m.SeverityNote,
		},
		"a satellite of a reference to a master": {
			entities: []m.Entity{
				table("products", []string{"product_id"}, col("product_id", m.TypeInt)),
				referencing(new(table("listings", nil, col("product_id", m.TypeInt),
					col("product_since", m.TypeDate))), "product_id", "products"),
			},
			want: []string{"6.4-A listings product_id"}, level: m.SeverityNote,
		},
		"no satellites": {entities: []m.Entity{people(col("born_on", m.TypeDate))}},
		"an event time is not a satellite": {
			entities: []m.Entity{people(col("spouse_at", m.TypeTimestamptz))},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := m.Analyze(src(tc.entities...))
			equals(t, tc.want, findingsOf(src(tc.entities...), "6.4-A"))
			for _, f := range r.Findings {
				if f.Design == "6.4-A" {
					equals(t, tc.level, f.Severity)
				}
			}
		})
	}
}

func TestFatJunctions(t *testing.T) {
	t.Parallel()
	students := table("students", []string{"student_id"}, col("student_id", m.TypeInt))
	courses := table("courses", []string{"course_id"}, col("course_id", m.TypeInt))
	link := func(fields ...m.Field) m.Entity {
		e := table("student_courses", []string{"student_id", "course_id"}, append([]m.Field{
			col("student_id", m.TypeInt), col("course_id", m.TypeInt),
		}, fields...)...)
		e = referencing(&e, "student_id", "students")
		return referencing(&e, "course_id", "courses")
	}
	grades := link(col("grade", m.TypeString), col("enrolled_on", m.TypeDate))
	cases := map[string]struct {
		entities []m.Entity
		queries  []m.Query
		want     []string
	}{
		"attributes and referenced elsewhere": {
			entities: []m.Entity{
				students, courses, grades,
				func() m.Entity {
					e := table("transcripts", nil, col("student_id", m.TypeInt),
						col("course_id", m.TypeInt))
					e.Refs = []m.Ref{{
						Fields: []string{"student_id", "course_id"},
						Target: "student_courses",
					}}
					return e
				}(),
			},
			want: []string{"6.4-B student_courses"},
		},
		"attributes and a query filtering on one": {
			entities: []m.Entity{students, courses, grades},
			queries: []m.Query{{
				Relations: []m.Relation{{Name: "student_courses", Alias: "student_courses"}},
				Filters:   []m.ColumnRef{{Column: "grade"}},
			}},
			want: []string{"6.4-B student_courses"},
		},
		"attributes but unused as an entity": {entities: []m.Entity{students, courses, grades}},
		"one attribute": {
			entities: []m.Entity{students, courses, link(col("grade", m.TypeString))},
			queries: []m.Query{{
				Relations: []m.Relation{{Name: "student_courses", Alias: "student_courses"}},
				Filters:   []m.ColumnRef{{Column: "grade"}},
			}},
		},
		"a surrogate key": {
			entities: []m.Entity{students, courses, func() m.Entity {
				e := grades
				e.PrimaryKey = []string{"enrollment_id"}
				e.Fields = append([]m.Field{col("enrollment_id", m.TypeInt)}, e.Fields...)
				return e
			}()},
			queries: []m.Query{{
				Relations: []m.Relation{{Name: "student_courses", Alias: "student_courses"}},
				Filters:   []m.ColumnRef{{Column: "grade"}},
			}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(withQueries(src(tc.entities...), tc.queries...), "6.4-B"))
		})
	}
}

func TestRelationshipGrowth(t *testing.T) {
	t.Parallel()
	before := referencing(new(table("people", []string{"person_id"},
		col("person_id", m.TypeInt), col("spouse_id", m.TypeInt))), "spouse_id", "people")
	with := func(fields ...m.Field) m.Entity {
		e := before
		e.Fields = append(append([]m.Field(nil), before.Fields...), fields...)
		return e
	}
	cases := map[string]struct {
		current m.Entity
		want    []string
	}{
		"unchanged": {current: before},
		"a new _since beside the reference": {
			current: with(col("spouse_since", m.TypeDate)),
			want:    []string{"6.4-C people spouse_since"},
		},
		"a reference split in two": {
			current: with(col("spouse_1_id", m.TypeInt), col("spouse_2_id", m.TypeInt)),
			want:    []string{"6.4-C people spouse_1_id", "6.4-C people spouse_2_id"},
		},
		"an unrelated new column": {current: with(col("nickname", m.TypeString))},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := src(tc.current)
			in.Lock = lockOf(t, before)
			equals(t, tc.want, findingsOf(in, "6.4-C"))
		})
	}
}
