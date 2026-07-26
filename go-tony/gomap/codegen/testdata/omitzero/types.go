package omitzero

// OZ exercises omitzero across scalar kinds (issue f69agjyeh12ks item 10).
//
//tony:schemagen=omitzero-oz,notag
type OZ struct {
	Plain string  `tony:"field=plain"`         // no omitzero: always emitted
	Str   string  `tony:"field=str,omitzero"`  // dropped when ""
	Num   int     `tony:"field=num,omitzero"`  // dropped when 0
	Flt   float64 `tony:"field=flt,omitzero"`  // dropped when 0
	Flag  bool    `tony:"field=flag,omitzero"` // dropped when false
}
