package mixedmodellint_test

import (
	"strings"
	"testing"
	"testing/quick"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestCodeownersPrincipalsList(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		text string
		want []string
	}{
		"every owner once, in order": {
			text: "# owners\n\n*.sql @acme/data @alice\n/docs/ @acme/data  # again\n" +
				"model/ ops@acme.example\nunowned\n",
			want: []string{"@acme/data", "@alice", "ops@acme.example"},
		},
		"an empty file": {text: "", want: []string{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, m.ParseCodeowners(tc.text).Principals())
		})
	}
}

func TestCodeownersResolution(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		text   string
		path   string
		want   []string
		wantOK bool
	}{
		"a global pattern": {
			text: "* @acme/all", path: "db/a.sql", want: []string{"@acme/all"}, wantOK: true,
		},
		"the last match wins": {
			text: "* @acme/all\n/db/ @acme/data", path: "db/a.sql",
			want: []string{"@acme/data"}, wantOK: true,
		},
		"an earlier, narrower match loses": {
			text: "/db/ @acme/data\n* @acme/all", path: "db/a.sql",
			want: []string{"@acme/all"}, wantOK: true,
		},
		"an extension at any depth": {
			text: "*.sql @acme/data", path: "db/migrations/001.sql",
			want: []string{"@acme/data"}, wantOK: true,
		},
		"an anchored directory and everything in it": {
			text: "/db @acme/data", path: "db/migrations/001.sql",
			want: []string{"@acme/data"}, wantOK: true,
		},
		"an anchored directory elsewhere does not match": {
			text: "/db/ @acme/data", path: "app/db/001.sql",
		},
		"a trailing slash does not anchor: a directory at any depth": {
			text: "db/ @acme/data", path: "app/db/001.sql",
			want: []string{"@acme/data"}, wantOK: true,
		},
		"a middle slash anchors": {text: "app/db @acme/data", path: "x/app/db/001.sql"},
		"a bare name at any depth": {
			text: "migrations @acme/data", path: "app/migrations/001.sql",
			want: []string{"@acme/data"}, wantOK: true,
		},
		"a directory pattern does not match a file of that name": {
			text: "db/ @acme/data", path: "db",
		},
		"a star stays within a segment": {
			text: "/db/*.sql @acme/data", path: "db/sub/001.sql",
		},
		"a double star crosses segments": {
			text: "/db/**/*.sql @acme/data", path: "db/sub/deeper/001.sql",
			want: []string{"@acme/data"}, wantOK: true,
		},
		"a double star may match no directories": {
			text: "/a/**/b @acme/data", path: "a/b", want: []string{"@acme/data"}, wantOK: true,
		},
		"a double star within a segment crosses segments": {
			text: "/db/**.sql @acme/data", path: "db/sub/001.sql",
			want: []string{"@acme/data"}, wantOK: true,
		},
		"a name is not a prefix of another name": {text: "/db @acme/data", path: "dbx.sql"},
		"a trailing double star": {
			text: "/db/** @acme/data", path: "db/sub/001.sql",
			want: []string{"@acme/data"}, wantOK: true,
		},
		"a question mark is one character": {
			text: "/db/00?.sql @acme/data", path: "db/001.sql",
			want: []string{"@acme/data"}, wantOK: true,
		},
		"dots are literal": {text: "*.sql @acme/data", path: "db/a_sql"},
		"a rule without owners unowns": {
			text: "* @acme/all\n/vendor/", path: "vendor/x.go", want: []string{}, wantOK: true,
		},
		"a non-ASCII pattern": {
			text: "/données/ @acme/fr", path: "données/a.sql",
			want: []string{"@acme/fr"}, wantOK: true,
		},
		"no rule matches": {text: "/docs/ @acme/docs", path: "db/a.sql"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := m.ParseCodeowners(tc.text).Owners(tc.path)
			equals(t, tc.wantOK, ok)
			if ok {
				equals(t, tc.want, got)
			}
		})
	}
}

// TestCodeownersLastRuleWins checks the property GitHub's resolution rests
// on: whatever the rules before it, a final "* @last" owns every path.
func TestCodeownersLastRuleWins(t *testing.T) {
	t.Parallel()
	patterns := []string{"*", "/db/", "*.sql", "docs", "/a/**/b", "x?y", "app/db/"}
	property := func(picks []uint8, segments []uint8) bool {
		var text strings.Builder
		for _, p := range picks {
			text.WriteString(patterns[int(p)%len(patterns)] + " @acme/team\n")
		}
		text.WriteString("* @last\n")
		path := "f"
		for _, s := range segments {
			path = string(rune('a'+s%26)) + "/" + path
		}
		owners, ok := m.ParseCodeowners(text.String()).Owners(path)
		return ok && len(owners) == 1 && owners[0] == "@last"
	}
	if err := quick.Check(property, nil); err != nil {
		t.Fatal(err)
	}
}
