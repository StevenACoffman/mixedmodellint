package mixedmodellint

import (
	"fmt"
	"slices"
	"strings"
)

func section9CalendarDesigns() []design {
	return []design{{
		id: "9.7-A", title: "Date-dimension shape and uniqueness", strength: Should,
		check: check97A,
	}}
}

// check97A keeps fiscal periods defined once (rule_plan.md §9.7-A): the model
// has at most one calendar entity, it carries the fiscal columns, and any
// other table that stores a fiscal period references it. A periodic snapshot
// may key on fiscal_month when it references the calendar. A model with no
// calendar and no fiscal columns has nothing to check. SQL only: Go and JSON
// Schema rarely hold a date dimension.
func check97A(c *catalog) []issue {
	cals := calendars(c)
	var out []issue
	for i, cal := range cals {
		if i > 0 {
			out = append(out, issue{
				entity: cal,
				measured: fmt.Sprintf("%s is a second calendar beside %s, so a fiscal period "+
					"can be defined two ways", cal.Name, cals[0].Name),
				suggestion: fmt.Sprintf("Keep %s as the one calendar and reference it from "+
					"every table that needs a fiscal period (§9.7).", cals[0].Name),
			})
		}
		if missing := calendarGaps(cal); len(missing) > 0 {
			out = append(out, issue{
				entity: cal,
				measured: fmt.Sprintf("%s is the calendar but has no %s", cal.Name,
					andList(missing)),
				suggestion: fmt.Sprintf("Give %s calendar_date DATE PRIMARY KEY, "+
					"fiscal_year, fiscal_quarter, fiscal_month, fiscal_week and "+
					"is_fiscal_year_end, so every fiscal question reads one definition (§9.7).",
					cal.Name),
			})
		}
	}
	for _, e := range c.entities {
		if is, ok := c.fiscalCopy(e, cals); ok {
			out = append(out, is)
		}
	}
	return out
}

// fiscalPeriods are the columns that hold a fiscal period, which the
// calendar defines and other tables should read from it.
func fiscalPeriods() []string {
	return []string{"fiscal_year", "fiscal_quarter", "fiscal_month", "fiscal_week"}
}

// calendars returns the SQL entities that are calendars: role=calendar, or
// named date, calendar or fiscal_calendar, singular or plural, with or
// without dim_.
func calendars(c *catalog) []*Entity {
	var out []*Entity
	for _, e := range c.entities {
		if e.Source != SourceSQL || outsideConventions(e) {
			continue
		}
		role, _ := e.Annotations.Get("role")
		name := strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(e.Name), "dim_"), "s")
		if role == "calendar" || name == "date" || name == "calendar" || name == "fiscal_calendar" {
			out = append(out, e)
		}
	}
	return out
}

// calendarGaps lists the columns §9.7-A asks of a calendar that cal lacks:
// a single DATE primary key, whatever its name, and the fiscal columns.
func calendarGaps(cal *Entity) []string {
	var missing []string
	if len(cal.PrimaryKey) != 1 {
		missing = append(missing, "single DATE primary key")
	} else if f, ok := cal.Field(cal.PrimaryKey[0]); !ok || f.Type != TypeDate {
		missing = append(missing, "single DATE primary key")
	}
	for _, name := range append(fiscalPeriods(), "is_fiscal_year_end") {
		if _, ok := cal.Field(name); !ok {
			missing = append(missing, name)
		}
	}
	return missing
}

// fiscalCopy reports a table other than a calendar that stores fiscal
// period columns without referencing a calendar, so its fiscal periods are
// computed on their own.
func (c *catalog) fiscalCopy(e *Entity, cals []*Entity) (issue, bool) {
	if e.Source != SourceSQL || e.Kind != KindTable || outsideConventions(e) ||
		slices.Contains(cals, e) {
		return issue{}, false
	}
	var cols []string
	for i := range e.Fields {
		if name := e.Fields[i].Name; containsFold(append(fiscalPeriods(), "fiscal_period"), name) {
			cols = append(cols, name)
		}
	}
	if len(cols) == 0 || slices.ContainsFunc(cals, func(cal *Entity) bool {
		return c.refersTo(e, cal)
	}) {
		return issue{}, false
	}
	f, _ := e.Field(cols[0])
	is := issue{entity: e, field: f.Name, pos: f.Pos}
	if len(cals) == 0 {
		is.measured = fmt.Sprintf("%s stores %s, but the model has no calendar to define "+
			"fiscal periods once", e.Name, andList(cols))
		is.suggestion = "Add a calendar table (dim_date) keyed by calendar_date with the " +
			"fiscal columns, then drop these and JOIN it USING (calendar_date) (§9.7)."
		return is, true
	}
	is.measured = fmt.Sprintf("%s stores %s independently of %s", e.Name, andList(cols),
		cals[0].Name)
	is.suggestion = fmt.Sprintf("Drop them and JOIN %s USING (calendar_date), or reference "+
		"%s if %s is a periodic snapshot keyed by the period; fiscal periods should be "+
		"defined once (§9.7).", cals[0].Name, cals[0].Name, e.Name)
	return is, true
}
