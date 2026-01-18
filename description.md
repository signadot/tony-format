# unquote patch seems broken

26-01-18 scott@air go-tony % O_DEBUG_PATCH=1 o p -s '!unquote {}'  it.tony
patch type Object at $ with tag "!bracket.unquote"
patch type Object at $ with tag ""
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x2 addr=0x0 pc=0x101027308]

goroutine 1 [running]:
github.com/signadot/tony-format/go-tony/encode.encode(0x0, {0x10150aa68, 0x1400005e028}, 0x14000194640)
        /Users/scott/Dev/github.com/signadot/tony-format/go-tony/encode/encode.go:205 +0x28
github.com/signadot/tony-format/go-tony/encode.Encode(0x0, {0x10150aa68, 0x1400005e028}, {0x140001832c0, 0x5, 0x1?})
        /Users/scott/Dev/github.com/signadot/tony-format/go-tony/encode/encode.go:44 +0xd0
main.patch(0x140001842d0, 0x14000188600, {0x14000183290?, 0x0?, 0x0?})
        /Users/scott/Dev/github.com/signadot/tony-format/go-tony/cmd/o/patch.go:56 +0x304
main.PatchCommand.func1(0x0?, {0x14000183290?, 0x0?, 0x0?})
        /Users/scott/Dev/github.com/signadot/tony-format/go-tony/cmd/o/commands.go:210 +0x38
github.com/scott-cotton/cli.(*Command).Run(0xa00196fc0?, 0x101c9bd48?, {0x14000183290?, 0x80?, 0x1018e0760?})
        /Users/scott/go/pkg/mod/github.com/scott-cotton/cli@v0.2.3/run.go:15 +0xd0
main.oMain(0x14000183040, 0x14000188600, {0x14000020060?, 0x100e73638?, 0x1400007e001?})
        /Users/scott/Dev/github.com/signadot/tony-format/go-tony/cmd/o/o.go:31 +0x2b8
main.MainCommand.func1(0x14000198400?, {0x14000020060?, 0x14000183040?, 0x140001e0160?})
        /Users/scott/Dev/github.com/signadot/tony-format/go-tony/cmd/o/commands.go:39 +0x38
github.com/scott-cotton/cli.(*Command).Run(0x140001e0630?, 0x140001e0580?, {0x14000020060?, 0x140001e02c0?, 0x140001e0160?})
        /Users/scott/go/pkg/mod/github.com/scott-cotton/cli@v0.2.3/run.go:15 +0xd0
github.com/scott-cotton/cli.MainContext({0x10150f368, 0x1019071e0}, 0x140001e0000)
        /Users/scott/go/pkg/mod/github.com/scott-cotton/cli@v0.2.3/main.go:23 +0x130
main.main()
        /Users/scott/Dev/github.com/signadot/tony-format/go-tony/cmd/o/main.go:12 +0x34
26-01-18 scott@air go-tony % cat it.tony
'{a: 1}'
26-01-18 scott@air go-tony %