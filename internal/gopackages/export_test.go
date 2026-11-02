package gopackages

// NamedSQLForTest exposes namedSQL to the external test package. It is
// compiled only with the tests, so it adds nothing to the package's API.
func NamedSQLForTest(sql string) (string, []string) { return namedSQL(sql) }
