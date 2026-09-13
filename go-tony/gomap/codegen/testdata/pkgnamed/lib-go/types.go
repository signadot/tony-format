// Package lib lives in a directory whose name is not its package name, as
// example.com/lib-go or gopkg.in/yaml.v3 do (p478tacqh12krg32msn0 item 25).
package lib

//tony:schemagen=lib-thing,notag
type Thing struct {
	V int `tony:"field=v"`
}
