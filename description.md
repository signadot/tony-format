# tony-codegen doesn't generate isZeroValue helper for new nested struct fields

## Problem

When adding a new nested struct field to a type that has `//tony:schemagen` annotation, `tony-codegen` generates code that references an `isZeroValue_<Type>_<Field>` function, but doesn't generate the function definition itself.

## Steps to Reproduce

1. Have an existing struct with tony codegen:
```go
//tony:schemagen=ci-agent-config
type Config struct {
    Session SessionConfig `tony:"field=session"`
    Server  ServerConfig  `tony:"field=server"`
    // ... existing fields
}
```

2. Add a new nested struct field:
```go
//tony:schemagen=ci-agent-config
type Config struct {
    Session SessionConfig `tony:"field=session"`
    Server  ServerConfig  `tony:"field=server"`
    Gateway GatewayConfig `tony:"field=gateway, optional"`  // NEW
}

//tony:schemagen=gateway-config
type GatewayConfig struct {
    Enabled bool `tony:"field=enabled, optional"`
    Namespace string `tony:"field=namespace, optional"`
    // ...
}
```

3. Run `tony-codegen -dir <package>`

4. Build fails:
```
internal/config/config_gen.go:64:6: undefined: isZeroValue_Config_Gateway
```

## Expected Behavior

`tony-codegen` should generate the `isZeroValue_Config_Gateway` function along with the other generated code.

## Workaround

Unknown - may need to manually add the missing function or regenerate all code from scratch.