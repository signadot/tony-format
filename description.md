# logd: a sparse array's integer keys read back as "", collapsing its entries into one

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0); reproduced on a live
`o system logd serve` at 546bd53.

    patch {x: !sparsearray {0: y, 1: z}}
    match x   ->  !sparsearray {"": z}

The stored delta is correct; the read loses the integer keys and collapses the entries
into one. Suspected start: stream/conversion.go:148 -- ir.FromInt leaves the key node's
String empty, while merge and diff key fields by String.

Through the storage API, a !replace over a stored sparse array panics the commit path
(strconv.ParseUint("") at libdiff/object.go:97), and logd has no recover(); over the
session wire the same write committed instead.

Consequence: data loss on read for any sparse array.

Related, closed: y5rad087h12kswh0g9n0 ({n} navigation).