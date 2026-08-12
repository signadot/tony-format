# libdiff multiline string panic

panic: runtime error: index out of range [3] with length 3

goroutine 5704 [running]:
github.com/signadot/tony-format/go-tony/libdiff.PatchStringMultiLine(0x7?, 0x3b5b071cecc0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/libdiff/patch_string.go:117 +0x10de
github.com/signadot/tony-format/go-tony/mergeop.strDiffOp.Patch({{{{0xc89b80?, 0x3b5b0717e2d0?}, 0x3b5b071cecc0?}}, 0xdb?}, 0x3b5b071d2d80, 0x3b5b072237c0, 0x3b5b0717e250?, 0xc7d400, 0x3b5b06ef94c8?)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/mergeop/strdiff.go:63 +0xd0
github.com/signadot/tony-format/go-tony.doPatchWith(0x3b5b071d2d80, 0x3b5b06f37e00, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:72 +0x444
github.com/signadot/tony-format/go-tony.PatchWith(0x3b5b071d2d80, 0x3b5b070f1b00, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:25 +0x48
github.com/signadot/tony-format/go-tony.objPatchYWith(0x3b5b06f61800, 0x3b5b06f1b080, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:170 +0xa5e
github.com/signadot/tony-format/go-tony.doPatchWith(0x3b5b06f61800, 0x3b5b06f1b080, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:89 +0x2a5
github.com/signadot/tony-format/go-tony.PatchWith(0x3b5b06f61800, 0x3b5b06f1a300, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:25 +0x48
github.com/signadot/tony-format/go-tony.objPatchYWith(0x3b5b0728c240, 0x3b5b06f1a240, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:170 +0xa5e
github.com/signadot/tony-format/go-tony.doPatchWith(0x3b5b0728c240, 0x3b5b06f1a240, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:89 +0x2a5
github.com/signadot/tony-format/go-tony.PatchWith(0x3b5b0728c240, 0x3b5b06d59080, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:25 +0x48
github.com/signadot/tony-format/go-tony.objPatchYWith(0x3b5b0728c780, 0x3b5b06d58fc0, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:170 +0xa5e
github.com/signadot/tony-format/go-tony.doPatchWith(0x3b5b0728c780, 0x3b5b06d58fc0, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:89 +0x2a5
github.com/signadot/tony-format/go-tony.PatchWith(0x3b5b0728c780, 0x3b5b070ebe00, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:25 +0x48
github.com/signadot/tony-format/go-tony.objPatchYWith(0x3b5b0728c900, 0x3b5b070ebd40, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:170 +0xa5e
github.com/signadot/tony-format/go-tony.doPatchWith(0x3b5b0728c900, 0x3b5b070ebd40, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:89 +0x2a5
github.com/signadot/tony-format/go-tony.PatchWith(0x3b5b0728c900, 0x3b5b070ea540, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:25 +0x48
github.com/signadot/tony-format/go-tony.objPatchYWith(0x3b5b0728ca80, 0x3b5b070ea480, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:170 +0xa5e
github.com/signadot/tony-format/go-tony.doPatchWith(0x3b5b0728ca80, 0x3b5b070ea480, 0x3b5b072237c0)
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:89 +0x2a5
github.com/signadot/tony-format/go-tony.Patch(0x3b5b0728ca80, 0x3b5b0709acc0, {0x0, 0x0, 0xbec4e0?})
        /Users/scott/go/pkg/mod/github.com/signadot/tony-format/go-tony@v0.0.123/patch.go:19 +0xc5
github.com/signadot/verse/entity.(*LogdStore).WatchSubtree.func1.2(...)
        /Users/scott/Dev/github.com/signadot/verse/entity/logdstore.go:768
github.com/signadot/verse/entity.(*LogdStore).WatchSubtree.func1()
        /Users/scott/Dev/github.com/signadot/verse/entity/logdstore.go:872 +0x3b2
created by github.com/signadot/verse/entity.(*LogdStore).WatchSubtree in goroutine 5702
        /Users/scott/Dev/github.com/signadot/verse/entity/logdstore.go:731 +0x3d3
26-08-12 scott@air verse %