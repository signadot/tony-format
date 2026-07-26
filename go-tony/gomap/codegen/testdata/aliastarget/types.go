package aliastarget

//tony:schemagen=aliastarget-real,notag
type Real struct {
	V string `tony:"field=v"`
}

// Format re-exports Real by alias — the cross-package alias shape from issue
// f69agjyeh12ks item 14.
type Format = Real
