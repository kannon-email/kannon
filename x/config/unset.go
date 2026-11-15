package config

// Or is the value of a key decoded into a pointer, or def when the key is unset. It tells an unset key
// from one set to the zero value, which matters for a port: 0 asks the system to pick one.
func Or[T any](set *T, def T) T {
	if set == nil {
		return def
	}
	return *set
}
