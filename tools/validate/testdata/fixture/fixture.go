// Package fixture is a minimal Go module used by the validate tool's
// self-tests. It exists so the validation stages can be exercised against
// a real, tiny module without touching the repository itself.
package fixture

// Add returns a plus b.
func Add(a, b int) int {
	return a + b
}
