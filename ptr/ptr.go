package ptr

func Ptr[T any](v T) *T {
	return &v
}

var (
	True  = Ptr(true)
	False = Ptr(false)
)

func Bool(v bool) *bool {
	if v {
		return True
	}
	return False
}

func Or[T any](v *T, def T) T {
	if v != nil {
		return *v
	}
	return def
}
