# view: panic on empty input

# view: panic on empty input

Running `o view` with empty input causes a nil pointer dereference panic.

## Reproduction

```bash
$ echo '' | o view -
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x2 addr=0x0 pc=0x1031b6ec8]

goroutine 1 [running]:
github.com/signadot/tony-format/go-tony/encode.encode(0x0, ...)
        /Users/scott/Dev/github.com/signadot/tony-format/go-tony/encode/encode.go:204 +0x28
...
```

## Expected

Should handle empty input gracefully - either output nothing or show a clear error.

## Location

`go-tony/encode/encode.go:204` - nil node passed to encode.