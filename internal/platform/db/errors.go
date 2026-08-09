package db

// IsUniqueViolation reports whether err is a PostgreSQL unique violation
// (SQLSTATE 23505).
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	type sqlState interface{ SQLState() string }
	if pe, ok := err.(sqlState); ok {
		return pe.SQLState() == "23505"
	}
	return false
}
