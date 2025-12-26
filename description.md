# codegen named types are extremely limited

named types seem only to work for built-ins,
type A struct{ ...}
type B A

then generate B doesn't work