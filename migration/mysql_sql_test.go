package migration

import (
	"testing"
	"testing/fstest"
)

func TestMySQLValidation(t *testing.T) {
	for _, sql := range []string{
		"CREATE TABLE `odd;COMMIT` (id BIGINT AUTO_INCREMENT PRIMARY KEY);",
		"# COMMIT\nSELECT 'it\\'s fine;';",
		"SELECT \"escaped\\\";text\"; -- COMMIT",
		"SELECT 'x'; /* normal comment */ SELECT 2;",
	} {
		if err := validateSQL(sql, "mysql"); err != nil {
			t.Errorf("%s: %v", sql, err)
		}
	}
	for _, sql := range []string{
		"SELECT 1; COMMIT;", "# comment\nSTART TRANSACTION;", "SET autocommit=0;", "USE other;",
		"/*!80000 COMMIT */;", "DELIMITER $$", "XA START 'x';", "SELECT 'unterminated",
		"/* outer /* inner */ COMMIT; */ SELECT 1;", "# only comment",
		"SELECT 'it\\'s'; ROLLBACK;",
	} {
		if err := validateSQL(sql, "mysql"); err == nil {
			t.Errorf("accepted %q", sql)
		}
	}
	fs := fstest.MapFS{"1_table.sql": {Data: []byte("-- +graft Up\nCREATE TABLE `items` (name TEXT);\n-- +graft Down\nDROP TABLE `items`;")}}
	m, err := LoadDialect(fs, ".", "mysql")
	if err != nil || len(m) != 1 || m[0].Dialect != "mysql" {
		t.Fatal(m, err)
	}
	if _, err := LoadDialect(fs, ".", "unknown"); err == nil {
		t.Fatal("unknown dialect accepted")
	}
}
