package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestFiscalCalendar(t *testing.T) {
	t.Parallel()
	calendar := func(name string) m.Entity {
		return table(name, []string{"calendar_date"}, col("calendar_date", m.TypeDate),
			col("fiscal_year", m.TypeInt), col("fiscal_quarter", m.TypeInt),
			col("fiscal_month", m.TypeInt), col("fiscal_week", m.TypeInt),
			col("is_fiscal_year_end", m.TypeBool))
	}
	thin := table("dim_date", []string{"calendar_date"}, col("calendar_date", m.TypeDate),
		col("fiscal_year", m.TypeInt))
	sales := table("monthly_sales", []string{"sale_id"}, col("sale_id", m.TypeInt),
		col("calendar_date", m.TypeDate), col("fiscal_quarter", m.TypeInt))
	snapshot := referencing(&sales, "calendar_date", "dim_date")
	roleCalendar := calendar("periods")
	roleCalendar.Annotations = ann("role", "calendar")
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"one complete calendar": {entities: []m.Entity{calendar("dim_date")}},
		"two calendars": {
			entities: []m.Entity{calendar("dim_date"), calendar("fiscal_calendar")},
			want:     []string{"9.7-A fiscal_calendar"},
		},
		"a calendar declared by role": {
			entities: []m.Entity{calendar("dim_date"), roleCalendar},
			want:     []string{"9.7-A periods"},
		},
		"a calendar missing columns": {
			entities: []m.Entity{thin}, want: []string{"9.7-A dim_date"},
		},
		"fiscal columns stored elsewhere": {
			entities: []m.Entity{calendar("dim_date"), sales},
			want:     []string{"9.7-A monthly_sales fiscal_quarter"},
		},
		"a snapshot referencing the calendar": {
			entities: []m.Entity{calendar("dim_date"), snapshot},
		},
		"fiscal columns and no calendar": {
			entities: []m.Entity{sales}, want: []string{"9.7-A monthly_sales fiscal_quarter"},
		},
		"no calendar and no fiscal columns": {
			entities: []m.Entity{table("orders", nil, col("order_id", m.TypeInt))},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "9.7-A"))
		})
	}
}
