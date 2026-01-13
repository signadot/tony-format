# --title codegen: map with pointer values issue

The generated code has a bug - when the map value is a pointer type like map[string]*ComponentConfig, it's generating:

  m := make(map[string]*struct)
  val := new(struct)

  Instead of:
  m := make(map[string]*ComponentConfig)
  val := new(ComponentConfig)

  It's outputting the literal keyword struct instead of the actual type name ComponentConfig. This looks like a bug in
  tony-codegen when handling pointer types in maps.