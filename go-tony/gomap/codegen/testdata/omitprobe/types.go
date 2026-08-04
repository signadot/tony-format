package omitprobe

// Probe exercises all three omission spellings (issue f69agjyeh12ks item 11).
//
//tony:schemagen=omitprobe-probe,notag
type Probe struct {
	ID     string `tony:"field=id"`
	Secret string `tony:"field=secret,omit"`
	Dashed string `tony:"-"`
	Named  string `tony:"field=-"`
}
