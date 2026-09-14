package migration

import (
	"fmt"
	"strings"
)

// validateSQL only enforces the transaction ownership contract; the database still
// validates SQL syntax. Quotes, dollar-quoted bodies and comments are skipped.
func validateSQL(sql string, dialect ...string) error {
	mysql := len(dialect) > 0 && dialect[0] == "mysql"
	if len(dialect) > 0 && dialect[0] != "" && dialect[0] != "postgres" && !mysql {
		return fmt.Errorf("unsupported migration dialect %q", dialect[0])
	}
	var first string
	hasSQL := false
	for i := 0; i < len(sql); {
		c := sql[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if (strings.HasPrefix(sql[i:], "--") && (!mysql || i+2 == len(sql) || sql[i+2] <= ' ')) || (mysql && c == '#') {
			j := strings.IndexByte(sql[i:], '\n')
			if j < 0 {
				break
			}
			i += j + 1
			continue
		}
		if strings.HasPrefix(sql[i:], "/*") {
			if mysql && strings.HasPrefix(sql[i:], "/*!") {
				return fmt.Errorf("MySQL executable comments are not supported; use plain SQL")
			}
			depth := 1
			i += 2
			for i < len(sql) && depth > 0 {
				if strings.HasPrefix(sql[i:], "/*") {
					if mysql {
						return fmt.Errorf("nested MySQL comments are not supported")
					}
					depth++
					i += 2
				} else if strings.HasPrefix(sql[i:], "*/") {
					depth--
					i += 2
				} else {
					i++
				}
			}
			if depth != 0 {
				return fmt.Errorf("unterminated SQL comment")
			}
			continue
		}
		if c == ';' {
			first = ""
			i++
			continue
		}
		hasSQL = true
		if c == '\'' || c == '"' || (mysql && c == '`') {
			quote := c
			escape := quote == '\'' && i > 0 && (sql[i-1] == 'E' || sql[i-1] == 'e')
			if mysql {
				escape = quote != '`'
			}
			i++
			closed := false
			for i < len(sql) {
				if escape && sql[i] == '\\' {
					i += 2
					continue
				}
				if sql[i] == quote {
					if i+1 < len(sql) && sql[i+1] == quote {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return fmt.Errorf("unterminated SQL quote")
			}
			continue
		}
		if c == '$' && !mysql {
			j := i + 1
			for j < len(sql) && ((sql[j] >= 'a' && sql[j] <= 'z') || (sql[j] >= 'A' && sql[j] <= 'Z') || (sql[j] >= '0' && sql[j] <= '9') || sql[j] == '_') {
				j++
			}
			if j < len(sql) && sql[j] == '$' {
				tag := sql[i : j+1]
				end := strings.Index(sql[j+1:], tag)
				if end < 0 {
					return fmt.Errorf("unterminated dollar quote")
				}
				i = j + 1 + end + len(tag)
				continue
			}
		}
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			j := i + 1
			for j < len(sql) && ((sql[j] >= 'a' && sql[j] <= 'z') || (sql[j] >= 'A' && sql[j] <= 'Z') || sql[j] == '_') {
				j++
			}
			if first == "" {
				first = strings.ToUpper(sql[i:j])
				if mysql {
					switch first {
					case "SET", "USE", "LOCK", "UNLOCK", "DELIMITER", "XA":
						return fmt.Errorf("MySQL session control %s is not supported in migrations", first)
					}
				}
				switch first {
				case "BEGIN", "START", "COMMIT", "END", "ROLLBACK", "ABORT", "SAVEPOINT", "RELEASE", "PREPARE":
					return fmt.Errorf("transaction control %s is not allowed; Graft owns the transaction", first)
				}
			}
			i = j
			continue
		}
		i++
	}
	if !hasSQL {
		return fmt.Errorf("Up and Down sections must contain SQL")
	}
	return nil
}
