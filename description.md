# o match input empty document crashes

26-02-05 scott@air sandboxes % echo --- | dist/bin/o m -I y  -s 'kind: Deployment' -
panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x2 addr=0x0 pc=0x1044c7910]

goroutine 1 [running]:
github.com/signadot/tony-format/go-tony.MatchWith(0x0, 0x140001d0000, 0x0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.70/match.go:56 +0xd0
github.com/signadot/tony-format/go-tony.Match(...)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.70/match.go:28
main.matchReader({0x0, 0x0, 0x0}, 0x14000190150, 0x140001d0000?, 0x140001d0000, {0x1047cf578?, 0x1400011c018?})
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.70/cmd/o/match.go:130 +0x170
main.matchFile({0x0, 0x0, 0x0}, 0x14000190150, 0x140001a8240, 0x140001d0000, {0x1046b3f78?, 0x6?})
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.70/cmd/o/match.go:116 +0x188
main.match(0x14000190150, 0x140001a8240, {0x140001a0490?, 0x0?, 0x0?})
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.70/cmd/o/match.go:42 +0x174
main.MatchCommand.func1(0x0?, {0x140001a0490?, 0x0?, 0x0?})
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.70/cmd/o/commands.go:170 +0x38
github.com/scott-cotton/cli.(*Command).Run(0xa00194730?, 0x14ba663a8?, {0x140001a0490?, 0x80?, 0x104ba8720?})
        /Users/scott/go/pkg/mod/github.com/scott-cotton/cli@v0.2.3/run.go:15 +0xd0
main.oMain(0x1400018e300, 0x140001a8240, {0x1400013c010?, 0x104133638?, 0x14000118001?})
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.70/cmd/o/o.go:31 +0x2b8
main.MainCommand.func1(0x140001a0300?, {0x1400013c010?, 0x1400018e300?, 0x140001ca160?})
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.70/cmd/o/commands.go:39 +0x38
github.com/scott-cotton/cli.(*Command).Run(0x140001ca630?, 0x140001ca580?, {0x1400013c010?, 0x140001ca2c0?, 0x140001ca160?})
        /Users/scott/go/pkg/mod/github.com/scott-cotton/cli@v0.2.3/run.go:15 +0xd0
github.com/scott-cotton/cli.MainContext({0x1047d3e48, 0x104bcf1a0}, 0x140001ca000)
        /Users/scott/go/pkg/mod/github.com/scott-cotton/cli@v0.2.3/main.go:23 +0x130
main.main()
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.70/cmd/o/main.go:12 +0x34
26-02-05 scott@air sandboxes %

the CLI should check and filter this