package main

import (
	"sort"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
)

// writeTagDoc answers -tags with a tony document rather than a bulleted list.
//
// The answer is data about the tool, and the tool's subject is data, so there is no
// reason for it to arrive in a shape nothing else here can read. Written this way it
// goes through the ordinary encoder, which means it is coloured on a terminal, honours
// -j, -y and -wire like every other output, and can be asked questions:
//
//	o patch -tags | o get .replace
//	o patch -tags -j
//
// The keys are the operation names without their '!', which is what a kpath can address.
func writeTagDoc(cc *cli.Context, opts []encode.EncodeOption, pairs [][2]string) error {
	sort.Slice(pairs, func(i, j int) bool { return pairs[i][0] < pairs[j][0] })
	kvs := make([]ir.KeyVal, 0, len(pairs))
	for _, p := range pairs {
		kvs = append(kvs, ir.KeyVal{Key: ir.FromString(p[0]), Val: ir.FromString(p[1])})
	}
	return encode.Encode(ir.FromKeyVals(kvs), cc.Out, opts...)
}

// writeTagList is writeTagDoc for a registry which knows its names and not what they do:
// a list rather than a map, so the document does not claim a description it lacks.
func writeTagList(cc *cli.Context, opts []encode.EncodeOption, names []string) error {
	sort.Strings(names)
	vals := make([]*ir.Node, 0, len(names))
	for _, n := range names {
		vals = append(vals, ir.FromString(n))
	}
	return encode.Encode(ir.FromSlice(vals), cc.Out, opts...)
}
