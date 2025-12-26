# eval: panic with no arguments

# eval: panic with no arguments

Running `o eval` with no arguments causes a nil pointer dereference panic.

## Reproduction

```bash
$ o eval
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x2 addr=0x58 pc=0x102961dd8]

goroutine 1 [running]:
github.com/signadot/tony-format/go-tony/eval.SplitChild(0x0?)
        /Users/scott/Dev/github.com/signadot/tony-format/go-tony/eval/split_child.go:11 +0x18
...
```

## Expected

Should show usage/help or return a meaningful error message.

## Location

`go-tony/eval/split_child.go:11` - likely missing nil check on input.